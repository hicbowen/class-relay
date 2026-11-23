package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"class-relay/internal/storage"
)

func (a *App) history(w http.ResponseWriter, r *http.Request, who actor) {
	query := r.URL.Query()
	conversation := query.Get("conversation")
	if who.ID == storage.TeacherID && conversation != "all" && !storage.ValidID(conversation) {
		problem(w, 400, "请选择一个会话")
		return
	}
	cursor, forward, ok := historyCursor(query.Get("before"), query.Get("after"))
	if !ok {
		problem(w, 400, "消息游标无效")
		return
	}
	a.mutations.Lock()
	store := a.roomStore()
	if lesson := query.Get("lesson"); lesson != "" {
		if a.cloud == nil || who.ID != storage.TeacherID {
			a.mutations.Unlock()
			problem(w, 403, "只有教师可查看课堂历史")
			return
		}
		if _, err := a.store.ClosedLesson(r.Context(), lesson); err != nil {
			a.mutations.Unlock()
			if errors.Is(err, sql.ErrNoRows) {
				problem(w, 404, "找不到这节已结束的课")
			} else {
				failed(w, err)
			}
			return
		}
		store = a.store.ForClass(lesson)
	} else {
		if !a.requireCurrentClass(w, r, who) {
			a.mutations.Unlock()
			return
		}
		if a.cloud != nil && who.ID != storage.TeacherID && query.Get("studentId") != "" && query.Get("studentId") != who.ID {
			a.mutations.Unlock()
			problem(w, 409, "当前学生身份已变更")
			return
		}
	}
	page, err := store.History(r.Context(), who.ID, conversation, cursor, forward)
	a.mutations.Unlock()
	if err != nil {
		failed(w, err)
		return
	}
	sendJSON(w, 200, page)
}

func historyCursor(before, after string) (int64, bool, bool) {
	if before != "" && after != "" {
		return 0, false, false
	}
	raw := before
	if after != "" {
		raw = after
	}
	if raw == "" {
		return 0, false, true
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	return cursor, after != "", err == nil && cursor >= 0
}

var languages = map[string]bool{
	"plaintext": true, "python": true, "cpp": true, "c": true, "javascript": true, "typescript": true, "go": true, "java": true, "html": true, "css": true, "json": true, "bash": true, "sql": true,
}

func (a *App) send(w http.ResponseWriter, r *http.Request, who actor) {
	var input struct {
		ReceiverID string `json:"receiverId"`
		Type       string `json:"type"`
		Content    string `json:"content"`
		Language   string `json:"language"`
		RequestID  string `json:"requestId"`
		Generation int64  `json:"generation"`
		StudentID  string `json:"studentId"`
	}
	// JSON may encode each input byte as a six-byte escape.
	if !decode(w, r, &input, 6*(64<<10)+2048) {
		return
	}
	if len(input.Content) > 64<<10 {
		problem(w, 413, "消息不能超过 64 KiB")
		return
	}
	if strings.TrimSpace(input.Content) == "" {
		problem(w, 400, "请输入消息内容")
		return
	}
	if !storage.ValidID(input.RequestID) {
		problem(w, 400, "缺少有效的请求编号")
		return
	}
	switch input.Type {
	case "text":
		input.Language = ""
	case "code":
		if !languages[input.Language] {
			problem(w, 400, "请选择受支持的代码语言")
			return
		}
	default:
		problem(w, 400, "消息类型无效")
		return
	}
	isTeacher := who.ID == storage.TeacherID
	if isTeacher {
		if input.ReceiverID != "all" && !storage.ValidID(input.ReceiverID) {
			problem(w, 400, "请选择接收学生")
			return
		}
	} else if input.ReceiverID != "teacher" {
		problem(w, 403, "学生只能向教师发送消息")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if !a.requireCurrentClass(w, r, who) {
		return
	}
	if a.cloud != nil && !isTeacher && input.StudentID != who.ID {
		problem(w, 409, "当前学生身份已变更")
		return
	}
	settings, err := a.roomStore().Settings(r.Context())
	if err != nil {
		failed(w, err)
		return
	}
	if settings.Generation != input.Generation {
		problem(w, 409, "历史消息已清空，请重新输入这条消息")
		return
	}
	existing, err := a.roomStore().FindRequest(r.Context(), who.ID, input.RequestID)
	if err == nil {
		if existing.ReceiverID != input.ReceiverID || existing.Content != input.Content || existing.Type != input.Type || existing.Language != input.Language {
			problem(w, 409, "请求编号已经用于另一条消息")
			return
		}
		sendJSON(w, 200, existing)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		failed(w, err)
		return
	}
	if isTeacher && input.ReceiverID != "all" {
		status, err := a.roomStore().StudentStatus(r.Context(), input.ReceiverID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			failed(w, err)
			return
		}
		if status != "approved" {
			problem(w, 409, "该学生已无法接收消息")
			return
		}
	}
	if !isTeacher && !settings.Sending {
		problem(w, 403, "教师已关闭学生发送权限")
		return
	}
	if !a.limits.allow("send:"+who.ID, 12, 700*time.Millisecond) {
		problem(w, 429, "发送过于频繁，请稍等片刻")
		return
	}
	saved, err := a.roomStore().Send(r.Context(), storage.Message{ID: token(), SenderID: who.ID, ReceiverID: input.ReceiverID,
		Type: input.Type, Content: input.Content, Language: input.Language, RequestID: input.RequestID})
	if err != nil {
		failed(w, err)
		return
	}
	switch input.ReceiverID {
	case "all":
		a.hub.notifyAll()
	case "teacher":
		a.hub.notifyTeacher()
	default:
		a.hub.notify([]string{input.ReceiverID})
	}
	sendJSON(w, 201, saved)
}

func validBatch(ids []string) bool {
	if len(ids) == 0 || len(ids) > 100 {
		return false
	}
	for _, id := range ids {
		if !storage.ValidID(id) {
			return false
		}
	}
	return true
}

func (a *App) ack(w http.ResponseWriter, r *http.Request, who actor) {
	var input struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &input, 12<<10) {
		return
	}
	if !validBatch(input.IDs) {
		problem(w, 400, "消息编号无效")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if !a.requireCurrentClass(w, r, who) {
		return
	}
	updated, err := a.roomStore().Ack(r.Context(), who.ID, input.IDs)
	if err != nil {
		failed(w, err)
		return
	}
	if updated {
		if who.ID == storage.TeacherID {
			a.hub.notifyAll()
		} else {
			a.hub.notifyTeacher()
		}
	}
	sendJSON(w, 200, struct{}{})
}

func (a *App) receipts(w http.ResponseWriter, r *http.Request, who actor) {
	var input struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &input, 12<<10) {
		return
	}
	if !validBatch(input.IDs) {
		problem(w, 400, "消息编号无效")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if !a.requireCurrentClass(w, r, who) {
		return
	}
	receipts, err := a.roomStore().Receipts(r.Context(), who.ID, input.IDs)
	if err != nil {
		failed(w, err)
		return
	}
	sendJSON(w, 200, receipts)
}

func (a *App) seen(w http.ResponseWriter, r *http.Request, _ actor) {
	var input struct {
		StudentID string `json:"studentId"`
		Seq       int64  `json:"seq"`
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	if !storage.ValidID(input.StudentID) || input.Seq <= 0 {
		problem(w, 400, "无效的会话位置")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if !a.roomOpen() {
		problem(w, 409, "当前没有进行中的课堂")
		return
	}
	updated, err := a.roomStore().Seen(r.Context(), input.StudentID, input.Seq)
	if err != nil {
		failed(w, err)
		return
	}
	if updated {
		a.hub.notifyTeacher()
	}
	sendJSON(w, 200, struct{}{})
}

func (a *App) requireCurrentClass(w http.ResponseWriter, r *http.Request, who actor) bool {
	if !a.roomOpen() {
		problem(w, 409, "当前没有进行中的课堂")
		return false
	}
	if who.ID == storage.TeacherID {
		return true
	}
	status, err := a.roomStore().StudentStatus(r.Context(), who.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failed(w, err)
		return false
	}
	if status != "approved" {
		problem(w, 403, "当前身份无法访问课堂消息")
		return false
	}
	return true
}
