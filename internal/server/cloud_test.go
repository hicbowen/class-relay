package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"class-relay/internal/storage"
)

const cloudHost = "relay.example.com"
const cloudOrigin = "https://" + cloudHost
const cloudPassword = "test-password-for-teacher"

type cloudState struct {
	Running    bool
	Cloud      bool
	PIN        string
	URL        string
	LessonID   string
	Generation int64
	Students   []storage.Student
	Student    *storage.Student
}

func TestCloudLessonsAndSharedDevice(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	newApp := func() *App {
		app, err := NewCloud(db, cloudOrigin, "teacher", cloudPassword)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(app.Close)
		return app
	}
	app := newApp()
	handler := app.Cloud()
	call := func(method, path string, cookie *http.Cookie, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := cloudRequest(method, path, cookie, body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s returned %d, want %d: %.200s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	signIn := func() *http.Cookie {
		t.Helper()
		result := call("POST", "/api/teacher/login", nil, map[string]string{"username": "teacher", "password": cloudPassword}, 200).Result()
		cookie := result.Cookies()[0]
		if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/teacher/" {
			t.Fatal("missing secure teacher cookie settings")
		}
		return cookie
	}
	state := func(role string, cookie *http.Cookie) cloudState {
		t.Helper()
		var value cloudState
		read(t, call("GET", "/api/"+role+"/state", cookie, nil, 200), &value)
		return value
	}
	join := func(old *http.Cookie, name string, lesson cloudState) (string, *http.Cookie) {
		t.Helper()
		rec := call("POST", "/api/student/join", old, map[string]string{"lessonId": lesson.LessonID, "name": name, "pin": lesson.PIN}, 201)
		var response struct{ ID string }
		read(t, rec, &response)
		cookie := rec.Result().Cookies()[0]
		if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/api/student/" {
			t.Fatal("missing secure student cookie settings")
		}
		return response.ID, cookie
	}
	approve := func(id string, teacher *http.Cookie) {
		t.Helper()
		call("POST", "/api/teacher/students/"+id, teacher, map[string]string{"action": "approve"}, 200)
	}

	call("GET", "/api/teacher/state", nil, nil, 401)
	for _, origin := range []string{"", "http://" + cloudHost, "https://untrusted.example"} {
		r := cloudRequest("POST", "/api/teacher/login", nil, map[string]string{"username": "teacher", "password": cloudPassword})
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("accepted origin %q", origin)
		}
	}
	r := cloudRequest("GET", "/api/student/state", nil, nil)
	r.Header.Del("X-Forwarded-Proto")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("accepted cleartext direct access")
	}

	call("POST", "/api/teacher/login", nil, map[string]string{"username": "teacher", "password": "incorrect"}, 401)
	teacher := signIn()
	idle := state("teacher", teacher)
	if !idle.Cloud || idle.Running || idle.URL != cloudOrigin+"/student" || idle.PIN != "" {
		t.Fatalf("initial cloud state %+v", idle)
	}
	call("GET", "/api/teacher/qr", teacher, nil, 200)
	call("POST", "/api/student/join", nil, map[string]string{"name": "甲", "pin": "000000"}, 409)
	call("POST", "/api/teacher/lessons/start", teacher, map[string]string{"name": "第一节"}, 201)
	call("POST", "/api/teacher/lessons/start", teacher, map[string]string{"name": "重复开课"}, 409)
	first := state("teacher", teacher)
	if !first.Running || first.URL != idle.URL || len(first.PIN) != 6 || len(first.Students) != 0 {
		t.Fatalf("first lesson %+v", first)
	}
	if student := state("student", nil); student.LessonID != first.LessonID || student.PIN != "" {
		t.Fatal("student state missing lesson or leaked PIN")
	}
	idA, cookieA := join(nil, "小明", first)
	approve(idA, teacher)
	call("GET", "/api/teacher/state", cookieA, nil, 401)
	var broadcast storage.Message
	input := payload("all", "first-broadcast", "第一节广播")
	input["generation"] = first.Generation
	read(t, call("POST", "/api/teacher/messages", teacher, input, 201), &broadcast)
	reply := payload("teacher", "first-private", "第一节私聊")
	reply["generation"], reply["studentId"] = first.Generation, idA
	call("POST", "/api/student/messages", cookieA, reply, 201)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := cloudRequest("GET", "/api/student/events", cookieA, nil).WithContext(ctx)
	stream := &streamResponse{header: make(http.Header), codes: make(chan int, 2), chunks: make(chan string, 8)}
	stopped := make(chan struct{})
	streamHandler := handler
	go func() { defer close(stopped); streamHandler.ServeHTTP(stream, req) }()
	nextEvent(t, stream)
	call("POST", "/api/teacher/lessons/end", teacher, map[string]string{"lessonId": first.LessonID}, 200)
	nextEvent(t, stream)
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("student SSE not revoked on lesson end")
	}
	if student := state("student", cookieA); student.Student != nil || student.LessonID != "" {
		t.Fatalf("post-end identity %+v", student)
	}
	call("GET", "/api/student/messages", cookieA, nil, 401)

	var history []storage.Lesson
	read(t, call("GET", "/api/teacher/lessons", teacher, nil, 200), &history)
	if len(history) != 1 || history[0].ID != first.LessonID {
		t.Fatalf("archive list %+v", history)
	}
	call("GET", "/api/teacher/lessons/"+first.LessonID, teacher, nil, 200)
	var archived storage.Page
	read(t, call("GET", "/api/teacher/messages?lesson="+first.LessonID+"&conversation=all", teacher, nil, 200), &archived)
	if len(archived.Messages) != 1 || archived.Messages[0].ID != broadcast.ID {
		t.Fatalf("archive broadcast %+v", archived)
	}
	read(t, call("GET", "/api/teacher/messages?lesson="+first.LessonID+"&conversation="+idA, teacher, nil, 200), &archived)
	if len(archived.Messages) != 1 || archived.Messages[0].Content != "第一节私聊" {
		t.Fatalf("archive private chat %+v", archived)
	}

	call("POST", "/api/teacher/lessons/start", teacher, map[string]string{"name": "第二节"}, 201)
	second := state("teacher", teacher)
	if second.URL != first.URL || second.PIN == first.PIN || second.LessonID == first.LessonID || len(second.Students) != 0 {
		t.Fatalf("second lesson %+v", second)
	}
	call("POST", "/api/teacher/lessons/end", teacher, map[string]string{"lessonId": first.LessonID}, 409)
	call("POST", "/api/student/join", cookieA, map[string]string{"name": "小红", "lessonId": first.LessonID, "pin": first.PIN}, 409)
	call("POST", "/api/student/join", cookieA, map[string]string{"name": "小红", "lessonId": second.LessonID, "pin": first.PIN}, 403)
	idB, cookieB := join(cookieA, "小红", second)
	approve(idB, teacher)
	if idA == idB || cookieA.Value == cookieB.Value {
		t.Fatal("old session reused on shared device")
	}
	var current storage.Page
	read(t, call("GET", "/api/student/messages", cookieB, nil, 200), &current)
	if len(current.Messages) != 0 {
		t.Fatal("old history leaked into new lesson")
	}
	call("GET", "/api/student/messages?lesson="+first.LessonID, cookieB, nil, 403)
	call("POST", "/api/student/messages", cookieB, reply, 409)

	app.Close()
	app = newApp()
	handler = app.Cloud()
	call("GET", "/api/teacher/state", teacher, nil, 401)
	teacher = signIn()
	if restored := state("student", cookieB); restored.Student == nil || restored.Student.ID != idB || restored.LessonID != second.LessonID {
		t.Fatalf("active lesson not restored %+v", restored)
	}
	call("POST", "/api/student/leave", cookieB, map[string]string{"studentId": idB}, 200)
	call("GET", "/api/student/messages", cookieB, nil, 401)
	idC, cookieC := join(cookieB, "第三位学生", second)
	approve(idC, teacher)
	if idC == idB {
		t.Fatal("same-lesson switch reused session")
	}
	input["generation"] = second.Generation
	call("POST", "/api/teacher/messages", teacher, input, 201)
	read(t, call("GET", "/api/student/messages", cookieC, nil, 200), &current)
	if len(current.Messages) != 1 || current.Messages[0].ID == broadcast.ID {
		t.Fatal("second lesson broadcast failed")
	}
	call("POST", "/api/teacher/logout", teacher, map[string]string{}, 200)
	call("GET", "/api/teacher/lessons", teacher, nil, 401)
}

func cloudRequest(method, path string, cookie *http.Cookie, body any) *http.Request {
	r := request(cloudHost, method, path, cookie, body)
	r.RemoteAddr = "127.0.0.1:23000"
	r.Header.Set("X-Forwarded-Proto", "https")
	if method == "POST" {
		r.Header.Set("Origin", cloudOrigin)
	}
	return r
}
