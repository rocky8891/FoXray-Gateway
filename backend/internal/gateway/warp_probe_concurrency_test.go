package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type warpProbeTestRunner func(context.Context, []string, string) (string, error)

func (f warpProbeTestRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	return f(ctx, args, input)
}

func TestWarpManualProbeIgnoresXrayBatchLock(t *testing.T) {
	a, f := fixture(t)
	a.components["warp"] = true
	a.cfg["endpoints"] = []any{}
	a.cfg["latency_url"] = "https://xray.invalid/latency"
	a.settings["warp_monitor"] = M{"latency_url": "https://warp.invalid/latency", "timeout": 7}
	var calls atomic.Int32
	a.runner = warpProbeTestRunner(func(ctx context.Context, args []string, input string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "--warp-probe latency") {
			p := M{}
			if json.Unmarshal([]byte(input), &p) != nil || p["latency_url"] != "https://warp.invalid/latency" || num(p["timeout"]) != 7 {
				t.Error("WARP policy was not used")
			}
			calls.Add(1)
			return `{"ok":true,"latency":25,"time":1,"probe_count":3,"probe_successes":3}`, nil
		}
		return f.Run(ctx, args, input)
	})
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	if err := a.detect(context.Background(), &Job{Started: now()}, false, []string{"warp"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !yes(a.results["warp"]["ok"]) {
		t.Fatal("WARP probe was not executed")
	}
}
func TestWarpProbeSerializesAndBackgroundSkipsBusy(t *testing.T) {
	a, _ := fixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan M, 1)
	var calls atomic.Int32
	a.runner = warpProbeTestRunner(func(ctx context.Context, args []string, input string) (string, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return `{"ok":true,"latency":20}`, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	go func() { firstDone <- a.warpLatency(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first probe not started")
	}
	if r := a.isolatedWarpProbeMode(context.Background(), "latency", false); !yes(r["busy"]) {
		t.Fatal("periodic probe did not skip busy tunnel", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := a.warpLatency(ctx); yes(r["ok"]) {
		t.Fatal("cancelled waiter ran")
	}
	if calls.Load() != 1 {
		t.Fatal("overlapping WARP probes")
	}
	close(release)
	select {
	case r := <-firstDone:
		if !yes(r["ok"]) {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("first probe not finished")
	}
	if r := a.warpLatency(context.Background()); !yes(r["ok"]) {
		t.Fatal("probe lock not released", r)
	}
}
func TestXrayProbeRetainsBatchExclusion(t *testing.T) {
	a, _ := fixture(t)
	a.components["xray"] = true
	a.cfg["endpoints"] = []any{M{"id": "test-node", "enabled": true}}
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	if err := a.detect(context.Background(), &Job{}, false, []string{"test-node"}); err == nil || !strings.Contains(err.Error(), "Xray") {
		t.Fatal("Xray batch exclusion lost", err)
	}
}
