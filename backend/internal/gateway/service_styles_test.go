package gateway

import "testing"

func TestIndependentServiceStylesAndOrders(t *testing.T) {
	a, f := fixture(t)
	calls := len(f.calls)
	if err := a.savePresentation(M{"services_style": "metrics-left", "services_host_style": "radial-diamond", "services_xray_style": "radial", "services_warp_style": "metrics", "services_wireguard_layout": "radial", "services_wireguard_style": "corner", "services_xray_order": []any{"connections", "latency", "download", "upload"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"services_warp_style": "radial-diamond"}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if str(p["services_host_style"]) != "radial-diamond" || str(p["services_xray_style"]) != "radial" || str(p["services_style"]) != "metrics-left" || str(p["services_wireguard_style"]) != "corner" || str(arr(p["services_xray_order"])[0]) != "connections" || len(f.calls) != calls {
		t.Fatal("independent card save modified other scope or ran service commands")
	}
	for _, bad := range []M{{"services_host_style": true}, {"services_xray_style": "other"}, {"services_wireguard_layout": "other"}, {"services_wireguard_style": "other"}, {"services_warp_order": []any{"upload", "download", "latency", "latency"}}, {"services_xray_order": []any{"upload", "download", "latency"}}} {
		before := fingerprint(a.settings)
		if a.savePresentation(bad) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid settings wrote preferences", bad)
		}
	}
}
