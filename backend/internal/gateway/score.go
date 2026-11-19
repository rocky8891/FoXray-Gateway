package gateway

import (
	"fmt"
	"math"
	"sort"
)

func latencyQuality(ms float64) float64 {
	a := [][2]float64{{0, 100}, {150, 100}, {200, 97}, {300, 92}, {500, 85}, {1000, 68}, {2000, 60}, {3000, 40}, {5000, 20}, {10000, 0}}
	for i := 1; i < len(a); i++ {
		if ms <= a[i][0] {
			l, r := a[i-1], a[i]
			return l[1] + (r[1]-l[1])*(math.Max(l[0], ms)-l[0])/(r[0]-l[0])
		}
	}
	return 0
}
func (a *App) scores() []M {
	eps := arr(a.cfg["endpoints"])
	out := []M{}
	if yes(a.components["xray"]) {
		pool := []M{}
		for _, v := range eps {
			e := obj(v)
			if str(e["id"]) != "warp" {
				pool = append(pool, e)
			}
		}
		out = append(out, a.scorePool(pool)...)
	}
	return out
}
func scoreLatencySamples(history []M, latest M, current, window float64) []float64 {
	byTime := map[float64]M{}
	for _, row := range append(append([]M{}, history...), latest) {
		stamp := num(row["time"])
		if current-stamp >= 0 && current-stamp <= window {
			byTime[stamp] = row
		}
	}
	times := []float64{}
	for stamp := range byTime {
		times = append(times, stamp)
	}
	sort.Float64s(times)
	if len(times) > 20 {
		times = times[len(times)-20:]
	}
	latencies := []float64{}
	for _, stamp := range times {
		row := byTime[stamp]
		if yes(row["ok"]) && num(row["latency"]) > 0 {
			latencies = append(latencies, num(row["latency"]))
		}
	}
	return latencies
}
func (a *App) scorePool(eps []M) []M {
	current := now()
	age := math.Max(120, num(a.cfg["interval"])*3+num(a.cfg["timeout"]))
	metrics := map[string]bool{}
	for _, v := range arr(a.cfg["score_metrics"]) {
		metrics[str(v)] = true
	}
	window := num(first(a.cfg["score_latency_window_minutes"], 30))
	minimum := int(num(first(a.cfg["score_latency_min_samples"], 1)))
	samples := map[string][]float64{}
	for _, endpoint := range eps {
		id := str(endpoint["id"])
		samples[id] = scoreLatencySamples(a.history[id], a.results[id], current, window*60)
	}
	weights := obj(a.cfg["score_weights"])
	lossCommon, speedCommon, validCount := true, true, 0
	ready := func(e M) bool {
		r := a.results[str(e["id"])]
		return (!metrics["latency"] || len(samples[str(e["id"])]) >= minimum) && yes(e["enabled"]) && yes(r["ok"]) && num(r["latency"]) > 0 && current-num(r["time"]) >= 0 && current-num(r["time"]) <= age
	}
	for _, e := range eps {
		if ready(e) {
			validCount++
			r := a.results[str(e["id"])]
			lossCommon = lossCommon && str(r["packet_loss_status"]) == "measured" && r["packet_loss_pct"] != nil && current-num(r["packet_loss_at"]) <= num(first(r["packet_loss_ttl"], 600))
			speedCommon = speedCommon && num(first(r["last_speed"], r["speed"])) > 0
		}
	}
	lossCommon = lossCommon && metrics["loss"] && validCount > 0
	speedCommon = speedCommon && metrics["speed"] && validCount > 0
	basis := []string{}
	if metrics["latency"] {
		basis = append(basis, "latency")
	}
	if lossCommon {
		basis = append(basis, "loss")
	}
	if speedCommon {
		basis = append(basis, "speed")
	}
	sum := 0.
	for _, k := range basis {
		sum += num(weights[k])
	}
	caps := M{}
	for _, k := range []string{"latency", "loss", "speed"} {
		caps[k] = 0.
		for _, b := range basis {
			if k == b && sum > 0 {
				caps[k] = num(weights[k]) * 100 / sum
			}
		}
	}
	rows := []M{}
	for _, e := range eps {
		identity := str(e["id"])
		r := a.results[identity]
		history := a.history[identity]
		latencies := samples[identity]
		mean := 0.
		for _, x := range latencies {
			mean += x
		}
		if len(latencies) > 0 {
			mean /= float64(len(latencies))
		}
		passed, total := 0., 0.
		start := len(history) - 60
		if start < 0 {
			start = 0
		}
		for _, h := range history[start:] {
			count := num(first(h["probe_count"], 1))
			total += count
			if h["probe_successes"] != nil {
				passed += num(h["probe_successes"])
			} else if yes(h["ok"]) {
				passed += count
			}
		}
		success := any(nil)
		if total > 0 {
			success = math.Round(passed/total*1000) / 10
		}
		reason := ""
		valid := ready(e)
		if !yes(e["enabled"]) {
			reason = "已停用"
		} else if metrics["latency"] && len(latencies) < minimum {
			reason = fmt.Sprintf("评分延迟成功样本不足（%d/%d）", len(latencies), minimum)
		} else if !valid {
			reason = "等待有效真连接检测"
		}
		lp, loss, speed := 0., 0., 0.
		var score any
		if valid && sum > 0 {
			if mean == 0 {
				mean = num(r["latency"])
			}
			lp = latencyQuality(mean) * num(caps["latency"]) / 100
			if lossCommon {
				loss = (1 - num(r["packet_loss_pct"])/100) * num(caps["loss"])
			}
			if speedCommon {
				v := num(first(r["last_speed"], r["speed"]))
				speed = v / (v + 1024) * num(caps["speed"])
			}
			score = math.Round((lp+loss+speed)*100) / 100
		} else if yes(e["enabled"]) && r["ok"] == false {
			score = 0
		}
		missing := []string{}
		if metrics["loss"] && !lossCommon {
			missing = append(missing, "ICMP 暂无共同有效数据")
		}
		if metrics["speed"] && !speedCommon {
			missing = append(missing, "部分候选没有成功带宽记录")
		}
		rows = append(rows, M{"id": identity, "score": score, "eligible": valid && sum > 0, "complete": valid && len(missing) == 0, "basis": basis, "selected": a.cfg["score_metrics"], "caps": caps, "reason": reason, "missing": missing, "latency_points": lp, "loss_points": loss, "speed_points": speed, "latency_average_ms": mean, "latency_average_samples": len(latencies), "latency_average_window_minutes": window, "latency_average_min_samples": minimum, "success_pct": success, "priority": e["priority"]})
	}
	return rows
}
