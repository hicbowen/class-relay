package server

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

func (a *App) protect(host string, next http.Handler) http.Handler {
	active := make(chan struct{}, 256)
	expectedOrigin := "http://" + host
	if a.cloud != nil {
		expectedOrigin = a.cloud.origin
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if a.cloud != nil {
			if ip := net.ParseIP(remoteIP(r)); !ip.IsLoopback() || r.Header.Get("X-Forwarded-Proto") != "https" {
				problem(w, 403, "请通过已配置的 HTTPS 反向代理访问")
				return
			}
		}
		if r.Host != host {
			problem(w, 421, "请使用 ClassRelay 显示的访问地址")
			return
		}
		isAPI := strings.HasPrefix(r.URL.Path, "/api/")
		if isAPI {
			w.Header().Set("Cache-Control", "no-store")
		}
		origin := r.Header.Get("Origin")
		if (origin != "" && origin != expectedOrigin) || ((r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch) && origin == "") || r.Header.Get("Sec-Fetch-Site") == "cross-site" && isAPI {
			problem(w, 403, "请求来源无效")
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			problem(w, 503, "连接已满，请稍后重试")
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/events") {
			controller := http.NewResponseController(w)
			controller.SetWriteDeadline(time.Now().Add(20 * time.Second))
		}
		next.ServeHTTP(w, r)
	})
}

func decode(w http.ResponseWriter, r *http.Request, target any, max int64) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		problem(w, 415, "需要 JSON 请求")
		return false
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		problem(w, 413, "请求内容过长")
		return false
	}
	if err != nil || !utf8.Valid(raw) {
		problem(w, 400, "请求内容无效")
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		problem(w, 400, "请求格式无效")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil || d.Decode(new(any)) != io.EOF {
		problem(w, 400, "请求格式无效")
		return false
	}
	return true
}

func sendJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func problem(w http.ResponseWriter, status int, message string) {
	sendJSON(w, status, struct {
		Error string `json:"error"`
	}{message})
}

func (a *App) setSession(w http.ResponseWriter, name, value string) {
	path := "/api/student/"
	if name == teacherCookie {
		path = "/api/teacher/"
	}
	maxAge := 60 * 60 * 24 * 90
	if a.cloud != nil {
		maxAge = 0
		if name == teacherCookie {
			maxAge = int(teacherSessionLifetime.Seconds())
		}
	}
	if value == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, Secure: a.cloud != nil, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func cleanName(value string) (string, bool) {
	value = strings.TrimSpace(value)
	n := utf8.RuneCountInString(value)
	return value, n > 0 && n <= 40 && strings.IndexFunc(value, unicode.IsControl) < 0
}

func remoteIP(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func (a *App) clientIP(r *http.Request) string {
	if a.cloud != nil {
		if ip, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil && ip.Zone() == "" {
			return ip.Unmap().String()
		}
	}
	return remoteIP(r)
}

type bucket struct {
	tokens  float64
	updated time.Time
}
type limiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
	cleanup time.Time
}

func (l *limiter) allow(key string, capacity int, interval time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.buckets == nil {
		l.buckets = make(map[string]bucket)
	}
	if now.After(l.cleanup) {
		for id, b := range l.buckets {
			if now.Sub(b.updated) > 10*time.Minute {
				delete(l.buckets, id)
			}
		}
		l.cleanup = now.Add(time.Minute)
	}
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= 4096 {
			return false
		}
		b.tokens = float64(capacity)
	} else {
		b.tokens = min(float64(capacity), b.tokens+float64(now.Sub(b.updated))/float64(interval))
	}
	b.updated = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.buckets[key] = b
	return allowed
}
