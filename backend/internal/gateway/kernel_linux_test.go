//go:build linux

package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type namespaceRunner struct{ name, binary string }

func (n namespaceRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	copyArgs := append([]string{}, args...)
	if args[0] == "ip" && len(args) > 1 && args[1] != "netns" || args[0] == "nft" || args[0] == "wg" {
		copyArgs = append([]string{"ip", "netns", "exec", n.name}, args...)
		if args[0] == "nft" {
			copyArgs = append([]string{"nsenter", "--net=/run/netns/" + n.name}, args...)
		}
	}
	if args[0] == "systemd-run" {
		for i, v := range copyArgs {
			if v == "--on-active=120s" {
				copyArgs[i] = "--on-active=3s"
			}
			if v == "--rollback-plan" && i > 0 {
				copyArgs[i-1] = n.binary
			}
		}
	}
	cmd := exec.CommandContext(ctx, copyArgs[0], copyArgs[1:]...)
	cmd.Stdin = strings.NewReader(input)
	out, e := cmd.CombinedOutput()
	if e != nil {
		return string(out), fmt.Errorf("isolated test command %v: %s", copyArgs, string(out))
	}
	return string(out), nil
}
func kernelFixture(t *testing.T) (*App, namespaceRunner) {
	t.Helper()
	if os.Getenv("FOXRAY_KERNEL_TESTS") != "1" {
		t.Skip("explicit isolated-kernel test flag required")
	}
	binary := os.Getenv("FOXRAY_TEST_BINARY")
	if binary == "" {
		t.Fatal("test gateway binary required")
	}
	name := "foxray-test-" + id()[:8]
	r := namespaceRunner{name: name, binary: binary}
	if _, e := adminRun([]string{"ip", "netns", "add", name}, ""); e != nil {
		t.Fatal(e)
	}
	a, e := New(t.TempDir(), t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	a.runner = r
	t.Cleanup(func() { a.Close(); _, _ = adminRun([]string{"ip", "netns", "del", name}, "") })
	for _, args := range [][]string{{"ip", "link", "add", "test0", "type", "dummy"}, {"ip", "link", "set", "test0", "up"}, {"ip", "addr", "add", "192.0.2.1/24", "dev", "test0"}} {
		if _, e = a.command(5, "", args...); e != nil {
			t.Fatal(e)
		}
	}
	return a, r
}
func TestKernelStandaloneWatchdogRemovesOnlyIsolatedRoute(t *testing.T) {
	a, r := kernelFixture(t)
	p, e := a.network.Preview(routeData())
	if e != nil {
		t.Fatal(e)
	}
	for i := range p.undo {
		p.undo[i].Args = append([]string{"ip", "netns", "exec", r.name}, p.undo[i].Args...)
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	rows, e := a.commandJSON("ip", "-j", "-4", "route", "show", "table", "2000")
	if e != nil || len(rows) != 1 {
		t.Fatal("isolated route was not installed")
	}
	t.Cleanup(func() { _, _ = adminRun([]string{"systemctl", "stop", "foxray-net-rollback-" + p.ID + ".timer"}, "") })
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		var s rollbackSpec
		_ = readJSON(a.network.specPath(p), &s)
		if s.Status == "rolled_back" {
			rows, _ = a.commandJSON("ip", "-j", "-4", "route", "show", "table", "2000")
			if len(rows) != 0 {
				t.Fatal("watchdog did not remove isolated route")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("independent rollback did not complete")
}
func TestKernelPolicyRuleConfirmedPersistsAndDeleteRollsBack(t *testing.T) {
	a, r := kernelFixture(t)
	p, e := a.network.Preview(M{"kind": "rules", "operation": "add", "item": M{"priority": 20050, "table": 2000, "from": "192.0.2.0/24", "family": 4}})
	if e != nil {
		t.Fatal(e)
	}
	for i := range p.undo {
		p.undo[i].Args = append([]string{"ip", "netns", "exec", r.name}, p.undo[i].Args...)
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	if e = a.network.Confirm(p.ID); e != nil {
		t.Fatal(e)
	}
	time.Sleep(4 * time.Second)
	rows, e := a.commandJSON("ip", "-j", "-4", "rule", "show")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range rows {
		found = found || num(obj(v)["priority"]) == 20050
	}
	if !found {
		t.Fatal("confirmed rule was rolled back")
	}
}
func TestKernelNftCaptureLoadsInIsolatedNamespace(t *testing.T) {
	a, _ := kernelFixture(t)
	for _, name := range []string{"ens16", "vpm-tun"} {
		if _, e := a.command(5, "", "ip", "link", "add", name, "type", "dummy"); e != nil {
			t.Fatal(e)
		}
		_, _ = a.command(5, "", "ip", "link", "set", name, "up")
	}
	cfg := clone(a.cfg)
	cfg["route_profile"] = "global"
	cfg["capture_host"], cfg["capture_lan"] = true, true
	if e := a.attach(cfg); e != nil {
		t.Fatal(e)
	}
	raw, e := a.command(5, "", "nft", "list", "table", "inet", "vpm")
	if e != nil || !strings.Contains(raw, "chain output") {
		t.Fatal("real nft capture was not installed")
	}
}

func TestKernelNativeFirewallRulesAreScopedAndReversible(t *testing.T) {
	a, r := kernelFixture(t)
	for _, kind := range []string{"port", "ip", "forward"} {
		item := M{"id": id(), "driver": "nftables", "zone": "host", "kind": kind, "port": "8080-8082", "protocol": "tcpudp", "source": "192.0.2.0/24", "action": "drop", "enabled": true, "family": "ipv4", "target_ip": "198.51.100.2", "target_port": "80-82"}
		normalized, e := normalizeFirewall(item)
		if e != nil {
			t.Fatal(e)
		}
		script := nativeNFTScript(normalized)
		if _, e = a.command(5, script, "nft", "--check", "-f", "-"); e != nil {
			t.Fatal(e)
		}
		if _, e = a.command(5, script, "nft", "-f", "-"); e != nil {
			t.Fatal(e)
		}
		if _, e = a.command(5, "", "nft", "delete", "table", "inet", nativeRuleName(item)); e != nil {
			t.Fatal(e)
		}
	}
	_ = r
}

func TestKernelWireGuardSNATScriptIsValidAndScoped(t *testing.T) {
	a, _ := kernelFixture(t)
	p := &WGProfile{Name: "testwg0", Network: &WGNetwork{Role: "client", RouteMode: "off", SNAT: true, LANSources: []string{"192.0.2.0/24"}, LANInterface: "test0"}}
	steps, e := wgHelperSteps(p)
	if e != nil || len(steps) != 1 {
		t.Fatal("missing scoped SNAT")
	}
	if _, e = a.command(5, steps[0].Input, "nft", "--check", "-f", "-"); e != nil {
		t.Fatal(e)
	}
}

func TestKernelXrayMetricsCountsActualIsolatedProxyDownload(t *testing.T) {
	a, r := kernelFixture(t)
	binary := os.Getenv("FOXRAY_TEST_XRAY")
	if binary == "" {
		t.Skip("test Xray path required")
	}
	_, _ = a.command(3, "", "ip", "link", "set", "lo", "up")
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "traffic.bin"), make([]byte, 32768), 0600); e != nil {
		t.Fatal(e)
	}
	config := M{"log": M{"loglevel": "none"}, "inbounds": []any{M{"listen": "127.0.0.1", "port": 18080, "protocol": "http", "tag": "test-proxy"}}, "outbounds": []any{M{"protocol": "freedom", "tag": "out-test"}}, "stats": M{}, "policy": M{"system": M{"statsOutboundUplink": true, "statsOutboundDownlink": true}}, "metrics": M{"listen": trafficMetricsAddress}}
	path := filepath.Join(dir, "engine.json")
	if e := writeJSON(path, config); e != nil {
		t.Fatal(e)
	}
	children := [][]string{{"python3", "-m", "http.server", "18081", "--bind", "127.0.0.1", "--directory", dir}, {binary, "run", "-c", path}}
	for _, args := range children {
		command := exec.Command("ip", append([]string{"netns", "exec", r.name}, args...)...)
		command.Stdout, command.Stderr = io.Discard, io.Discard
		if e := command.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	}
	ready := false
	for i := 0; i < 40; i++ {
		if _, e := a.command(2, "", "ip", "netns", "exec", r.name, "curl", "-fsS", "--max-time", "1", "http://"+trafficMetricsAddress+"/debug/vars"); e == nil {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("isolated core did not start metrics")
	}
	if _, e := a.command(5, "", "ip", "netns", "exec", r.name, "curl", "-fsS", "--max-time", "3", "--noproxy", "", "--proxy", "http://127.0.0.1:18080", "http://127.0.0.1:18081/traffic.bin", "-o", "/dev/null"); e != nil {
		t.Fatal(e)
	}
	raw, e := a.command(3, "", "ip", "netns", "exec", r.name, "curl", "-fsS", "http://"+trafficMetricsAddress+"/debug/vars")
	if e != nil {
		t.Fatal(e)
	}
	a.traffic.client.Transport = trafficTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})
	counts, e := a.traffic.readXray(a.ctx)
	if e != nil || counts["test"].Down < 32768 || !counts["test"].Available {
		t.Fatal("actual outbound download was not counted")
	}
}

var _ = errors.New

func TestKernelSNATRulesLoadInIsolatedNamespace(t *testing.T) {
	a, _ := kernelFixture(t)
	for _, action := range []string{"masquerade", "snat"} {
		item := snatRule("nftables", action)
		item["id"] = id()
		normalized, err := normalizeFirewall(item)
		if err != nil {
			t.Fatal(err)
		}
		script := nativeNFTScript(normalized)
		if _, err = a.command(5, script, "nft", "--check", "-f", "-"); err != nil {
			t.Fatal(err)
		}
		if _, err = a.command(5, script, "nft", "-f", "-"); err != nil {
			t.Fatal(err)
		}
		if _, err = a.command(5, "", "nft", "delete", "table", "inet", nativeRuleName(normalized)); err != nil {
			t.Fatal(err)
		}
	}
}
