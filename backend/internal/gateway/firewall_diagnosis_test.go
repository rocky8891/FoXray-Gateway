package gateway

import (
	"context"
	"net/netip"
	"net/url"
	"testing"
)

func TestFirewallDiagnosisSeparatesLocalServicesAndForwarding(t *testing.T) {
	a, runner := policyFixture(t)
	const flowRoute = "ip -j -4 route get 198.51.100.2 from 192.0.2.12 iif eth0 ipproto tcp dport 9090"
	for _, cmd := range []string{"firewall-cmd --zone=public --list-all", "firewall-cmd --permanent --zone=public --list-all"} {
		runner.responses[cmd] += "ports:\nservices: dns cockpit dhcpv6-client\nforward-ports:\nrich rules:\n"
	}
	runner.responses["firewall-cmd --get-zone-of-interface=eth0"] = "public"
	runner.responses["firewall-cmd --get-active-zones"] = "public\n  interfaces: eth0\nwarp\n  interfaces: CloudflareWARP\n"
	runner.responses["ip -j -4 route get 192.0.2.12"] = `[{"dev":"eth0"}]`
	for service, ports := range map[string]string{"dns": "53/tcp 53/udp", "cockpit": "9090/tcp", "dhcpv6-client": "546/udp"} {
		runner.responses["firewall-cmd --info-service="+service] = "ports: " + ports + "\nprotocols:\nsource-ports:\nincludes:\ndestination:\n"
	}
	query := url.Values{"source": {"192.0.2.12"}, "destination": {"198.51.100.2"}, "ingress": {"eth0"}, "protocol": {"tcp"}, "port": {"9090"}}
	runner.responses[flowRoute] = `[{"type":"local","dev":"lo"}]`
	local, err := a.network.FirewallDiagnosis(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	rules := local["rules"].([]M)
	if local["path_scope"] != "input" || len(rules) != 1 || rules[0]["name"] != "cockpit" || rules[0]["zone"] != "public" || str(rules[0]["id"]) == "" {
		t.Fatalf("wrong local matches: %v", rules)
	}
	runner.responses[flowRoute] = `[{"dev":"CloudflareWARP"}]`
	routed, err := a.network.FirewallDiagnosis(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if routed["path_scope"] != "forward" || routed["egress_zone"] != "warp" || len(routed["rules"].([]M)) != 0 || obj(routed["nat"])["source"] != "192.0.2.12" {
		t.Fatalf("wrong forwarded verdict: %v", routed)
	}
}

func TestFirewallDiagnosisModuleSNATLinksToOriginRows(t *testing.T) {
	const snapshot = `{"nftables":[
		{"chain":{"family":"ip","table":"foxray_wg_test","name":"postrouting","hook":"postrouting","type":"nat","prio":100}},
		{"rule":{"family":"ip","table":"foxray_wg_test","chain":"postrouting","handle":17,"expr":[{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":"wg-test"}},{"masquerade":null}]}},
		{"rule":{"family":"ip","table":"foxray_wg_test","chain":"postrouting","handle":18,"expr":[{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":"wg-test"}},{"masquerade":null}]}}
	]}`
	rows, err := nftablesInventory(snapshot, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	origins := firewallOriginRows(rows)
	flow := firewallFlow{source: netip.MustParseAddr("192.0.2.19")}
	candidates := moduleSNATCandidates(snapshot, flow, "wg-test")
	if len(candidates) != 2 || len(origins) != 2 || candidates[0]["id"] == candidates[1]["id"] {
		t.Fatalf("expected two individually identifiable module rules: %v", candidates)
	}
	for i, row := range candidates {
		if row["id"] != origins[i]["id"] || row["rule_origin"] != "wireguard" || origins[i]["editable"] != false {
			t.Fatalf("diagnosis link does not identify the read-only inventory row: %v vs %v", row, origins[i])
		}
	}
}
