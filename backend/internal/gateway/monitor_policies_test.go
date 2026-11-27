package gateway

import (
	"path/filepath"
	"testing"
)

func TestTrafficRetentionAndSizeKeepNewestAndLifetimeTotals(t *testing.T) {
	a, _ := fixture(t)
	tx, err := a.histdb.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare("INSERT INTO traffic_samples(node,t,up,down,seconds) VALUES(?,?,?,?,?)")
	if err != nil {
		t.Fatal(err)
	}
	stamp := now()
	for i := 0; i < 30000; i++ {
		if _, err = statement.Exec("n-retention-test", stamp-float64(i), 100, 200, 60); err != nil {
			t.Fatal(err)
		}
	}
	statement.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, _ = a.histdb.Exec("INSERT INTO traffic_totals(node,up,down) VALUES('n-retention-test',1234,5678)")
	if err = a.traffic.configure(M{"interval": 60, "retention_days": 1, "max_storage_mb": 1}); err != nil {
		t.Fatal(err)
	}
	var count int
	var latest float64
	_ = a.histdb.QueryRow("SELECT COUNT(*),MAX(t) FROM traffic_samples").Scan(&count, &latest)
	if count >= 30000 || count == 0 || latest != stamp {
		t.Fatal("size retention did not preserve newest data", count, latest)
	}
	if a.traffic.storageBytes > 1024*1024 {
		t.Fatal("retained size exceeds limit", a.traffic.storageBytes)
	}
	var up, down int64
	_ = a.histdb.QueryRow("SELECT up,down FROM traffic_totals WHERE node='n-retention-test'").Scan(&up, &down)
	if up != 1234 || down != 5678 {
		t.Fatal("cleanup reset lifetime totals")
	}
	_, _ = a.histdb.Exec("INSERT INTO traffic_samples(node,t,up,down,seconds) VALUES('old',?,1,1,1)", stamp-3*86400)
	if err = a.traffic.configure(M{"interval": 60, "retention_days": 1, "max_storage_mb": 1}); err != nil {
		t.Fatal(err)
	}
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples WHERE node='old'").Scan(&count)
	if count != 0 {
		t.Fatal("expired history retained")
	}
}

func TestMonitorPoliciesValidateAndDoNotTouchNetwork(t *testing.T) {
	a, runner := fixture(t)
	for _, p := range []M{{"interval": 0, "retention_days": 30, "max_storage_mb": 10}, {"interval": 2, "retention_days": 0, "max_storage_mb": 10}, {"interval": 2, "retention_days": 30, "max_storage_mb": 0}} {
		if a.traffic.configure(p) == nil {
			t.Fatal("invalid traffic policy accepted")
		}
	}
	oldSelected := a.cfg["selected"]
	p := M{"interval": 17, "timeout": 9, "latency_url": "https://example.invalid/204", "retention_days": 7}
	if err := a.configureProbePolicy(p); err != nil {
		t.Fatal(err)
	}
	if a.cfg["selected"] != oldSelected || num(a.settings["measurement_retention_days"]) != 7 {
		t.Fatal("probe policy modified unrelated state")
	}
	if len(runner.calls) != 0 {
		t.Fatal("monitor setting changed network")
	}
	stored := M{}
	if readJSON(filepath.Join(a.base, "manager-settings.json"), &stored) != nil || num(stored["measurement_retention_days"]) != 7 {
		t.Fatal("retention not persisted")
	}
}

func TestTrafficSnapshotUsesCurrentIntervalForEachModule(t *testing.T) {
	a, _ := fixture(t)
	a.traffic.policy["interval"] = 1 // The old in-memory interval must not expire newly reconfigured samples.
	for module, interval := range map[string]int{"xray": 10, "wireguard": 60, "warp": 2} {
		if err := a.saveMonitorData(module, "traffic", M{"independent": true, "interval": interval, "retention_days": 30, "max_storage_mb": 256}); err != nil {
			t.Fatal(err)
		}
	}
	stamp := now()
	for id, age := range map[string]float64{"n": 9, "wg:fresh": 40, "wg:stale": 200, "warp": 10} {
		a.traffic.rows[id] = &trafficReading{ID: id, Available: true, At: stamp - age, UploadRate: 123}
	}
	snapshot := a.traffic.snapshot()
	if got := num(snapshot["interval"]); got != 2 {
		t.Fatalf("snapshot interval = %v, want 2", got)
	}
	wantAvailable := map[string]bool{"n": true, "wg:fresh": true, "wg:stale": false, "warp": false}
	for _, row := range snapshot["items"].([]trafficReading) {
		if row.Available != wantAvailable[row.ID] || (row.UploadRate != 0) != wantAvailable[row.ID] {
			t.Errorf("%s availability = %v, upload = %v", row.ID, row.Available, row.UploadRate)
		}
	}
}

func TestUnifiedICMPValidationPrecedesAnyConfigurationChange(t *testing.T) {
	a, runner := fixture(t)
	original := fingerprint(a.cfg)
	bad := clone(a.icmp)
	bad["count"] = 0
	if a.saveXraySettings(M{"interval": 30, "icmp_policy": bad}) == nil {
		t.Fatal("invalid ICMP accepted")
	}
	if fingerprint(a.cfg) != original || len(runner.calls) != 0 {
		t.Fatal("invalid bundle partially applied")
	}
	good := clone(a.icmp)
	good["size"] = 128
	if err := a.saveXraySettings(M{"icmp_policy": good}); err != nil {
		t.Fatal(err)
	}
	if num(a.icmp["size"]) != 128 || len(runner.calls) != 0 {
		t.Fatal("ICMP-only save restarted network")
	}
}

func TestProbeAndICMPBundleIsValidatedAndSavedTogether(t *testing.T) {
	a, runner := fixture(t)
	original := fingerprint(a.cfg)
	originalSettings := fingerprint(a.settings)
	bad := clone(a.icmp)
	bad["count"] = 0
	p := M{"interval": 60, "timeout": 8, "latency_url": "https://example.invalid/204", "retention_days": 12, "icmp_policy": bad}
	if a.configureProbePolicy(p) == nil || fingerprint(a.cfg) != original || fingerprint(a.settings) != originalSettings {
		t.Fatal("invalid bundle partially applied")
	}
	good := clone(a.icmp)
	good["size"] = 128
	p["icmp_policy"] = good
	if err := a.configureProbePolicy(p); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]float64{"config.json": 60, "manager-settings.json": 12, "icmp-policy.json": 128} {
		stored := M{}
		if err := readJSON(filepath.Join(a.base, name), &stored); err != nil {
			t.Fatal(err)
		}
		key := "interval"
		if name == "manager-settings.json" {
			key = "measurement_retention_days"
		}
		if name == "icmp-policy.json" {
			key = "size"
		}
		if num(stored[key]) != expected {
			t.Fatalf("%s not persisted", name)
		}
	}
	if len(runner.calls) != 0 {
		t.Fatal("detection save touched network")
	}
}

func TestLatencyDetectionSwitchPreservesScheduleAndRoutes(t *testing.T) {
	a, runner := fixture(t)
	p := M{"latency_enabled": false, "interval": 120, "timeout": 9, "latency_url": "https://example.invalid/204", "retention_days": 7}
	if err := a.configureProbePolicy(p); err != nil {
		t.Fatal(err)
	}
	if latencyDetectionEnabled(a.cfg) || num(a.cfg["interval"]) != 120 {
		t.Fatal("disabled switch discarded interval")
	}
	stored := M{}
	if err := readJSON(filepath.Join(a.base, "config.json"), &stored); err != nil || latencyDetectionEnabled(stored) {
		t.Fatal("switch not persisted", err)
	}
	p["latency_enabled"] = true
	if err := a.configureProbePolicy(p); err != nil || !latencyDetectionEnabled(a.cfg) {
		t.Fatal("enable failed", err)
	}
	p["latency_enabled"] = "false"
	if a.configureProbePolicy(p) == nil {
		t.Fatal("string switch accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatal("detection switch touched network")
	}
	if !latencyDetectionEnabled(M{}) {
		t.Fatal("legacy schedules disabled")
	}
}
