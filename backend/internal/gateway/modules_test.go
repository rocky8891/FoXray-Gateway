package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDisableModulesDoesNotStopServicesOrModifyProxy(t *testing.T) {
	a, f := fixture(t)
	a.readonly = false
	a.components["xray"], a.components["warp"], a.components["wireguard"] = true, true, true
	before := fingerprint(a.cfg)
	for _, name := range []string{"xray", "warp", "wireguard"} {
		if err := a.changeModule(context.Background(), &Job{}, M{"module": name, "enabled": false}); err != nil {
			t.Fatal(err)
		}
	}
	if fingerprint(a.cfg) != before || len(f.calls) != 0 {
		t.Fatal("hiding modules modified forwarding or services")
	}
	for _, p := range []struct {
		path string
		data M
	}{{"/api/proxy", M{}}, {"/api/warp/policy", M{}}, {"/api/network/preview", M{"kind": "wireguard"}}} {
		if a.moduleRequestAllowed(p.path, p.data) == nil {
			t.Fatal("disabled module write allowed", p.path)
		}
	}
}
func TestFreshModuleDefaultsAndKomariManualOnly(t *testing.T) {
	base := t.TempDir()
	if e := InitializeModules(base, false, false, false, false); e != nil {
		t.Fatal(e)
	}
	c := M{}
	if readJSON(filepath.Join(base, "components.json"), &c) != nil {
		t.Fatal("manifest missing")
	}
	for _, name := range []string{"xray", "warp", "wireguard", "komari"} {
		if yes(c[name]) {
			t.Fatal("unselected module enabled", name)
		}
	}
	for _, address := range []string{"file:///tmp/test", "http://name:secret@example.invalid/", "https://example.invalid/?token=synthetic", "javascript:alert(1)"} {
		if _, err := normalizeKomariURL(address); err == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
}
func TestKomariProbeRecognizesVersionWithoutInstallingService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"test","hash":"test"}}`))
	}))
	defer server.Close()
	row, err := probeKomari(context.Background(), server.URL)
	if err != nil || !yes(row["recognized"]) || !yes(row["reachable"]) {
		t.Fatal("Komari probe failed", err)
	}
}
func TestCPUBreakdownAndMemoryHistoryPreserveMissingValues(t *testing.T) {
	before := M{"_cpu_total": 1000, "_cpu_idle": 800, "_cpu_hi": 5, "_cpu_si": 20, "_cpu_st": 8}
	current := M{"_cpu_total": 1100, "_cpu_idle": 880, "_cpu_hi": 6, "_cpu_si": 24, "_cpu_st": 10}
	hostRates(before, current, 2)
	for key, value := range map[string]float64{"cpu_percent": 20, "cpu_hi": 1, "cpu_si": 4, "cpu_st": 2} {
		if num(current[key]) != value {
			t.Fatalf("%s incorrect", key)
		}
	}
	old := M{"_cpu_total": 1000, "_cpu_idle": 800}
	next := M{"_cpu_total": 1100, "_cpu_idle": 880}
	hostRates(old, next, 2)
	if next["cpu_hi"] != nil {
		t.Fatal("old sample fabricated interrupt data")
	}
	aggregate := hostAggregate([]M{{"memory_used": 100, "cpu_hi": 2}, {"memory_used": 200, "cpu_hi": 4}})
	if num(aggregate["memory_used"]) != 150 || num(aggregate["cpu_hi"]) != 3 {
		t.Fatal("new metrics not aggregated")
	}
}
