package gateway

import "testing"

func TestWireguardFontSize(t *testing.T) {
	a, r := fixture(t)
	n := len(r.calls)
	if e := a.savePresentation(M{"services_wireguard_label_size": 20, "services_wireguard_value_size": 32}); e != nil {
		t.Fatal(e)
	}
	for _, v := range []M{{"services_wireguard_label_size": 25}, {"services_wireguard_value_size": 41}, {"services_wireguard_value_size": 11}} {
		if a.savePresentation(v) == nil {
			t.Fatal("invalid font accepted")
		}
	}
	if len(r.calls) != n {
		t.Fatal("ran commands")
	}
}
