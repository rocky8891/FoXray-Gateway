package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticMeasuresTargetResponseAndCorrelatesActualNode(t *testing.T) {
	a, _ := fixture(t)
	var target string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.routes.record(routeEvent{Timestamp: time.Now().Unix(), Inbound: "tun", Outbound: "out-n1", Source: r.RemoteAddr, Original: "tcp:" + target, Network: "tcp"})
		time.Sleep(70 * time.Millisecond)
		w.WriteHeader(204)
	}))
	defer server.Close()
	target = server.Listener.Addr().String()
	host, port, _ := net.SplitHostPort(target)
	p, _ := strconv.Atoi(port)
	trust := server.Client().Transport.(*http.Transport).TLSClientConfig
	started := time.Now()
	result, err := measureDiagnosticHTTPS(context.Background(), host, host, p, trust)
	if err != nil || !yes(result["tls_verified"]) || num(result["http_status"]) != 204 {
		t.Fatal(result, err)
	}
	if num(result["http_ttfb_ms"]) < num(result["tcp_ms"])+50 {
		t.Fatal("target delay replaced by TCP dial", result)
	}
	event, ok := a.diagnosticRouteEvent(context.Background(), str(result["local_source"]), target, started)
	if !ok {
		t.Fatal("actual flow event not matched")
	}
	engine := M{"outbounds": []any{M{"tag": "out-n1", "protocol": "hysteria", "settings": M{"version": 2, "address": "198.51.100.7", "port": 52702}, "streamSettings": M{"sockopt": M{"interface": "ens16"}}}}}
	if err := writeJSON(filepath.Join(a.base, "engine.json"), engine); err != nil {
		t.Fatal(err)
	}
	node := a.observedDiagnosticNode(event)
	path := diagnosticObservedPath(node, "www.google.com", 443)
	if node["address"] != "198.51.100.7:52702" || !strings.Contains(path, "Hysteria2") || !strings.Contains(path, "www.google.com:443") {
		t.Fatal(node, path)
	}
	if _, ok := a.routes.match("127.0.0.1:1", target, started); ok {
		t.Fatal("another source port matched")
	}
	result["parameters"] = M{"target": "www.google.com", "protocol": "tls"}
	result["ok"] = true
	if !strings.Contains(diagnosticResultSummary(result), "HTTPS首字节") {
		t.Fatal("summary preferred local TCP")
	}
}
func TestRoutingWebhookSchemaAndNoConfigurationGuess(t *testing.T) {
	a, _ := fixture(t)
	payload := map[string]any{"ts": time.Now().Unix(), "inboundTag": "tun", "outboundTag": "out-direct", "source": "192.0.2.2:12345", "originalTarget": "tcp:198.51.100.5:443", "network": "tcp"}
	raw, _ := json.Marshal(payload)
	response := httptest.NewRecorder()
	a.routes.ServeHTTP(response, httptest.NewRequest("POST", "/routing", strings.NewReader(string(raw))))
	if response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
	key := socketMetadataKey("tcp", "192.0.2.2:12345", "198.51.100.5:443")
	if a.routes.hints()[key] != "out-direct" {
		t.Fatal("official ts/source schema not consumed")
	}
	engine := routeTestEngine(M{"type": "field", "outboundTag": "out-n1"})
	a.attachRoutingWebhooks(engine)
	for _, v := range arr(obj(engine["routing"])["rules"]) {
		hook := obj(obj(v)["webhook"])
		if num(hook["deduplication"]) != 0 || !strings.HasSuffix(str(hook["url"]), ":/routing") {
			t.Fatal(hook)
		}
	}
	c, _, _ := classifyXrayRoute(routeTestRow(), engine, nil)
	if c != "unconfirmed" {
		t.Fatal("config guessed actual outbound")
	}
	if !strings.Contains(diagnosticObservedPath(M{}, "www.google.com", 443), "未观测") {
		t.Fatal("missing event invented selected node")
	}
}
func TestDiagnosticRejectsUntrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	result, err := measureDiagnosticHTTPS(context.Background(), host, host, p, nil)
	if err == nil || yes(result["tls_verified"]) || yes(result["http_response_received"]) {
		t.Fatal("untrusted TLS counted as target success", result)
	}
}
