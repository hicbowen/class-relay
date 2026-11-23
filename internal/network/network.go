package network

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

type Address struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
}

func Addresses() []Address {
	result := make([]Address, 0)
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			prefix, err := netip.ParsePrefix(addr.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr()
			if ip.Is4() && ip.IsGlobalUnicast() {
				result = append(result, Address{IP: ip.String(), Name: iface.Name})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		score := func(a Address) int {
			s := 0
			if netip.MustParseAddr(a.IP).IsPrivate() {
				s += 8
			}
			if a.Name == "en0" || strings.HasPrefix(a.Name, "eth") || strings.HasPrefix(a.Name, "wlan") || strings.Contains(strings.ToLower(a.Name), "wi-fi") {
				s += 4
			}
			return s
		}
		return score(result[i]) > score(result[j])
	})
	return result
}

func HTTP(handler http.Handler) *http.Server {
	return &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 12 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
	}
}

type Listener struct {
	mu      sync.Mutex
	server  *http.Server
	ip      string
	port    string
	handler func(host string) http.Handler
}

func NewListener(port string, handler func(host string) http.Handler) *Listener {
	return &Listener{port: port, handler: handler}
}

func (l *Listener) State() (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ip, l.server != nil
}

func (l *Listener) Start(ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.server != nil && l.ip == ip {
		return nil
	}
	host := net.JoinHostPort(ip, l.port)
	socket, err := ListenIPv4(host)
	if err != nil {
		return fmt.Errorf("无法监听 %s：%w", host, err)
	}
	current := HTTP(l.handler(host))
	if l.server != nil {
		l.server.Close()
	}
	l.server, l.ip = current, ip
	go func() {
		current.Serve(socket)
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.server == current {
			l.server = nil
		}
	}()
	return nil
}

func (l *Listener) URL(ip string) string {
	return "http://" + net.JoinHostPort(ip, l.port) + "/student"
}

func (l *Listener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.server != nil {
		l.server.Close()
		l.server = nil
	}
}

func Shutdown(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if server.Shutdown(ctx) != nil {
		server.Close()
	}
}
