package gateway

import "testing"

func TestPageWidthBoundsAndScopedSave(t *testing.T) {
	a, f := fixture(t)
	before, calls := fingerprint(a.settings), len(f.calls)
	if num(a.presentationPolicy()["content_width_percent"]) != 99 || before != fingerprint(a.settings) {
		t.Fatal("width default must be read-only")
	}
	if err := a.savePresentation(M{"nodes_layout": "single", "services_style": "radial"}); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{80, 90, 100} {
		if err := a.savePresentation(M{"content_width_percent": width}); err != nil {
			t.Fatal(err)
		}
	}
	if str(a.presentationPolicy()["nodes_layout"]) != "single" || str(a.presentationPolicy()["services_style"]) != "radial" || len(f.calls) != calls {
		t.Fatal("page width changed another scope")
	}
	for _, bad := range []any{79, 101, 80.5, true, "bad"} {
		before = fingerprint(a.settings)
		if a.savePresentation(M{"content_width_percent": bad}) == nil || before != fingerprint(a.settings) {
			t.Fatal("bad width changed preferences", bad)
		}
	}
}
