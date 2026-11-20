package gateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func measureDiagnosticHTTPS(ctx context.Context, host, target string, port int, tlsConfig *tls.Config) (M, error) {
	started := time.Now()
	result := M{"latency_scope": "HTTPS首字节：从发起连接到收到目标HTTP响应首字节，包含TCP、TLS和请求响应；不等同于网络RTT"}
	var mu sync.Mutex
	set := func(key string, value any) { mu.Lock(); result[key] = value; mu.Unlock() }
	var tlsStarted time.Time
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { tlsStarted = time.Now() },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			set("tls_handshake_ms", float64(time.Since(tlsStarted))/float64(time.Millisecond))
			set("tls_total_ms", float64(time.Since(started))/float64(time.Millisecond))
			if err == nil && len(state.VerifiedChains) > 0 {
				set("tls_verified", true)
			}
		},
		GotFirstResponseByte: func() { set("http_ttfb_ms", float64(time.Since(started))/float64(time.Millisecond)) },
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if tlsConfig != nil {
		config = tlsConfig.Clone()
		config.ServerName = host
	}
	address := net.JoinHostPort(target, strconv.Itoa(port))
	transport := &http.Transport{Proxy: nil, TLSClientConfig: config, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 12 * time.Second, DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, ignored string) (net.Conn, error) {
			begin := time.Now()
			dial := net.Dialer{Timeout: 8 * time.Second}
			c, err := dial.DialContext(ctx, "tcp", address)
			set("tcp_ms", float64(time.Since(begin))/float64(time.Millisecond))
			if err == nil {
				set("tcp_connected", true)
				set("local_source", c.LocalAddr().String())
			}
			return c, err
		},
	}
	defer transport.CloseIdleConnections()
	path := "/"
	google := host == "www.google.com" || host == "google.com"
	if google {
		path = "/generate_204"
	}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "GET", "https://"+net.JoinHostPort(host, strconv.Itoa(port))+path, nil)
	if err != nil {
		return result, err
	}
	request.Header.Set("User-Agent", "FoXray-Diagnostics/0.9.3")
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
		set("http_status", response.StatusCode)
		set("http_response_received", true)
	}
	mu.Lock()
	snapshot := clone(result)
	mu.Unlock()
	if err != nil {
		return snapshot, fmt.Errorf("目标HTTPS请求未完成：%w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return snapshot, fmt.Errorf("目标HTTP返回%d", response.StatusCode)
	}
	if google && response.StatusCode != 204 {
		return snapshot, fmt.Errorf("Google连通性接口预期204，实际%d", response.StatusCode)
	}
	return snapshot, nil
}

func (a *App) observedDiagnosticNode(e routeEvent) M {
	engine := M{}
	if readJSON(filepath.Join(a.base, "engine.json"), &engine) != nil {
		return M{"status": "unconfirmed", "note": "已收到核心路由事件，但无法核对出站配置"}
	}
	var out M
	for _, value := range arr(engine["outbounds"]) {
		if str(obj(value)["tag"]) == e.Outbound {
			out = obj(value)
			break
		}
	}
	if out == nil {
		return M{"status": "unconfirmed", "outbound_tag": e.Outbound, "note": "事件出站标签不在当前核心配置中，未猜测节点"}
	}
	settings := obj(out["settings"])
	host, port := str(settings["address"]), num(settings["port"])
	for _, key := range []string{"servers", "vnext"} {
		if values := arr(settings[key]); len(values) > 0 {
			item := obj(values[0])
			host, port = str(item["address"]), num(item["port"])
			break
		}
	}
	protocol := str(out["protocol"])
	if protocol == "hysteria" && num(settings["version"]) == 2 {
		protocol = "Hysteria2"
	}
	sock := obj(obj(out["streamSettings"])["sockopt"])
	node := M{"inbound_tag": e.Inbound, "status": "observed", "outbound_tag": e.Outbound, "protocol": protocol, "interface": sock["interface"], "basis": "本次源连接元组与Xray核心Webhook事件匹配；节点地址、协议及接口由该出站配置解析"}
	if e.Outbound == "out-direct" {
		node["display"] = "Xray直连"
	} else if e.Outbound == "out-block" {
		node["display"] = "Xray阻断"
	} else if host != "" && port > 0 {
		node["address"] = net.JoinHostPort(host, strconv.Itoa(int(port)))
		node["display"] = str(node["address"]) + "（" + protocol + "）"
	} else {
		node["display"] = e.Outbound + "（节点地址不可确认）"
	}
	a.mu.RLock()
	for _, value := range arr(a.native["nodes"]) {
		n := obj(value)
		if "out-"+str(n["id"]) == e.Outbound {
			node["node_id"], node["name"] = n["id"], n["name"]
			break
		}
	}
	a.mu.RUnlock()
	return node
}
func diagnosticObservedPath(node M, host string, port int) string {
	if str(node["status"]) != "observed" {
		return "未观测到本次连接的Xray出站事件；不以当前选中节点代替实测"
	}
	parts := []string{"网关诊断进程", "vpm-tun（核心事件）", "Xray"}
	if strings.HasPrefix(str(node["inbound_tag"]), "smartdns-") {
		parts = []string{"网关诊断进程", "LAN DNS解析器（规则模拟）", "Xray智能DNS入口（核心事件）"}
	}
	if device := str(node["interface"]); device != "" {
		parts = append(parts, device+"（出站配置）")
	}
	parts = append(parts, str(node["display"]), net.JoinHostPort(host, strconv.Itoa(port)))
	return strings.Join(parts, " → ")
}
