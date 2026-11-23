package gateway

import (
	"strings"
	"testing"
)

func TestFirewallServiceDoesNotReadRules(t *testing.T) {
	a, f := fixture(t)
	state := a.network.FirewallServiceState("firewalld")
	if str(obj(state["firewalld"])["state"]) != "active" {
		t.Fatal("missing service state")
	}
	for _, args := range f.calls {
		if args[0] != "systemctl" {
			t.Fatal("service status read rule inventory")
		}
	}
}
func TestFirewallSnapshotReusesZoneQueries(t *testing.T) {
	a, f := policyFixture(t)
	zoneFields := "services: ssh dns\nports: 18880/tcp\nforward-ports: \ninterfaces: eth0\nsources: 192.0.2.0/24\nrich rules:\n"
	live := policyText("yes", "no", "echo-request") + zoneFields + `    rule family="ipv4" source address="192.0.2.0/24" masquerade` + "\n"
	f.responses["firewall-cmd --permanent --zone=public --list-all"] += zoneFields
	f.responses["firewall-cmd --zone=public --list-all"] = live
	snapshot := a.network.FirewallSnapshot("public")
	if !yes(snapshot["complete"]) || !yes(obj(snapshot["zone_policy"])["complete"]) || !yes(snapshot["masquerade"]) {
		t.Fatal("policy data not reused")
	}
	if len(snapshot["allowed_services"].([]string)) != 2 {
		t.Fatal("services not reused")
	}
	calls := map[string]int{}
	for _, args := range f.calls {
		calls[strings.Join(args, " ")]++
	}
	for _, command := range []string{"firewall-cmd --zone=public --list-all", "firewall-cmd --permanent --zone=public --list-all", "firewall-cmd --get-icmptypes"} {
		if calls[command] != 1 {
			t.Fatal("duplicate or missing inventory query", command, calls[command])
		}
	}
	if len(calls) != 6 {
		t.Fatalf("unexpected extra queries: %v", calls)
	}
	matches := 0
	for _, row := range snapshot["entries"].([]M) {
		if str(row["kind"]) == "masquerade" && yes(row["runtime"]) && !yes(row["permanent"]) {
			matches++
		}
	}
	if matches != 2 {
		t.Fatalf("expected zone and rich-rule masquerade entries, got %d", matches)
	}
	f.fail = "--list-all"
	delete(f.responses, "firewall-cmd --zone=public --list-all")
	delete(f.responses, "firewall-cmd --permanent --zone=public --list-all")
	snapshot = a.network.FirewallSnapshot("public")
	if yes(obj(snapshot["zone_policy"])["complete"]) || snapshot["masquerade"] != nil {
		t.Fatal("failed policy read presented as confirmed")
	}
}

func TestFirewallInventoryReadsManagedSNATByScope(t *testing.T) {
	a, runner := policyFixture(t)
	a.network.managed["firewall"] = []any{snatRule("firewalld", "snat")}
	for _, command := range []string{"firewall-cmd --zone=public --list-all", "firewall-cmd --permanent --zone=public --list-all"} {
		runner.responses[command] += "ports:\nservices:\nforward-ports:\nrich rules:\n"
	}
	runner.responses["firewall-cmd --direct --get-all-rules"] = "ipv4 nat POSTROUTING 0 -s 192.0.2.0/24 -o eth0 -j SNAT --to-source 198.51.100.9\n"
	runner.responses["firewall-cmd --permanent --direct --get-all-rules"] = ""
	snapshot := a.network.FirewallSnapshot("public")
	for _, entry := range snapshot["entries"].([]M) {
		if entry["id"] == "nat-test" {
			if !yes(snapshot["complete"]) || !yes(entry["runtime"]) || yes(entry["permanent"]) || !yes(entry["drift"]) {
				t.Fatalf("incorrect SNAT scope: %v", entry)
			}
			return
		}
	}
	t.Fatal("managed SNAT missing from inventory")
}
