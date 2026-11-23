package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFirewallBundleReusesOneClientAndRetainsInventory(t *testing.T) {
	a, f := fixture(t)
	settings := M{"target": "default", "interfaces": []any{"ens16"}, "sources": []any{}, "services": []any{"ssh"}, "ports": []any{[]any{"443", "tcp"}}, "masquerade": true, "icmp_block_inversion": false, "icmp_blocks": []any{}, "forward_ports": []any{[]any{"8080", "tcp", "80", "192.0.2.10"}}, "rules_str": []any{"rule family=ipv4 source address=192.0.2.0/24 accept"}}
	raw, _ := json.Marshal(M{"zones": []any{"public"}, "runtime": settings, "permanent": settings, "icmp_types": []any{"echo-request"}})
	f.responses = map[string]string{strings.Join([]string{"/usr/bin/python3", "-c", firewallBundleScript, "public"}, " "): string(raw)}
	snapshot := a.network.FirewallSnapshot("public")
	if !yes(snapshot["complete"]) || !yes(snapshot["masquerade"]) || len(snapshot["allowed_services"].([]string)) != 1 {
		t.Fatal("bundle lost rule information")
	}
	for _, call := range f.calls {
		if call[0] == "firewall-cmd" {
			t.Fatal("CLI queried despite complete bundle", call)
		}
	}
	kinds := map[string]bool{}
	for _, row := range snapshot["entries"].([]M) {
		kinds[str(row["kind"])] = true
	}
	if !kinds["forward"] || !kinds["port"] || !kinds["service-rule"] {
		t.Fatal("bundle did not preserve inventory", kinds)
	}
}
