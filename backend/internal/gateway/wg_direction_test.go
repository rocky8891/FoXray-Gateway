package gateway

import (
	"strings"
	"testing"
)

func TestWGDirectionRoleDoesNotChangeFlows(t *testing.T) {
	for _, role := range []string{"server", "client"} {
		network, err := validateWGNetwork(WGNetwork{Role: role, LANInterface: "eth0", LANToVPN: &WGDirection{Forward: true, SNAT: true, Sources: []string{"192.0.2.0/24"}}, VPNToLAN: &WGDirection{Forward: true, SNAT: true, Sources: []string{"10.24.1.2/32"}, Targets: []string{"192.0.2.0/24"}}})
		if err != nil {
			t.Fatal(err)
		}
		p := &WGProfile{Name: "wg-test", Network: &network, Peers: []WGPeer{{AllowedIPs: []string{"172.16.0.0/12"}}}}
		flows, err := wgDirectionFlows(p)
		if err != nil || len(flows) != 2 {
			t.Fatal(flows, err)
		}
		if flows[0].Ingress != "eth0" || flows[0].Egress != "wg-test" || flows[1].Ingress != "wg-test" || flows[1].Egress != "eth0" {
			t.Fatal("direction still depends on role", flows)
		}
		steps, err := wgHelperSteps(p)
		if err != nil || len(steps) != 1 {
			t.Fatal(steps, err)
		}
		for _, text := range []string{`oifname "wg-test"`, `oifname "eth0"`, `ip daddr { 172.16.0.0/12 }`, `ip daddr { 192.0.2.0/24 }`} {
			if !strings.Contains(steps[0].Input, text) {
				t.Fatal("missing SNAT scope", text)
			}
		}
		if strings.Count(steps[0].Input, "hook postrouting") != 1 || strings.Count(steps[0].Input, "masquerade;") != 2 {
			t.Fatal("duplicate hook or missing rule")
		}
		parsed, err := parseWG(p.Name, p.render())
		if err != nil || parsed.Network == nil || parsed.Network.LANToVPN == nil || !parsed.Network.VPNToLAN.SNAT {
			t.Fatal("direction roundtrip failed", err)
		}
	}
}
func TestWGDirectionLegacyAndExplicitOff(t *testing.T) {
	enabled := true
	for _, role := range []string{"server", "client"} {
		old := WGNetwork{Role: role, LANForward: &enabled, SNAT: true, LANSources: []string{"192.0.2.0/24"}, LANInterface: "eth0", AdvertisedRoutes: []string{"198.51.100.0/24"}}
		p := &WGProfile{Name: "wg-test", Network: &old, Peers: []WGPeer{{AllowedIPs: []string{"172.16.0.0/12"}}}}
		flows, err := wgDirectionFlows(p)
		if err != nil || len(flows) != 1 || !flows[0].Forward || !flows[0].SNAT {
			t.Fatal("legacy changed", err)
		}
		if (flows[0].Ingress == "wg-test") != (role == "server") {
			t.Fatal("legacy direction changed")
		}
		old.LANToVPN = &WGDirection{}
		old.VPNToLAN = &WGDirection{}
		if old.hasSNAT() || old.allowsLANForward() {
			t.Fatal("legacy flags overrode explicit off")
		}
		steps, err := wgHelperSteps(p)
		if err != nil || len(steps) != 0 {
			t.Fatal("explicit off emitted rules", err)
		}
	}
}
func TestWGDirectionValidationAndForwardWithoutSNAT(t *testing.T) {
	for _, prefix := range []string{"invalid", "0.0.0.0/0", "::/0", "::ffff:192.0.2.0/120"} {
		_, err := validateWGNetwork(WGNetwork{LANInterface: "eth0", LANToVPN: &WGDirection{Forward: true, Sources: []string{prefix}}})
		if err == nil {
			t.Fatal("invalid source accepted", prefix)
		}
	}
	network, err := validateWGNetwork(WGNetwork{Role: "server", LANInterface: "eth0", LANToVPN: &WGDirection{Forward: true, Sources: []string{"192.0.2.0/24"}}})
	if err != nil {
		t.Fatal(err)
	}
	p := &WGProfile{Name: "wg-test", Network: &network, Peers: []WGPeer{{AllowedIPs: []string{"172.16.0.0/12"}}}}
	steps, err := wgHelperSteps(p)
	if err != nil || len(steps) != 0 || network.hasSNAT() || !network.allowsLANForward() {
		t.Fatal("forward implied SNAT", err)
	}
	p.Peers = nil
	if _, err := wgDirectionFlows(p); err == nil {
		t.Fatal("empty effective targets accepted")
	}
}

func TestWGDirectionDisabledPeersDoNotInstallAutomaticRules(t *testing.T) {
	network := WGNetwork{Role: "server", LANInterface: "eth0", LANToVPN: &WGDirection{Forward: true, SNAT: true, Sources: []string{"192.0.2.0/24"}}}
	p := &WGProfile{Name: "wg-test", Network: &network, Peers: []WGPeer{{Disabled: true, AllowedIPs: []string{"172.16.0.0/12"}}}}
	flows, err := wgDirectionFlows(p)
	if err != nil || len(flows) != 0 {
		t.Fatal("disabled Peer retained auto destination", err)
	}
	steps, err := wgHelperSteps(p)
	if err != nil || len(steps) != 0 {
		t.Fatal("disabled Peer prevented interface startup", err)
	}
}
