package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"class-relay/internal/network"
	"class-relay/internal/storage"
	"class-relay/web"
)

const teacherCookie = "classrelay_teacher"
const studentCookie = "classrelay_student"

type App struct {
	store        *storage.Store
	cloud        *cloudConfig
	lesson       atomic.Pointer[storage.Store]
	lan          *network.Listener
	ctx          context.Context
	cancel       context.CancelFunc
	mutations    sync.Mutex
	loginKey     string
	loginUsed    bool
	sessionHash  string
	networkError string
	hub          hub
	limits       limiter
}

func New(store *storage.Store, port string) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{store: store, ctx: ctx, cancel: cancel, loginKey: token(), hub: newHub()}
	app.lan = network.NewListener(port, app.Student)
	settings, err := store.Settings(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	if settings.PIN == "" {
		if err := store.SetSettings(ctx, settings.Name, newPIN(), settings.Sending); err != nil {
			cancel()
			return nil, err
		}
	}
	if !settings.Stopped {
		ip := settings.IP
		addresses := network.Addresses()
		if !hasIP(addresses, ip) && len(addresses) > 0 {
			ip = addresses[0].IP
		}
		switch {
		case !hasIP(addresses, ip):
			app.networkError = "未检测到可用的局域网 IPv4 地址"
		default:
			if err := app.lan.Start(ip); err != nil {
				app.networkError = err.Error()
			}
		}
	}
	return app, nil
}

func (a *App) LoginKey() string                         { return a.loginKey }
func (a *App) BaseContext(net.Listener) context.Context { return a.ctx }
func (a *App) Close() {
	a.cancel()
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if a.lan != nil {
		a.lan.Stop()
	}
}

func hasIP(addresses []network.Address, ip string) bool {
	for _, addr := range addresses {
		if addr.IP == ip {
			return true
		}
	}
	return false
}

func token() string { return base64.RawURLEncoding.EncodeToString(randomBytes(32)) }

func randomBytes(n int) []byte {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return bytes
}

func newPIN() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (a *App) Teacher(host string) http.Handler {
	routes := http.NewServeMux()
	a.teacherRoutes(routes)
	routes.Handle("/", web.Handler("teacher"))
	return a.protect(host, routes)
}

func (a *App) teacherRoutes(routes *http.ServeMux) {
	routes.HandleFunc("POST /api/teacher/login", a.login)
	routes.HandleFunc("GET /api/teacher/mode", func(w http.ResponseWriter, _ *http.Request) {
		sendJSON(w, 200, struct {
			Cloud bool `json:"cloud"`
		}{a.cloud != nil})
	})
	routes.HandleFunc("GET /api/teacher/state", a.teacher(a.teacherState))
	routes.HandleFunc("GET /api/teacher/qr", a.teacher(a.qr))
	routes.HandleFunc("POST /api/teacher/settings", a.teacher(a.settings))
	if a.cloud == nil {
		routes.HandleFunc("POST /api/teacher/network", a.teacher(a.changeNetwork))
	}
	routes.HandleFunc("POST /api/teacher/students/{id}", a.teacher(a.manageStudent))
	routes.HandleFunc("POST /api/teacher/history/clear", a.teacher(a.clear))
	routes.HandleFunc("POST /api/teacher/seen", a.teacher(a.seen))
	a.messageRoutes(routes, "teacher", a.teacher)
}

func (a *App) Student(host string) http.Handler {
	routes := http.NewServeMux()
	a.studentRoutes(routes)
	routes.Handle("/", web.Handler("student"))
	return a.protect(host, routes)
}

func (a *App) studentRoutes(routes *http.ServeMux) {
	routes.HandleFunc("POST /api/student/join", a.join)
	routes.HandleFunc("GET /api/student/state", a.studentState)
	a.messageRoutes(routes, "student", func(handler action) http.HandlerFunc {
		return a.student(true, handler)
	})
	routes.HandleFunc("GET /api/student/events", a.student(false, a.events))
}

func (a *App) messageRoutes(routes *http.ServeMux, role string, auth func(action) http.HandlerFunc) {
	prefix := "/api/" + role
	if role == "teacher" {
		routes.HandleFunc("GET "+prefix+"/events", auth(a.events))
	}
	routes.HandleFunc("GET "+prefix+"/messages", auth(a.history))
	routes.HandleFunc("POST "+prefix+"/messages", auth(a.send))
	routes.HandleFunc("POST "+prefix+"/ack", auth(a.ack))
	routes.HandleFunc("POST "+prefix+"/receipts", auth(a.receipts))
}

type actor struct {
	ID      string
	Student storage.Student
}
type action func(http.ResponseWriter, *http.Request, actor)

func (a *App) teacher(next action) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.teacherAuthorized(r) {
			message := "请使用本次启动的教师登录链接"
			if a.cloud != nil {
				message = "请登录教师账号"
			}
			problem(w, 401, message)
			return
		}
		next(w, r, actor{ID: storage.TeacherID})
	}
}

func (a *App) teacherAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie(teacherCookie)
	if err != nil || len(cookie.Value) != 43 {
		return false
	}
	hash := digest(cookie.Value)
	if a.cloud != nil {
		a.cloud.mu.Lock()
		defer a.cloud.mu.Unlock()
		return a.cloud.sessions[hash].After(time.Now())
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	return equal(a.sessionHash, hash)
}

func (a *App) student(requireApproved bool, next action) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := a.studentSession(r)
		if errors.Is(err, sql.ErrNoRows) {
			if requireApproved || r.URL.Path == "/api/student/events" {
				problem(w, 401, "请先申请加入课堂")
				return
			}
		} else if err != nil {
			failed(w, err)
			return
		}
		if requireApproved && v.Status != "approved" {
			problem(w, 403, "当前身份无法访问课堂消息")
			return
		}
		next(w, r, actor{ID: v.ID, Student: v})
	}
}

func (a *App) studentSession(r *http.Request) (storage.Student, error) {
	cookie, err := r.Cookie(studentCookie)
	if err != nil || len(cookie.Value) != 43 {
		return storage.Student{}, sql.ErrNoRows
	}
	lesson := a.lesson.Load()
	if a.cloud != nil {
		if lesson == nil {
			return storage.Student{}, sql.ErrNoRows
		}
		return lesson.StudentByToken(r.Context(), digest(cookie.Value))
	}
	return a.store.StudentByToken(r.Context(), digest(cookie.Value))
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if a.cloud != nil {
		a.cloudLogin(w, r)
		return
	}
	if !a.limits.allow("teacher-login", 10, 6*time.Second) {
		problem(w, 429, "尝试过于频繁")
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if !decode(w, r, &body, 1024) {
		return
	}
	a.mutations.Lock()
	defer a.mutations.Unlock()
	if a.loginUsed || !equal(body.Key, a.loginKey) {
		problem(w, 401, "教师登录链接无效或已使用")
		return
	}
	session := token()
	a.sessionHash = digest(session)
	a.loginUsed = true
	a.setSession(w, teacherCookie, session)
	sendJSON(w, 200, struct{}{})
}

type teacherSnapshot struct {
	storage.Settings
	Students     []storage.Student `json:"students"`
	Addresses    []network.Address `json:"addresses"`
	IP           string            `json:"ip"`
	Running      bool              `json:"running"`
	URL          string            `json:"url"`
	NetworkError string            `json:"networkError,omitempty"`
	Cloud        bool              `json:"cloud,omitempty"`
	LessonID     string            `json:"lessonId,omitempty"`
}

func (a *App) teacherState(w http.ResponseWriter, r *http.Request, _ actor) {
	a.mutations.Lock()
	defer a.mutations.Unlock()
	store := a.roomStore()
	settings, err := store.Settings(r.Context())
	if err != nil {
		failed(w, err)
		return
	}
	snapshot := teacherSnapshot{Settings: settings, Students: []storage.Student{}, Addresses: []network.Address{}}
	if a.roomOpen() {
		snapshot.Students, err = store.Students(r.Context())
		if err != nil {
			failed(w, err)
			return
		}
		for i := range snapshot.Students {
			student := &snapshot.Students[i]
			student.Online = student.Status == "approved" && a.hub.online(student.ID)
		}
	}
	if a.cloud != nil {
		snapshot.Cloud = true
		snapshot.LessonID = a.lessonID()
		snapshot.Running = snapshot.LessonID != ""
		snapshot.URL = a.cloud.origin + "/student"
		if !snapshot.Running {
			snapshot.PIN = ""
		}
	} else {
		snapshot.IP, snapshot.Running = a.lan.State()
		snapshot.Addresses = network.Addresses()
		snapshot.NetworkError = a.networkError
		if snapshot.Running {
			snapshot.URL = a.lan.URL(snapshot.IP)
			if !hasIP(snapshot.Addresses, snapshot.IP) {
				snapshot.NetworkError = "当前局域网地址已失效，请切换网卡"
			}
		} else if snapshot.IP == "" {
			snapshot.IP = settings.IP
			if !hasIP(snapshot.Addresses, snapshot.IP) && len(snapshot.Addresses) > 0 {
				snapshot.IP = snapshot.Addresses[0].IP
			}
		}
	}
	sendJSON(w, 200, snapshot)
}

func (a *App) studentState(w http.ResponseWriter, r *http.Request) {
	a.mutations.Lock()
	defer a.mutations.Unlock()
	current, err := a.studentSession(r)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		failed(w, err)
		return
	}
	settings, err := a.roomStore().Settings(r.Context())
	if err != nil {
		failed(w, err)
		return
	}
	settings.PIN = ""
	sendJSON(w, 200, struct {
		storage.Settings
		Student  *storage.Student `json:"student"`
		Cloud    bool             `json:"cloud,omitempty"`
		LessonID string           `json:"lessonId,omitempty"`
	}{Settings: settings, Student: studentPointer(current), Cloud: a.cloud != nil, LessonID: a.lessonID()})
}

func (a *App) lessonID() string {
	if lesson := a.lesson.Load(); lesson != nil {
		return lesson.ClassID()
	}
	return ""
}

func studentPointer(student storage.Student) *storage.Student {
	if student.ID == "" {
		return nil
	}
	return &student
}

func failed(w http.ResponseWriter, err error) {
	if !errors.Is(err, context.Canceled) {
		log.Printf("ClassRelay: %v", err)
	}
	problem(w, 500, "操作暂时失败，请稍后重试")
}
