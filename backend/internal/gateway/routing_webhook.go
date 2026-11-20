package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type routeEvent struct {
	Timestamp   int64 `json:"ts"`
	receivedAt  int64
	Inbound     string `json:"inboundTag"`
	Outbound    string `json:"outboundTag"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Original    string `json:"originalTarget"`
	Target      string `json:"routeTarget"`
	Network     string `json:"network"`
	Protocol    string `json:"protocol"`
}
type routeObserver struct {
	mu       sync.RWMutex
	events   map[string][]routeEvent
	ready    bool
	issue    string
	received uint64
}

func newRouteObserver() *routeObserver { return &routeObserver{events: map[string][]routeEvent{}} }
func webhookAddress(s string) string {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "tcp:"), "udp:")
	host, port, err := net.SplitHostPort(s)
	if err != nil || net.ParseIP(host) == nil {
		return ""
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return ""
	}
	return net.JoinHostPort(host, port)
}
func eventTarget(e routeEvent) string {
	return firstNonempty(webhookAddress(e.Original), webhookAddress(e.Destination))
}
func eventKey(e routeEvent) string { return socketMetadataKey(e.Network, e.Source, eventTarget(e)) }
func (o *routeObserver) record(e routeEvent) bool {
	stamp := time.Now().Unix()
	if e.Inbound != "tun" || (e.Network != "tcp" && e.Network != "udp") || e.Outbound == "" || len(e.Outbound) > 128 || webhookAddress(e.Source) == "" || eventTarget(e) == "" || e.Timestamp < stamp-10 || e.Timestamp > stamp+2 {
		return false
	}
	e.Source = webhookAddress(e.Source)
	e.receivedAt = time.Now().UnixNano()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.received++
	if o.received%256 == 0 || len(o.events) >= 16384 {
		for key, events := range o.events {
			if len(events) == 0 || events[len(events)-1].Timestamp < stamp-600 {
				delete(o.events, key)
			}
		}
	}
	key := eventKey(e)
	if _, exists := o.events[key]; len(o.events) >= 16384 && !exists {
		return false
	}
	events := append(o.events[key], e)
	if len(events) > 8 {
		events = events[len(events)-8:]
	}
	o.events[key] = events
	return true
}
func (o *routeObserver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.Path != "/routing" {
		http.Error(w, "not found", 404)
		return
	}
	var e routeEvent
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&e) != nil || !o.record(e) {
		http.Error(w, "invalid route event", 400)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (o *routeObserver) match(source, target string, started time.Time) (routeEvent, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	key := socketMetadataKey("tcp", source, target)
	var result routeEvent
	found := false
	for _, e := range o.events[key] {
		if e.receivedAt < started.UnixNano() || e.Timestamp > time.Now().Unix()+2 {
			continue
		}
		if found && result.Outbound != e.Outbound {
			return routeEvent{}, false
		}
		result, found = e, true
	}
	return result, found
}
func (o *routeObserver) hints() map[string]string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := map[string]string{}
	stamp := time.Now().Unix()
	for key, events := range o.events {
		for _, e := range events {
			if e.Timestamp < stamp-600 {
				continue
			}
			if old, exists := result[key]; exists && old != e.Outbound {
				result[key] = ""
			} else if !exists {
				result[key] = e.Outbound
			}
		}
	}
	return result
}
func (a *App) routingWebhookPath() string { return filepath.Join(a.base, "run", "routing.sock") }
func (a *App) startRoutingWebhook() {
	if a.readonly || runtime.GOOS == "windows" {
		return
	}
	fail := func(issue string) { a.routes.mu.Lock(); a.routes.issue = issue; a.routes.mu.Unlock() }
	group, err := user.LookupGroup("vpm-engine")
	if err != nil {
		fail("核心服务组不可用，Webhook接收未启动")
		return
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return
	}
	path := a.routingWebhookPath()
	dir := filepath.Dir(path)
	if info, e := os.Lstat(dir); e == nil && info.Mode()&os.ModeSymlink != 0 {
		fail("Webhook目录为符号链接，未更改")
		return
	}
	if err = os.MkdirAll(dir, 0750); err != nil {
		fail("Webhook目录不可用")
		return
	}
	if os.Chmod(dir, 0750) != nil || os.Chown(dir, os.Getuid(), gid) != nil {
		fail("Webhook目录权限设置失败")
		return
	}
	if info, e := os.Lstat(path); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			fail("Webhook路径已有非套接字文件，已保留")
			return
		}
		connection, e := net.DialTimeout("unix", path, 200*time.Millisecond)
		if e == nil {
			connection.Close()
			fail("已有Webhook监听，未替换")
			return
		}
		if os.Remove(path) != nil {
			return
		}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		fail("Webhook套接字监听失败")
		return
	}
	if os.Chmod(path, 0660) != nil || os.Chown(path, os.Getuid(), gid) != nil {
		listener.Close()
		fail("Webhook套接字权限设置失败")
		return
	}
	a.routes.mu.Lock()
	a.routes.ready = true
	a.routes.mu.Unlock()
	server := &http.Server{Handler: a.routes, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxHeaderBytes: 4096}
	go func() { <-a.ctx.Done(); server.Close() }()
	go server.Serve(listener)
}
func (a *App) attachRoutingWebhooks(engine M) {
	rules := arr(obj(engine["routing"])["rules"])
	outs := arr(engine["outbounds"])
	if len(outs) == 0 {
		return
	}
	rules = append(rules, M{"type": "field", "inboundTag": []string{"tun"}, "outboundTag": obj(outs[0])["tag"]})
	for _, value := range rules {
		rule := obj(value)
		if rule["webhook"] == nil {
			rule["webhook"] = M{"url": a.routingWebhookPath() + ":/routing", "deduplication": 0}
		}
	}
	obj(engine["routing"])["rules"] = rules
}
func (a *App) routingWebhookStatus() M {
	a.routes.mu.RLock()
	ready, issue := a.routes.ready, a.routes.issue
	a.routes.mu.RUnlock()
	routing := M{}
	configured := false
	if readEngineField(filepath.Join(a.base, "engine.json"), "routing", &routing) == nil {
		for _, value := range arr(routing["rules"]) {
			if str(obj(obj(value)["webhook"])["url"]) == a.routingWebhookPath()+":/routing" {
				configured = true
				break
			}
		}
	}
	note := issue
	if note == "" {
		if !ready {
			note = "Webhook接收器尚未启动"
		} else if !configured {
			note = "核心尚未应用Webhook配置；诊断不会自动重启核心"
		} else {
			note = "等待本次连接的核心实际出站事件"
		}
	}
	return M{"receiver_ready": ready, "configured": configured, "note": note}
}
func (a *App) diagnosticRouteEvent(ctx context.Context, source, target string, started time.Time) (routeEvent, bool) {
	deadline := time.NewTimer(1500 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e, ok := a.routes.match(source, target, started); ok {
			return e, true
		}
		select {
		case <-ctx.Done():
			return routeEvent{}, false
		case <-deadline.C:
			return routeEvent{}, false
		case <-ticker.C:
		}
	}
}
