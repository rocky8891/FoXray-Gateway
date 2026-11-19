package gateway

import "testing"

func TestRuntimeOrderIndependentAndLegacyPreserved(t *testing.T) {
	a, _ := fixture(t)
	p := clone(obj(defaultMonitorItems()["host"]))
	p["runtime_order"] = []any{"root", "memory", "cpu", "load"}
	p["order"] = []any{"load", "cpu", "memory", "network", "disk", "connections"}
	if err := a.saveMonitorItems("host", p); err != nil {
		t.Fatal(err)
	}
	saved := obj(a.monitorItemsSnapshot()["host"])
	if fingerprint(saved["order"]) != fingerprint(p["order"]) {
		t.Fatal("runtime order changed history order")
	}
	delete(p, "runtime_order")
	if err := a.saveMonitorItems("host", p); err != nil {
		t.Fatal(err)
	}
	if len(arr(obj(a.monitorItemsSnapshot()["host"])["runtime_order"])) != 4 {
		t.Fatal("legacy save lost runtime order")
	}
	for _, bad := range []any{[]any{"load", "load", "cpu", "root"}, []any{"load", "cpu"}, []any{"load", "cpu", "memory", "disk"}} {
		p["runtime_order"] = bad
		if a.saveMonitorItems("host", p) == nil {
			t.Fatal("invalid runtime order accepted")
		}
	}
}
