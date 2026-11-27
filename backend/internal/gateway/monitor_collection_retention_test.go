package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMonitorDefaultsAndIndependentPrecedence(t *testing.T) {
	a, f := fixture(t)
	probeInterval := num(a.cfg["interval"])
	if err := a.saveMonitorData("all", "", M{"interval": 5, "retention_days": 20, "max_storage_mb": 8}); err != nil {
		t.Fatal(err)
	}
	if err := a.saveMonitorData("xray", "traffic", M{"interval": 1, "retention_days": 10, "max_storage_mb": 2, "independent": true}); err != nil {
		t.Fatal(err)
	}
	if err := a.saveMonitorData("all", "", M{"interval": 9, "retention_days": 30, "max_storage_mb": 16}); err != nil {
		t.Fatal(err)
	}
	if num(a.monitorDataPolicy("xray", "traffic")["interval"]) != 1 || num(a.monitorDataPolicy("warp", "traffic")["interval"]) != 9 {
		t.Fatal("override priority lost")
	}
	for _, module := range []string{"host", "xray", "warp", "wireguard"} {
		if num(a.monitorDataPolicy(module, "connections")["retention_days"]) != 30 {
			t.Fatal("global connections policy not inherited")
		}
	}
	if num(a.cfg["interval"]) != probeInterval || num(a.icmp["interval"]) != 300 || len(f.calls) > 0 {
		t.Fatal("passive policy changed probes or services")
	}
	if err := a.saveMonitorData("xray", "traffic", M{"independent": false}); err != nil {
		t.Fatal(err)
	}
	if num(a.monitorDataPolicy("xray", "traffic")["interval"]) != 9 {
		t.Fatal("follow-all did not restore inheritance")
	}
	stored := M{}
	if readJSON(filepath.Join(a.base, "monitor-data-policy.json"), &stored) != nil || num(obj(stored["defaults"])["max_storage_mb"]) != 16 {
		t.Fatal(stored)
	}
}
func TestConnectionModuleIntervalUsesCachedSampleUntilDue(t *testing.T) {
	a, f := fixture(t)
	a.components = M{}
	if err := a.saveMonitorData("host", "connections", M{"independent": true, "interval": 30, "retention_days": 1, "max_storage_mb": 1}); err != nil {
		t.Fatal(err)
	}
	line := "ipv4 2 tcp 6 100 ESTABLISHED src=192.0.2.1 dst=198.51.100.2 sport=1234 dport=443"
	f.responses = map[string]string{"conntrack -L -o extended": line}
	a.connections.collect()
	f.calls = nil
	f.responses["conntrack -L -o extended"] = line + "\n" + line
	a.connections.collect()
	if num(obj(a.connections.snapshot("")["summaries"])["host"].(M)["count"]) != 1 || len(f.calls) > 0 {
		t.Fatal("independent interval not respected", f.calls)
	}
	a.connections.sampleAt["host"] -= 31
	a.connections.collect()
	if num(obj(obj(a.connections.snapshot("")["summaries"])["host"])["count"]) != 2 {
		t.Fatal("due sample not refreshed")
	}
}
func TestLogRetentionSeparatesTypesAndAppliesSize(t *testing.T) {
	a, _ := fixture(t)
	for i, action := range []string{"settings.updated", "settings.updated", "probe.latency", "probe.speed", "diagnostic.result", "switch.result"} {
		_, err := a.logdb.Exec("INSERT INTO operations VALUES(?,?,?,?,?,?,?)", intstr(i), now()+float64(i), "info", action, "success", "test", "{}")
		if err != nil {
			t.Fatal(err)
		}
	}
	p := defaultLogRetention()
	p["rows_enabled"], p["max_rows"] = true, 1
	if err := a.saveLogRetention("probes", p); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = a.logdb.QueryRow("SELECT COUNT(*) FROM operations WHERE action LIKE 'probe.%'").Scan(&count)
	if count != 1 {
		t.Fatal("row cap not enforced", count)
	}
	_ = a.logdb.QueryRow("SELECT COUNT(*) FROM operations WHERE action='settings.updated'").Scan(&count)
	if count != 2 {
		t.Fatal("unrelated logs removed")
	}
	p["size_enabled"], p["max_bytes"] = true, 1
	if err := a.saveLogRetention("probes", p); err != nil {
		t.Fatal(err)
	}
	_ = a.logdb.QueryRow("SELECT COUNT(*) FROM operations WHERE action LIKE 'probe.%'").Scan(&count)
	if count != 0 {
		t.Fatal("size window not enforced")
	}
	for _, kind := range []string{"operations", "switches", "diagnostics"} {
		policy := obj(a.logRetentionView()[kind])
		if !yes(policy["days_enabled"]) || yes(policy["rows_enabled"]) || yes(policy["size_enabled"]) || num(policy["retention_days"]) != 30 {
			t.Fatal("wrong defaults", policy)
		}
	}
}
func TestSwitchLogMigrationPreservesOriginalAndIsIdempotent(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "switch-history.json")
	original, _ := json.Marshal([]M{{"time": now(), "source": "a", "target": "b", "status": "success", "method": "manual"}})
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(base, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	a.loadLogRetention()
	if len(a.switchLogs()["items"].([]M)) != 1 {
		t.Fatal("migration duplicated records")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != string(original) {
		t.Fatal("original switch log changed")
	}
	a.Close()
}
