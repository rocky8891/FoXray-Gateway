package gateway

import "testing"

func TestWireguardRingColorAndLegacyNames(t *testing.T) {
	a, runner := fixture(t)
	calls := len(runner.calls)
	for _, color := range []string{"", "#7b8B9c"} {
		if err := a.savePresentation(M{"services_wireguard_ring_color": color}); err != nil {
			t.Fatal(err)
		}
		if got := str(a.presentationPolicy()["services_wireguard_ring_color"]); got != color {
			t.Fatalf("lost ring color: %q", got)
		}
	}
	for _, color := range []any{"red", "#123", "#gg4455", "#123456;", 2} {
		before := fingerprint(a.settings)
		if a.savePresentation(M{"services_wireguard_ring_color": color}) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid color changed settings")
		}
	}
	for _, name := range []string{"center", "above", "classic", "corner", "corner-left"} {
		if err := a.savePresentation(M{"services_wireguard_style": name}); err != nil {
			t.Fatal("legacy appearance blocks unrelated saves", err)
		}
	}
	if err := a.savePresentation(M{"service_custom_themes": []any{M{"id": "custom-ring", "name": "圆环配色", "values": M{"services_wireguard_ring_color": "#abc123", "services_wireguard_style": "above"}}}}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != calls {
		t.Fatal("appearance save executed service commands")
	}
}
