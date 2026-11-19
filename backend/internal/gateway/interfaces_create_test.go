package gateway

import (
	"strings"
	"testing"
)

func TestInterfaceCreationPreviewAndRecheck(t *testing.T) {
	for _, kind := range []string{"veth", "dummy", "bridge", "vlan"} {
		t.Run(kind, func(t *testing.T) {
			a, f := fixture(t)
			f.responses = map[string]string{"ip -j -d link show": `[{"ifname":"eth0","ifindex":2}]`}
			p, err := a.network.Preview(M{"kind": "interfaces", "operation": "add", "item": M{"name": "qa0", "type": kind, "peer": "qa1", "parent": "eth0", "vlan_id": 4094}})
			if err != nil {
				t.Fatal(err)
			}
			if len(p.steps) != 1 || len(p.undo) != 1 || strings.Join(p.undo[0].Args, " ") != "ip link delete dev qa0" {
				t.Fatal("unexpected commands")
			}
			for _, call := range f.calls {
				if call[0] == "ip" && !strings.Contains(strings.Join(call, " "), "-j") {
					t.Fatal("preview mutated network")
				}
			}
			f.responses["ip -j -d link show"] = `[{"ifname":"qa0"}]`
			if p.precondition() == nil || a.network.Apply(p.ID) == nil {
				t.Fatal("stale conflict accepted")
			}
		})
	}
}
func TestInterfaceCreationRejectsInvalidInput(t *testing.T) {
	a, f := fixture(t)
	for _, item := range []M{
		{"name": "lo", "type": "dummy"}, {"name": "wg-new", "type": "bridge"}, {"name": "bad;cmd", "type": "dummy"},
		{"name": "qa0", "type": "macvlan"}, {"name": "qa0", "type": "veth", "peer": "qa0"}, {"name": "qa0", "type": "veth", "peer": ""},
		{"name": "qa0", "type": "vlan", "parent": "eth0", "vlan_id": 0}, {"name": "qa0", "type": "vlan", "parent": "eth0", "vlan_id": 4095},
		{"name": "qa0", "type": "vlan", "parent": "eth0", "vlan_id": 1.5}, {"name": "qa0", "type": "vlan", "parent": "missing", "vlan_id": 1},
	} {
		if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "add", "item": item}); err == nil {
			t.Fatal("invalid creation accepted")
		}
	}
	f.fail = "ip -j -d link show"
	if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "add", "item": M{"name": "qa0", "type": "dummy"}}); err == nil {
		t.Fatal("unreadable snapshot accepted")
	}
	f.fail = ""
	f.responses = map[string]string{"ip -j -d link show": `[{"ifname":"eth0","ifindex":2}]`}
	p, err := a.network.Preview(M{"kind": "interfaces", "operation": "add", "item": M{"name": "qa0", "type": "vlan", "parent": "eth0", "vlan_id": 1}})
	if err != nil {
		t.Fatal(err)
	}
	f.responses["ip -j -d link show"] = `[{"ifname":"eth0","ifindex":3}]`
	if p.precondition() == nil {
		t.Fatal("recreated parent accepted")
	}
}
