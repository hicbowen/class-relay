package server

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"class-relay/internal/storage"
	"class-relay/web"
)

const teacherSessionLifetime = 12 * time.Hour

type cloudConfig struct {
	origin      string
	host        string
	username    string
	salt        []byte
	passwordKey []byte
	mu          sync.Mutex
	sessions    map[string]time.Time
}

func NewCloud(store *storage.Store, origin, username, password string) (*App, error) {
	public, err := url.Parse(origin)
	if err != nil || public.Scheme != "https" || public.Hostname() == "" || public.User != nil ||
		(public.Path != "" && public.Path != "/") || public.RawQuery != "" || public.ForceQuery || public.Fragment != "" {
		return nil, errors.New("云端地址必须是 HTTPS 源地址，例如 https://relay.example.com")
	}
	username, ok := cleanName(username)
	if !ok || len(password) < 12 || len(password) > 256 {
		return nil, errors.New("需要配置教师账号及 12–256 字节的教师密码")
	}
	ctx, cancel := context.WithCancel(context.Background())
	active, err := store.ActiveLesson(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	host := strings.ToLower(public.Host)
	if public.Port() == "443" {
		host = strings.TrimSuffix(host, ":443")
	}
	cfg := &cloudConfig{origin: "https://" + host, host: host, username: username,
		salt: randomBytes(32), sessions: make(map[string]time.Time)}
	cfg.passwordKey = cfg.derive(password)
	a := &App{store: store, ctx: ctx, cancel: cancel, hub: newHub(), cloud: cfg}
	if active != "" {
		a.lesson.Store(store.ForClass(active))
	}
	return a, nil
}

func (c *cloudConfig) derive(password string) []byte {
	key, err := pbkdf2.Key(sha256.New, password, c.salt, 600_000, 32)
	if err != nil {
		panic(err)
	}
	return key
}

func (a *App) roomStore() *storage.Store {
	if lesson := a.lesson.Load(); lesson != nil {
		return lesson
	}
	return a.store
}

func (a *App) roomOpen() bool { return a.cloud == nil || a.lesson.Load() != nil }

func (a *App) Cloud() http.Handler {
	routes := http.NewServeMux()
	a.teacherRoutes(routes)
	a.studentRoutes(routes)
	routes.HandleFunc("POST /api/teacher/logout", a.teacher(a.logout))
	routes.HandleFunc("POST /api/teacher/lessons/start", a.teacher(a.startLesson))
	routes.HandleFunc("POST /api/teacher/lessons/end", a.teacher(a.endLesson))
	routes.HandleFunc("GET /api/teacher/lessons", a.teacher(a.listLessons))
	routes.HandleFunc("GET /api/teacher/lessons/{id}", a.teacher(a.lessonDetail))
	routes.HandleFunc("POST /api/student/leave", a.student(false, a.leave))
	routes.Handle("/teacher", web.Handler("teacher"))
	routes.Handle("/", web.Handler("student"))
	return a.protect(a.cloud.host, routes)
}

func (a *App) cloudLogin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input, 4096) {
		return
	}
	if len(input.Password) > 256 {
		problem(w, 401, "账号或密码不正确")
		return
	}
	if !a.limits.allow("cloud-login:"+a.clientIP(r), 5, 12*time.Second) ||
		!a.limits.allow("cloud-login-all", 12, 3*time.Second) {
		problem(w, 429, "登录尝试过于频繁，请稍后再试")
		return
	}
	c := a.cloud
	if subtle.ConstantTimeCompare(c.derive(input.Password), c.passwordKey) != 1 ||
		!equal(digest(strings.TrimSpace(input.Username)), digest(c.username)) {
		problem(w, 401, "账号或密码不正确")
		return
	}
	session := token()
	now := time.Now()
	c.mu.Lock()
	for hash, until := range c.sessions {
		if !until.After(now) {
			delete(c.sessions, hash)
		}
	}
	if previous, err := r.Cookie(teacherCookie); err == nil {
		delete(c.sessions, digest(previous.Value))
	}
	if len(c.sessions) >= 64 {
		var oldest string
		var expires time.Time
		for hash, until := range c.sessions {
			if oldest == "" || until.Before(expires) {
				oldest, expires = hash, until
			}
		}
		delete(c.sessions, oldest)
	}
	c.sessions[digest(session)] = now.Add(teacherSessionLifetime)
	c.mu.Unlock()
	a.setSession(w, teacherCookie, session)
	sendJSON(w, 200, struct{}{})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request, _ actor) {
	if cookie, err := r.Cookie(teacherCookie); err == nil {
		a.cloud.mu.Lock()
		delete(a.cloud.sessions, digest(cookie.Value))
		a.cloud.mu.Unlock()
	}
	a.setSession(w, teacherCookie, "")
	a.hub.notifyTeacher()
	sendJSON(w, 200, struct{}{})
}

func (a *App) startLesson(w http.ResponseWriter, r *http.Request, _ actor) {
	var input struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	name, valid := cleanName(input.Name)
	if !valid {
		problem(w, 400, "请输入 1–40 个字符的课堂名称")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	id := token()
	err := a.store.StartLesson(r.Context(), id, name, newPIN)
	if errors.Is(err, storage.ErrLessonChanged) {
		problem(w, 409, "已有课堂正在进行")
		return
	}
	if err != nil {
		failed(w, err)
		return
	}
	a.lesson.Store(a.store.ForClass(id))
	a.hub.notifyAll()
	sendJSON(w, 201, struct{}{})
}

func (a *App) endLesson(w http.ResponseWriter, r *http.Request, _ actor) {
	var input struct {
		LessonID string `json:"lessonId"`
	}
	if !decode(w, r, &input, 1024) {
		return
	}
	if !storage.ValidID(input.LessonID) {
		problem(w, 400, "无效的课堂编号")
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	err := a.store.EndLesson(r.Context(), input.LessonID)
	if errors.Is(err, storage.ErrLessonChanged) {
		problem(w, 409, "这节课已经结束或发生变化")
		return
	}
	if err != nil {
		failed(w, err)
		return
	}
	a.lesson.Store(nil)
	a.hub.notifyAll()
	sendJSON(w, 200, struct{}{})
}

func (a *App) listLessons(w http.ResponseWriter, r *http.Request, _ actor) {
	before := r.URL.Query().Get("before")
	if before != "" && !storage.ValidID(before) {
		problem(w, 400, "无效的历史游标")
		return
	}
	lessons, err := a.store.ClosedLessons(r.Context(), before)
	if err != nil {
		failed(w, err)
		return
	}
	sendJSON(w, 200, lessons)
}

func (a *App) lessonDetail(w http.ResponseWriter, r *http.Request, _ actor) {
	lesson, err := a.store.ClosedLesson(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "找不到这节已结束的课")
		return
	}
	if err != nil {
		failed(w, err)
		return
	}
	students, err := a.store.ForClass(lesson.ID).Students(r.Context())
	if err != nil {
		failed(w, err)
		return
	}
	sendJSON(w, 200, struct {
		storage.Lesson
		Students []storage.Student `json:"students"`
	}{lesson, students})
}

func (a *App) leave(w http.ResponseWriter, r *http.Request, who actor) {
	var input struct {
		StudentID string `json:"studentId"`
	}
	if !decode(w, r, &input, 1024) {
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	current, err := a.studentSession(r)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failed(w, err)
		return
	}
	if current.ID == "" || current.ID != input.StudentID || current.ID != who.ID {
		problem(w, 409, "当前学生身份已变更")
		return
	}
	cookie, _ := r.Cookie(studentCookie)
	updated, err := a.roomStore().Leave(r.Context(), who.ID, digest(cookie.Value))
	if err != nil {
		failed(w, err)
		return
	}
	if !updated {
		problem(w, 409, "当前学生身份已失效")
		return
	}
	a.setSession(w, studentCookie, "")
	a.hub.notify([]string{who.ID})
	sendJSON(w, 200, struct{}{})
}
