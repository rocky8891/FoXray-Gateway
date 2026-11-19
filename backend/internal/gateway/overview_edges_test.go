package gateway

import "testing"

func TestOverviewEdgesAndTextPreserveExistingPreferences(t *testing.T) {
	a, runner := fixture(t)
	calls := len(runner.calls)
	legacy := M{"enabled": true, "color": "#171f2b", "opacity": 80, "blur": 12}
	if err := a.savePresentation(M{"overview_surfaces": M{"container": legacy}}); err != nil {
		t.Fatal(err)
	}
	edge := clone(legacy)
	edge["border_enabled"], edge["border_color"], edge["border_opacity"], edge["shadow"] = true, "#247baf", 50, 16
	if err := a.savePresentation(M{"overview_surfaces": M{"container": legacy, "card": edge}, "overview_text": M{"label_size": 14, "value_size": 24}, "services_wireguard_style": "corner-left"}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"services_layout": "sidebar"}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if num(obj(p["overview_text"])["value_size"]) != 24 || num(obj(obj(p["overview_surfaces"])["card"])["shadow"]) != 16 || str(p["services_wireguard_style"]) != "corner-left" || len(runner.calls) != calls {
		t.Fatal("scoped save lost appearance or ran service commands")
	}
	for _, bad := range []M{
		{"overview_text": M{"label_size": 7}}, {"overview_text": M{"label_size": 25}}, {"overview_text": M{"value_size": 11}}, {"overview_text": M{"value_size": 41}}, {"overview_text": M{"value_size": 22.5}}, {"overview_text": M{"unknown": 1}},
	} {
		before := fingerprint(a.settings)
		if a.savePresentation(bad) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid text changed settings")
		}
	}
	for key, value := range (M{"border_color": "red", "border_opacity": 101, "shadow": -1, "border_enabled": "yes"}) {
		invalid := clone(edge)
		invalid[key] = value
		before := fingerprint(a.settings)
		if a.savePresentation(M{"overview_surfaces": M{"card": invalid}}) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid edge changed settings")
		}
	}
}
