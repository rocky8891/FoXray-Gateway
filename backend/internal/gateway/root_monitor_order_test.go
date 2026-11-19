package gateway

import "testing"

func TestRootStatusSwitchRemainsOutsideHistoryOrder(t *testing.T) {
	a, f := fixture(t)
	before := fingerprint(a.cfg)
	calls := len(f.calls)
	p := clone(obj(defaultMonitorItems()["host"]))
	p["root"] = false
	p["order"] = []any{"connections", "disk", "network", "memory", "cpu", "load"}
	if err := a.saveMonitorItems("host", p); err != nil {
		t.Fatal(err)
	}
	if a.monitorEnabled("host", "root") || len(arr(obj(a.monitorItemsSnapshot()["host"])["order"])) != 6 {
		t.Fatal("status switch entered history order")
	}
	delete(p, "root")
	if err := a.saveMonitorItems("host", p); err != nil {
		t.Fatal(err)
	}
	if a.monitorEnabled("host", "root") {
		t.Fatal("old client overwrote root choice")
	}
	p["root"] = "invalid"
	old := fingerprint(a.monitoringItems)
	if a.saveMonitorItems("host", p) == nil || fingerprint(a.monitoringItems) != old {
		t.Fatal("invalid root changed settings")
	}
	if fingerprint(a.cfg) != before || len(f.calls) != calls {
		t.Fatal("display switch changed runtime")
	}
}
