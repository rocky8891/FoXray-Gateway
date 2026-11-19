package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type unboundZoneRunner struct{ Runner }

func (r unboundZoneRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	if strings.Contains(strings.Join(args, " "), "--get-zone-of-interface=") {
		return "no zone\n", errors.New("exit status 1")
	}
	return r.Runner.Run(ctx, args, input)
}
func TestZoneInterfaceNoZoneNonzeroExitIsUnbound(t *testing.T) {
	a, f := fixture(t)
	a.runner = unboundZoneRunner{f}
	p, err := a.network.Preview(M{"kind": "firewall", "operation": "add", "allow_protected": true, "item": M{"kind": "zone-interface", "zone": "work", "interfaces": []any{"qa0"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.ZoneChanges[0].RuntimeZone != "" || p.ZoneChanges[0].PermanentZone != "" {
		t.Fatal("unbound state not preserved")
	}
	for _, undo := range p.undo {
		if !strings.Contains(strings.Join(undo.Args, " "), "--remove-interface=qa0") {
			t.Fatal("unbound rollback must remove binding")
		}
	}
}

func TestZoneInterfaceMultiPreviewKeepsPerScopeRollback(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"firewall-cmd --get-zone-of-interface=qa0": "public", "firewall-cmd --permanent --get-zone-of-interface=qa0": "internal", "firewall-cmd --get-zone-of-interface=qa1": "", "firewall-cmd --permanent --get-zone-of-interface=qa1": "trusted"}
	p, err := a.network.Preview(M{"kind": "firewall", "operation": "add", "allow_protected": true, "item": M{"kind": "zone-interface", "zone": "work", "interfaces": []any{"qa0", "qa1", "qa0"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ZoneChanges) != 2 || len(p.steps) != 4 || len(p.undo) != 4 {
		t.Fatal("batch or duplicate handling incorrect")
	}
	if p.ZoneChanges[0].RuntimeZone != "public" || p.ZoneChanges[0].PermanentZone != "internal" || p.ZoneChanges[1].PermanentZone != "trusted" {
		t.Fatal("original zones lost")
	}
	if !strings.Contains(strings.Join(p.undo[0].Args, " "), "--zone=public --change-interface=qa0") || !strings.Contains(strings.Join(p.undo[1].Args, " "), "--zone=internal --change-interface=qa0") {
		t.Fatal("scope rollback mismatch")
	}
	if !strings.Contains(strings.Join(p.undo[2].Args, " "), "--zone=work --remove-interface=qa1") {
		t.Fatal("unbound interface must return to unbound state")
	}
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), "--change-interface=") {
			t.Fatal("preview executed a mutation")
		}
	}
	f.responses["firewall-cmd --get-zone-of-interface=qa1"] = "external"
	if p.precondition() == nil {
		t.Fatal("stale region preview was accepted")
	}
}

func TestZoneInterfaceBatchValidationAndLegacy(t *testing.T) {
	a, f := fixture(t)
	for _, interfaces := range []any{[]any{}, []any{"qa0;bad"}, []any{12}, "qa0"} {
		_, err := a.network.Preview(M{"kind": "firewall", "operation": "add", "allow_protected": true, "item": M{"kind": "zone-interface", "zone": "public", "interfaces": interfaces}})
		if err == nil {
			t.Fatalf("invalid interfaces accepted: %T", interfaces)
		}
	}
	many := make([]any, 51)
	for i := range many {
		many[i] = "qa0"
	}
	if _, err := a.network.Preview(M{"kind": "firewall", "operation": "add", "allow_protected": true, "item": M{"kind": "zone-interface", "zone": "public", "interfaces": many}}); err == nil {
		t.Fatal("oversized batch accepted")
	}
	data := M{"kind": "firewall", "operation": "add", "item": M{"kind": "zone-interface", "zone": "public", "interface": "qa0"}}
	if _, err := a.network.Preview(data); err == nil {
		t.Fatal("confirmation missing")
	}
	data["allow_protected"] = true
	if p, err := a.network.Preview(data); err != nil || len(p.ZoneChanges) != 1 {
		t.Fatal("legacy single-interface request broken", err)
	}
	f.fail = "--get-zone-of-interface=qa0"
	if _, err := a.network.Preview(data); err == nil {
		t.Fatal("unknown original zone accepted")
	}
}
