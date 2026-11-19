package gateway

import "testing"

func TestOverviewSurfaceSettingsAreValidatedAndIsolated(t *testing.T) {
	a, f := fixture(t)
	calls := len(f.calls)
	style := M{"enabled": true, "color": "#243b53", "opacity": 55, "blur": 12}
	if err := a.savePresentation(M{"overview_surfaces": M{"container": style}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"services_layout": "sidebar"}); err != nil {
		t.Fatal(err)
	}
	if num(obj(obj(a.presentationPolicy()["overview_surfaces"])["container"])["opacity"]) != 55 || len(f.calls) != calls {
		t.Fatal("layout save lost appearance or ran a service command")
	}
	for _, bad := range []any{
		[]any{}, M{"unknown": style}, M{"card": "blue"},
		M{"card": M{"enabled": true, "color": "url(x)", "opacity": 50, "blur": 12}},
		M{"card": M{"enabled": "yes", "color": "#ffffff", "opacity": 50, "blur": 12}},
		M{"card": M{"enabled": true, "color": "#ffffff", "opacity": 101, "blur": 12}},
		M{"card": M{"enabled": true, "color": "#ffffff", "opacity": 50, "blur": -1}},
		M{"card": M{"enabled": true, "color": "#ffffff", "opacity": 50, "blur": 41}},
		M{"card": M{"enabled": true, "color": "#ffffff", "opacity": 50, "blur": 12, "extra": true}},
	} {
		before := fingerprint(a.settings)
		if a.savePresentation(M{"overview_surfaces": bad}) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid appearance accepted or changed settings")
		}
	}
	for _, opacity := range []int{0, 100} {
		if err := a.savePresentation(M{"overview_surfaces": M{"detail": M{"enabled": true, "color": "#00aAbB", "opacity": opacity, "blur": 0}}}); err != nil {
			t.Fatal(err)
		}
	}
}
