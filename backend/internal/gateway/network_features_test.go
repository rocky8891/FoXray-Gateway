package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewInterfacesHaveIndependentKeysAndNoPrivateResponse(t *testing.T) {
	a, _ := fixture(t)
	one, e := a.newWGKey("wg-test-one")
	if e != nil {
		t.Fatal(e)
	}
	two, e := a.newWGKey("wg-test-two")
	if e != nil {
		t.Fatal(e)
	}
	if one["public_key"] == two["public_key"] {
		t.Fatal("interfaces share key")
	}
	secret, e := a.consumeWGKey(str(one["key_id"]), "wg-test-one")
	if e != nil || publicWGKey(secret) != str(one["public_key"]) {
		t.Fatal("invalid keypair")
	}
	encoded, _ := json.Marshal(one)
	if strings.Contains(string(encoded), secret) {
		t.Fatal("private key exposed")
	}
	if _, e = a.consumeWGKey(str(one["key_id"]), "wg-test-two"); e == nil {
		t.Fatal("key not bound to interface")
	}
	three, e := a.newWGKey("wg-test-one")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.consumeWGKey(str(one["key_id"]), "wg-test-one"); e == nil {
		t.Fatal("regeneration kept old key")
	}
	a.mu.Lock()
	v := a.wgKeys[str(three["key_id"])]
	v.Expires = time.Now().Add(-time.Second)
	a.wgKeys[str(three["key_id"])] = v
	a.mu.Unlock()
	if _, e = a.consumeWGKey(str(three["key_id"]), "wg-test-one"); e == nil {
		t.Fatal("expired key accepted")
	}
}
func TestCreateConfigNeverOverwritesConcurrentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.conf")
	if e := atomicNewFile(path, []byte("first"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := atomicNewFile(path, []byte("second"), 0600); e == nil {
		t.Fatal("concurrent file overwritten")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "first" {
		t.Fatal("file changed")
	}
}
func TestRoutingAddressFamiliesAndPolicyMarks(t *testing.T) {
	if _, e := normalizeRoute(M{"dst": "2001:db8::/32", "dev": "test0", "family": 4}); e == nil {
		t.Fatal("mixed family route accepted")
	}
	for _, mark := range []string{"0x100cf", "0x10000/0xffff0000", "0/0"} {
		if _, e := normalizePolicy(M{"priority": 2000, "table": 2000, "fwmark": mark}); e == nil {
			t.Fatal("reserved mark match accepted")
		}
	}
	r, e := normalizePolicy(M{"priority": 2000, "table": 2000, "fwmark": "0x200/0xfff", "uidrange": "1000-1999", "iif": "test0", "invert": true})
	if e != nil {
		t.Fatal(e)
	}
	spec := strings.Join(policySpec(r), " ")
	if !strings.Contains(spec, "not") || !strings.Contains(spec, "uidrange 1000-1999") {
		t.Fatal("advanced selectors missing")
	}
}
func TestDisabledPolicyIsNotInstalled(t *testing.T) {
	a, _ := fixture(t)
	p, e := a.network.Preview(M{"kind": "rules", "operation": "add", "item": M{"family": 4, "priority": 2000, "table": 2000, "enabled": false}})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) != 0 {
		t.Fatal("disabled policy installed")
	}
}
func TestFirewallRichIPv6RoundtripAndRange(t *testing.T) {
	raw := `rule family="ipv6" source address="2001:db8::/32" port port="8000-8010" protocol="tcp" reject`
	row := parseFirewallEntry("public", "rich-rule", raw)
	if !yes(row["editable"]) || row["family"] != "ipv6" {
		t.Fatal("IPv6 rule lost")
	}
	if _, e := portRange("65535-1"); e == nil {
		t.Fatal("invalid range accepted")
	}
}
func TestFirewallProtocolExpansionPreservesExistingTCP(t *testing.T) {
	a, f := fixture(t)
	a.network.managed["firewall"] = []any{M{"id": "owned", "zone": "public", "kind": "port", "port": "8080", "protocol": "tcp", "enabled": true}}
	f.responses = map[string]string{"firewall-cmd --zone=public --query-port=8080/tcp": "yes", "firewall-cmd --zone=public --permanent --query-port=8080/tcp": "yes"}
	p, e := a.network.Preview(M{"kind": "firewall", "operation": "replace", "item": M{"id": "owned", "protocol": "tcpudp"}})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) != 2 {
		t.Fatalf("expected 2 UDP additions; got %d", len(p.steps))
	}
	for _, step := range p.steps {
		if strings.Contains(strings.Join(step.Args, " "), "8080/tcp") {
			t.Fatal("overlapping TCP rule changed")
		}
	}
}

func TestWireGuardNetworkOptionsRoundTripAndScope(t *testing.T) {
	v, e := validateWGNetwork(WGNetwork{Role: "client", RouteMode: "manual", Routes: []string{"198.51.100.0/24"}, SNAT: true, LANSources: []string{"192.0.2.0/24"}, LANInterface: "test0"})
	if e != nil {
		t.Fatal(e)
	}
	p := &WGProfile{Name: "testwg0", Network: &v, Addresses: []string{"10.100.0.1/24"}, Table: "off"}
	parsed, e := parseWG(p.Name, p.render())
	if e != nil || parsed.Network == nil || parsed.Network.RouteMode != "manual" {
		t.Fatal("network options lost")
	}
	steps, e := wgHelperSteps(p)
	if e != nil || len(steps) != 2 {
		t.Fatal("manual routes and SNAT missing")
	}
	if !strings.Contains(steps[1].Input, `oifname "testwg0"`) || !strings.Contains(steps[1].Input, "192.0.2.0/24") {
		t.Fatal("SNAT is not scoped")
	}
	if strings.Contains(steps[1].Input, "flush") {
		t.Fatal("global firewall change")
	}
	if _, e = validateWGNetwork(WGNetwork{SNAT: true, LANSources: []string{"0.0.0.0/0"}, LANInterface: "test0"}); e == nil {
		t.Fatal("global SNAT source accepted")
	}
}
func TestServiceAndPriorityFirewallRules(t *testing.T) {
	service, e := normalizeFirewall(M{"kind": "service-rule", "zone": "public", "service": "ssh"})
	if e != nil || firewallSpecs(service)[0] != [2]string{"service", "ssh"} {
		t.Fatal("service rule invalid")
	}
	r, e := normalizeFirewall(M{"kind": "port", "zone": "public", "port": 8443, "protocol": "tcp", "priority": -10, "logging": true, "log_rate": "5/m", "action": "drop"})
	if e != nil {
		t.Fatal(e)
	}
	spec := firewallSpecs(r)[0]
	if spec[0] != "rich-rule" || !strings.Contains(spec[1], `priority="-10"`) || !strings.Contains(spec[1], `limit value="5/m"`) {
		t.Fatal("priority or log limit missing")
	}
	parsed := parseFirewallEntry("public", "rich-rule", spec[1])
	if !yes(parsed["editable"]) {
		t.Fatal("generated rule cannot roundtrip")
	}
}
func TestVPNInterfaceCannotBeStoppedThroughGenericInterfaceModule(t *testing.T) {
	a, _ := fixture(t)
	for _, name := range []string{"wg-home", "vone0", "CloudflareWARP", "vpm-tun", "wg-zstack"} {
		if _, e := a.network.Preview(M{"kind": "interfaces", "operation": "stop", "item": M{"name": name}, "allow_protected": true}); e == nil {
			t.Fatal("VPN interface generic stop accepted")
		}
	}
}
