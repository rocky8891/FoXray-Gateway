package gateway

import "testing"

func TestWireguardInterfaceCount(t *testing.T) {
	a, r := fixture(t)
	n := len(r.calls)
	for i := 1; i <= 4; i++ {
		if e := a.savePresentation(M{"services_wireguard_count": i, "services_wireguard_interfaces": []any{"qa-a", "qa-b", "qa-c", "qa-d"}[:i]}); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []M{{"services_wireguard_count": 0}, {"services_wireguard_count": 5}, {"services_wireguard_interfaces": []any{"a", "a"}}, {"services_wireguard_interfaces": []any{"a", "b", "c", "d", "e"}}} {
		before := fingerprint(a.settings)
		if a.savePresentation(v) == nil || before != fingerprint(a.settings) {
			t.Fatal("invalid interface selection accepted")
		}
	}
	if len(r.calls) != n {
		t.Fatal("service commands ran")
	}
}
