package server

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"class-relay/internal/storage"
)

type stream struct {
	id       string
	wake     chan struct{}
	approved bool
}

type hub struct {
	mu      sync.Mutex
	streams map[*stream]struct{}
	byID    map[string]int
	present map[string]int
}

func newHub() hub {
	return hub{streams: make(map[*stream]struct{}), byID: make(map[string]int), present: make(map[string]int)}
}

func (h *hub) subscribe(id string) *stream {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.streams) >= 120 || h.byID[id] >= 3 {
		return nil
	}
	s := &stream{id: id, wake: make(chan struct{}, 1)}
	h.streams[s] = struct{}{}
	h.byID[id]++
	return s
}

func (h *hub) presence(s *stream, approved bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.approved == approved {
		return false
	}
	s.approved = approved
	if approved {
		h.present[s.id]++
		return h.present[s.id] == 1
	}
	h.present[s.id]--
	if h.present[s.id] == 0 {
		delete(h.present, s.id)
		return true
	}
	return false
}

func (h *hub) unsubscribe(s *stream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.streams, s)
	h.byID[s.id]--
	if h.byID[s.id] == 0 {
		delete(h.byID, s.id)
	}
}

func (h *hub) online(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.present[id] > 0
}

func (h *hub) notify(ids []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.streams {
		selected := ids == nil || s.id == storage.TeacherID
		for _, id := range ids {
			selected = selected || s.id == id
		}
		if selected {
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
	}
}

func (h *hub) notifyAll()     { h.notify(nil) }
func (h *hub) notifyTeacher() { h.notify([]string{}) }

func (a *App) updatePresence(s *stream, approved bool) {
	if !a.hub.presence(s, approved) {
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
	defer cancel()
	a.roomStore().Touch(ctx, s.id)
	a.hub.notifyTeacher()
}

func (a *App) events(w http.ResponseWriter, r *http.Request, who actor) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "请使用 GET 建立实时连接")
		return
	}
	if who.Student.Status == "denied" || who.Student.Status == "removed" {
		problem(w, 403, "请重新申请加入课堂")
		return
	}
	s := a.hub.subscribe(who.ID)
	if s == nil {
		problem(w, 429, "实时连接已满，请关闭重复的页面")
		return
	}
	defer a.hub.unsubscribe(s)
	defer a.updatePresence(s, false)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	write := func(data string) bool {
		controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.WriteString(w, data); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	sendEvent := func(event string) bool {
		var revoked bool
		if who.ID == storage.TeacherID {
			revoked = a.cloud != nil && !a.teacherAuthorized(r)
		} else {
			current, err := a.studentSession(r)
			revoked = err != nil || current.ID != who.ID || current.Status == "removed" || current.Status == "denied"
			a.updatePresence(s, !revoked && current.Status == "approved")
		}
		if revoked {
			event = "sync"
		}
		return write("event: "+event+"\ndata: {}\n\n") && !revoked
	}
	if !sendEvent("sync") {
		return
	}
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-r.Context().Done():
			return
		case <-s.wake:
			if !sendEvent("sync") {
				return
			}
		case <-heartbeat.C:
			if !sendEvent("heartbeat") {
				return
			}
		}
	}
}
