//go:build linux

package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestKernelManagedInterfaceDeleteRestoresEmptyDevices(t *testing.T) {
	for _, kind := range []string{"dummy", "veth", "bridge", "vlan"} {
		t.Run(kind, func(t *testing.T) {
			a, r := kernelFixture(t)
			args := []string{"ip", "link", "add", "name", "qa0", "type", kind}
			owned := M{"id": "qa0", "name": "qa0", "type": kind}
			if kind == "veth" {
				args = append(args, "peer", "name", "qa1")
				owned["peer"] = "qa1"
			}
			if kind == "vlan" {
				args = []string{"ip", "link", "add", "link", "test0", "name", "qa0", "type", "vlan", "id", "42"}
				owned["parent"], owned["vlan_id"] = "test0", 42
			}
			if _, err := a.command(5, "", args...); err != nil {
				t.Fatal(err)
			}
			if _, err := a.command(5, "", "ip", "link", "set", "dev", "qa0", "mtu", "1400"); err != nil {
				t.Fatal(err)
			}
			a.network.managed["interfaces"] = []any{owned}
			before, err := a.commandJSON("ip", "-j", "-d", "link", "show", "dev", "qa0")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := a.network.Preview(M{"kind": "interfaces", "operation": "delete", "item": M{"name": "qa0"}, "allow_protected": true})
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range plan.steps {
				if _, err = r.Run(context.Background(), step.Args, step.Input); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := a.command(5, "", "ip", "link", "show", "dev", "qa0"); err == nil {
				t.Fatal("interface not deleted")
			}
			for i := len(plan.undo) - 1; i >= 0; i-- {
				step := plan.undo[i]
				if _, err = r.Run(context.Background(), step.Args, step.Input); err != nil {
					t.Fatal(err)
				}
			}
			after, err := a.commandJSON("ip", "-j", "-d", "link", "show", "dev", "qa0")
			if err != nil || len(after) != 1 {
				t.Fatal("interface not restored", err)
			}
			if num(obj(after[0])["mtu"]) != 1400 || str(obj(after[0])["address"]) != str(obj(before[0])["address"]) {
				t.Fatal("restored interface identity differs")
			}
			if kind == "veth" {
				if _, err := a.command(5, "", "ip", "link", "show", "dev", "qa1"); err != nil {
					t.Fatal("veth peer missing")
				}
			}
			baseline, err := a.command(5, "", "ip", "-4", "address", "show", "dev", "test0")
			if err != nil || !strings.Contains(baseline, "192.0.2.1/24") {
				t.Fatal("unrelated interface changed")
			}
		})
	}
}
