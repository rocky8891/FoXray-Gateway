package gateway

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestHistoryIntervalsAtomicIndependentAndPersistent(t *testing.T) {
	a, _ := fixture(t)
	before := fingerprint(a.monitorDataPolicy("xray", "traffic"))
	probes := fingerprint(a.cfg)
	if err := a.saveHistoryIntervals("xray", M{"traffic": 1, "latency": 120, "connections": 2}); err != nil {
		t.Fatal(err)
	}
	if fingerprint(a.monitorDataPolicy("xray", "traffic")) != before || fingerprint(a.cfg) != probes {
		t.Fatal("history changed collection or probe policy")
	}
	saved := fingerprint(a.monitorData)
	for _, bad := range []M{{"traffic": 1, "latency": 0, "connections": 2}, {"traffic": 1, "latency": 1.5, "connections": 2}, {"traffic": 1, "latency": 1, "extra": 2}} {
		if a.saveHistoryIntervals("xray", bad) == nil || fingerprint(a.monitorData) != saved {
			t.Fatal("invalid batch partially saved")
		}
	}
	a.loadMonitorData()
	if a.historyInterval("xray", "traffic") != 1 || a.historyInterval("xray", "latency") != 120 || a.historyInterval("warp", "traffic") != 60 {
		t.Fatal("intervals not restored or module independence lost")
	}
}

func TestLatencyHistoryThrottlePreservesPerNodeAndProtocol(t *testing.T) {
	a, _ := fixture(t)
	if err := a.saveHistoryInterval("xray", "latency", 120); err != nil {
		t.Fatal(err)
	}
	for _, row := range []M{{"time": 1000., "latency": 100}, {"time": 1060., "latency": 110}, {"time": 1120., "latency": 120}} {
		if err := a.storeMeasurement("n1", false, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.storeMeasurement("n2", false, M{"time": 1060., "latency": 50}); err != nil {
		t.Fatal(err)
	}
	if err := a.storeMeasurement("n1", true, M{"packet_loss_at": 1060., "icmp_average_ms": 5, "icmp_success_pct": 100, "icmp_sent": 3, "icmp_received": 3, "packet_loss_status": "ok"}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.histdb.QueryRow("SELECT COUNT(*) FROM samples WHERE node='n1'").Scan(&count); err != nil || count != 2 {
		t.Fatal("latency throttle did not preserve interval", count, err)
	}
	if err := a.histdb.QueryRow("SELECT COUNT(*) FROM icmp_samples WHERE node='n1'").Scan(&count); err != nil || count != 1 {
		t.Fatal("ICMP history incorrectly shared HTTPS timestamp", count, err)
	}
}

func TestDenseTrafficHistoryUsesActualSamplingCadence(t *testing.T) {
	a, _ := fixture(t)
	if err := a.traffic.saveHistoryPolicy("xray", M{"storage_interval": 1}); err != nil {
		t.Fatal(err)
	}
	base := float64(int64(now())/2*2) - 20
	for i := 0; i < 8; i++ {
		if _, err := a.histdb.Exec("INSERT INTO traffic_samples VALUES(?,?,?,?,?)", "dense-regression", base+float64(i*2), 100, 200, 2); err != nil {
			t.Fatal(err)
		}
	}
	view := a.traffic.history(url.Values{"module": {"xray"}, "node": {"dense-regression"}, "hours": {"0.016666666666666666"}})
	if num(view["storage_interval"]) != 1 || num(view["sampling_interval"]) != 2 || num(view["bucket_seconds"]) != 2 {
		t.Fatal("record interval confused with sampling cadence", view["bucket_seconds"])
	}
	rows := view["items"].([]M)
	if len(rows) != 8 {
		t.Fatal("dense samples discarded", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if num(rows[i]["time"])-num(rows[i-1]["time"]) > num(view["bucket_seconds"])*1.8 {
			t.Fatal("healthy samples would be drawn as isolated gaps")
		}
	}
}

func TestOptionalICMPCardDoesNotEnableProbes(t *testing.T) {
	a, _ := fixture(t)
	before := fingerprint(a.icmp)
	if err := a.savePresentation(M{"overview_cards": []any{"info", "traffic", "https", "icmp"}}); err != nil {
		t.Fatal(err)
	}
	if fingerprint(a.icmp) != before {
		t.Fatal("presentation enabled probes")
	}
	if a.savePresentation(M{"overview_cards": []any{"info", "traffic", "https", "connections", "icmp"}}) == nil {
		t.Fatal("five overview cards accepted")
	}
}

func TestLegacyTrafficIntervalMigratesWithoutStartupWrites(t *testing.T) {
	a, _ := fixture(t)
	if err := writeJSON(filepath.Join(a.base, "traffic-history-policy.json"), M{"xray": 1, "warp": 30}); err != nil {
		t.Fatal(err)
	}
	a.loadMonitorData()
	if a.historyInterval("xray", "traffic") != 1 || a.historyInterval("warp", "traffic") != 30 {
		t.Fatal("legacy intervals lost")
	}
}

func TestAttributedConnectionProtocolCounts(t *testing.T) {
	a, _ := fixture(t)
	m := a.connections
	m.at = now()
	m.summaries = M{"wireguard": M{"available": true, "count": 2, "time": m.at}}
	m.rows = []M{{"node_id": "wg:test", "protocol": "tcp"}, {"node_id": "wg:test", "protocol": "udp"}}
	counts := m.counts()
	node := obj(obj(counts["module:wireguard"]["nodes"])["wg:test"])
	if num(node["count"]) != 2 || num(obj(node["protocols"])["tcp"]) != 1 || num(obj(node["protocols"])["udp"]) != 1 {
		t.Fatal("attributed protocol counts lost")
	}
}

func TestGlobalHistoryIntervalPreservesAndClearsItemOverrides(t *testing.T) {
	a, _ := fixture(t)
	if err := writeJSON(filepath.Join(a.base, "host-monitor-policy.json"), M{"storage_interval": 45}); err != nil {
		t.Fatal(err)
	}
	a.loadMonitorData()
	if a.historyInterval("xray", "latency") != 1 || a.historyInterval("wireguard", "traffic") != 60 || a.historyInterval("host", "metrics") != 45 {
		t.Fatal("existing history intervals were not preserved")
	}
	if err := a.saveHistoryInterval("xray", "latency", 12); err != nil {
		t.Fatal(err)
	}
	defaults := standardMonitorData()
	defaults["storage_interval"] = 5
	if err := a.saveMonitorData("all", "", defaults); err != nil {
		t.Fatal(err)
	}
	if a.historyInterval("xray", "latency") != 12 || a.historyInterval("wireguard", "traffic") != 5 || a.historyInterval("host", "metrics") != 45 {
		t.Fatal("global interval replaced an override or did not apply to inheriting data")
	}
	for _, item := range [][2]string{{"xray", "latency"}, {"host", "metrics"}} {
		if err := a.saveMonitorData(item[0], item[1], M{"independent": false, "history_only": true, "storage_interval": nil}); err != nil {
			t.Fatal(err)
		}
	}
	a.loadMonitorData()
	if a.historyInterval("xray", "latency") != 5 || a.historyInterval("host", "metrics") != 10 {
		t.Fatal("cleared override was restored on reload or host minimum was ignored")
	}
}
