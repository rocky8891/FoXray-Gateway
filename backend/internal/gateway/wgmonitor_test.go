package gateway

import "testing"

func TestWGMonitorDisabledByDefaultAndStrictInputs(t *testing.T) {
	a, r := fixture(t)
	if yes(a.wgMonitorPolicy()["enabled"]) {
		t.Fatal("default probe enabled")
	}
	p := M{"enabled": false, "interval": 60, "count": 3, "size": 56, "timeout": 2, "targets": M{"wg-home": "10.24.255.2"}}
	if err := a.saveWGMonitor(p); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Fatal("policy changed routes")
	}
	p["targets"] = M{"wg-home": "127.0.0.1"}
	if a.saveWGMonitor(p) == nil {
		t.Fatal("loopback target accepted")
	}
}

func TestWGCandidatesAreSpecificPeerHosts(t *testing.T) {
	got := wgProbeCandidates([]string{"10.24.255.1/32", "10.24.255.2/32", "10.0.0.0/24", "0.0.0.0/0", "::/0"}, []string{"10.24.255.1/24"})
	if len(got) != 1 || got[0] != "10.24.255.2" {
		t.Fatal(got)
	}
}
