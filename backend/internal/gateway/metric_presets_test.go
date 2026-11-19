package gateway

import "testing"

func TestLeftMetricPresetDoesNotChangeOtherScopes(t *testing.T) {
	a, f := fixture(t)
	if err := a.savePresentation(M{"services_style": "radial", "nodes_layout": "single", "services_wireguard_interfaces": []any{"wg-one"}}); err != nil {
		t.Fatal(err)
	}
	before, calls := fingerprint(a.cfg), len(f.calls)
	if err := a.savePresentation(M{"services_style": "metrics-left", "services_layout": "grid", "services_cards": []any{"service-host", "service-xray", "service-warp", "service-wireguard"}}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if str(p["services_style"]) != "metrics-left" || str(p["nodes_layout"]) != "single" || len(arr(p["services_wireguard_interfaces"])) != 1 || before != fingerprint(a.cfg) || calls != len(f.calls) {
		t.Fatal("preset changed unrelated scope")
	}
}
