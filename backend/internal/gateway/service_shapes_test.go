package gateway

import "testing"

func TestServiceShapeRestrictionsAndScopedSaves(t *testing.T) {
	a, runner := fixture(t)
	calls := len(runner.calls)
	for _, style := range []string{"radial", "radial-square", "radial-diamond", "radial-diamond-compact", "radial-wave", "radial-lightning", "radial-mixed"} {
		if err := a.savePresentation(M{"services_host_style": style, "services_xray_style": style, "services_warp_style": style}); err != nil {
			t.Fatal(style, err)
		}
	}
	if err := a.savePresentation(M{"services_layout": "sidebar", "services_host_style": "radial-vertical", "services_xray_style": "radial-vertical", "services_warp_style": "radial-vertical", "services_wireguard_layout": "radial-vertical"}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"content_width_percent": 90}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []M{{"services_layout": "grid"}, {"services_layout": "top"}, {"services_style": "radial-vertical"}, {"services_host_style": "unknown"}} {
		before := fingerprint(a.settings)
		if a.savePresentation(bad) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid shape changed settings", bad)
		}
	}
	patch := M{"services_layout": "grid", "services_host_style": "radial-square", "services_xray_style": "radial-square", "services_warp_style": "radial-square"}
	if err := a.savePresentation(patch); err != nil {
		t.Fatal(err)
	}
	if str(obj(a.settings["presentation"])["services_wireguard_layout"]) != "radial-vertical" {
		t.Fatal("WireGuard vertical style must survive a grid layout change")
	}
	if len(runner.calls) != calls {
		t.Fatal("style save ran service commands")
	}
}
