package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit opt-in only: scan the configured endpoint pool without switching the official client.
func TestLiveWarpDiscovery(t *testing.T) {
	if os.Getenv("FOXRAY_TEST_WARP_DISCOVERY") != "1" {
		t.Skip("live discovery requires explicit opt-in")
	}
	base, err := os.MkdirTemp("/var/tmp", "foxray-discovery-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err = os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(base, "probe-tools"), 0755); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile("/opt/foxray/probe-tools/warpscout")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base, "probe-tools", "warpscout"), binary, 0755); err != nil {
		t.Fatal(err)
	}
	// Only public candidate measurements are copied; live user configuration is never loaded.
	if raw, e := os.ReadFile("/opt/foxray/warp-scout-results.json"); e == nil {
		if err = os.WriteFile(filepath.Join(base, "warp-scout-results.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(base, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.components["warp"] = true
	before := a.warp()
	if !yes(before["connected"]) {
		t.Fatal("official WARP must be connected before test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	j := &Job{}
	if err = a.discoverWarp(ctx, j, "masque-h2", 443); err != nil {
		t.Fatal(err)
	}
	a.invalidateWarp()
	after := a.warp()
	for _, key := range []string{"connected", "protocol", "masque_options", "override", "endpoint"} {
		if strOrNumber(before[key]) != strOrNumber(after[key]) {
			t.Fatal("discovery changed official tunnel", key)
		}
	}
	d := M{}
	if err = readJSON(filepath.Join(base, "warp-scout-results.json"), &d); err != nil {
		t.Fatal(err)
	}
	summary := M{"discovery": d["discovery"], "official_unchanged": true, "rows": []any{}}
	for _, value := range arr(obj(d["collections"])["masque-h2"]) {
		r := obj(value)
		item := M{}
		for _, key := range []string{"endpoint", "durable", "tested_at", "rtt_ms", "loss_pct", "true_latency_ms", "true_latency_attempt_ok", "speed_kib_s", "speed_attempt_ok", "probe_profile"} {
			item[key] = r[key]
		}
		summary["rows"] = append(arr(summary["rows"]), item)
	}
	raw, _ := json.Marshal(summary)
	t.Log("discovery_summary=" + string(raw))
}
