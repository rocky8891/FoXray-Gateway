package gateway

import (
	"net"
	"strings"
	"testing"
)

func useTopologyFixture(t *testing.T) {
	t.Helper()
	old := topologyCache
	topologyCache = func() (Topology, error) {
		return Topology{Interface: "ens3", LAN: "172.18.0.0/16", DNS: "172.18.0.53"}, nil
	}
	t.Cleanup(func() { topologyCache = old })
}

func TestTopologyUsesLowestMetricIPv4Default(t *testing.T) {
	raw := "Iface Destination Gateway Flags RefCnt Use Metric Mask\nwg0 00000000 00000000 0000 0 0 1 00000000\nens18 00000000 0100000A 0003 0 0 300 00000000\nens3 00000000 010012AC 0003 0 0 100 00000000\n"
	if got := defaultRouteInterface(raw); got != "ens3" {
		t.Fatalf("route interface = %q", got)
	}
}

func TestPortableLANCaptureAndDNSAgree(t *testing.T) {
	useTopologyFixture(t)
	a, _ := fixture(t)
	c := M{"capture_lan": true, "lan_dns_intercept": true}
	rules, err := a.captureRulesForEngine(c, M{}, "123")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`iifname != "ens3" return`, `ip saddr != 172.18.0.0/16 return`, `iifname "ens3" ip saddr 172.18.0.0/16`} {
		if !strings.Contains(rules, expected) {
			t.Fatalf("missing %s in %s", expected, rules)
		}
	}
	if outboundSocket(true)["interface"] != "ens3" || !lanDNSClientAllowed(net.ParseIP("172.18.30.166")) || lanDNSClientAllowed(net.ParseIP("192.168.1.8")) {
		t.Fatal("topology not propagated")
	}
}
