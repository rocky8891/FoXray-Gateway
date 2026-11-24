package gateway

import (
	"net/netip"
	"strings"
	"testing"
)

func TestDNATModesPreserveMatchingAndDestinationPort(t *testing.T) {
	for _, driver := range []string{"firewalld", "nftables", "iptables"} {
		for _, targetPort := range []string{"8443", ""} {
			r, err := normalizeFirewall(M{"kind": "forward", "driver": driver, "zone": "public", "protocol": "tcp", "port": "443", "destination": "192.0.2.1", "target_ip": "198.51.100.2", "target_port": targetPort})
			if err != nil {
				t.Fatal(driver, err)
			}
			switch driver {
			case "firewalld":
				spec := firewallSpecs(r)[0]
				parsed := parseFirewallEntry("public", spec[0], spec[1])
				if !yes(parsed["editable"]) || str(parsed["target_port"]) != targetPort {
					t.Fatal(spec, parsed)
				}
			case "nftables":
				got := nativeNFTScript(r)
				if !strings.Contains(got, "ip daddr 192.0.2.1 tcp dport 443 dnat ip to 198.51.100.2") || strings.Contains(got, "198.51.100.2:") != (targetPort != "") {
					t.Fatal(got)
				}
			case "iptables":
				got := strings.Join(nativeIPSpecs(r)[0], " ")
				if !strings.Contains(got, "-d 192.0.2.1 -p tcp --dport 443 -j DNAT --to-destination 198.51.100.2") || strings.Contains(got, "198.51.100.2:") != (targetPort != "") {
					t.Fatal(got)
				}
			}
		}
		whole, err := normalizeFirewall(M{"kind": "forward", "driver": driver, "zone": "public", "protocol": "all", "destination": "192.0.2.1", "target_ip": "198.51.100.2"})
		if driver == "firewalld" {
			if err == nil {
				t.Fatal("firewalld accepted address-wide DNAT")
			}
			continue
		}
		if err != nil || strings.Contains(nativeNFTScript(whole), "dport") || strings.Contains(strings.Join(nativeIPSpecs(whole)[0], " "), "--dport") {
			t.Fatal(whole, err)
		}
	}
}

func TestStaticFirewallDiagnosisReportsBroadRuleAndRuntimeOnly(t *testing.T) {
	flow := firewallFlow{source: netip.MustParseAddr("192.0.2.12"), destination: netip.MustParseAddr("198.51.100.2"), protocol: "tcp", port: 18880, ingress: "eth0"}
	rows := []any{M{"id": "limited", "source": "192.0.2.0/24", "protocol": "tcp", "port": "18880", "action": "accept", "runtime": true, "permanent": true}, M{"id": "broad", "protocol": "tcp", "port": "1-65535", "action": "accept", "runtime": true, "priority": -1}, M{"id": "permanent-only", "action": "accept", "permanent": true}, M{"id": "dynamic", "editable": false, "runtime": true}}
	candidates, _ := firewallFlowCandidates(rows, flow, "firewalld")
	if len(candidates) != 3 || str(candidates[0]["id"]) != "broad" || str(candidates[0]["overlaps_with"]) != "limited" || !yes(candidates[0]["runtime_only"]) || str(candidates[2]["evidence"]) == "已匹配可解析字段" {
		t.Fatal(candidates)
	}
}

func TestWireGuardIndependentLANForwarding(t *testing.T) {
	enabled, disabled := true, false
	for _, tc := range []struct {
		forward           *bool
		snat, wantForward bool
	}{{nil, true, true}, {&enabled, false, true}, {&disabled, true, false}} {
		network, err := validateWGNetwork(WGNetwork{LANForward: tc.forward, SNAT: tc.snat, LANSources: []string{"192.0.2.0/24"}, LANInterface: "eth0"})
		if err != nil || network.allowsLANForward() != tc.wantForward {
			t.Fatal(network, err)
		}
		profile := &WGProfile{Name: "wg-test", Network: &network}
		steps, err := wgHelperSteps(profile)
		if err != nil || (len(steps) > 0) != tc.snat || !strings.Contains(profile.render(), "PostUp") {
			t.Fatal(steps, err)
		}
	}
}
