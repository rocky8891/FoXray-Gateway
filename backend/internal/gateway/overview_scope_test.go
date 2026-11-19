package gateway

import "testing"

func TestOverviewScopesPreserveEachOtherAndRuntime(t *testing.T) {
	a, f := fixture(t)
	config := fingerprint(a.cfg)
	calls := len(f.calls)
	if err := a.savePresentation(M{"overview_cards": []any{"info", "traffic"}, "overview_layout": "sidebar", "range_value": 5, "range_unit": "minute"}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"services_cards": []any{"service-host", "service-wireguard"}, "services_layout": "top"}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if str(p["overview_layout"]) != "sidebar" || len(arr(p["overview_cards"])) != 2 || num(p["range_value"]) != 5 {
		t.Fatal("service save overwrote current exit or global range")
	}
	if err := a.savePresentation(M{"overview_layout": "grid"}); err != nil {
		t.Fatal(err)
	}
	p = a.presentationPolicy()
	if str(p["services_layout"]) != "top" || len(arr(p["services_cards"])) != 2 {
		t.Fatal("current exit save overwrote services")
	}
	before := fingerprint(a.settings)
	if a.savePresentation(M{"services_cards": []any{"info"}}) == nil || fingerprint(a.settings) != before {
		t.Fatal("invalid service scope changed settings")
	}
	if fingerprint(a.cfg) != config || len(f.calls) != calls {
		t.Fatal("presentation changed runtime")
	}
}
func TestLegacyServicePresetSeedsIndependentViewWithoutWriting(t *testing.T) {
	a, _ := fixture(t)
	a.settings["presentation"] = M{"overview_cards": []any{"service-warp", "service-host"}, "overview_layout": "sidebar"}
	before := fingerprint(a.settings)
	p := a.presentationPolicy()
	if str(p["services_layout"]) != "sidebar" || len(arr(p["services_cards"])) != 2 || fingerprint(a.settings) != before {
		t.Fatal("legacy preset not preserved read-only")
	}
	if err := a.savePresentation(M{"overview_cards": []any{"info", "traffic"}, "overview_layout": "grid"}); err != nil {
		t.Fatal(err)
	}
	p = a.presentationPolicy()
	if str(p["services_layout"]) != "sidebar" || len(arr(p["services_cards"])) != 2 {
		t.Fatal("editing current exit discarded legacy service choices")
	}
}
