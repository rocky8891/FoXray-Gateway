package gateway

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
)

func TestWarpDetectionSwitchesRemainIndependent(t *testing.T) {
	a, _ := fixture(t)
	p := a.warpMonitorPolicy()
	p["enabled"] = true
	p["speed_enabled"] = false
	if err := a.saveWarpMonitor(p); err != nil {
		t.Fatal(err)
	}
	if !yes(a.warpMonitorPolicy()["enabled"]) || yes(a.warpMonitorPolicy()["speed_enabled"]) {
		t.Fatal("switches coupled")
	}
	if result := a.warpBandwidth(context.Background()); yes(result["ok"]) || str(result["error"]) != "WARP 速度检测已关闭" {
		t.Fatal("disabled speed was executed")
	}
	if result := RunWarpProbe("speed", p); yes(result["ok"]) {
		t.Fatal("helper bypassed speed switch")
	}
	p["enabled"] = false
	p["speed_enabled"] = true
	if err := a.saveWarpMonitor(p); err != nil {
		t.Fatal(err)
	}
	if yes(a.warpMonitorPolicy()["enabled"]) || !yes(a.warpMonitorPolicy()["speed_enabled"]) {
		t.Fatal("latency disabled speed too")
	}
	p["speed_enabled"] = "true"
	if err := a.saveWarpMonitor(p); err == nil {
		t.Fatal("invalid switch accepted")
	}
	delete(p, "speed_enabled")
	if err := a.saveWarpMonitor(p); err != nil {
		t.Fatal("legacy policy rejected", err)
	}
	if !yes(a.warpMonitorPolicy()["speed_enabled"]) {
		t.Fatal("legacy speed default changed")
	}
}
func TestBuiltinRuleViewReflectsDatasetAndBoundsPages(t *testing.T) {
	a, _ := fixture(t)
	direct := []any{}
	for i := 0; i < 121; i++ {
		direct = append(direct, fmt.Sprintf("site%03d.example", i))
	}
	if err := writeJSON(filepath.Join(a.base, "rules", "gfwlist_domains.json"), M{"direct": direct, "proxy": []any{"proxy.example"}}); err != nil {
		t.Fatal(err)
	}
	result := a.builtinRuleView(url.Values{"profile": {"gfwlist"}, "offset": {"100"}})
	if num(result["total"]) != 123 || len(result["items"].([]M)) != 23 {
		t.Fatal("page mismatch", result)
	}
	result = a.builtinRuleView(url.Values{"profile": {"gfwlist"}, "query": {"proxy.example"}})
	if num(result["total"]) != 1 || str(result["items"].([]M)[0]["action"]) != "proxy" {
		t.Fatal("dataset content lost")
	}
	result = a.builtinRuleView(url.Values{"profile": {"../private"}})
	if result["error"] == nil {
		t.Fatal("invalid profile accepted")
	}
	result = a.builtinRuleView(url.Values{"profile": {"global"}})
	if num(result["total"]) != 1 {
		t.Fatal("global rule missing")
	}
}

func TestSpeedSwitchIsScopedToSelectedModule(t *testing.T) {
	config := M{"speed_enabled": false}
	warp := M{"speed_enabled": true}
	if err := validateSpeedTargets(config, warp, []M{{"id": "warp"}}); err != nil {
		t.Fatal("Xray switch affected WARP", err)
	}
	if err := validateSpeedTargets(config, warp, []M{{"id": "native"}}); err == nil {
		t.Fatal("disabled Xray speed allowed")
	}
	config["speed_enabled"] = true
	warp["speed_enabled"] = false
	if err := validateSpeedTargets(config, warp, []M{{"id": "native"}}); err != nil {
		t.Fatal("WARP switch affected Xray")
	}
	if err := validateSpeedTargets(config, warp, []M{{"id": "warp"}}); err == nil {
		t.Fatal("disabled WARP speed allowed")
	}
}
