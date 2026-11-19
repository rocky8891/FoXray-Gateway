package gateway

import "testing"

func TestNodeDisplaySettings(t *testing.T) {
	a, r := fixture(t)
	n := len(r.calls)
	if err := a.savePresentation(M{"nodes_limit": 3, "nodes_visibility_mode": "selected", "nodes_visible_ids": []any{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []M{{"nodes_limit": -1}, {"nodes_limit": 4097}, {"nodes_visibility_mode": "x"}, {"nodes_visible_ids": []any{"a", "a"}}, {"nodes_visible_ids": []any{2}}} {
		before := fingerprint(a.settings)
		if a.savePresentation(bad) == nil || before != fingerprint(a.settings) {
			t.Fatal("bad setting accepted")
		}
	}
	if len(r.calls) != n {
		t.Fatal("display settings ran commands")
	}
}
