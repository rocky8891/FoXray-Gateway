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
	live := policyText("yes", "no", "echo-request") + "services: ssh dns\ninterfaces: eth0\nsources: 192.0.2.0/24\n"
	f.responses["firewall-cmd --zone=public --list-all"] = live
	snapshot := a.network.FirewallSnapshot("public")
	if !yes(obj(snapshot["zone_policy"])["complete"]) || !yes(snapshot["masquerade"]) {
		t.Fatal("policy data not reused")
	}
	if len(snapshot["allowed_services"].([]string)) != 2 {
		t.Fatal("services not reused")
	}
	calls := map[string]int{}
	for _, args := range f.calls {
		calls[strings.Join(args, " ")]++
	}
	for _, command := range []string{"firewall-cmd --zone=public --list-all", "firewall-cmd --permanent --zone=public --list-all", "firewall-cmd --zone=public --list-services", "firewall-cmd --zone=public --query-masquerade"} {
		if calls[command] != 1 {
			t.Fatal("duplicate or missing inventory query", command, calls[command])
		}
	}
	f.fail = "--list-all"
	delete(f.responses, "firewall-cmd --zone=public --list-all")
	delete(f.responses, "firewall-cmd --permanent --zone=public --list-all")
	snapshot = a.network.FirewallSnapshot("public")
	if yes(obj(snapshot["zone_policy"])["complete"]) || snapshot["masquerade"] != nil {
		t.Fatal("failed policy read presented as confirmed")
	}
}
