package gateway

import "testing"

func TestDefaultServiceTheme(t *testing.T) {
	p := defaultPresentation()
	for key, want := range map[string]string{
		"default_overview_view": "services", "services_layout": "top",
		"services_host_style": "radial", "services_xray_style": "metrics",
		"services_warp_style": "radial-diamond-compact", "services_wireguard_layout": "metrics",
	} {
		if p[key] != want {
			t.Fatalf("%s: got %v, want %s", key, p[key], want)
		}
	}
	if err := validatePresentation(p); err != nil {
		t.Fatal(err)
	}
	a, _ := fixture(t)
	if err := a.savePresentation(M{"services_layout": "grid", "services_host_style": "metrics", "default_overview_view": "dashboard"}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"clock_source": "server"}); err != nil {
		t.Fatal(err)
	}
	saved := a.presentationPolicy()
	if saved["services_layout"] != "grid" || saved["services_host_style"] != "metrics" || saved["default_overview_view"] != "dashboard" {
		t.Fatal("saved preferences changed by new defaults")
	}
}
