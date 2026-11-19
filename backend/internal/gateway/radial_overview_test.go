package gateway

import "testing"

func TestRadialOverviewAndSingleNodesAreIndependent(t *testing.T) {
	a, f := fixture(t)
	before, calls := fingerprint(a.settings), len(f.calls)
	if str(a.presentationPolicy()["services_style"]) != "metrics" || before != fingerprint(a.settings) {
		t.Fatal("style default writes preferences")
	}
	if err := a.savePresentation(M{"services_style": "radial", "nodes_layout": "single", "services_wireguard_interfaces": []any{"wg-b", "wg-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"services_layout": "sidebar"}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if str(p["services_style"]) != "radial" || str(p["nodes_layout"]) != "single" || str(p["overview_layout"]) != "grid" || len(arr(p["services_wireguard_interfaces"])) != 2 {
		t.Fatal("layout scope overwritten")
	}
	before = fingerprint(a.settings)
	for _, bad := range []M{{"services_style": "unknown"}, {"services_style": true}, {"services_wireguard_interfaces": []any{"a", "a"}}, {"services_wireguard_interfaces": []any{"a", "b", "c", "d", "e"}}, {"services_wireguard_interfaces": "bad"}, {"nodes_layout": "invalid"}} {
		if a.savePresentation(bad) == nil || fingerprint(a.settings) != before {
			t.Fatal("invalid style changed preferences")
		}
	}
	if len(f.calls) != calls {
		t.Fatal("visual setting changed services")
	}
}
