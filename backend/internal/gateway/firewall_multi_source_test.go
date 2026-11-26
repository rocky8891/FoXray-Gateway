package gateway

import (
	"net/netip"
	"strings"
	"testing"
)

func TestNFTSNATMultipleSourceNetworks(t *testing.T) {
	rule := M{"id": "fixture", "driver": "nftables", "kind": "masquerade", "zone": "host", "source": "192.0.2.10/24, 198.51.100.0/24, 192.0.2.3", "out_interface": "eth0"}
	normalized, err := normalizeFirewall(rule)
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeNFTScript(normalized); !strings.Contains(got, `ip saddr { 192.0.2.0/24, 198.51.100.0/24 } oifname "eth0" counter masquerade`) {
		t.Fatalf("unexpected nftables rule: %s", got)
	}
	for _, sample := range []struct {
		address string
		match   bool
	}{{"192.0.2.5", true}, {"198.51.100.5", true}, {"203.0.113.5", false}} {
		if firewallAddressMatch(str(normalized["source"]), netip.MustParseAddr(sample.address)) != sample.match {
			t.Errorf("unexpected match for %s", sample.address)
		}
	}
	for _, invalid := range []string{"192.0.2.0/24,", "192.0.2.0/24,2001:db8::/32"} {
		bad := clone(rule)
		bad["source"] = invalid
		if _, err := normalizeFirewall(bad); err == nil {
			t.Errorf("accepted invalid source list %q", invalid)
		}
	}
	rule["kind"] = "ip"
	if _, err := normalizeFirewall(rule); err == nil {
		t.Fatal("unexpected list support in non-SNAT rule")
	}
}

func TestSNATMultiSourceBackendsAndRollback(t *testing.T) {
	for _, driver := range []string{"nftables", "iptables", "firewalld"} {
		for _, action := range []string{"masquerade", "snat"} {
			t.Run(driver+"/"+action, func(t *testing.T) {
				a, f := fixture(t)
				f.responses = map[string]string{"firewall-cmd --get-zone-of-interface=eth0": "public"}
				raw := snatRule(driver, action)
				raw["source"] = "192.0.2.0/24,198.51.100.0/24"
				rule, err := normalizeFirewall(raw)
				if err != nil {
					t.Fatal(err)
				}
				if driver == "firewalld" {
					p, err := a.network.Preview(M{"kind": "firewall", "operation": "add", "item": rule, "allow_protected": true})
					if err != nil {
						t.Fatal(err)
					}
					if len(p.steps) != 4 || len(p.undo) != 4 {
						t.Fatal("both networks need runtime/permanent operations and rollback", len(p.steps))
					}
					for i, step := range p.steps {
						if strings.Contains(strings.Join(step.Args, " "), ",") {
							t.Fatal("unsplit firewalld source")
						}
						if len(p.undo[i].Args) == 0 {
							t.Fatal("missing rollback")
						}
					}
					// Both source rules remain one editable row; incomplete state is explicit.
					entries := map[string]M{}
					liveDirect, savedDirect := map[string]bool{}, map[string]bool{}
					for i, spec := range firewallSpecs(rule) {
						if spec[0] == "direct-rule" {
							liveDirect[compactRule(spec[1])] = true
							savedDirect[compactRule(spec[1])] = i == 0
						} else {
							entries[spec[0]+"|"+compactRule(spec[1])] = M{"runtime": true, "permanent": i == 0}
						}
					}
					mergeManagedZoneRows(entries, "public", []any{rule}, liveDirect, savedDirect, true, true)
					merged := entries["managed|"+str(rule["id"])]
					if len(entries) != 1 || !yes(merged["runtime"]) || !yes(merged["partial_permanent"]) || !yes(merged["drift"]) {
						t.Fatal("source group lost partial state", entries)
					}
				} else {
					steps, undo, err := a.network.nativeSteps(rule, true)
					if err != nil || len(steps) != len(undo) {
						t.Fatal("invalid add/rollback pair", err)
					}
					if driver == "iptables" {
						if specs := nativeIPSpecs(rule); len(specs) != 2 || strings.Join(specs[0], " ") == strings.Join(specs[1], " ") {
							t.Fatal("expected independent source matches", specs)
						}
						for _, step := range steps {
							if strings.Contains(strings.Join(step.Args, " "), ",") {
								t.Fatal("unsplit iptables source")
							}
						}
					}
					removed, restore, err := a.network.nativeSteps(rule, false)
					if err != nil || len(removed) != len(steps) || len(restore) != len(undo) {
						t.Fatal("delete/restore lost sources", err)
					}
				}
				for _, call := range f.calls {
					text := strings.Join(call, " ")
					if strings.Contains(text, "--add-") || strings.Contains(text, " -A ") {
						t.Fatal("preview mutated firewall", text)
					}
				}
			})
		}
	}
}
