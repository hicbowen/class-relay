package server

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"time"

	"class-relay/internal/network"
	"github.com/skip2/go-qrcode"
)

var pinFormat = regexp.MustCompile("^[0-9]{6}$")

func (a *App) join(w http.ResponseWriter, r *http.Request) {
	ip := a.clientIP(r)
	if !a.limits.allow("join-all", 120, 500*time.Millisecond) || !a.limits.allow("join:"+ip, 60, time.Second) {
		problem(w, 429, "加入申请过于频繁，请稍后再试")
		return
	}
	var input struct {
		Name     string `json:"name"`
		PIN      string `json:"pin"`
		LessonID string `json:"lessonId"`
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	name, ok := cleanName(input.Name)
	if !ok {
		problem(w, 400, "请输入 1–40 个字符的姓名")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if a.cloud != nil && (!a.roomOpen() || a.lessonID() != input.LessonID) {
		problem(w, 409, "当前课堂已变更，请使用本节课的口令")
		return
	}
	student, err := a.studentSession(r)
	if err == nil && (student.Status == "pending" || student.Status == "approved") {
		sendJSON(w, 200, student)
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failed(w, err)
		return
	}
	settings, err := a.roomStore().Settings(r.Context())
	if err != nil {
		failed(w, err)
		return
	}
	wrongPIN := !pinFormat.MatchString(input.PIN) || !equal(input.PIN, settings.PIN)
	if !a.limits.allow("pin:"+ip, 8, 8*time.Second) ||
		(a.cloud != nil && wrongPIN && !a.limits.allow("cloud-pin-all", 24, 2*time.Second)) {
		problem(w, 429, "口令尝试过于频繁，请稍后再试")
		return
	}
	if wrongPIN {
		problem(w, 403, "课堂口令不正确")
		return
	}
	// A valid join should not penalize a shared IP (e.g. an access point).
	a.limits.refund("pin:"+ip, 8)
	session, id := token(), token()
	inserted, err := a.roomStore().Join(r.Context(), id, name, digest(session))
	if err != nil {
		failed(w, err)
		return
	}
	if !inserted {
		problem(w, 503, "课堂人数已满")
		return
	}
	a.setSession(w, studentCookie, session)
	a.hub.notifyTeacher()
	sendJSON(w, 201, struct {
		ID string `json:"id"`
	}{id})
}

func (l *limiter) refund(key string, capacity int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	b.tokens = min(float64(capacity), b.tokens+1)
	l.buckets[key] = b
}

func (a *App) settings(w http.ResponseWriter, r *http.Request, _ actor) {
	var input struct {
		Name       string `json:"name"`
		PIN        string `json:"pin"`
		Regenerate bool   `json:"regenerate"`
		Sending    bool   `json:"sending"`
		LessonID   string `json:"lessonId"`
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	name, ok := cleanName(input.Name)
	if !ok {
		problem(w, 400, "请输入 1–40 个字符的课堂名称")
		return
	}
	if !input.Regenerate && !pinFormat.MatchString(input.PIN) {
		problem(w, 400, "课堂口令须为 6 位数字")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if a.cloud != nil && (!a.roomOpen() || a.lessonID() != input.LessonID) {
		problem(w, 409, "当前课堂已变更")
		return
	}
	if input.Regenerate {
		old, err := a.roomStore().Settings(r.Context())
		if err != nil {
			failed(w, err)
			return
		}
		input.PIN = newPIN()
		for input.PIN == old.PIN {
			input.PIN = newPIN()
		}
	}
	if err := a.roomStore().SetSettings(r.Context(), name, input.PIN, input.Sending); err != nil {
		failed(w, err)
		return
	}
	a.hub.notifyAll()
	sendJSON(w, 200, struct {
		PIN string `json:"pin"`
	}{input.PIN})
}

func (a *App) changeNetwork(w http.ResponseWriter, r *http.Request, _ actor) {
	var input struct {
		IP      string `json:"ip"`
		Running bool   `json:"running"`
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if a.ctx.Err() != nil {
		problem(w, 503, "ClassRelay 正在退出")
		return
	}
	oldIP, oldRunning := a.lan.State()
	if input.Running {
		if !hasIP(network.Addresses(), input.IP) {
			problem(w, 400, "请选择当前电脑的可用局域网地址")
			return
		}
		if err := a.lan.Start(input.IP); err != nil {
			problem(w, 409, err.Error())
			return
		}
	} else {
		input.IP = oldIP
	}
	if err := a.store.SetNetwork(r.Context(), input.IP, !input.Running); err != nil {
		if input.Running && !oldRunning {
			a.lan.Stop()
		}
		if input.Running && oldRunning && input.IP != oldIP {
			if restoreErr := a.lan.Start(oldIP); restoreErr != nil {
				a.networkError = restoreErr.Error()
			}
		}
		failed(w, err)
		return
	}
	if !input.Running {
		a.lan.Stop()
	}
	a.networkError = ""
	a.hub.notifyAll()
	sendJSON(w, 200, struct{}{})
}

func (a *App) qr(w http.ResponseWriter, _ *http.Request, _ actor) {
	var entry string
	if a.cloud != nil {
		entry = a.cloud.origin + "/student"
	} else {
		ip, running := a.lan.State()
		if !running {
			problem(w, 409, "请先启动局域网服务")
			return
		}
		entry = a.lan.URL(ip)
	}
	data, err := qrcode.Encode(entry, qrcode.Medium, 256)
	if err != nil {
		failed(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(data)
}

func (a *App) manageStudent(w http.ResponseWriter, r *http.Request, _ actor) {
	var input struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	id := r.PathValue("id")
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if !a.roomOpen() {
		problem(w, 409, "当前没有进行中的课堂")
		return
	}
	var ok bool
	var err error
	switch input.Action {
	case "approve":
		ok, err = a.roomStore().Decide(r.Context(), id, "approved", "pending")
	case "deny":
		ok, err = a.roomStore().Decide(r.Context(), id, "denied", "pending")
	case "remove":
		ok, err = a.roomStore().Decide(r.Context(), id, "removed", "approved")
	case "rename":
		name, valid := cleanName(input.Name)
		if !valid {
			problem(w, 400, "请输入 1–40 个字符的姓名")
			return
		}
		ok, err = a.roomStore().Rename(r.Context(), id, name)
	default:
		problem(w, 400, "操作无效")
		return
	}
	if err != nil {
		failed(w, err)
		return
	}
	if !ok {
		problem(w, 409, "学生状态已经变化")
		return
	}
	a.hub.notify([]string{id})
	sendJSON(w, 200, struct{}{})
}

func (a *App) clear(w http.ResponseWriter, r *http.Request, _ actor) {
	var input struct {
		Confirm  bool   `json:"confirm"`
		LessonID string `json:"lessonId"`
	}
	if !decode(w, r, &input, 1024) {
		return
	}
	if !input.Confirm {
		problem(w, 400, "请确认清空历史消息")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if a.cloud != nil && (!a.roomOpen() || a.lessonID() != input.LessonID) {
		problem(w, 409, "当前课堂已变更")
		return
	}
	if err := a.roomStore().Clear(r.Context()); err != nil {
		failed(w, err)
		return
	}
	a.hub.notifyAll()
	sendJSON(w, 200, struct{}{})
}
