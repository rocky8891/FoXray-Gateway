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
	domains  bool
	probes   map[string]time.Time
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
	if (e.Inbound != "tun" && e.Inbound != "smartdns-direct" && e.Inbound != "smartdns-proxy") || (e.Network != "tcp" && e.Network != "udp") || e.Outbound == "" || len(e.Outbound) > 128 || len(e.Protocol) > 32 || webhookAddress(e.Source) == "" || eventTarget(e) == "" || e.Timestamp < stamp-10 || e.Timestamp > stamp+2 {
		return false
	}
	e.Source = webhookAddress(e.Source)
	e.receivedAt = time.Now().UnixNano()
	o.mu.Lock()
	defer o.mu.Unlock()
	if e.Inbound != "tun" {
		if expiry, ok := o.probes[eventKey(e)]; !ok || time.Now().After(expiry) {
			return true
		}
	}
	if !o.domains {
		e.Target = ""
		if webhookAddress(e.Original) == "" {
			e.Original = ""
		}
		if webhookAddress(e.Destination) == "" {
			e.Destination = ""
		}
	}
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

func (o *routeObserver) setDomainCollection(enabled bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.domains = enabled
	if !enabled {
		for key, events := range o.events {
			for i := range events {
				events[i].Target = ""
				if webhookAddress(events[i].Original) == "" {
					events[i].Original = ""
				}
				if webhookAddress(events[i].Destination) == "" {
					events[i].Destination = ""
				}
			}
			o.events[key] = events
		}
	}
}

type domainEvidence struct{ domain, basis, note string }

func routeEventDomain(e routeEvent) domainEvidence {
	for _, candidate := range []string{e.Target, e.Destination} {
		address := strings.TrimPrefix(strings.TrimPrefix(candidate, "tcp:"), "udp:")
		host, port, err := net.SplitHostPort(address)
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		n, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || n < 1 || n > 65535 || net.ParseIP(host) != nil || !safeHostname(host) {
			continue
		}
		basis := "核心目标域名"
		switch strings.ToLower(e.Protocol) {
		case "http":
			basis = "HTTP Host"
		case "tls":
			basis = "TLS SNI"
		case "quic":
			basis = "QUIC TLS SNI"
		}
		return domainEvidence{domain: host, basis: basis, note: "Xray核心Webhook完整连接元组匹配；" + basis + "。名称来自请求或握手，不代表应用类型；ECH外层名称可能不是真实访问域名"}
	}
	return domainEvidence{}
}
func (o *routeObserver) domainHints() map[string]domainEvidence {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := map[string]domainEvidence{}
	if !o.domains {
		return result
	}
	ambiguous := map[string]bool{}
	for key, events := range o.events {
		for _, e := range events {
			if e.Timestamp < time.Now().Unix()-600 {
				continue
			}
			evidence := routeEventDomain(e)
			if evidence.domain == "" {
				continue
			}
			if prior, ok := result[key]; ok && prior.domain != evidence.domain {
				ambiguous[key] = true
			}
			result[key] = evidence
		}
	}
	for key := range ambiguous {
		delete(result, key)
	}
	return result
}
func enrichWebhookDomains(rows []M, hints map[string]domainEvidence, enabled bool) {
	for _, row := range rows {
		if row["module"] != "xray" || yes(row["_deferred"]) {
			continue
		}
		if !enabled {
			row["domain"] = ""
			row["domain_basis"] = ""
			row["domain_note"] = "域名识别已关闭，可在Xray代理模式中开启"
			continue
		}
		key := socketMetadataKey(str(row["protocol"]), str(row["source"]), str(row["destination"]))
		if evidence, ok := hints[key]; ok {
			row["domain"] = evidence.domain
			row["domain_basis"] = evidence.basis
			row["domain_note"] = evidence.note
		}
	}
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
	return o.matchNetwork("tcp", source, target, started)
}
func (o *routeObserver) matchNetwork(network, source, target string, started time.Time) (routeEvent, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	key := socketMetadataKey(network, source, target)
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
	return a.diagnosticRouteEventNetwork(ctx, "tcp", source, target, started)
}
func (a *App) diagnosticRouteEventNetwork(ctx context.Context, network, source, target string, started time.Time) (routeEvent, bool) {
	deadline := time.NewTimer(1500 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e, ok := a.routes.matchNetwork(network, source, target, started); ok {
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

func (a *App) diagnosticRouteCandidates(ctx context.Context, candidates []any, started time.Time) (routeEvent, bool) {
	if len(candidates) == 0 {
		return routeEvent{}, false
	}
	deadline := time.NewTimer(1500 * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(30 * time.Millisecond)
	defer tick.Stop()
	for {
		for _, value := range candidates {
			c := obj(value)
			if e, ok := a.routes.matchNetwork(str(c["network"]), str(c["source"]), str(c["target"]), started); ok {
				return e, true
			}
		}
		select {
		case <-ctx.Done():
			return routeEvent{}, false
		case <-deadline.C:
			return routeEvent{}, false
		case <-tick.C:
		}
	}
}

func (o *routeObserver) expectProbe(network, source, target string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.probes == nil {
		o.probes = map[string]time.Time{}
	}
	for key, expires := range o.probes {
		if time.Now().After(expires) {
			delete(o.probes, key)
		}
	}
	if len(o.probes) < 128 {
		o.probes[socketMetadataKey(network, source, target)] = time.Now().Add(30 * time.Second)
	}
}
