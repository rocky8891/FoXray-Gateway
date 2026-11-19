package gateway

import "testing"

func TestBatchManagedRoutesApplyAndConfirmOnce(t *testing.T) {
	a, _ := fixture(t)
	a.network.managed["routes"] = []any{M{"id": "r1", "family": 4, "dst": "192.0.2.0/24", "dev": "test0", "table": 2000, "metric": 10, "enabled": true}, M{"id": "r2", "family": 4, "dst": "198.51.100.0/24", "dev": "test0", "table": 2000, "metric": 10, "enabled": true}}
	before := fingerprint(a.network.managed)
	p, e := a.network.Preview(M{"kind": "routes", "operation": "disable", "items": []any{M{"id": "r1"}, M{"id": "r2"}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) != 2 || len(p.undo) != 2 || len(arr(p.persist["changes"])) != 2 || fingerprint(a.network.managed) != before {
		t.Fatal("incomplete or mutating preview")
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	if fingerprint(a.network.managed) != before {
		t.Fatal("unconfirmed batch persisted")
	}
	if e = a.network.Confirm(p.ID); e != nil {
		t.Fatal(e)
	}
	for _, v := range arr(a.network.managed["routes"]) {
		if yes(obj(v)["enabled"]) {
			t.Fatal("only part of batch persisted")
		}
	}
}
func TestBatchInterfacesSinglePlanAndProtection(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"ip -j link show dev qa0": `[{"flags":["UP"]}]`, "ip -j link show dev qa1": `[{"flags":["UP"]}]`}
	data := M{"kind": "interfaces", "operation": "stop", "items": []any{M{"name": "qa0"}, M{"name": "qa1"}}, "allow_protected": true}
	before := fingerprint(a.network.managed)
	p, e := a.network.Preview(data)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) != 2 || len(p.undo) != 2 || p.persist != nil {
		t.Fatal("interface batch missing steps or changing configuration")
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	if e = a.network.Confirm(p.ID); e != nil {
		t.Fatal(e)
	}
	if fingerprint(a.network.managed) != before {
		t.Fatal("interface state changed management metadata")
	}
	for _, bad := range []M{{"allow_protected": false}, {"items": []any{M{"name": "qa0"}, M{"name": "lo"}}}, {"items": []any{M{"name": "CloudflareWARP"}}}, {"items": []any{M{"name": "qa0"}, M{"name": "qa0"}}}, {"operation": "delete"}} {
		d := clone(data)
		for k, v := range bad {
			d[k] = v
		}
		if _, e = a.network.Preview(d); e == nil {
			t.Fatal("invalid interface batch accepted")
		}
	}
}
func TestBatchManagedSelectionValidation(t *testing.T) {
	a, _ := fixture(t)
	before := fingerprint(a.network.managed)
	for _, items := range [][]any{{M{"id": "external"}}, {M{"id": "same"}, M{"id": "same"}}} {
		if _, e := a.network.Preview(M{"kind": "rules", "operation": "disable", "items": items}); e == nil {
			t.Fatal("unmanaged selection accepted")
		}
	}
	if fingerprint(a.network.managed) != before {
		t.Fatal("invalid batch changed configuration")
	}
}
func TestBatchInterfacesRollbackRestoresEveryLink(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"ip -j link show dev qa0": `[{"flags":["UP"]}]`, "ip -j link show dev qa1": `[{"flags":["UP"]}]`}
	p, e := a.network.Preview(M{"kind": "interfaces", "operation": "stop", "items": []any{M{"name": "qa0"}, M{"name": "qa1"}}, "allow_protected": true})
	if e != nil {
		t.Fatal(e)
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	calls := len(f.calls)
	if e = rollbackFile(a.network.specPath(p), f); e != nil {
		t.Fatal(e)
	}
	restored := map[string]bool{}
	for _, args := range f.calls[calls:] {
		if len(args) == 6 && args[0] == "ip" && args[1] == "link" && args[2] == "set" && args[3] == "dev" && args[5] == "up" {
			restored[args[4]] = true
		}
	}
	if !restored["qa0"] || !restored["qa1"] {
		t.Fatal("rollback omitted an interface")
	}
}
