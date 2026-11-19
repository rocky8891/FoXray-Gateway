package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWarpMetricValidationAndIndependentLatency(t *testing.T) {
	policy := M{"enabled": true, "speed_enabled": true}
	metrics, err := validateWarpMetrics(M{"metrics": []any{"latency", "loss", "true_latency", "speed", "speed"}}, policy)
	if err != nil || len(metrics) != 4 {
		t.Fatal(metrics, err)
	}
	for _, data := range []M{{"metrics": []any{}}, {"metrics": []any{"unknown"}}} {
		if _, err := validateWarpMetrics(data, policy); err == nil {
			t.Fatal("invalid metric accepted")
		}
	}
	if _, err := validateWarpMetrics(M{"metrics": []any{"speed"}}, M{"enabled": true, "speed_enabled": false}); err == nil {
		t.Fatal("disabled speed accepted")
	}
	result := M{"rtt_ms": 12., "true_latency_ms": 34., "true_latency_at": 1.}
	applyScoutTrueLatency(result, M{"ok": false, "error": "synthetic failure"})
	if num(result["true_latency_ms"]) != 34 || yes(result["true_latency_attempt_ok"]) {
		t.Fatal("last success lost")
	}
	applyScoutTrueLatency(result, M{"ok": true, "latency": 40., "time": 2.})
	if num(result["rtt_ms"]) != 12 || num(result["true_latency_ms"]) != 40 || !yes(result["true_latency_attempt_ok"]) {
		t.Fatal("RTT and true latency mixed")
	}
	ping := parseWarpPing("5 packets transmitted, 4 received, 20% packet loss, time 4010ms\nrtt min/avg/max/mdev = 10.1/12.4/15.2/1.4 ms")
	if num(ping["loss_pct"]) != 20 || num(ping["rtt_ms"]) != 12.4 {
		t.Fatal(ping)
	}
	if len(parseWarpPing("ping failed before any measurement")) != 0 {
		t.Fatal("invented missing values")
	}
}

func TestCurrentWarpMetricsAreBoundToTunnelAndRecordedIndependently(t *testing.T) {
	a, f := fixture(t)
	a.components["warp"] = true
	a.warpCache = M{"connected": true, "endpoint_address": "162.159.198.87:443"}
	a.warpAt = time.Now()
	a.results["warp"] = M{"last_speed": 999., "last_speed_time": 1.}
	a.runner = warpProbeTestRunner(func(ctx context.Context, args []string, input string) (string, error) {
		command := strings.Join(args, " ")
		if strings.Contains(command, " ping ") {
			if !strings.Contains(command, "ip netns exec foxray-warp ping -n -I CloudflareWARP") {
				t.Error("ping is not bound to official WARP tunnel")
			}
			return "5 packets transmitted, 5 received, 0% packet loss\nrtt min/avg/max/mdev = 10/12/14/1 ms", nil
		}
		if strings.Contains(command, "--warp-probe latency") {
			return `{"ok":true,"latency":40,"time":2,"probe_count":3,"probe_successes":3}`, nil
		}
		return f.Run(ctx, args, input)
	})
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	if err := a.testCurrentWarp(context.Background(), &Job{}, []string{"latency", "loss", "true_latency"}); err != nil {
		t.Fatal(err)
	}
	rows := M{}
	if err := readJSON(filepath.Join(a.base, "warp-endpoint-results.json"), &rows); err != nil {
		t.Fatal(err)
	}
	measured := obj(rows["162.159.198.87:443"])
	if num(measured["rtt_ms"]) != 12 || num(measured["true_latency_ms"]) != 40 || num(measured["loss_pct"]) != 0 {
		t.Fatal(measured)
	}
	if len(a.history["warp"]) != 1 || num(a.results["warp"]["last_speed"]) != 999 || num(a.results["warp"]["probe_count"]) != 3 {
		t.Fatal("current metrics/history were not preserved independently")
	}
}

func TestPartialTaskStatusPersistsInTaskHistory(t *testing.T) {
	var partial *partialTaskError
	if !errors.As(taskCompletionError(1, 2, "partial"), &partial) || taskCompletionError(2, 2, "") != nil {
		t.Fatal("outcome classification")
	}
	if errors.As(taskCompletionError(0, 2, "failed"), &partial) {
		t.Fatal("complete failure called partial")
	}
	a, _ := fixture(t)
	a.readonly = false
	_, err := a.submit("测试部分结果", "test.partial", func(context.Context, *Job) error { return taskCompletionError(1, 2, "成功1项，失败1项") })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rows := a.taskCenterHistory("")["items"].([]M)
		if len(rows) > 0 {
			if str(obj(rows[0])["status"]) != "partial" {
				t.Fatal(rows)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("partial task history missing")
}

func TestManagedEmptyVirtualInterfaceDeletionHasOrderedRollback(t *testing.T) {
	a, f := fixture(t)
	a.network.managed["interfaces"] = []any{M{"id": "qa0", "name": "qa0", "type": "dummy"}}
	f.responses = map[string]string{"ip -j -d address show": `[{"ifname":"qa0","ifindex":7,"flags":["BROADCAST"],"address":"02:00:00:00:00:07","mtu":1400,"linkinfo":{"info_kind":"dummy"},"addr_info":[]}]`}
	p, err := a.network.Preview(M{"kind": "interfaces", "operation": "delete", "item": M{"name": "qa0"}, "allow_protected": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 1 || len(p.undo) != 1 || strings.Join(p.undo[0].Args, " ") != "ip -batch -" {
		t.Fatal("rollback must be one ordered operation")
	}
	if !strings.HasPrefix(p.undo[0].Input, "link add name qa0 type dummy\nlink set dev qa0 address") {
		t.Fatal(p.undo[0].Input)
	}
	f.responses["ip -j -d address show"] = `[{"ifname":"qa0","ifindex":7,"flags":["UP"],"addr_info":[]}]`
	if p.precondition() == nil {
		t.Fatal("changed active interface deletion accepted")
	}
	if _, err := a.network.Preview(M{"kind": "interfaces", "operation": "delete", "item": M{"name": "external0"}, "allow_protected": true}); err == nil {
		t.Fatal("external interface deletion accepted")
	}
}

func TestPolicyAdoptionDoesNotRecreateKernelRule(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"ip -j -4 rule show": `[{"priority":2200,"src":"10.23.0.0/24","table":200}]`}
	p, err := a.network.Preview(M{"kind": "rules", "operation": "adopt", "item": M{"family": 4, "priority": 2200, "from": "10.23.0.0/24", "table": 200}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 0 || len(p.undo) != 0 || str(p.persist["operation"]) != "add" {
		t.Fatal("adoption changed kernel")
	}
	f.responses["ip -j -4 rule show"] = `[]`
	if p.precondition() == nil {
		t.Fatal("stale system rule adopted")
	}
}
