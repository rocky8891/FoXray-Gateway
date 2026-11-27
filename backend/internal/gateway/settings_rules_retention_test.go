package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayRefreshAndLogSavePreserveOtherSettings(t *testing.T) {
	a, runner := fixture(t)
	a.readonly = false
	a.settings["wireguard_monitor"] = M{"enabled": true}
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/monitoring/display", strings.NewReader(`{"chart_refresh_seconds":5}`))
	r.RemoteAddr = "127.0.0.1:12"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 || num(a.settings["chart_refresh_seconds"]) != 5 {
		t.Fatal(w.Code, w.Body.String())
	}
	_, e := a.postAPI(httptest.NewRequest("POST", "http://127.0.0.1/api/settings", nil), M{"log_retention_days": 30, "measurement_retention_days": 30, "log_max_rows": 10000})
	if e != nil || !yes(obj(a.settings["wireguard_monitor"])["enabled"]) || num(a.settings["chart_refresh_seconds"]) != 5 {
		t.Fatal("unrelated settings overwritten", e)
	}
	for _, call := range runner.calls {
		if len(call) < 2 || call[0] != "systemctl" || call[1] != "show" {
			t.Fatal("settings issued a mutation", call)
		}
	}
}
func TestCustomRulesValidationAndEngineScope(t *testing.T) {
	c := M{"custom_rules": []any{M{"enabled": true, "type": "domain", "targets": []any{"example.com"}, "action": "direct", "network": "tcp,udp", "port": "80,443"}}}
	if validateCustomRules(c) != nil {
		t.Fatal("valid rule rejected")
	}
	r := obj(customEngineRules(c, "n1")[0])
	if r["outboundTag"] != "out-direct" || len(r["inboundTag"].([]string)) != 1 {
		t.Fatal(r)
	}
	obj(arr(c["custom_rules"])[0])["port"] = "65536"
	if validateCustomRules(c) == nil {
		t.Fatal("invalid port accepted")
	}
}
func TestModuleRetentionNeverDeletesAnotherModule(t *testing.T) {
	a, _ := fixture(t)
	for _, node := range []string{"n1", "warp", "wg:work"} {
		_, _ = a.histdb.Exec("INSERT INTO samples(node,t,latency) VALUES(?,?,?)", node, now()-10*86400, 12)
	}
	if e := a.saveLatencyRetention(M{"retention_days": 1, "max_storage_mb": 1}, "xray"); e != nil {
		t.Fatal(e)
	}
	var n int
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM samples WHERE node='n1'").Scan(&n)
	if n != 0 {
		t.Fatal("xray expired sample kept")
	}
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM samples WHERE node IN ('warp','wg:work')").Scan(&n)
	if n != 2 {
		t.Fatal("cross-module deletion")
	}
}
func TestWarpHasNoScoreOrEngineProbeAndLegacyPortsPreserved(t *testing.T) {
	a, _ := fixture(t)
	a.cfg["endpoints"] = []any{M{"id": "warp", "enabled": true}, M{"id": "n1", "enabled": true}}
	if len(a.scores()) != 1 {
		t.Fatal("WARP was scored")
	}
	_ = writeJSON(filepath.Join(a.base, "engine.json"), M{"inbounds": []any{M{"tag": "probe-n1", "listen": "127.0.0.1", "port": 16101}}})
	if a.probePortLocked("n1") != 16101 || a.probePortLocked("warp") != 0 {
		t.Fatal("legacy process probe ports changed")
	}
}
func TestLatencyAcceptsConfiguredHTTP200Targets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	r := latencyWithClient(context.Background(), server.Client(), server.URL)
	if !yes(r["ok"]) || num(r["latency"]) <= 0 {
		t.Fatal(r)
	}
}

func TestTrafficLiveTailModuleIsolationAndNoDoubleCounting(t *testing.T) {
	a, _ := fixture(t)
	for _, node := range []string{"n1", "warp", "wg:home"} {
		a.traffic.rows[node] = &trafficReading{ID: node, At: now() - 1, Available: true, UploadRate: 10, DownloadRate: 20}
		_, _ = a.histdb.Exec("INSERT INTO traffic_samples(node,t,up,down,seconds) VALUES(?,?,?,?,?)", node, now()-120, 100, 200, 10)
	}
	for _, module := range []string{"xray", "warp", "wireguard"} {
		result := a.traffic.history(url.Values{"module": {module}})
		rows := result["items"].([]M)
		if len(rows) != 2 {
			t.Fatalf("%s: %v", module, rows)
		}
		for _, row := range rows {
			if !trafficModuleMatches(module, str(row["id"])) {
				t.Fatal("cross-module live tail", row)
			}
			if yes(row["live_tail"]) && (num(row["upload"]) != 0 || num(row["download"]) != 0) {
				t.Fatal("double counting")
			}
		}
	}
}
func TestTrafficRetentionPoliciesPersistIndependently(t *testing.T) {
	a, _ := fixture(t)
	for _, module := range []string{"xray", "warp", "wireguard"} {
		_, _ = a.histdb.Exec("INSERT INTO traffic_samples(node,t,up,down,seconds) VALUES(?,?,?,?,?)", map[string]string{"xray": "n1", "warp": "warp", "wireguard": "wg:home"}[module], now()-10*86400, 1, 1, 1)
	}
	if e := a.traffic.configureModule("xray", M{"interval": 10, "retention_days": 1, "max_storage_mb": 1}); e != nil {
		t.Fatal(e)
	}
	if num(a.traffic.modulePolicyLocked("warp")["interval"]) != 10 {
		t.Fatal("xray modified WARP sampling")
	}
	var n int
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples WHERE node IN ('warp','wg:home')").Scan(&n)
	if n != 2 {
		t.Fatal("another module history deleted")
	}
}
func TestManualPauseAndExistingFallbackDoNotQueueSwitches(t *testing.T) {
	a, _ := fixture(t)
	a.components["xray"], a.components["warp"] = true, true
	a.cfg["endpoints"] = []any{M{"id": "n1", "enabled": true}, M{"id": "warp", "enabled": true}}
	a.results["n1"] = M{"ok": false, "failures": 10, "time": now(), "latency": -1}
	a.results["warp"] = M{"ok": true, "time": now(), "latency": 100}
	a.active = "warp"
	a.applied = false
	a.cfg["fallback_active"] = false
	a.autoSelect()
	if len(a.jobs.items) != 0 {
		t.Fatal("manual pause reconnected")
	}
	a.cfg["fallback_active"] = true
	a.autoSelect()
	if len(a.jobs.items) != 0 {
		t.Fatal("repeated WARP fallback")
	}
}
func TestWarpProbeOnlyExecutesIsolatedHelper(t *testing.T) {
	a, r := fixture(t)
	_ = a.warpLatency(context.Background())
	if len(r.calls) != 1 {
		t.Fatal(r.calls)
	}
	call := r.calls[0]
	if len(call) != 7 || strings.Join(call[:4], " ") != "ip netns exec foxray-warp" || call[5] != "--warp-probe" || call[6] != "latency" {
		t.Fatal(call)
	}
}

func TestMonitoringRetentionGETForEveryModule(t *testing.T) {
	a, _ := fixture(t)
	for _, module := range []string{"xray", "warp", "wireguard"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1/api/monitoring/retention?module="+module, nil)
		r.RemoteAddr = "127.0.0.1:12"
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "retention_days") {
			t.Fatal(module, w.Code, w.Body.String())
		}
	}
}
