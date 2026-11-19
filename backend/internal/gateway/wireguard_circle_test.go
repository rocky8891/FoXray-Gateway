package gateway

import "testing"

func TestWireguardCircleLayout(t *testing.T) {
	a, r := fixture(t)
	n := len(r.calls)
	for _, axis := range []string{"auto", "row", "column"} {
		if e := a.savePresentation(M{"services_wireguard_axis": axis, "services_wireguard_diameter": 200}); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []M{{"services_wireguard_axis": "other"}, {"services_wireguard_diameter": 39}, {"services_wireguard_diameter": 1201}} {
		if a.savePresentation(v) == nil {
			t.Fatal("invalid circle settings accepted")
		}
	}
	if len(r.calls) != n {
		t.Fatal("service commands ran")
	}
}
