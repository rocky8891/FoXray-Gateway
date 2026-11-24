package gateway

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticHistoryKeepsTypedCurrentResults(t *testing.T) {
	a, _ := fixture(t)
	a.audit("diagnostic.result", "synthetic", true, M{"parameters": M{"target": "127.0.0.1"}, "ok": true})
	items, ok := a.diagnosticHistory("")["items"].([]M)
	if !ok || len(items) != 1 || items[0]["message"] != "synthetic" {
		t.Fatal("current result disappeared during history conversion")
	}
}
func TestDiagnosticResultIncludesTaskAndLogsOnSuccessAndFailure(t *testing.T) {
	a, _ := fixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	go func() {
		c, e := listener.Accept()
		if e == nil {
			c.Close()
		}
	}()
	params := M{"target": "127.0.0.1", "port": number, "protocol": "tcp", "scope": "host"}
	job, err := a.submit("test diagnostic", "diagnostic.run", func(ctx context.Context, j *Job) error { return a.diagnose(ctx, j, params) })
	if err != nil {
		t.Fatal(err)
	}
	var response M
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		response = a.diagnosticResult(job)
		if response["result"] != nil && obj(response["task"])["status"] != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	result := obj(response["result"])
	details := obj(result["details"])
	if !yes(details["ok"]) || details["job_id"] != job || len(arr(details["steps"])) == 0 || obj(response["task"])["status"] != "success" {
		t.Fatal("result/task/log correlation missing")
	}
	if str(a.diagnosticParams(str(result["id"]))["target"]) != "127.0.0.1" {
		t.Fatal("saved parameters cannot replay")
	}
	bad := &Job{ID: "invalid-diagnostic"}
	if a.diagnose(a.ctx, bad, M{"target": "bad target"}) == nil {
		t.Fatal("bad target accepted")
	}
	failed := obj(obj(a.diagnosticResult(bad.ID)["result"])["details"])
	if failed["error"] == nil || yes(failed["ok"]) {
		t.Fatal("early failure not saved")
	}
}
func TestCustomVPNExclusionsValidateAndPrecedeRoutingRules(t *testing.T) {
	a, _ := fixture(t)
	p := M{"mode": "custom", "interfaces": []any{"tun-extra"}, "cidrs": []any{"198.51.100.5/24"}, "domains": []any{"*.Example.com", "full:exact.example"}}
	normalized, err := normalizeVPNExclusions(p)
	if err != nil {
		t.Fatal(err)
	}
	if str(arr(normalized["cidrs"])[0]) != "198.51.100.0/24" || str(arr(normalized["domains"])[0]) != "domain:example.com" {
		t.Fatal(normalized)
	}
	c := clone(a.cfg)
	c["vpn_exclusions"] = normalized
	c["endpoints"] = []any{M{"id": "n1", "kind": "socks", "port": 1080, "enabled": true}}
	c["route_profile"] = "global"
	engine, err := a.buildEngine(c, M{}, "n1")
	if err != nil {
		t.Fatal(err)
	}
	rules := arr(obj(engine["routing"])["rules"])
	found := false
	for _, value := range rules {
		r := obj(value)
		if len(arr(r["domain"])) > 0 && r["outboundTag"] == "out-direct" {
			found = true
			break
		}
	}
	if !found || !yes(obj(obj(arr(engine["inbounds"])[0])["sniffing"])["routeOnly"]) {
		t.Fatal("domain exclusion not routed directly with route-only sniffing")
	}
	if !vpnExcludedTarget(c, "sub.example.com", "192.0.2.1") || !vpnExcludedTarget(c, "other.invalid", "198.51.100.8") || vpnExcludedTarget(c, "sub.exact.example", "192.0.2.1") {
		t.Fatal("exclusion matching wrong")
	}
	for _, bad := range []M{{"mode": "unknown"}, {"mode": "custom", "interfaces": []any{"eth0;bad"}}, {"mode": "custom", "cidrs": []any{"bad"}}, {"mode": "custom", "domains": []any{"https://example.com"}}} {
		if _, err := normalizeVPNExclusions(bad); err == nil {
			t.Fatal("invalid exclusion accepted")
		}
	}
}
func TestBatchAdoptionOnlyChangesManagementMetadata(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"firewall-cmd --zone=public --query-port=18080/tcp": "yes", "firewall-cmd --zone=public --query-port=18081/tcp": "yes"}
	before := fingerprint(a.network.managed)
	plan, err := a.network.Preview(M{"kind": "firewall", "operation": "adopt", "items": []any{M{"id": "external-a", "driver": "firewalld", "zone": "public", "kind": "port", "port": "18080", "protocol": "tcp", "enabled": true}, M{"id": "external-b", "driver": "firewalld", "zone": "public", "kind": "port", "port": "18081", "protocol": "tcp", "enabled": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.steps) != 0 || len(plan.undo) != 0 || len(arr(plan.persist["changes"])) != 2 || fingerprint(a.network.managed) != before {
		t.Fatal("adoption modified rules before confirmation")
	}
	for _, call := range f.calls {
		s := strings.Join(call, " ")
		if strings.Contains(s, "--add-") || strings.Contains(s, "--remove-") {
			t.Fatal("adoption changed firewall")
		}
	}
}
func TestTrafficStorageIntervalsIndependentAndDenseHistory(t *testing.T) {
	a, _ := fixture(t)
	m := a.traffic
	if err := m.saveHistoryPolicy("xray", M{"storage_interval": 1}); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now()
	m.lastCleanup = stamp
	m.rows = map[string]*trafficReading{"n1": {ID: "n1", pendingSeconds: 5, pendingUp: 50, pendingDown: 100, lastStoredAt: stamp.Add(-5 * time.Second)}, "wg:test": {ID: "wg:test", pendingSeconds: 5, pendingUp: 70, pendingDown: 140, lastStoredAt: stamp.Add(-5 * time.Second)}}
	if err := m.flushDueLocked(stamp, false); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples WHERE node='n1'").Scan(&count)
	if count != 1 || m.rows["wg:test"].pendingUp != 70 {
		t.Fatal("module interval reset unrelated pending samples")
	}
	if err := m.flushLocked(); err != nil {
		t.Fatal(err)
	}
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples WHERE node='wg:test'").Scan(&count)
	if count != 1 {
		t.Fatal("shutdown did not flush pending module")
	}
	for i := 1; i <= 4; i++ {
		_, err := a.histdb.Exec("INSERT INTO traffic_samples VALUES(?,?,?,?,?)", "dense", now()-float64(i*2), 10, 20, 2)
		if err != nil {
			t.Fatal(err)
		}
	}
	view := m.history(url.Values{"hours": {"0.016666666666666666"}, "module": {"xray"}, "node": {"dense"}})
	if num(view["bucket_seconds"]) != 2 || len(view["items"].([]M)) != 4 {
		t.Fatal("short history still collapsed into minute buckets")
	}
	before := fingerprint(m.historyPolicy)
	for _, p := range []M{{"storage_interval": 0}, {"storage_interval": 1.5}, {"storage_interval": 3601}, {"storage_interval": 1, "extra": true}} {
		if m.saveHistoryPolicy("xray", p) == nil || fingerprint(m.historyPolicy) != before {
			t.Fatal("invalid interval accepted")
		}
	}
}

func TestDiagnosticPersistenceFailureIsNotReportedAsSuccess(t *testing.T) {
	a, _ := fixture(t)
	if _, err := a.logdb.Exec("DROP TABLE operations"); err != nil {
		t.Fatal(err)
	}
	err := a.diagnose(a.ctx, &Job{ID: "save-failed"}, M{"target": "invalid target"})
	if err == nil || !strings.Contains(err.Error(), "结果保存失败") {
		t.Fatal("failed persistence looked successful")
	}
}

func TestDiagnosticExpectedPathSharesCaptureExclusions(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "100.64.1.1", "169.254.1.1", "192.168.1.8", "239.1.1.1", "::1"} {
		if !defaultCaptureBypass(address) {
			t.Fatal("excluded address predicted as proxy", address)
		}
	}
	if defaultCaptureBypass("198.51.100.1") {
		t.Fatal("public target bypassed")
	}
}

func TestFirewallDiagnosticDomainHistoryAndReplay(t *testing.T) {
	toolDir := t.TempDir()
	tool := "iptables"
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	if err := os.WriteFile(filepath.Join(toolDir, tool), nil, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	a, runner := fixture(t)
	runner.mu.Lock()
	runner.responses = map[string]string{"iptables-save": "*filter\n:FORWARD ACCEPT [0:0]\nCOMMIT\n"}
	runner.mu.Unlock()
	params := M{"kind": "firewall", "driver": "iptables", "source": "192.0.2.10", "destination": "localhost", "ingress": "eth0", "protocol": "tcp", "port": "443"}
	if err := a.audit("diagnostic.result", "global diagnostic", true, M{"parameters": M{"target": "localhost"}}); err != nil {
		t.Fatal(err)
	}
	j := &Job{ID: "firewall-diagnostic-test"}
	if err := a.diagnose(a.ctx, j, params); err != nil {
		t.Fatal(err)
	}
	rows := a.diagnosticHistory("", true)["items"].([]M)
	if len(rows) != 1 {
		t.Fatalf("firewall history count = %d", len(rows))
	}
	details := obj(obj(a.diagnosticResult(j.ID)["result"])["details"])
	if net.ParseIP(str(details["resolved_ip"])).To4() == nil || len(arr(details["steps"])) == 0 || details["gateway_probe"] != nil {
		t.Fatal("missing resolved address or logs, or a static diagnostic started a gateway probe")
	}
	replay := a.diagnosticParams(str(rows[0]["id"]))
	if replay["destination"] != "localhost" || replay["ingress"] != "eth0" {
		t.Fatalf("invalid replay parameters: %v", replay)
	}
	if err := a.diagnose(a.ctx, &Job{ID: "firewall-diagnostic-replay"}, replay); err != nil {
		t.Fatal(err)
	}
}
