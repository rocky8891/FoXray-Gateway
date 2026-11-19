package gateway

import "testing"

func TestDefaultOverviewAndThreeIndependentLayoutScopes(t *testing.T) {
	a, f := fixture(t)
	before := fingerprint(a.settings)
	if str(a.presentationPolicy()["default_overview_view"]) != "services" || fingerprint(a.settings) != before {
		t.Fatal("default view must be service overview and read-only")
	}
	cfg, calls := fingerprint(a.cfg), len(f.calls)
	if err := a.savePresentation(M{"overview_layout": "sidebar", "overview_cards": []any{"info", "traffic"}, "services_layout": "top", "services_cards": []any{"service-host", "service-wireguard"}, "range_value": 5, "range_unit": "minute"}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"default_overview_view": "nodes", "nodes_layout": "top", "nodes_order": []any{"qa-b", "qa-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"range_value": 9, "range_unit": "hour"}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if str(p["default_overview_view"]) != "nodes" || str(p["overview_layout"]) != "sidebar" || str(p["services_layout"]) != "top" || str(p["nodes_layout"]) != "top" || len(arr(p["overview_cards"])) != 2 || len(arr(p["services_cards"])) != 2 || len(arr(p["nodes_order"])) != 2 {
		t.Fatal("one scope overwrote another")
	}
	for _, bad := range []M{{"default_overview_view": "unknown"}, {"default_overview_view": true}, {"nodes_layout": "list"}, {"nodes_order": "qa"}, {"nodes_order": []any{"qa", "qa"}}, {"nodes_order": []any{"warp"}}, {"nodes_order": []any{""}}, {"nodes_order": []any{"bad\nvalue"}}, {"nodes_order": []any{1}}} {
		before = fingerprint(a.settings)
		if a.savePresentation(bad) == nil || fingerprint(a.settings) != before {
			t.Fatal("invalid scope changed settings", bad)
		}
	}
	if fingerprint(a.cfg) != cfg || len(f.calls) != calls {
		t.Fatal("layout changed runtime")
	}
}
