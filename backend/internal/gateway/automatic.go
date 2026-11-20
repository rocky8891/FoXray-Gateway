package gateway

import (
	"context"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (a *App) warpAutomatic() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	due := time.Now().Add(time.Minute)
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if !a.moduleEnabled("warp") {
				continue
			}
			policy := M{}
			if readJSON(filepath.Join(a.base, "warp-endpoints.json"), &policy) != nil || !yes(policy["auto"]) || time.Now().Before(due) {
				continue
			}
			if !a.scoutOperate.TryLock() {
				continue
			}
			a.scoutOperate.Unlock()
			if !a.warpOperate.TryLock() {
				continue
			}
			a.warpOperate.Unlock()
			current := a.warp()
			if !yes(current["connected"]) {
				continue
			}
			protocol := scoutProtocol(current)
			state := a.scoutStatus()
			rows := arr(obj(state["collections"])[protocol])
			targets := []string{}
			for _, v := range rows {
				endpoint := str(obj(v)["endpoint"])
				if endpointValid(endpoint) {
					targets = append(targets, endpoint)
				}
			}
			if len(targets) > 10 {
				targets = targets[:10]
			}
			if len(targets) == 0 {
				continue
			}
			due = time.Now().Add(time.Duration(math.Max(60, num(policy["interval"]))) * time.Second)
			metrics := []string{}
			for _, v := range arr(policy["metrics"]) {
				key := str(v)
				if key == "latency" || key == "loss" || key == "speed" {
					metrics = append(metrics, key)
				}
			}
			if len(metrics) == 0 {
				metrics = []string{"latency", "loss"}
			}
			_, _ = a.submitAutomatic("WARP 自动端点评估", "warp.auto", func(ctx context.Context, j *Job) error {
				if e := a.scanWarp(ctx, j, protocol, targets, metrics); e != nil {
					return e
				}
				return a.chooseWarp(ctx, j, protocol, policy)
			})
		}
	}
}
func (a *App) chooseWarp(ctx context.Context, j *Job, protocol string, policy M) error {
	fresh := a.scoutStatus()
	if len(arr(policy["metrics"])) == 0 {
		policy = clone(policy)
		policy["metrics"] = []any{"latency", "loss"}
	}
	current := recordMap(fresh["current"])
	endpoint := firstNonempty(str(current["endpoint_address"]), str(current["override"]))
	rows := []M{}
	quality := func(r M) float64 {
		score := 0.
		count := 0.
		for _, v := range arr(policy["metrics"]) {
			switch str(v) {
			case "latency":
				if r["rtt_ms"] != nil {
					score += latencyQuality(num(r["rtt_ms"]))
					count++
				}
			case "loss":
				if r["loss_pct"] != nil {
					score += 100 - num(r["loss_pct"])
					count++
				}
			case "speed":
				if num(r["speed_kib_s"]) > 0 {
					speed := num(r["speed_kib_s"])
					score += speed / (speed + 1024) * 100
					count++
				}
			}
		}
		if count == 0 {
			return 0
		}
		return score / float64(count)
	}
	var active M
	for _, v := range arr(obj(fresh["collections"])[protocol]) {
		r := obj(v)
		if !officialWarpCoolingDown(r) && str(r["probe_profile"]) == scoutProbeProfile && yes(r["durable"]) && now()-num(r["tested_at"]) < 900 && r["rtt_ms"] != nil && r["loss_pct"] != nil && num(r["loss_pct"]) <= 20 {
			rows = append(rows, r)
			if str(r["endpoint"]) == endpoint {
				active = r
			}
		}
	}
	if len(rows) == 0 || active == nil {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return quality(rows[i]) > quality(rows[j]) })
	best := rows[0]
	if str(best["endpoint"]) == endpoint {
		return nil
	}
	state := M{}
	_ = readJSON(filepath.Join(a.base, "private", "warp-auto.json"), &state)
	if now()-num(state["last_switch"]) < num(first(policy["hold"], 600)) {
		return nil
	}
	if quality(best)-quality(active) < num(first(policy["improvement"], 10)) {
		return nil
	}
	streak := 1.
	if state["candidate"] == best["endpoint"] {
		streak = num(state["streak"]) + 1
	}
	state["candidate"], state["streak"] = best["endpoint"], streak
	_ = writeJSON(filepath.Join(a.base, "private", "warp-auto.json"), state)
	if streak < num(first(policy["confirmations"], 2)) {
		a.jobDetail(j, "候选更优，等待后续检测确认；当前隧道保持")
		return nil
	}
	if e := a.verifyBestWarp(ctx, j, protocol); e != nil {
		return e
	}
	a.invalidateWarp()
	after := a.warp()
	if firstNonempty(str(after["endpoint_address"]), str(after["override"])) != endpoint {
		state["last_switch"] = now()
	}
	state["streak"] = 0
	return writeJSON(filepath.Join(a.base, "private", "warp-auto.json"), state)
}
func recordMap(v any) M { return obj(v) }
func (a *App) icmpAutomatic() {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	next := time.Now().Add(20 * time.Second)
	lastInterval := -1.
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
			a.mu.RLock()
			policy := clone(a.icmp)
			a.mu.RUnlock()
			interval := num(policy["interval"])
			if lastInterval >= 0 && lastInterval != interval {
				next = time.Now().Add(time.Duration(max(1., interval)) * time.Second)
			}
			lastInterval = interval
			if !yes(policy["enabled"]) || num(policy["interval"]) == 0 || time.Now().Before(next) {
				continue
			}
			next = time.Now().Add(time.Duration(num(policy["interval"])) * time.Second)
			a.mu.RLock()
			native := clone(a.native)
			cfg := M{"endpoints": clone(a.cfg["endpoints"])}
			a.mu.RUnlock()
			for _, value := range arr(cfg["endpoints"]) {
				endpoint := obj(value)
				if !yes(endpoint["enabled"]) || str(endpoint["id"]) == "warp" {
					continue
				}
				identity := str(endpoint["id"])
				if identity == "warp" && !a.moduleEnabled("warp") || identity != "warp" && !a.moduleEnabled("xray") {
					continue
				}
				a.mu.RLock()
				old := clone(a.results[identity])
				disabled := identity == "warp" && a.warpDisabled
				a.mu.RUnlock()
				if disabled || now()-num(old["packet_loss_at"]) < num(policy["interval"]) {
					continue
				}
				host := ""
				if identity == "warp" {
					host = str(a.warp()["endpoint"])
				} else {
					for _, v := range arr(native["nodes"]) {
						n := obj(v)
						if n["id"] == identity {
							host = str(obj(n["outbound"])["server"])
						}
					}
				}
				if host == "" {
					continue
				}
				r := a.measureICMP(a.ctx, host)
				a.mu.Lock()
				latest := a.results[identity]
				if latest == nil {
					latest = M{}
				}
				for k, v := range r {
					latest[k] = v
				}
				a.results[identity] = latest
				a.mu.Unlock()
				if r["packet_loss_at"] != nil {
					_ = a.storeMeasurement(identity, true, r)
				}
				if a.ctx.Err() != nil {
					return
				}
			}
			a.saveMetrics()
		}
	}
}

var _ = strings.TrimSpace
