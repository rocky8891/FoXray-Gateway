package gateway

import "testing"

func TestModuleMonitorLayoutsIndependent(t *testing.T) {
	a, _ := fixture(t)
	for _, module := range []string{"xray", "warp", "wireguard"} {
		key := "monitor_" + module + "_traffic_layout"
		if err := a.savePresentation(M{key: "double", "monitor_" + module + "_traffic_sections": []any{"statistics", "trend"}}); err != nil {
			t.Fatal(err)
		}
		p := a.presentationPolicy()
		if p[key] != "double" || p["monitor_"+module+"_latency_layout"] != "single" {
			t.Fatal("layout scopes mixed")
		}
		if a.savePresentation(M{key: "invalid"}) == nil {
			t.Fatal("invalid layout accepted")
		}
	}
}
