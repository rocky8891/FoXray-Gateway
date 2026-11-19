package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

const testInterfaceAddressJSON = `[{"ifname":"eth0","ifindex":2,"flags":["UP"],"addr_info":[{"local":"192.0.2.10","prefixlen":24,"scope":"global","broadcast":"192.0.2.255"},{"local":"2001:db8::10","prefixlen":64,"scope":"global"}]}]`

func addressFixture(t *testing.T) (*App, *fakeRunner) {
	a, f := fixture(t)
	f.responses = map[string]string{"ip -j -d link show": `[{"ifname":"eth0","ifindex":2}]`, "ip -j -d address show dev eth0": testInterfaceAddressJSON}
	return a, f
}
func TestInterfaceAddressMaskParsing(t *testing.T) {
	for _, row := range []struct {
		ip   string
		mask any
		want string
	}{{"192.0.2.10", "255.255.255.0", "192.0.2.10/24"}, {"192.0.2.10", 24, "192.0.2.10/24"}, {"2001:db8::10", "64", "2001:db8::10/64"}} {
		got, _, _, err := interfacePrefix(row.ip, row.mask)
		if err != nil || got != row.want {
			t.Fatal(got, err)
		}
	}
	for _, row := range []struct {
		ip   string
		mask any
	}{{"192.0.2.10", "255.0.255.0"}, {"192.0.2.10", 33}, {"2001:db8::10", 129}, {"2001:db8::10", "255.255.255.0"}, {"127.0.0.1", 8}, {"255.255.255.255", 32}, {"bad", 24}} {
		if _, _, _, err := interfacePrefix(row.ip, row.mask); err == nil {
			t.Fatal("invalid prefix accepted", row)
		}
	}
}
func TestInterfaceAddressPlansPreserveOtherFamiliesAndRollback(t *testing.T) {
	a, f := addressFixture(t)
	p, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": M{"name": "eth0", "original_address": "192.0.2.10/24", "address": "192.0.2.20", "prefix": "255.255.255.0", "gateway_action": "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 2 || len(p.undo) != 2 {
		t.Fatal("unexpected address steps", p.steps)
	}
	if strings.Join(p.steps[0].Args, " ") != "ip address del 192.0.2.10/24 dev eth0" || strings.Join(p.steps[1].Args, " ") != "ip address add 192.0.2.20/24 dev eth0 broadcast +" {
		t.Fatal(p.steps)
	}
	for _, step := range p.steps {
		raw := strings.Join(step.Args, " ")
		if strings.Contains(raw, "flush") || strings.Contains(raw, "route") || strings.Contains(raw, "2001:db8") {
			t.Fatal("unrelated data changed")
		}
	}
	for _, call := range f.calls {
		if call[0] == "ip" && !strings.Contains(strings.Join(call, " "), "-j") {
			t.Fatal("preview mutated network")
		}
	}
	f.fail = "ip address add 192.0.2.20/24"
	if a.network.Apply(p.ID) == nil {
		t.Fatal("expected synthetic failure")
	}
	restored := false
	for _, call := range f.calls {
		if strings.HasPrefix(strings.Join(call, " "), "ip address add 192.0.2.10/24 dev eth0") {
			restored = true
		}
	}
	if !restored {
		t.Fatal("old address not restored after failed add")
	}
}
func TestNewInterfaceAddressAndGatewayReversibleSteps(t *testing.T) {
	a, _ := addressFixture(t)
	p, err := a.network.Preview(M{"kind": "interfaces", "operation": "add", "item": M{"name": "test0", "type": "dummy", "address": "198.51.100.10", "prefix": 24, "activate": true, "gateway": "198.51.100.1", "gateway_metric": 601}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 4 || len(p.undo) != 4 {
		t.Fatal(p.steps, p.undo)
	}
	if !strings.Contains(strings.Join(p.steps[3].Args, " "), "route add default via 198.51.100.1 dev test0") {
		t.Fatal(p.steps)
	}
	if strings.Join(p.undo[0].Args, " ") != "ip link delete dev test0" {
		t.Fatal("create undo lost")
	}
}
func TestInterfaceGatewayScopedReplacementAndRemoval(t *testing.T) {
	for _, action := range []string{"set", "remove"} {
		t.Run(action, func(t *testing.T) {
			a, f := addressFixture(t)
			original := M{"dst": "default", "gateway": "192.0.2.1", "dev": "eth0", "metric": 100, "protocol": "static"}
			other := M{"dst": "default", "gateway": "198.51.100.1", "dev": "eth1", "metric": 200, "protocol": "static"}
			raw, _ := json.Marshal([]any{original, other})
			f.responses["ip -j -4 route show table main default"] = string(raw)
			original["family"], original["table"] = 4, 254
			p, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": M{"name": "eth0", "address": "192.0.2.10", "prefix": 24, "original_address": "192.0.2.10/24", "gateway_action": action, "gateway_key": interfaceRouteKey(original), "gateway": "192.0.2.2"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(p.steps) != 1 || len(p.undo) != 1 {
				t.Fatal(p.steps)
			}
			if !strings.Contains(strings.Join(p.undo[0].Args, " "), "via 192.0.2.1 dev eth0") || strings.Contains(strings.Join(p.steps[0].Args, " "), "eth1") {
				t.Fatal("gateway scope/undo incorrect")
			}
		})
	}
}
func TestInterfaceAddressRejectsStaleOrUnsafePlans(t *testing.T) {
	a, f := addressFixture(t)
	base := M{"name": "eth0", "original_address": "192.0.2.10/24", "address": "192.0.2.20", "prefix": 24}
	for _, patch := range []M{{"original_address": "192.0.2.99/24"}, {"address": "2001:db8::20", "prefix": 64}, {"gateway_action": "set", "gateway": "198.51.100.1"}, {"gateway_action": "set", "gateway": "2001:db8::1"}, {"gateway_action": "remove"}, {"name": "lo"}} {
		item := clone(base)
		for k, v := range patch {
			item[k] = v
		}
		if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": item}); err == nil {
			t.Fatal("invalid edit accepted", patch)
		}
	}
	if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "item": base}); err == nil {
		t.Fatal("missing acknowledgment accepted")
	}
	p, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": base})
	if err != nil {
		t.Fatal(err)
	}
	f.responses["ip -j -d address show dev eth0"] = strings.ReplaceAll(testInterfaceAddressJSON, "192.0.2.10", "192.0.2.11")
	if p.precondition() == nil {
		t.Fatal("stale address accepted")
	}
	f.responses["ip -j -d address show dev eth0"] = strings.Replace(testInterfaceAddressJSON, `"scope":"global"`, `"scope":"global","dynamic":true`, 1)
	if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": base}); err == nil {
		t.Fatal("DHCP address replacement accepted")
	}
	f.responses["ip -j -d address show dev eth0"] = strings.Replace(testInterfaceAddressJSON, `{"local":"2001:db8::10","prefixlen":64,"scope":"global"}`, `{"local":"192.0.2.11","prefixlen":24,"scope":"global"}`, 1)
	if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": base}); err == nil {
		t.Fatal("dependent alias accepted")
	}
}

func TestKeepGatewayIgnoresHiddenDraftValues(t *testing.T) {
	a, _ := addressFixture(t)
	p, err := a.network.Preview(M{"kind": "interfaces", "operation": "add", "item": M{"type": "dummy", "name": "test-keep0", "address": "198.51.100.10", "prefix": 24, "gateway_action": "keep", "gateway": "198.51.100.1", "gateway_key": "old-hidden-value"}})
	if err != nil || len(p.steps) != 2 {
		t.Fatal("hidden gateway affected plan", p, err)
	}
	for _, step := range p.steps {
		if strings.Contains(strings.Join(step.Args, " "), "route") {
			t.Fatal("gateway keep still changed routes")
		}
	}
}

func TestAddressChangeCannotOrphanKeptGateway(t *testing.T) {
	a, f := addressFixture(t)
	f.responses["ip -j -4 route show table main default"] = `[{"dst":"default","gateway":"192.0.2.1","dev":"eth0","metric":100,"protocol":"static"}]`
	if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": M{"name": "eth0", "original_address": "192.0.2.10/24", "address": "198.51.100.10", "prefix": 24, "gateway_action": "keep"}}); err == nil {
		t.Fatal("orphaned kept gateway")
	}
}
