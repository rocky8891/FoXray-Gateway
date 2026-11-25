package gateway

import (
	"strings"
	"testing"
)

func TestAllZoneInventoryKeepsIdenticalRulesInTheirZones(t *testing.T) {
	a, runner := policyFixture(t)
	zone := func(name, interfaces string) string {
		return name + "\n  target: default\n  interfaces: " + interfaces + "\n  sources:\n  services:\n  ports: 18880/tcp\n  forward-ports:\n  masquerade: no\n  rich rules:\n\n"
	}
	runner.responses["firewall-cmd --list-all-zones"] = zone("public (active)", "eth0") + zone("internal (active)", "eth1 wg-home")
	runner.responses["firewall-cmd --permanent --list-all-zones"] = zone("public", "")
	snapshot := a.network.FirewallSnapshot("*")
	if !yes(snapshot["complete"]) {
		t.Fatalf("incomplete snapshot: %v", snapshot)
	}
	rows := snapshot["entries"].([]M)
	if len(rows) != 2 || rows[0]["id"] != rows[1]["id"] || rows[0]["zone"] != "internal" || rows[1]["zone"] != "public" {
		t.Fatalf("expected the shared external identity in two separate zones: %v", rows)
	}
	if !yes(rows[0]["runtime"]) || yes(rows[0]["permanent"]) || !yes(rows[1]["runtime"]) || !yes(rows[1]["permanent"]) {
		t.Fatalf("incorrect scope: %v", rows)
	}
	if binding := snapshot["zone_bindings"].(map[string]M)["internal"]; !strings.Contains(str(binding["interfaces"]), "wg-home") {
		t.Fatalf("internal binding missing: %v", binding)
	}
	for _, name := range []string{"internal", "public"} {
		runner.responses["firewall-cmd --zone="+name+" --query-port=18880/tcp"] = "yes\n"
	}
	plan, err := a.network.Preview(M{"kind": "firewall", "operation": "adopt", "items": []any{rows[0], rows[1]}})
	if err != nil || len(arr(plan.persist["changes"])) != 2 || len(plan.steps) != 0 {
		t.Fatalf("expected two zone-specific metadata changes, got %v (%v)", plan, err)
	}
	if _, err = a.network.Preview(M{"kind": "firewall", "operation": "adopt", "items": []any{rows[0], rows[0]}}); err == nil {
		t.Fatal("same-zone duplicate was accepted")
	}
	delete(runner.responses, "firewall-cmd --permanent --list-all-zones")
	runner.fail = "--permanent --list-all-zones"
	partial := a.network.FirewallSnapshot("*")
	if yes(partial["complete"]) || len(partial["entries"].([]M)) != 2 {
		t.Fatalf("partial result must retain runtime rows and flag incomplete: %v", partial)
	}
}
