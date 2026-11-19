package gateway

import "testing"

func TestWireguardRingEffects(t *testing.T) {
	a, r := fixture(t)
	n := len(r.calls)
	for _, v := range []int{0, 50, 100} {
		if e := a.savePresentation(M{"services_wireguard_ring_opacity": v, "services_wireguard_ring_shadow": 20}); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []M{{"services_wireguard_ring_opacity": 101}, {"services_wireguard_ring_shadow": 41}} {
		if a.savePresentation(v) == nil {
			t.Fatal("accepted invalid ring effect")
		}
	}
	if len(r.calls) != n {
		t.Fatal("ran service commands")
	}
}
