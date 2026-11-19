package gateway

import (
	"fmt"
	"net/url"
	"testing"
)

func TestProbeIntervalsPreserveOtherSettingsAndCore(t *testing.T) {
	a, f := fixture(t)
	before := clone(a.cfg)
	icmp := clone(a.icmp)
	calls := len(f.calls)
	if err := a.saveProbeIntervals("xray", M{"interval": 15, "icmp_interval": 120}); err != nil {
		t.Fatal(err)
	}
	before["interval"] = 15
	icmp["interval"] = 120
	if fingerprint(a.cfg) != fingerprint(before) || fingerprint(a.icmp) != fingerprint(icmp) || len(f.calls) != calls {
		t.Fatal("probe interval changed unrelated settings or core")
	}
	for _, module := range []string{"warp", "wireguard"} {
		old := a.probeIntervals(module)
		if err := a.saveProbeIntervals(module, M{"interval": 20}); err != nil {
			t.Fatal(err)
		}
		updated := a.probeIntervals(module)
		if updated["enabled"] != old["enabled"] || num(updated["interval"]) != 20 {
			t.Fatal("interval changed detection enable state")
		}
	}
	for _, bad := range []M{{"interval": -1, "icmp_interval": 120}, {"interval": 1.5, "icmp_interval": 120}, {"interval": 10, "icmp_interval": 120, "enabled": true}} {
		if a.saveProbeIntervals("xray", bad) == nil {
			t.Fatal("invalid or overbroad probe update accepted")
		}
	}
	if a.saveProbeIntervals("warp", M{"interval": 1}) == nil {
		t.Fatal("existing service range bypassed")
	}
}

func TestMonitorMasterSwitchesPreserveItemsAndProbes(t *testing.T) {
	a, _ := fixture(t)
	before := fingerprint(a.monitoringItems)
	cfg := fingerprint(a.cfg)
	icmp := fingerprint(a.icmp)
	modules := M{"host": false, "xray": false, "warp": false, "wireguard": false}
	if err := a.saveMonitorModules(modules); err != nil {
		t.Fatal(err)
	}
	for module, items := range monitorItems {
		for _, item := range items {
			if a.monitorEnabled(module, item) {
				t.Fatal("master did not mask item", module, item)
			}
		}
	}
	if fingerprint(a.cfg) != cfg || fingerprint(a.icmp) != icmp {
		t.Fatal("master changed probe strategies")
	}
	restored := clone(a.monitoringItems)
	delete(restored, "_modules")
	if fingerprint(restored) != before {
		t.Fatal("master overwrote individual choices")
	}
	for key := range modules {
		modules[key] = true
	}
	if err := a.saveMonitorModules(modules); err != nil {
		t.Fatal(err)
	}
	if !a.monitorEnabled("xray", "traffic") {
		t.Fatal("master did not restore item")
	}
	if a.saveMonitorModules(M{"xray": false}) == nil {
		t.Fatal("incomplete master accepted")
	}
}

func TestTrafficJitterKeepsRealTimestampsAcrossRefresh(t *testing.T) {
	a, _ := fixture(t)
	if err := a.traffic.saveHistoryPolicy("xray", M{"storage_interval": 1}); err != nil {
		t.Fatal(err)
	}
	base := float64(int64(now())/2*2) - 40
	for i := 0; i < 8; i++ {
		stamp := base + 1.9 + float64(i)*2.1
		if _, err := a.histdb.Exec("INSERT INTO traffic_samples VALUES(?,?,?,?,?)", "jitter", stamp, 10, 20, 2.1); err != nil {
			t.Fatal(err)
		}
	}
	views := [][]M{}
	for _, offset := range []float64{0, 1} {
		q := url.Values{"module": {"xray"}, "node": {"jitter"}, "start": {fmt.Sprint(base - 10 + offset)}, "end": {fmt.Sprint(base + 50 + offset)}}
		views = append(views, a.traffic.history(q)["items"].([]M))
	}
	if len(views[0]) != 8 || len(views[1]) != 8 {
		t.Fatal("jitter samples collapsed")
	}
	for i, row := range views[0] {
		if num(row["time"]) != num(views[1][i]["time"]) {
			t.Fatal("refresh moved historical timestamp")
		}
		if i > 0 && num(row["time"])-num(views[0][i-1]["time"]) > 3.6 {
			t.Fatal("healthy jitter would be drawn as gap")
		}
	}
}

func TestServiceSummaryCardsArePresentationOnly(t *testing.T) {
	a, f := fixture(t)
	cfg := fingerprint(a.cfg)
	calls := len(f.calls)
	if err := a.savePresentation(M{"overview_cards": []any{"service-host", "service-xray", "service-warp", "service-wireguard"}}); err != nil {
		t.Fatal(err)
	}
	if fingerprint(a.cfg) != cfg || len(f.calls) != calls {
		t.Fatal("summary preset changed runtime")
	}
}
