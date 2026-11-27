package gateway

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func managerArchiveFixture(t *testing.T, extra string, corrupt bool) []byte {
	t.Helper()
	files := map[string][]byte{"foxray_v9.0.0": append([]byte{0x7f, 'E', 'L', 'F'}, []byte("test-program")...), "web/index.html": []byte("<html>test</html>")}
	if extra != "" {
		files[extra] = []byte("bad")
	}
	manifest := map[string]string{}
	for name, data := range files {
		manifest[name] = hex.EncodeToString(sha256sum(data))
	}
	if corrupt {
		manifest["web/index.html"] = "bad"
	}
	raw, _ := json.Marshal(manifest)
	files["MANIFEST_SHA256.json"] = raw
	buffer := &bytes.Buffer{}
	z := zip.NewWriter(buffer)
	for name, data := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	_ = z.Close()
	return buffer.Bytes()
}
func TestManagerReleaseArchiveValidation(t *testing.T) {
	if _, err := verifyManagerArchive(managerArchiveFixture(t, "", false), "9.0.0"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../outside", "/outside", "web/../../outside", "web\\outside", "C:/outside"} {
		if _, err := verifyManagerArchive(managerArchiveFixture(t, path, false), "9.0.0"); err == nil {
			t.Fatal("unsafe archive accepted", path)
		}
	}
	if _, err := verifyManagerArchive(managerArchiveFixture(t, "", true), "9.0.0"); err == nil {
		t.Fatal("bad checksum accepted")
	}
	if _, err := verifyManagerArchive(managerArchiveFixture(t, "", false), "8.0.0"); err == nil {
		t.Fatal("wrong binary version accepted")
	}
}
func TestManagerUpdateHealthFailureRollsBackOnlyManager(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("atomic replacement of live symlinks is Linux-specific; exercised in Linux CI")
	}
	base := t.TempDir()
	oldBin, oldWeb := filepath.Join(base, "old-program"), filepath.Join(base, "old-web")
	newBin, newWeb := filepath.Join(base, "releases", "test", "foxray_v9.0.0"), filepath.Join(base, "releases", "test", "web")
	for path, data := range map[string][]byte{oldBin: []byte("old"), filepath.Join(oldWeb, "index.html"): []byte("old"), newBin: []byte("new"), filepath.Join(newWeb, "index.html"): []byte("new")} {
		if err := atomicFile(path, data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(oldBin, filepath.Join(base, "foxray")); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if err := os.Symlink(oldWeb, filepath.Join(base, "web")); err != nil {
		t.Skip("directory symlinks unavailable", err)
	}
	plan := M{"version": "9.0.0", "binary": newBin, "web": newWeb, "previous_binary": oldBin, "previous_web": oldWeb, "health": "http://127.0.0.1/", "binary_sha256": hex.EncodeToString(sha256sum([]byte("new"))), "file_sha256": M{"foxray_v9.0.0": hex.EncodeToString(sha256sum([]byte("new"))), "web/index.html": hex.EncodeToString(sha256sum([]byte("new")))}}
	runner := &fakeRunner{}
	if err := applyManagerPlan(base, plan, runner, func(string, string) bool { return false }); err == nil {
		t.Fatal("unhealthy release accepted")
	}
	target, _ := filepath.EvalSymlinks(filepath.Join(base, "foxray"))
	if target != oldBin {
		t.Fatal("old binary not restored")
	}
	target, _ = filepath.EvalSymlinks(filepath.Join(base, "web"))
	if target != oldWeb {
		t.Fatal("old web not restored")
	}
	if len(runner.calls) != 2 {
		t.Fatal("restart and rollback missing")
	}
	for _, args := range runner.calls {
		if strings.Join(args, " ") != "systemctl restart foxray.service" {
			t.Fatal("component service changed")
		}
	}
	runner.calls = nil
	if err := applyManagerPlan(base, plan, runner, func(string, string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatal("successful upgrade restarted more than manager")
	}
	if err := os.WriteFile(filepath.Join(newWeb, "index.html"), []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	if err := applyManagerPlan(base, plan, runner, func(string, string) bool { return true }); err == nil || len(runner.calls) > 0 {
		t.Fatal("tampered staged web accepted")
	}
}
func TestRefreshPolicyOneSecondIsOptIn(t *testing.T) {
	a, _ := fixture(t)
	if a.realtimeInterval(60) != 60 || num(a.displayPolicy()["live_refresh_seconds"]) != 5 {
		t.Fatal("defaults changed")
	}
	if err := a.saveDisplayPolicy(M{"live_sampling_seconds": 1, "live_refresh_seconds": 1, "chart_refresh_seconds": 10}); err != nil {
		t.Fatal(err)
	}
	if a.realtimeInterval(60) != 1 || a.traffic.intervalLocked() != time.Second {
		t.Fatal("one-second sampling not applied")
	}
	if num(a.host.policy["storage_interval"]) != 60 || num(a.displayPolicy()["chart_refresh_seconds"]) != 10 {
		t.Fatal("history intervals changed")
	}
	for _, data := range []M{{"live_sampling_seconds": -1}, {"live_refresh_seconds": 0}, {"chart_refresh_seconds": 0}, {"unexpected": 1}} {
		if err := a.saveDisplayPolicy(data); err == nil {
			t.Fatal("invalid policy accepted", data)
		}
	}
	if err := a.saveDisplayPolicy(M{"live_sampling_seconds": 0}); err != nil || a.realtimeInterval(60) != 60 {
		t.Fatal("module policy not restored")
	}
}
func TestVersionComparisonAndPackageChecks(t *testing.T) {
	for _, pair := range [][2]string{{"v0.9.2", "0.9.1"}, {"v26.12.1", "26.11.9"}} {
		if !newerVersion(pair[0], pair[1]) {
			t.Fatal("new version lost")
		}
	}
	if newerVersion("v0.9.1", "0.9.2") || newerVersion("v0.9.2-beta", "0.9.1") {
		t.Fatal("downgrade or prerelease accepted")
	}
	a, runner := fixture(t)
	a.components["wireguard"] = true
	runner.responses = map[string]string{"rpm -q wireguard-tools --qf %{VERSION}-%{RELEASE}": "1.0.1-1", "dnf -q --refresh repoquery --upgrades --latest-limit=1 --qf %{version}-%{release} wireguard-tools": "1.0.2-1", "dnf -q repoquery --latest-limit=1 --changelogs wireguard-tools": "test release notes"}
	j := &Job{}
	if err := a.checkServiceVersion(context.Background(), j, "wireguard"); err != nil {
		t.Fatal(err)
	}
	row := obj(obj(a.updatesSnapshot()["services"])["wireguard"])
	if !yes(row["update_available"]) || str(row["latest"]) != "1.0.2-1" || str(row["release_notes"]) != "test release notes" {
		t.Fatal("update details missing")
	}
	runner.fail = "repoquery"
	runner.responses = nil
	if err := a.checkServiceVersion(context.Background(), j, "wireguard"); err == nil {
		t.Fatal("source failure reported as latest")
	}
	row = obj(obj(a.updatesSnapshot()["services"])["wireguard"])
	if str(row["status"]) != "error" {
		t.Fatal("check failure hidden")
	}
}
func TestManagerUpdatePlanRejectsTamperedFilesBeforeAnyRestart(t *testing.T) {
	base := t.TempDir()
	stage := filepath.Join(base, "releases", "test")
	bin := filepath.Join(stage, "foxray_v9.0.0")
	web := filepath.Join(stage, "web")
	if err := atomicFile(bin, []byte("program"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := atomicFile(filepath.Join(web, "index.html"), []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	plan := M{"version": "9.0.0", "binary": bin, "web": web, "previous_binary": filepath.Join(base, "old"), "previous_web": filepath.Join(base, "old-web"), "binary_sha256": hex.EncodeToString(sha256sum([]byte("program"))), "file_sha256": M{"foxray_v9.0.0": hex.EncodeToString(sha256sum([]byte("program"))), "web/index.html": hex.EncodeToString(sha256sum([]byte("original")))}}
	runner := &fakeRunner{}
	if err := applyManagerPlan(base, plan, runner, func(string, string) bool { return true }); err == nil || len(runner.calls) > 0 {
		t.Fatal("tampered web reached restart")
	}
	plan["previous_binary"] = filepath.Join(base, "..", "outside")
	if err := applyManagerPlan(base, plan, runner, func(string, string) bool { return true }); err == nil || len(runner.calls) > 0 {
		t.Fatal("outside target reached restart")
	}
}

func TestFasterSamplingKeepsCounterDeltaAcrossPolicyChange(t *testing.T) {
	a, _ := fixture(t)
	a.components["warp"] = false
	a.native["nodes"] = []any{M{"id": "n-test", "name": "test", "enabled": true}}
	if err := writeJSON(filepath.Join(a.base, "private", "traffic-policy.json"), M{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	a.traffic.client.Transport = trafficTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"stats":{"outbound":{"out-n-test":{"uplink":150,"downlink":250}}}}`))}, nil
	})
	a.traffic.rows["n-test"] = &trafficReading{ID: "n-test", previous: trafficCounter{Up: 100, Down: 100, Available: true}, previousAt: time.Now().Add(-45 * time.Second), previousInterval: 60}
	if err := a.saveDisplayPolicy(M{"live_sampling_seconds": 1}); err != nil {
		t.Fatal(err)
	}
	a.traffic.collect()
	row := a.traffic.rows["n-test"]
	if row.TotalUp != 50 || row.TotalDown != 150 || row.previousInterval != 1 {
		t.Fatal("counter delta lost when sampling became faster")
	}
}
