package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"class-relay/internal/storage"
)

const teacherHost = "127.0.0.1:17888"
const studentHost = "192.168.42.7:17887"

type fixture struct {
	t                *testing.T
	dir              string
	db               *storage.Store
	app              *App
	teacher, student http.Handler
	auth             *http.Cookie
	pin              string
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, dir: t.TempDir()}
	f.open()
	t.Cleanup(func() { f.app.Close(); f.db.Close() })
	return f
}

func (f *fixture) open() {
	f.t.Helper()
	var err error
	f.db, err = storage.Open(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	if err = f.db.SetNetwork(context.Background(), "", true); err != nil {
		f.t.Fatal(err)
	}
	f.app, err = New(f.db, "17887")
	if err != nil {
		f.t.Fatal(err)
	}
	f.teacher, f.student = f.app.Teacher(teacherHost), f.app.Student(studentHost)
	status := f.call(f.teacher, teacherHost, "POST", "/api/teacher/login", nil, map[string]string{"key": f.app.LoginKey()}, 200)
	f.auth = status.Result().Cookies()[0]
	if !f.auth.HttpOnly || f.auth.SameSite != http.SameSiteStrictMode || f.auth.MaxAge <= 0 {
		f.t.Fatal("teacher cookie flags")
	}
	var state storage.Settings
	read(f.t, f.teacherRequest("GET", "state", nil, 200), &state)
	if len(state.PIN) != 6 {
		f.t.Fatalf("invalid generated PIN: %q", state.PIN)
	}
	f.pin = state.PIN
}

func request(host, method, path string, cookie *http.Cookie, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "http://"+host+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if method == "POST" {
		req.Header.Set("Origin", "http://"+host)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func (f *fixture) call(handler http.Handler, host, method, path string, cookie *http.Cookie, body any, status int) *httptest.ResponseRecorder {
	f.t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request(host, method, path, cookie, body))
	if rec.Code != status {
		f.t.Fatalf("%s %s: got %d; want %d; %.300s", method, path, rec.Code, status, rec.Body.String())
	}
	return rec
}

func (f *fixture) teacherRequest(method, path string, body any, status int) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.call(f.teacher, teacherHost, method, "/api/teacher/"+path, f.auth, body, status)
}

func (f *fixture) studentRequest(cookie *http.Cookie, method, path string, body any, status int) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.call(f.student, studentHost, method, "/api/student/"+path, cookie, body, status)
}

func read(t *testing.T, rec *httptest.ResponseRecorder, to any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), to); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) join(name string) (string, *http.Cookie) {
	f.t.Helper()
	rec := f.studentRequest(nil, "POST", "join", map[string]string{"name": name, "pin": f.pin}, 201)
	var response struct{ ID string }
	read(f.t, rec, &response)
	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		f.t.Fatal("student cookie flags")
	}
	return response.ID, cookie
}

func (f *fixture) approve(id string) {
	f.t.Helper()
	f.teacherRequest("POST", "students/"+id, map[string]string{"action": "approve"}, 200)
}

func payload(receiver, id, content string) map[string]any {
	return map[string]any{"receiverId": receiver, "requestId": id, "type": "text", "language": "", "content": content, "generation": 1}
}

func TestAuthAndApproval(t *testing.T) {
	f := newFixture(t)
	f.call(f.teacher, teacherHost, "GET", "/api/teacher/state", nil, nil, 401)
	f.call(f.student, studentHost, "GET", "/api/teacher/state", f.auth, nil, 404)
	f.call(f.teacher, teacherHost, "GET", "/api/student/state", nil, nil, 404)
	f.call(f.student, studentHost, "GET", "/teacher", nil, nil, 404)
	f.call(f.teacher, "other.local", "GET", "/api/teacher/state", f.auth, nil, 421)
	f.call(f.teacher, teacherHost, "POST", "/api/teacher/login", nil, map[string]string{"key": f.app.LoginKey()}, 401)
	for _, origin := range []string{"", "http://other.local"} {
		rec := httptest.NewRecorder()
		req := request(teacherHost, "POST", "/api/teacher/settings", f.auth, map[string]string{})
		req.Header.Set("Origin", origin)
		f.teacher.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("accepted origin %q: %d", origin, rec.Code)
		}
	}
	for i := 0; i < 8; i++ {
		f.studentRequest(nil, "POST", "join", map[string]string{"name": "小明", "pin": "invalid"}, 403)
	}
	f.studentRequest(nil, "POST", "join", map[string]string{"name": "小明", "pin": f.pin}, 429)
	// Release the test's shared address from the deliberately exhausted bucket.
	f.app.limits.mu.Lock()
	delete(f.app.limits.buckets, "pin:192.0.2.1")
	f.app.limits.mu.Unlock()
	a, ca := f.join("小明")
	b, cb := f.join("小明")
	if a == b || ca.Value == cb.Value {
		t.Fatal("same-name students merged")
	}
	f.studentRequest(ca, "GET", "messages", nil, 403)
	f.approve(a)
	f.teacherRequest("POST", "students/"+a, map[string]string{"action": "rename", "name": "甲"}, 200)
	f.teacherRequest("POST", "students/"+b, map[string]string{"action": "deny"}, 200)
	var state struct {
		Student storage.Student
		PIN     string
	}
	read(t, f.studentRequest(ca, "GET", "state", nil, 200), &state)
	if state.Student.ID != a || state.Student.Name != "甲" || state.Student.Status != "approved" || state.PIN != "" {
		t.Fatalf("%+v", state)
	}
	f.studentRequest(cb, "GET", "messages", nil, 403)
	f.teacherRequest("POST", "students/"+a, map[string]string{"action": "remove"}, 200)
	f.studentRequest(ca, "GET", "messages", nil, 403)
	f.studentRequest(ca, "POST", "messages", payload("teacher", "request-0001", "移除后不能发"), 403)
	f.studentRequest(ca, "POST", "ack", map[string][]string{"ids": {"valid-id"}}, 403)
}

func TestMessagesDurablePrivateAndIdempotent(t *testing.T) {
	f := newFixture(t)
	a, ca := f.join("甲")
	_, cb := f.join("乙")
	f.approve(a)
	b, cc := f.join("丙")
	f.approve(b)
	readTeacher := func(path string) storage.Page {
		t.Helper()
		var page storage.Page
		read(t, f.teacherRequest("GET", path, nil, 200), &page)
		return page
	}
	readStudent := func(c *http.Cookie, path string) storage.Page {
		t.Helper()
		var page storage.Page
		read(t, f.studentRequest(c, "GET", path, nil, 200), &page)
		return page
	}
	broadcast := payload("all", "broadcast-1", "def f():\n  return '<script>x</script>'")
	broadcast["type"], broadcast["language"] = "code", "python"
	var msg, duplicate storage.Message
	read(t, f.teacherRequest("POST", "messages", broadcast, 201), &msg)
	read(t, f.teacherRequest("POST", "messages", broadcast, 200), &duplicate)
	if msg.ID != duplicate.ID || msg.Recipients != 2 || msg.Delivered != 0 {
		t.Fatalf("initial message %+v; duplicate %+v", msg, duplicate)
	}
	f.teacherRequest("POST", "messages", payload("all", "broadcast-1", "different"), 409)
	c, cd := f.join("丁")
	f.approve(c)
	if page := readStudent(cd, "messages"); len(page.Messages) != 0 {
		t.Fatal("student approved after broadcast received it")
	}
	p := readStudent(ca, "messages")
	if len(p.Messages) != 1 || p.Messages[0].Content != msg.Content || p.Messages[0].RequestID != "" || p.Messages[0].Recipients != 0 {
		t.Fatalf("student A view %+v", p)
	}
	f.studentRequest(ca, "POST", "ack", map[string][]string{"ids": {msg.ID}}, 200)
	f.studentRequest(ca, "POST", "ack", map[string][]string{"ids": {msg.ID}}, 200)
	if got := readTeacher("messages?conversation=all").Messages[0].Delivered; got != 1 {
		t.Fatalf("delivery count %d", got)
	}
	f.studentRequest(cc, "POST", "ack", map[string][]string{"ids": {msg.ID}}, 200)
	if got := readTeacher("messages?conversation=all").Messages[0].Delivered; got != 2 {
		t.Fatalf("delivery count %d", got)
	}
	var private storage.Message
	read(t, f.teacherRequest("POST", "messages", payload(a, "private-123", "给甲的私信"), 201), &private)
	if got := readStudent(cc, "messages?conversation="+a+"&after="+fmt.Sprint(msg.Seq)); len(got.Messages) != 0 {
		t.Fatalf("private message leaked: %+v", got)
	}
	f.studentRequest(cc, "POST", "ack", map[string][]string{"ids": {private.ID}}, 200)
	if readTeacher("messages?conversation=" + a).Messages[0].Delivered != 0 {
		t.Fatal("unrelated student acknowledged private message")
	}
	f.studentRequest(cb, "GET", "messages", nil, 403)
	f.studentRequest(ca, "POST", "messages", payload("all", "cannot-1", "invalid"), 403)
	f.studentRequest(ca, "POST", "messages", payload(b, "cannot-2", "invalid"), 403)
	forged := payload("teacher", "cannot-3", "invalid")
	forged["senderId"] = b
	f.studentRequest(ca, "POST", "messages", forged, 400)
	f.studentRequest(ca, "POST", "messages", payload("teacher", "long-message", strings.Repeat("你", 21846)), 413)
	var answer storage.Message
	read(t, f.studentRequest(ca, "POST", "messages", payload("teacher", "answer-123", "我的答案"), 201), &answer)
	var state struct{ Students []storage.Student }
	read(t, f.teacherRequest("GET", "state", nil, 200), &state)
	if len(state.Students) != 4 {
		t.Fatalf("unexpected roster: %+v", state)
	}
	for _, student := range state.Students {
		if student.ID == a && student.Unread != 1 || student.ID != a && student.Unread != 0 {
			t.Fatalf("unread: %+v", student)
		}
	}
	f.teacherRequest("POST", "seen", map[string]any{"studentId": a, "seq": answer.Seq}, 200)
	f.teacherRequest("POST", "ack", map[string][]string{"ids": {answer.ID}}, 200)
	var receipts []storage.Receipt
	read(t, f.studentRequest(ca, "POST", "receipts", map[string][]string{"ids": {answer.ID, private.ID}}, 200), &receipts)
	if len(receipts) != 1 || receipts[0].ID != answer.ID || receipts[0].Delivered != 1 {
		t.Fatalf("receipts %+v", receipts)
	}
	f.teacherRequest("POST", "settings", map[string]any{"name": "新课堂", "pin": f.pin, "sending": false}, 200)
	f.studentRequest(ca, "POST", "messages", payload("teacher", "answer-123", "我的答案"), 200)
	f.studentRequest(ca, "POST", "messages", payload("teacher", "disabled-123", "blocked"), 403)
	if data, err := os.ReadFile(filepath.Join(f.dir, "classrelay.sqlite-wal")); err == nil && bytes.Contains(data, []byte(ca.Value)) {
		t.Fatal("raw cookie was written to DB")
	}
	oldTeacher := f.auth
	f.app.Close()
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.open()
	f.call(f.teacher, teacherHost, "GET", "/api/teacher/state", oldTeacher, nil, 401)
	restored := readStudent(ca, "messages?after="+fmt.Sprint(msg.Seq))
	if len(restored.Messages) != 2 || restored.Messages[0].ID != private.ID || restored.Messages[1].ID != answer.ID {
		t.Fatalf("restart recovery %+v", restored)
	}
	f.teacherRequest("POST", "history/clear", map[string]bool{"confirm": false}, 400)
	f.teacherRequest("POST", "history/clear", map[string]bool{"confirm": true}, 200)
	f.teacherRequest("POST", "messages", broadcast, 409)
	if page := readStudent(ca, "messages"); page.Generation != 2 || len(page.Messages) != 0 {
		t.Fatalf("history clear %+v", page)
	}
}

func TestHistoryPagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 85; i++ {
		if _, err := f.db.Send(ctx, storage.Message{ID: fmt.Sprintf("message-%03d", i), SenderID: "teacher", ReceiverID: "all", Type: "text", Content: "test", RequestID: fmt.Sprintf("request-%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	var latest, older, catchup storage.Page
	read(t, f.teacherRequest("GET", "messages?conversation=all", nil, 200), &latest)
	if len(latest.Messages) != 40 || !latest.More || latest.Messages[0].Seq != 46 || latest.Messages[39].Seq != 85 {
		t.Fatalf("invalid latest page")
	}
	read(t, f.teacherRequest("GET", "messages?conversation=all&before=46", nil, 200), &older)
	if len(older.Messages) != 40 || !older.More || older.Messages[0].Seq != 6 || older.Messages[39].Seq != 45 {
		t.Fatalf("invalid older page")
	}
	read(t, f.teacherRequest("GET", "messages?conversation=all&after=44", nil, 200), &catchup)
	if len(catchup.Messages) != 40 || !catchup.More || catchup.Messages[0].Seq != 45 || catchup.Messages[39].Seq != 84 {
		t.Fatalf("invalid catchup page")
	}
}

type streamResponse struct {
	header http.Header
	codes  chan int
	chunks chan string
	buffer strings.Builder
}

func (s *streamResponse) Header() http.Header            { return s.header }
func (s *streamResponse) WriteHeader(status int)         { s.codes <- status }
func (s *streamResponse) Write(body []byte) (int, error) { return s.buffer.Write(body) }
func (s *streamResponse) Flush()                         { s.chunks <- s.buffer.String(); s.buffer.Reset() }

func (f *fixture) subscribe(handler http.Handler, host, path string, cookie *http.Cookie) (*streamResponse, context.CancelFunc, <-chan struct{}) {
	context, cancel := context.WithCancel(context.Background())
	req := request(host, "GET", path, cookie, nil).WithContext(context)
	w := &streamResponse{header: make(http.Header), codes: make(chan int, 2), chunks: make(chan string, 256)}
	done := make(chan struct{})
	go func() { defer close(done); handler.ServeHTTP(w, req) }()
	f.t.Cleanup(cancel)
	return w, cancel, done
}

func nextEvent(t *testing.T, w *streamResponse) {
	t.Helper()
	select {
	case data := <-w.chunks:
		if !strings.Contains(data, "event: sync") {
			t.Fatalf("unexpected event %q", data)
		}
	case status := <-w.codes:
		t.Fatalf("stream rejected: %d", status)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE")
	}
}

func TestFiftyStudentsSSEAndReconnect(t *testing.T) {
	f := newFixture(t)
	type peer struct {
		id     string
		cookie *http.Cookie
		stream *streamResponse
		cancel context.CancelFunc
		done   <-chan struct{}
	}
	peers := make([]peer, 50)
	teacher, _, _ := f.subscribe(f.teacher, teacherHost, "/api/teacher/events", f.auth)
	nextEvent(t, teacher)
	for i := range peers {
		id, cookie := f.join(fmt.Sprint("学生", i))
		f.approve(id)
		peers[i].id, peers[i].cookie = id, cookie
	}
	for i := range peers {
		p := &peers[i]
		p.stream, p.cancel, p.done = f.subscribe(f.student, studentHost, "/api/student/events", p.cookie)
	}
	for _, p := range peers {
		nextEvent(t, p.stream)
		if !f.app.hub.online(p.id) {
			t.Fatalf("offline connected student %s", p.id)
		}
	}
	away := peers[0]
	away.cancel()
	select {
	case <-away.done:
	case <-time.After(5 * time.Second):
		t.Fatal("student did not disconnect")
	}
	if f.app.hub.online(away.id) {
		t.Fatal("disconnected student still online")
	}
	var sent storage.Message
	read(t, f.teacherRequest("POST", "messages", payload("all", "round-one", "全班消息"), 201), &sent)
	if sent.Recipients != 50 {
		t.Fatalf("wrong broadcast audience %d", sent.Recipients)
	}
	for _, p := range peers[1:] {
		nextEvent(t, p.stream)
	}
	back, _, _ := f.subscribe(f.student, studentHost, "/api/student/events", away.cookie)
	nextEvent(t, back)
	for _, p := range peers {
		var page storage.Page
		read(t, f.studentRequest(p.cookie, "GET", "messages?after=0", nil, 200), &page)
		if len(page.Messages) != 1 || page.Messages[0].ID != sent.ID {
			t.Fatalf("missing or duplicate broadcast %+v", page)
		}
		f.studentRequest(p.cookie, "POST", "ack", map[string][]string{"ids": {sent.ID}}, 200)
	}
	var result []storage.Receipt
	read(t, f.teacherRequest("POST", "receipts", map[string][]string{"ids": {sent.ID}}, 200), &result)
	if len(result) != 1 || result[0].Delivered != 50 {
		t.Fatalf("broadcast delivery %+v", result)
	}
	f.teacherRequest("POST", "students/"+away.id, map[string]string{"action": "remove"}, 200)
	nextEvent(t, back)
	f.studentRequest(away.cookie, "GET", "messages", nil, 403)
}

func TestConcurrentMessageRetrySavesOnce(t *testing.T) {
	f := newFixture(t)
	const retries = 20
	results := make(chan *httptest.ResponseRecorder, retries)
	start := make(chan struct{})
	for range retries {
		go func() {
			rec := httptest.NewRecorder()
			req := request(teacherHost, "POST", "/api/teacher/messages", f.auth, payload("all", "same-request-id", "once"))
			<-start
			f.teacher.ServeHTTP(rec, req)
			results <- rec
		}()
	}
	close(start)
	var id string
	created := 0
	for range retries {
		rec := <-results
		switch rec.Code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body)
		}
		var msg storage.Message
		read(t, rec, &msg)
		if id != "" && id != msg.ID {
			t.Fatalf("retries returned different message IDs")
		}
		id = msg.ID
	}
	if created != 1 {
		t.Fatalf("created %d messages for the same request", created)
	}
}
