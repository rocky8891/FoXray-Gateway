package gateway

import "testing"

func TestServiceRingPresetsAndOrder(t *testing.T) {
	a, f := fixture(t)
	before, calls := fingerprint(a.settings), len(f.calls)
	p := a.presentationPolicy()
	if !validOrder(p["services_host_order"], []string{"cpu", "load", "memory", "root"}, true) || before != fingerprint(a.settings) {
		t.Fatal("ring defaults must not rewrite settings")
	}
	for _, style := range []string{"metrics", "metrics-left", "radial", "radial-diamond"} {
		if err := a.savePresentation(M{"services_style": style, "services_host_order": []any{"root", "memory", "load", "cpu"}, "services_wireguard_style": "classic"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.savePresentation(M{"content_width_percent": 90}); err != nil {
		t.Fatal(err)
	}
	p = a.presentationPolicy()
	if str(p["services_wireguard_style"]) != "classic" || str(p["services_style"]) != "radial-diamond" || str(arr(p["services_host_order"])[0]) != "root" || len(f.calls) != calls {
		t.Fatal("scoped save changed ring positions or executed a service command")
	}
	for _, bad := range []M{
		{"services_host_order": []any{"cpu", "load", "memory"}},
		{"services_host_order": []any{"cpu", "load", "memory", "memory"}},
		{"services_host_order": []any{"cpu", "load", "memory", "other"}},
		{"services_host_order": "cpu"},
		{"services_host_order": []any{true, "load", "memory", "root"}},
		{"services_style": "other"},
		{"services_wireguard_style": "invalid"},
	} {
		before = fingerprint(a.settings)
		if a.savePresentation(bad) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid ring preference changed settings", bad)
		}
	}
}
