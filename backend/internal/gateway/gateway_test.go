package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu        sync.Mutex
	calls     [][]string
	fail      string
	responses map[string]string
}

func (f *fakeRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{}, args...))
	joined := strings.Join(args, " ")
	if value, ok := f.responses[joined]; ok {
		return value, nil
	}
	if f.fail != "" && strings.Contains(joined, f.fail) {
		return "", errors.New("synthetic command failure")
	}
	if args[0] == "systemctl" && len(args) > 1 && args[1] == "show" {
		return "ActiveState=active\nSubState=running\nMainPID=42\nUnitFileState=enabled\n", nil
	}
	if args[0] == "ip" && strings.Contains(joined, "-j") {
		return "[]", nil
	}
	return "", nil
}
func fixture(t *testing.T) (*App, *fakeRunner) {
	t.Helper()
	a, e := New(t.TempDir(), t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	f := &fakeRunner{}
	a.runner = f
	t.Cleanup(a.Close)
	return a, f
}
func TestPublicStateCannotExposeNodeOrSubscriptionCredentials(t *testing.T) {
	a, _ := fixture(t)
	a.native = M{"nodes": []any{M{"id": "n1", "name": "test", "enabled": true, "priority": 20, "outbound": M{"type": "vless", "server": "203.0.113.1", "server_port": 443, "uuid": "synthetic-node-auth"}}}, "subscriptions": []any{M{"id": "s1", "name": "subscription", "url": "https://example.invalid/private-synthetic-url"}}}
	a.cfg["endpoints"] = []any{M{"id": "n1", "name": "test", "enabled": true, "kind": "native", "priority": 20}}
	b, _ := json.Marshal(a.state())
	if strings.Contains(string(b), "synthetic-node-auth") || strings.Contains(string(b), "private-synthetic-url") {
		t.Fatal("credential leaked into public state")
	}
}
func TestWireGuardJSONNeverContainsPrivateOrPresharedKey(t *testing.T) {
	raw := "[Interface]\nPrivateKey = synthetic-private\nAddress = 10.10.0.1/24\nPostUp = preserve-existing-hook\n[Peer]\nPublicKey = synthetic-public\nPresharedKey = synthetic-psk\nAllowedIPs = 10.10.0.2/32\n"
	p, e := parseWG("wg-home", raw)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(p)
	for _, secret := range []string{"synthetic-private", "synthetic-psk", "preserve-existing-hook"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("private config exposed")
		}
	}
	if !strings.Contains(p.render(), "preserve-existing-hook") {
		t.Fatal("existing hooks lost")
	}
}
func TestWireGuardHasNoHardcodedInterfaceExclusions(t *testing.T) {
	if _, err := parseWG("wg-zstack", "[Interface]\nAddress = 10.0.0.1/24\n"); err != nil {
		t.Fatal(err)
	}
}

func TestUntrustedOriginCannotChangeNetwork(t *testing.T) {
	a, f := fixture(t)
	a.readonly = false
	r := httptest.NewRequest(http.MethodPost, "http://192.168.1.8:18880/api/network/plan", strings.NewReader(`{"kind":"routes","operation":"add"}`))
	r.RemoteAddr = "192.168.1.10:5500"
	r.Header.Set("Origin", "https://evil.invalid")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("got %d", w.Code)
	}
	if len(f.calls) != 0 {
		t.Fatal("request reached system commands")
	}
}
func TestReadOnlyInstanceRejectsEveryMutation(t *testing.T) {
	a, f := fixture(t)
	for _, path := range []string{"/api/proxy", "/api/network/apply", "/api/native/node", "/api/services/action"} {
		r := httptest.NewRequest("POST", "http://192.168.1.8:18881"+path, strings.NewReader(`{}`))
		r.RemoteAddr = "192.168.1.10:5500"
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 409 {
			t.Fatalf("path %s returned %d", path, w.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("read-only instance executed system commands")
	}
}
func routeData() M {
	return M{"kind": "routes", "operation": "add", "item": M{"dst": "203.0.113.0/24", "dev": "test0", "table": 2000, "metric": 10, "family": 4}}
}
func TestRoutePreviewRejectsInjectionAndReservedTables(t *testing.T) {
	for _, change := range []M{{"dst": "203.0.113.0/24;reboot"}, {"dev": "test0;reboot"}, {"table": 16080}, {"table": 65743}, {"table": 255}} {
		a, f := fixture(t)
		d := routeData()
		for k, v := range change {
			obj(d["item"])[k] = v
		}
		if _, e := a.network.Preview(d); e == nil {
			t.Fatalf("accepted invalid fields %v", change)
		}
		if len(f.calls) > 0 {
			t.Fatal("invalid preview executed a command")
		}
	}
}
func TestNetworkChangeRemainsPendingUntilConfirmed(t *testing.T) {
	a, f := fixture(t)
	p, e := a.network.Preview(routeData())
	if e != nil {
		t.Fatal(e)
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	if p.Status != "awaiting_confirmation" {
		t.Fatal(p.Status)
	}
	if len(arr(a.network.managed["routes"])) != 0 {
		t.Fatal("unconfirmed route persisted")
	}
	if e = a.network.Confirm(p.ID); e != nil {
		t.Fatal(e)
	}
	if len(arr(a.network.managed["routes"])) != 1 {
		t.Fatal("confirmed route missing")
	}
	calls := len(f.calls)
	if e = rollbackFile(a.network.specPath(p), f); e != nil {
		t.Fatal(e)
	}
	if len(f.calls) != calls {
		t.Fatal("confirmed change was rolled back")
	}
}
func TestFailedRouteAddCannotDeleteExistingRoute(t *testing.T) {
	a, f := fixture(t)
	f.fail = "route add"
	p, e := a.network.Preview(routeData())
	if e != nil {
		t.Fatal(e)
	}
	if e = a.network.Apply(p.ID); e == nil {
		t.Fatal("expected failure")
	}
	for _, args := range f.calls {
		if strings.Contains(strings.Join(args, " "), "route del") {
			t.Fatal("failed add deleted a route")
		}
	}
}
func TestStandaloneRollbackWorksWithoutManagerMemory(t *testing.T) {
	a, f := fixture(t)
	p, e := a.network.Preview(routeData())
	if e != nil {
		t.Fatal(e)
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	if e = rollbackFile(a.network.specPath(p), f); e != nil {
		t.Fatal(e)
	}
	spec := rollbackSpec{}
	if e = readJSON(a.network.specPath(p), &spec); e != nil {
		t.Fatal(e)
	}
	if spec.Status != "rolled_back" {
		t.Fatal(spec.Status)
	}
	calls := len(f.calls)
	_ = rollbackFile(a.network.specPath(p), f)
	if len(f.calls) != calls {
		t.Fatal("rollback was not idempotent")
	}
}
func TestReservedRulePrioritiesAreProtected(t *testing.T) {
	a, _ := fixture(t)
	for _, pref := range []int{16070, 16079, 16080} {
		_, e := a.network.Preview(M{"kind": "rules", "operation": "add", "item": M{"priority": pref, "table": 2000, "from": "all", "family": 4}})
		if e == nil {
			t.Fatal("reserved priority accepted")
		}
	}
}
func TestInactiveNodesDoNotAlterCommonScoreMetrics(t *testing.T) {
	a, _ := fixture(t)
	a.cfg["score_metrics"] = []any{"latency", "loss", "speed"}
	a.cfg["endpoints"] = []any{M{"id": "n1", "enabled": true, "priority": 20}, M{"id": "n2", "enabled": false, "priority": 20}}
	a.results["n1"] = M{"ok": true, "latency": 100, "time": now(), "packet_loss_status": "measured", "packet_loss_pct": 0, "packet_loss_at": now(), "packet_loss_ttl": 600, "last_speed": 2000}
	rows := a.scores()
	if len(rows) != 2 || len(rows[0]["basis"].([]string)) != 3 {
		t.Fatal("disabled node changed scoring basis")
	}
	if rows[1]["score"] != nil {
		t.Fatal("disabled node scored")
	}
}
func TestWarpCannotChangeNativeScoring(t *testing.T) {
	a, _ := fixture(t)
	a.cfg["score_metrics"] = []any{"latency", "loss", "speed"}
	a.cfg["endpoints"] = []any{M{"id": "n1", "enabled": true, "priority": 20}, M{"id": "warp", "enabled": true, "priority": 20}}
	a.results["n1"] = M{"ok": true, "latency": 100, "time": now(), "packet_loss_status": "measured", "packet_loss_pct": 0, "packet_loss_at": now(), "packet_loss_ttl": 600, "last_speed": 2000}
	a.results["warp"] = M{"ok": true, "latency": 100, "time": now()}
	before := a.scores()[0]["score"]
	a.results["warp"]["ok"] = false
	after := a.scores()[0]["score"]
	if before != after {
		t.Fatal("warp changed native score")
	}
}
func TestFailedProbeParsesAsFailureNotZero(t *testing.T) {
	r := parseScoutReport("", "masque-h2", "203.0.113.1:443")
	if r["rtt_ms"] != nil || r["loss_pct"] != nil || yes(r["durable"]) {
		t.Fatal("failed probe was a valid zero")
	}
}
func TestRealityAndTransportSurviveCanonicalConversion(t *testing.T) {
	o := M{"type": "vless", "server": "node.example", "server_port": 443, "uuid": "synthetic", "tls": M{"enabled": true, "server_name": "front.example", "reality": M{"enabled": true, "public_key": "public", "short_id": "abcd"}}, "transport": M{"type": "grpc", "service_name": "svc"}}
	r, e := canonicalOutbound(o, "n1")
	if e != nil {
		t.Fatal(e)
	}
	stream := obj(r["streamSettings"])
	if str(obj(stream["realitySettings"])["serverName"]) != "front.example" || str(obj(stream["grpcSettings"])["authority"]) != "node.example" {
		t.Fatal("SNI or authority lost")
	}
}
func TestExpiredNetworkConfirmationCannotPersist(t *testing.T) {
	a, _ := fixture(t)
	p, e := a.network.Preview(routeData())
	if e != nil {
		t.Fatal(e)
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	var s rollbackSpec
	_ = readJSON(a.network.specPath(p), &s)
	s.Expires = now() + 1
	_ = writeJSON(a.network.specPath(p), s)
	if a.network.Confirm(p.ID) == nil {
		t.Fatal("expired change confirmed")
	}
	if len(arr(a.network.managed["routes"])) > 0 {
		t.Fatal("expired ownership persisted")
	}
}
func TestStructuredMaskedEditKeepsAuthentication(t *testing.T) {
	old := M{"uuid": "synthetic", "tls": M{"server_name": "old.example", "reality": M{"public_key": "public"}}}
	m := mergeMasked(old, M{"uuid": "<redacted>", "tls": M{"server_name": "new.example"}})
	if m["uuid"] != "synthetic" || str(obj(m["tls"])["server_name"]) != "new.example" || obj(obj(m["tls"])["reality"])["public_key"] != "public" {
		t.Fatal("masked edit corrupted data")
	}
}
func TestOldSQLiteHistoryRemainsReadable(t *testing.T) {
	a, _ := fixture(t)
	_, e := a.histdb.Exec("INSERT INTO samples(node,t,latency) VALUES(?,?,?)", "n1", now()-2, 123)
	if e != nil {
		t.Fatal(e)
	}
	r := a.latencyHistory(map[string][]string{"node": {"n1"}})
	if len(arr(obj(r["series"])["n1"])) != 1 {
		t.Fatal("legacy samples not visible")
	}
}
func TestWatchdogRejectsArbitraryFilePaths(t *testing.T) {
	a, f := fixture(t)
	path := filepath.Join(a.base, "untrusted.json")
	_ = writeJSON(path, rollbackSpec{Status: "awaiting_confirmation", Undo: []Step{{Args: []string{"ip", "route", "flush", "table", "main"}}}})
	if rollbackFile(path, f) == nil {
		t.Fatal("arbitrary watchdog file accepted")
	}
}

var _ = time.Second

func TestDeletingOwnedRouteCannotTargetClientSuppliedRoute(t *testing.T) {
	a, _ := fixture(t)
	a.network.managed["routes"] = []any{M{"id": "owned", "dst": "203.0.113.0/24", "dev": "test0", "table": 2000, "metric": 10, "family": 4}}
	p, e := a.network.Preview(M{"kind": "routes", "operation": "delete", "item": M{"id": "owned", "dst": "default", "dev": "ens16", "table": 254, "metric": 0}})
	if e != nil {
		t.Fatal(e)
	}
	cmd := strings.Join(p.steps[0].Args, " ")
	if !strings.Contains(cmd, "203.0.113.0/24") || strings.Contains(cmd, "default") {
		t.Fatal("client fields changed deletion target")
	}
}
func TestDeletingOwnedRuleUsesRecordedPriority(t *testing.T) {
	a, _ := fixture(t)
	a.network.managed["rules"] = []any{M{"id": "owned", "priority": 2000, "from": "192.0.2.1/32", "table": 2000, "family": 4}}
	p, e := a.network.Preview(M{"kind": "rules", "operation": "delete", "item": M{"id": "owned", "priority": 32766, "table": 254}})
	if e != nil {
		t.Fatal(e)
	}
	cmd := strings.Join(p.steps[0].Args, " ")
	if !strings.Contains(cmd, "pref 2000") || strings.Contains(cmd, "32766") {
		t.Fatal("client fields changed deletion target")
	}
}
func TestExistingFirewallPortCannotBeAdoptedOrRemovedByRollback(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"firewall-cmd --zone=public --query-port=8080/tcp": "yes"}
	_, e := a.network.Preview(M{"kind": "firewall", "operation": "add", "item": M{"zone": "public", "protocol": "tcp", "port": 8080}})
	if e == nil {
		t.Fatal("pre-existing firewall rule adopted")
	}
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), "--remove-port") {
			t.Fatal("external firewall rule removed")
		}
	}
}
func TestFingerprintMatchesLegacyUnicodeEncoding(t *testing.T) {
	got := fingerprint(M{"name": "节点"})
	want := fingerprint(json.RawMessage(`{"name":"\u8282\u70b9"}`))
	if got != want {
		t.Fatal("legacy unicode digest differs")
	}
}
