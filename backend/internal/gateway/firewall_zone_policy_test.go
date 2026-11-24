package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func policyText(masq, inversion, blocks string) string {
	return "target: default\nmasquerade: " + masq + "\nicmp-block-inversion: " + inversion + "\nicmp-blocks: " + blocks + "\n"
}
func policyFixture(t *testing.T) (*App, *fakeRunner) {
	a, f := fixture(t)
	f.responses = map[string]string{
		"firewall-cmd --zone=public --list-all":             policyText("yes", "no", "echo-request"),
		"firewall-cmd --permanent --zone=public --list-all": policyText("no", "yes", "destination-unreachable"),
		"firewall-cmd --get-icmptypes":                      "echo-request echo-reply destination-unreachable time-exceeded",
		"firewall-cmd --get-zones":                          "public work",
		"firewall-cmd --permanent --get-zones":              "public work",
	}
	return a, f
}
func policyRequest() M {
	return M{"kind": "firewall", "operation": "add", "allow_protected": true, "item": M{"kind": "zone-policy", "zone": "public", "masquerade": false, "icmp_inversion": false, "icmp_blocks": []any{"echo-reply"}}}
}
func TestZonePolicyPreviewDiffRollbackAndStaleCheck(t *testing.T) {
	a, f := policyFixture(t)
	p, e := a.network.Preview(policyRequest())
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) < 4 || len(p.steps) != len(p.undo) || len(p.Rollback) != len(p.undo) || len(arr(p.Details["policy_changes"])) != 2 {
		t.Fatal("missing per-scope changes or rollback")
	}
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), "--add-") || strings.Contains(strings.Join(call, " "), "--remove-") {
			t.Fatal("preview mutated firewall")
		}
	}
	for i, s := range p.steps {
		if strings.Contains(strings.Join(s.Args, " "), "reload") {
			t.Fatal("global reload")
		}
		if len(p.undo[i].Args) < 2 {
			t.Fatal("missing undo")
		}
	}
	if p.precondition() != nil {
		t.Fatal("fresh snapshot rejected")
	}
	f.responses["firewall-cmd --zone=public --list-all"] = policyText("no", "no", "")
	if p.precondition() == nil {
		t.Fatal("stale policy accepted")
	}
}
func TestZonePolicyValidationAndManagedMasquerade(t *testing.T) {
	a, _ := policyFixture(t)
	for _, change := range []M{{"icmp_blocks": []any{"unknown-type"}}, {"icmp_blocks": "echo-reply"}, {"target": "ACCEPT"}, {"masquerade": "yes"}, {"icmp_inversion": "no"}} {
		d := policyRequest()
		for k, v := range change {
			obj(d["item"])[k] = v
		}
		if _, e := a.network.Preview(d); e == nil {
			t.Fatal("invalid accepted", change)
		}
	}
	d := policyRequest()
	delete(d, "allow_protected")
	if _, e := a.network.Preview(d); e == nil {
		t.Fatal("ack missing")
	}
	a.network.managed["firewall"] = []any{M{"id": "global-masq", "kind": "masquerade", "zone": "public", "action": "masquerade", "enabled": true}}
	p, e := a.network.Preview(policyRequest())
	if e != nil {
		t.Fatal(e)
	}
	if str(obj(p.persist["item"])["id"]) != "global-masq" || yes(obj(p.persist["item"])["enabled"]) {
		t.Fatal("owned masquerade not reconciled")
	}
}
func TestZonePolicyNoChangesAndUnknownSnapshot(t *testing.T) {
	a, f := policyFixture(t)
	same := policyText("no", "yes", "echo-reply")
	f.responses["firewall-cmd --zone=public --list-all"] = same
	f.responses["firewall-cmd --permanent --zone=public --list-all"] = same
	d := policyRequest()
	obj(d["item"])["icmp_inversion"] = true
	if _, e := a.network.Preview(d); e == nil {
		t.Fatal("unchanged inversion produced mutations")
	}
	f.responses["firewall-cmd --zone=public --list-all"] = "target: default"
	if _, e := a.network.Preview(d); e == nil {
		t.Fatal("incomplete before accepted")
	}
}
func sourceRequest(action, zone, source string) M {
	return M{"kind": "firewall", "operation": "add", "allow_protected": true, "item": M{"kind": "zone-source", "zone": zone, "source": source, "binding_action": action}}
}
func TestZoneSourceMoveKeepsIndependentOriginalScopes(t *testing.T) {
	a, f := policyFixture(t)
	f.responses["firewall-cmd --zone=public --list-sources"] = "192.0.2.9"
	f.responses["firewall-cmd --permanent --zone=work --list-sources"] = "192.0.2.9/32"
	p, e := a.network.Preview(sourceRequest("bind", "work", "192.0.2.9"))
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) != 2 || len(p.undo) != 2 || len(arr(p.Details["source_changes"])) != 1 {
		t.Fatal("unchanged permanent binding not preserved")
	}
	if !strings.Contains(strings.Join(p.undo[0].Args, " "), "--zone=public --add-source=192.0.2.9") {
		t.Fatal("original source spelling not restored")
	}
	f.responses["firewall-cmd --zone=public --list-sources"] = "192.0.2.10"
	if p.precondition() == nil {
		t.Fatal("stale source accepted")
	}
}
func TestZoneSourceUnbindOverlapAndValidation(t *testing.T) {
	a, f := policyFixture(t)
	f.responses["firewall-cmd --zone=public --list-sources"] = "192.0.2.0/24"
	if _, e := a.network.Preview(sourceRequest("bind", "work", "192.0.2.4")); e == nil {
		t.Fatal("overlapping move accepted")
	}
	p, e := a.network.Preview(sourceRequest("unbind", "public", "192.0.2.0/24"))
	if e != nil || len(p.steps) != 1 || !strings.Contains(strings.Join(p.undo[0].Args, " "), "--add-source=192.0.2.0/24") {
		t.Fatal("unbind rollback", e)
	}
	for _, source := range []string{"bad;ip", "ipset:group", "192.0.2.1\n--reload", "::ffff:192.0.2.1"} {
		if _, e := a.network.Preview(sourceRequest("bind", "public", source)); e == nil {
			t.Fatal("invalid source", source)
		}
	}
	d := sourceRequest("bind", "work", "2001:db8::/64")
	delete(d, "allow_protected")
	if _, e := a.network.Preview(d); e == nil {
		t.Fatal("source ack missing")
	}
}

type zoneReadFailure struct{ Runner }

func (r zoneReadFailure) Run(ctx context.Context, args []string, input string) (string, error) {
	if strings.Contains(strings.Join(args, " "), "--list-sources") {
		return "", errors.New("unavailable")
	}
	return r.Runner.Run(ctx, args, input)
}
func TestZoneSourceCannotProceedOnUnknownOriginal(t *testing.T) {
	a, f := policyFixture(t)
	a.runner = zoneReadFailure{f}
	if _, e := a.network.Preview(sourceRequest("bind", "work", "192.0.2.9")); e == nil {
		t.Fatal("unknown original accepted")
	}
}
func TestZoneSourcePartialApplyRestoresOriginalBinding(t *testing.T) {
	a, f := policyFixture(t)
	f.responses["firewall-cmd --zone=public --list-sources"] = "192.0.2.9"
	p, err := a.network.Preview(sourceRequest("bind", "work", "192.0.2.9"))
	if err != nil {
		t.Fatal(err)
	}
	f.fail = "--zone=work --add-source="
	if a.network.Apply(p.ID) == nil {
		t.Fatal("synthetic partial failure not surfaced")
	}
	if p.Status != "rolled_back" {
		t.Fatal("partial source move not rolled back", p.Status)
	}
	last := f.calls[len(f.calls)-1]
	if strings.Join(last, " ") != "firewall-cmd --zone=public --add-source=192.0.2.9" {
		t.Fatal("original source binding not restored last", last)
	}
}
func TestZonePolicyStandaloneRollbackKeepsEachScope(t *testing.T) {
	a, f := policyFixture(t)
	p, err := a.network.Preview(policyRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err = a.network.Apply(p.ID); err != nil {
		t.Fatal(err)
	}
	start := len(f.calls)
	if err = rollbackFile(a.network.specPath(p), f); err != nil {
		t.Fatal(err)
	}
	if len(f.calls)-start != len(p.undo) {
		t.Fatal("rollback did not restore every applied step")
	}
	for i, args := range f.calls[start:] {
		if strings.Join(args, " ") != strings.Join(p.undo[len(p.undo)-1-i].Args, " ") {
			t.Fatal("durable rollback lost scope or ordering", args)
		}
	}
}
func TestStructuredRichRulesRoundTripAndUnsupportedRemainReadonly(t *testing.T) {
	for _, match := range []string{"port", "service", "all"} {
		r, e := normalizeFirewall(M{"kind": "rich", "zone": "public", "family": "ipv4", "source": "192.0.2.0/24", "rich_match": match, "service": "ssh", "port": "443", "protocol": "tcp", "action": "reject", "priority": -5, "logging": true, "log_rate": "5/m"})
		if e != nil {
			t.Fatal(e)
		}
		spec := firewallSpecs(r)[0]
		parsed := parseFirewallEntry("public", spec[0], spec[1])
		if !yes(parsed["editable"]) {
			t.Fatal("supported rich became readonly", spec)
		}
		if compactRule(firewallSpecs(parsed)[0][1]) != compactRule(spec[1]) {
			t.Fatal("rich roundtrip lost conditions")
		}
	}
	for _, raw := range []string{`rule family="ipv4" source NOT address="192.0.2.0/24" drop`, `rule family="ipv4" destination NOT address="198.51.100.1" service name="ssh" accept`, `rule family="ipv4" destination address="198.51.100.1" service name="ssh" accept limit value="1/m"`} {
		if yes(parseFirewallEntry("public", "rich-rule", raw)["editable"]) {
			t.Fatal("unsupported condition editable")
		}
	}
}

func TestFirewallDestinationSurvivesInventoryAndRuleGeneration(t *testing.T) {
	for _, tc := range []struct{ raw, kind, destination string }{
		{`rule family="ipv4" source address="192.0.2.0/24" destination address="198.51.100.3/32" accept`, "ip", "198.51.100.3/32"},
		{`rule family="ipv6" destination address="2001:db8::3" port port="443" protocol="tcp" accept`, "port", "2001:db8::3"},
		{`rule family="ipv4" destination address="198.51.100.1" service name="ssh" accept`, "rich", "198.51.100.1"},
		{`rule family="ipv4" destination address="198.51.100.1" forward-port port="443" protocol="tcp" to-port="8443" to-addr="192.0.2.9"`, "forward", "198.51.100.1"},
		{`rule family="ipv4" destination address="198.51.100.0/24" masquerade`, "masquerade", "198.51.100.0/24"},
	} {
		entry := parseFirewallEntry("public", "rich-rule", tc.raw)
		if !yes(entry["editable"]) || str(entry["kind"]) != tc.kind || str(entry["destination"]) != tc.destination {
			t.Fatalf("wrong inventory classification: %v", entry)
		}
		// Normalization runs again on persisted and edited rules.
		rule, err := normalizeFirewall(entry)
		if err != nil {
			t.Fatal(err)
		}
		spec := firewallSpecs(rule)
		if len(spec) != 1 || spec[0] != [2]string{"rich-rule", tc.raw} {
			t.Fatalf("match changed: %v", spec)
		}
	}
	for _, invalid := range []M{
		{"kind": "ip", "destination": "2001:db8::1", "source": "192.0.2.0/24"},
		{"kind": "ip", "destination": "198.51.100.1", "family": "ipv6"},
		{"kind": "ip", "destination": "not-an-ip"},
		{"kind": "ip", "destination": "198.51.100.1", "driver": "nftables"},
		{"kind": "service-rule", "destination": "198.51.100.1", "service": "ssh"},
	} {
		invalid["zone"] = "public"
		if _, err := normalizeFirewall(invalid); err == nil {
			t.Fatalf("accepted invalid destination: %v", invalid)
		}
	}
}
