package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type liveWarpRunner struct{}

func (liveWarpRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	// Go test executables do not implement the manager's --warp-probe helper mode.
	if len(args) == 7 && args[0] == "ip" && args[3] == "foxray-warp" && args[5] == "--warp-probe" {
		args = append([]string{}, args...)
		args[4] = "/opt/foxray/foxray"
	}
	return (SystemRunner{}).Run(ctx, args, input)
}

func TestLiveWarpVerifyBest(t *testing.T) {
	if os.Getenv("FOXRAY_TEST_WARP_VERIFY") != "1" {
		t.Skip("official tunnel interruptions require explicit opt-in")
	}
	seed := os.Getenv("FOXRAY_TEST_WARP_CANDIDATES")
	if seed == "" {
		t.Fatal("verified discovery candidate file required")
	}
	d := M{}
	if err := readJSON(seed, &d); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("/var/tmp", "foxray-official-test-")
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
	if err = writeJSON(filepath.Join(base, "warp-scout-results.json"), M{"collections": M{"masque-h2": d["rows"]}, "previous": M{}}); err != nil {
		t.Fatal(err)
	}
	a, err := New(base, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.components["warp"] = true
	a.runner = liveWarpRunner{}
	original := a.warp()
	if !yes(original["connected"]) {
		t.Fatal("connected baseline required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	j := &Job{Group: "warp.verify"}
	if err = a.verifyBestWarp(ctx, j, "masque-h2"); err != nil {
		t.Fatal(err)
	}
	a.invalidateWarp()
	final := a.warp()
	if !yes(final["connected"]) {
		t.Fatal("no connected tunnel at end of live verification")
	}
	records := M{}
	_ = readJSON(filepath.Join(base, "warp-official-results.json"), &records)
	summary := M{"message": j.Message, "details": j.Details, "official_results": records, "final_connected": true, "final_endpoint": final["endpoint"]}
	raw, _ := json.Marshal(summary)
	t.Log("official_summary=" + string(raw))
}
