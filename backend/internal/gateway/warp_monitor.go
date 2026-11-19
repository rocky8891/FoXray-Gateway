package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func (a *App) warpMonitorPolicy() M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p := M{"enabled": true, "speed_enabled": true, "interval": 60, "timeout": 10, "latency_url": "https://www.google.com/generate_204", "speed_url": "https://speed.cloudflare.com/__down?bytes=10000000", "speed_timeout": 15, "speed_size_mb": 50}
	for k, v := range obj(a.settings["warp_monitor"]) {
		p[k] = v
	}
	return p
}
func (a *App) saveWarpMonitor(p M) error {
	p = clone(p)
	if _, exists := p["speed_enabled"]; !exists {
		p["speed_enabled"] = true
	}
	if _, ok := p["speed_enabled"].(bool); !ok {
		return errors.New("速度检测启用状态无效")
	}
	if _, ok := p["enabled"].(bool); !ok {
		return errors.New("探测启用状态无效")
	}
	for _, f := range []struct {
		k        string
		min, max int
	}{{"interval", 10, 3600}, {"timeout", 1, 60}, {"speed_timeout", 1, 120}, {"speed_size_mb", 1, 500}} {
		if _, e := integer(p[f.k], f.min, f.max); e != nil {
			return e
		}
	}
	for _, k := range []string{"latency_url", "speed_url"} {
		u, e := url.Parse(str(p[k]))
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("检测地址须为 HTTP(S)")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.settings["warp_monitor"]
	a.settings["warp_monitor"] = clone(p)
	if e := writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings); e != nil {
		a.settings["warp_monitor"] = old
		return e
	}
	return nil
}

// RunWarpProbe is a short-lived helper executed inside the WARP network namespace.
// It never initializes the manager, core, services or routing.
func RunWarpProbe(kind string, p M) M {
	ctx := context.Background()
	if kind == "speed" {
		if value, exists := p["speed_enabled"]; exists && !yes(value) {
			return M{"ok": false, "error": "WARP 速度检测已关闭"}
		}
		return bandwidthWithPolicy(ctx, 0, "warp", p)
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = warpDialer().DialContext
	defer t.CloseIdleConnections()
	return latencyWithClient(ctx, &http.Client{Transport: t, Timeout: time.Duration(num(p["timeout"])) * time.Second}, str(p["latency_url"]))
}
func (a *App) isolatedWarpProbe(ctx context.Context, kind string) M {
	return a.isolatedWarpProbeMode(ctx, kind, true)
}
func (a *App) isolatedWarpProbeMode(ctx context.Context, kind string, wait bool) M {
	for !a.warpProbeMu.TryLock() {
		if !wait {
			return M{"busy": true}
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return M{"ok": false, "time": now(), "error": "WARP 检测等待已取消"}
		case <-timer.C:
		}
	}
	defer a.warpProbeMu.Unlock()
	if ctx.Err() != nil {
		return M{"ok": false, "time": now(), "error": "WARP 检测已取消"}
	}

	p := a.warpMonitorPolicy()
	if kind == "speed" && !yes(p["speed_enabled"]) {
		return M{"ok": false, "error": "WARP 速度检测已关闭", "time": now()}
	}
	executable, e := os.Executable()
	if e != nil {
		return M{"ok": false, "latency": -1, "time": now(), "error": "无法定位 WARP 探测程序"}
	}
	input, _ := json.Marshal(p)
	seconds := num(p["timeout"])*3 + 2
	if kind == "speed" {
		seconds = num(p["speed_timeout"]) + 3
	}
	child, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	raw, e := a.runner.Run(child, []string{"ip", "netns", "exec", "foxray-warp", executable, "--warp-probe", kind}, string(input))
	r := M{}
	if e != nil || json.Unmarshal([]byte(raw), &r) != nil {
		return M{"ok": false, "latency": -1, "speed": 0, "time": now(), "error": "WARP 独立探测失败或超时"}
	}
	return r
}
func (a *App) warpLatency(ctx context.Context) M   { return a.isolatedWarpProbe(ctx, "latency") }
func (a *App) warpBandwidth(ctx context.Context) M { return a.isolatedWarpProbe(ctx, "speed") }
func (a *App) warpMonitorLoop() {
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	next := time.Now().Add(15 * time.Second)
	lastInterval := -1.
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
			p := a.warpMonitorPolicy()
			interval := num(p["interval"])
			if lastInterval >= 0 && lastInterval != interval {
				next = time.Now().Add(time.Duration(max(1., interval)) * time.Second)
			}
			lastInterval = interval
			if !a.moduleEnabled("warp") || !yes(p["enabled"]) || time.Now().Before(next) {
				continue
			}
			next = time.Now().Add(time.Duration(num(p["interval"])) * time.Second)
			a.mu.RLock()
			stopped := a.warpDisabled
			a.mu.RUnlock()
			if stopped {
				continue
			}
			r := M{"ok": false, "latency": -1, "time": now(), "error": "WARP 隧道未连接"}
			if yes(a.warp()["connected"]) {
				r = a.isolatedWarpProbeMode(a.ctx, "latency", false)
				if yes(r["busy"]) {
					continue
				}
			}
			a.mu.Lock()
			old := a.results["warp"]
			if old == nil {
				old = M{}
			}
			if yes(r["ok"]) {
				old["failures"] = 0
			} else {
				old["failures"] = num(old["failures"]) + 1
			}
			for k, v := range r {
				old[k] = v
			}
			a.results["warp"] = old
			a.history["warp"] = append(a.history["warp"], clone(r))
			if len(a.history["warp"]) > 120 {
				a.history["warp"] = a.history["warp"][len(a.history["warp"])-120:]
			}
			a.mu.Unlock()
			_ = a.storeMeasurement("warp", false, r)
			a.saveMetrics()
		}
	}
}
