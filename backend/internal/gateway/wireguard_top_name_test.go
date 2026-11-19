package gateway

import "testing"

func TestWireGuardOutsideTopNameIsScopedAndValidated(t *testing.T) {
	a, runner := fixture(t)
	calls := len(runner.calls)
	if err := a.savePresentation(M{"services_wireguard_style": "above", "services_wireguard_layout": "radial"}); err != nil {
		t.Fatal(err)
	}
	if err := a.savePresentation(M{"services_layout": "top"}); err != nil {
		t.Fatal(err)
	}
	p := a.presentationPolicy()
	if str(p["services_wireguard_style"]) != "above" || str(p["services_wireguard_layout"]) != "radial" || len(runner.calls) != calls {
		t.Fatal("layout save lost the outside name style or ran a service command")
	}
	before := fingerprint(a.settings)
	if a.savePresentation(M{"services_wireguard_style": "invalid"}) == nil || before != fingerprint(a.settings) {
		t.Fatal("invalid interface-name style changed settings")
	}
}
