package gateway

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestScoutMergeHonorsSelectedMetricsAndKeepsLastSuccess(t *testing.T) {
	old := M{"rtt_ms": 10., "rtt_at": "old-rtt", "loss_pct": 2., "loss_at": "old-loss", "speed_kib_s": 400.}
	measured := M{"endpoint": "162.159.198.87:443", "durable": true, "tested_at": "new", "rtt_ms": 20., "loss_pct": 0.}
	r := mergeScoutMetrics(old, measured, []string{"loss"})
	if num(r["rtt_ms"]) != 10 || r["rtt_at"] != "old-rtt" || num(r["loss_pct"]) != 0 || r["loss_at"] != "new" {
		t.Fatal(r)
	}
	failed := mergeScoutMetrics(r, M{"durable": false, "probe_error": "failed"}, []string{"latency", "loss"})
	if num(failed["rtt_ms"]) != 10 || failed["loss_at"] != "new" || yes(failed["latency_attempt_ok"]) {
		t.Fatal(failed)
	}
	fresh := mergeScoutMetrics(nil, measured, []string{"latency"})
	if num(fresh["rtt_ms"]) != 20 || fresh["loss_pct"] != nil {
		t.Fatal(fresh)
	}
	if !reflect.DeepEqual(old, M{"rtt_ms": 10., "rtt_at": "old-rtt", "loss_pct": 2., "loss_at": "old-loss", "speed_kib_s": 400.}) {
		t.Fatal("mutated previous result")
	}
}
func TestScoutSpeedFailureIsRecordedWithoutErasingSuccess(t *testing.T) {
	r := M{"speed_kib_s": 300., "speed_at": "old"}
	applyScoutBandwidth(r, M{"ok": false, "error": "timeout"})
	if r["speed_error"] != "timeout" || yes(r["speed_attempt_ok"]) || r["speed_at"] != "old" || num(r["speed_kib_s"]) != 300 {
		t.Fatal(r)
	}
	applyScoutBandwidth(r, M{"ok": true, "speed": 500., "time": "new", "source": "test"})
	if !yes(r["speed_attempt_ok"]) || r["speed_error"] != nil || r["speed_at"] != "new" {
		t.Fatal(r)
	}
}
func TestScoutScanUsesPinnedMASQUEConfiguration(t *testing.T) {
	args := strings.Join(scoutScanArgs(scoutProbeEnvironment{binary: "tool", account: "account", uid: "1", gid: "2"}, "masque-h2", "162.159.198.87:443", "report", true), " ")
	for _, want := range []string{"--masque-sni " + scoutMASQUESNI, "--interface ens16", "--tun-ping-count 10", "--target 162.159.198.87 --port 443"} {
		if !strings.Contains(args, want) {
			t.Fatal(args)
		}
	}
	if len(scoutMASQUEArgs("wg")) != 0 {
		t.Fatal("MASQUE arguments added to WireGuard")
	}
}
func TestScoutPreflightFailureDoesNotTouchOfficialTunnel(t *testing.T) {
	a, f := fixture(t)
	if err := a.preflightWarpCandidate(context.Background(), "162.159.198.87:443"); err == nil {
		t.Fatal("missing tool accepted")
	}
	if len(f.calls) != 0 {
		t.Fatal("unexpected system operation", f.calls)
	}
}
func TestScoutReportRejectsMissingAndTornTunnel(t *testing.T) {
	target := "162.159.198.87:443"
	for _, raw := range []string{"", "No working endpoints found.", "# 1 torn down (handshake ok)\n" + target + " 1ms 20ms 0% US LAX Los Angeles"} {
		if yes(parseScoutReport(raw, "masque-h2", target)["durable"]) {
			t.Fatal(raw)
		}
	}
	r := parseScoutReport(target+" 1ms 120ms 0% US LAX Los Angeles", "masque-h2", target)
	if !yes(r["durable"]) || num(r["rtt_ms"]) != 120 {
		t.Fatal(r)
	}
}

// Explicit opt-in: one isolated authenticated candidate and at most 1 MiB download.
// The official WARP client, manager settings and forwarding configuration are untouched.
func TestLiveScoutCandidate(t *testing.T) {
	if os.Getenv("FOXRAY_TEST_SCOUT") != "1" {
		t.Skip("live probe is opt-in")
	}
	base, err := os.MkdirTemp("", "foxray-scout-test-")
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a := &App{base: base, ctx: ctx, runner: SystemRunner{}, settings: M{"warp_monitor": M{"speed_timeout": 15, "speed_size_mb": 1, "speed_url": "https://speed.cloudflare.com/__down?bytes=1048576"}}}
	if err = a.preflightWarpCandidate(ctx, "162.159.198.87:443"); err != nil {
		t.Fatal(err)
	}
	env, cleanup, err := a.prepareScoutProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	r := a.scoutBandwidth(ctx, "masque-h2", "162.159.198.87:443", env.binary, env.account, env.uid, env.gid)
	if !yes(r["ok"]) {
		t.Fatal(str(r["error"]))
	}
	if num(r["bytes"]) > 1048576 || num(r["speed"]) <= 0 {
		t.Fatal("invalid speed result")
	}
	t.Logf("independent preflight and SOCKS download passed: %.1f KiB/s, %.0f bytes", num(r["speed"]), num(r["bytes"]))
}

type scoutSocksRunner struct {
	t       *testing.T
	stopped chan struct{}
	latency bool
}

func (f scoutSocksRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--masque-sni "+scoutMASQUESNI) {
		f.t.Error("SOCKS SNI differs from scan")
	}
	port := ""
	for i, v := range args {
		if v == "--port" {
			port = args[i+1]
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return "", err
	}
	defer close(f.stopped)
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return "", nil
		}
		go func() {
			defer c.Close()
			c.SetDeadline(time.Now().Add(3 * time.Second))
			head := make([]byte, 2)
			if _, e := io.ReadFull(c, head); e != nil {
				return
			}
			io.CopyN(io.Discard, c, int64(head[1]))
			c.Write([]byte{5, 0})
			req := make([]byte, 4)
			if _, e := io.ReadFull(c, req); e != nil {
				return
			}
			size := 0
			switch req[3] {
			case 1:
				size = 4
			case 4:
				size = 16
			case 3:
				b := make([]byte, 1)
				io.ReadFull(c, b)
				size = int(b[0])
			}
			io.CopyN(io.Discard, c, int64(size+2))
			c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
			r, e := http.ReadRequest(bufio.NewReader(c))
			if e != nil {
				return
			}
			if f.latency {
				if r.Host != "warp-latency.invalid" || r.URL.Path != "/generate_204" || r.Header.Get("Range") != "" {
					f.t.Error("true latency did not use the WARP target through SOCKS")
				}
				fmt.Fprint(c, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
				return
			}
			if r.Host != "warp-speed.invalid" || r.Header.Get("Range") != "bytes=0-1048575" {
				f.t.Error("WARP download policy not used")
			}
			body := strings.Repeat("x", 65536)
			fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		}()
	}
}

func TestScoutTrueLatencyUsesIndependentSOCKSAndReapsProcess(t *testing.T) {
	a, _ := fixture(t)
	stopped := make(chan struct{})
	a.runner = scoutSocksRunner{t: t, stopped: stopped, latency: true}
	a.settings["warp_monitor"] = M{"latency_url": "http://warp-latency.invalid/generate_204", "timeout": 2}
	r := a.scoutTrueLatency(context.Background(), "masque-h2", "162.159.198.87:443", scoutProbeEnvironment{binary: "tool", account: "account", uid: "1", gid: "2"})
	if !yes(r["ok"]) || num(r["probe_successes"]) != 3 || num(r["latency"]) < 1 {
		t.Fatal(r)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("candidate SOCKS process was not reaped")
	}
}
func TestScoutBandwidthUsesWarpPolicyAndReapsProcess(t *testing.T) {
	a, _ := fixture(t)
	stopped := make(chan struct{})
	a.runner = scoutSocksRunner{t: t, stopped: stopped}
	a.settings["warp_monitor"] = M{"speed_url": "http://warp-speed.invalid/file", "speed_size_mb": 1, "speed_timeout": 2}
	a.cfg["speed_url"] = "http://xray-speed.invalid/file"
	r := a.scoutBandwidth(context.Background(), "masque-h2", "162.159.198.87:443", "tool", "account", "1", "2")
	if !yes(r["ok"]) || num(r["bytes"]) != 65536 {
		t.Fatal(r)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("SOCKS process not reaped")
	}
}
