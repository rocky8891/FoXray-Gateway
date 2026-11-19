package gateway

import "testing"

func TestTrafficChartWindowIsDisplayOnlyAndValidated(t *testing.T) {
	a, f := fixture(t)
	before := fingerprint(a.settings)
	if num(a.displayPolicy()["traffic_chart_window_seconds"]) != 30 || fingerprint(a.settings) != before {
		t.Fatal("default must be 30 and read-only")
	}
	cfg, monitor, history := fingerprint(a.cfg), fingerprint(a.monitorData), fingerprint(a.traffic.policy)
	calls := len(f.calls)
	for _, seconds := range []int{1, 30, 45, 3600} {
		if err := a.saveDisplayPolicy(M{"traffic_chart_window_seconds": seconds}); err != nil {
			t.Fatal(err)
		}
		if num(a.displayPolicy()["traffic_chart_window_seconds"]) != float64(seconds) {
			t.Fatal("window not saved")
		}
	}
	for _, seconds := range []any{0, -1, 3601, 1.5, "bad", nil} {
		before = fingerprint(a.settings)
		if a.saveDisplayPolicy(M{"traffic_chart_window_seconds": seconds}) == nil || fingerprint(a.settings) != before {
			t.Fatal("invalid window changed settings")
		}
	}
	if err := a.saveDisplayPolicy(M{"chart_refresh_seconds": 3}); err != nil {
		t.Fatal(err)
	}
	if num(a.displayPolicy()["traffic_chart_window_seconds"]) != 3600 {
		t.Fatal("old client reset the curve setting")
	}
	if fingerprint(a.cfg) != cfg || fingerprint(a.monitorData) != monitor || fingerprint(a.traffic.policy) != history || len(f.calls) != calls {
		t.Fatal("display setting changed sampling, history or runtime")
	}
}
