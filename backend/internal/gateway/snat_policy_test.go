package gateway

import (
	"strings"
	"testing"
)

func snatRule(driver, action string) M {
	return M{"id": "nat-test", "driver": driver, "zone": "public", "kind": "masquerade", "source": "192.0.2.0/24", "action": action, "to_source": "198.51.100.9", "out_interface": "eth0", "enabled": true}
}
func TestSNATValidationAndLegacyMasquerade(t *testing.T) {
	old, err := normalizeFirewall(M{"kind": "masquerade", "zone": "public", "action": "accept"})
	if err != nil || old["action"] != "masquerade" || firewallSpecs(old)[0][0] != "masquerade" {
		t.Fatal("legacy masquerade changed", old, err)
	}
	for _, fields := range []M{{"to_source": ""}, {"to_source": "2001:db8::1"}, {"to_source": "0.0.0.0"}, {"to_source": "198.51.100.9;bad"}, {"out_interface": "eth0;bad"}, {"source": ""}, {"family": "ipv6"}, {"action": "DROP"}} {
		item := snatRule("firewalld", "snat")
		for k, v := range fields {
			item[k] = v
		}
		if _, err := normalizeFirewall(item); err == nil {
			t.Fatal("invalid SNAT accepted", fields)
		}
	}
}
func TestSNATFirewalldPreviewPairsRuntimePermanentAndUndo(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"firewall-cmd --get-zone-of-interface=eth0": "public"}
	p, err := a.network.Preview(M{"kind": "firewall", "operation": "add", "item": snatRule("firewalld", "snat"), "allow_protected": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 2 || len(p.undo) != 2 {
		t.Fatal("runtime/permanent commands missing")
	}
	for i, step := range p.steps {
		add, undo := strings.Join(step.Args, " "), strings.Join(p.undo[i].Args, " ")
		if !strings.Contains(add, "--direct --add-rule ipv4 nat POSTROUTING 0 -s 192.0.2.0/24 -o eth0 -j SNAT --to-source 198.51.100.9") || !strings.Contains(undo, "--remove-rule") || strings.Contains(add, "--zone") {
			t.Fatal(add, undo)
		}
		if (i == 1) != strings.Contains(add, "--permanent") {
			t.Fatal("permanent mismatch", add)
		}
	}
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), "--add-rule") {
			t.Fatal("preview changed firewall")
		}
	}
	f.responses["firewall-cmd --get-zone-of-interface=eth0"] = "other"
	if _, err = a.network.Preview(M{"kind": "firewall", "operation": "add", "item": snatRule("firewalld", "snat"), "allow_protected": true}); err == nil {
		t.Fatal("zone mismatch accepted")
	}
}
func TestSNATNativeTablesAndReverseSteps(t *testing.T) {
	a, _ := fixture(t)
	for _, driver := range []string{"nftables", "iptables"} {
		for _, action := range []string{"masquerade", "snat"} {
			item, err := normalizeFirewall(snatRule(driver, action))
			if err != nil {
				t.Fatal(err)
			}
			steps, undo, err := a.network.nativeSteps(item, true)
			if err != nil || len(steps) != len(undo) {
				t.Fatal(err)
			}
			if driver == "nftables" {
				script := steps[0].Input
				if !strings.Contains(script, `hook postrouting priority 100`) || !strings.Contains(script, `oifname "eth0"`) || strings.Contains(script, `\"`) || strings.Contains(script, "dport") {
					t.Fatal(script)
				}
				if action == "snat" && !strings.Contains(script, "snat ip to 198.51.100.9") {
					t.Fatal(script)
				}
			} else {
				all := []string{}
				for _, step := range steps {
					all = append(all, strings.Join(step.Args, " "))
				}
				text := strings.Join(all, "\n")
				if !strings.Contains(text, "-t nat") || !strings.Contains(text, "-I POSTROUTING") || strings.Contains(text, "--dport") || !strings.Contains(text, "-o eth0 -j "+strings.ToUpper(action)) {
					t.Fatal(text)
				}
				if action == "snat" && !strings.Contains(text, "--to-source 198.51.100.9") {
					t.Fatal(text)
				}
			}
			removed, restore, err := a.network.nativeSteps(item, false)
			if err != nil || len(removed) != len(restore) || len(removed) != len(steps) {
				t.Fatal("delete/restore not paired")
			}
		}
	}
}
func TestDisplayOneSecondLimitsAndNamedErrors(t *testing.T) {
	a, f := fixture(t)
	if err := a.saveDisplayPolicy(M{"status_refresh_seconds": 1, "live_refresh_seconds": 1, "chart_refresh_seconds": 1}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status_refresh_seconds", "live_refresh_seconds", "chart_refresh_seconds"} {
		before := fingerprint(a.settings)
		for _, value := range []float64{0, 3601, 1.5} {
			if err := a.saveDisplayPolicy(M{key: value}); err == nil || !strings.Contains(err.Error(), "1～3600") || !strings.Contains(err.Error(), "间隔") || fingerprint(a.settings) != before {
				t.Fatal("unclear validation or partial save", key, err)
			}
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("display settings ran a system command")
	}
}
