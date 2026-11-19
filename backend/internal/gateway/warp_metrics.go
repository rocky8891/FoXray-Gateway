package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

func validateWarpMetrics(data M, policy M) ([]string, error) {
	metrics := []string{}
	seen := map[string]bool{}
	for _, value := range arr(data["metrics"]) {
		metric := str(value)
		if metric != "latency" && metric != "loss" && metric != "true_latency" && metric != "speed" {
			return nil, errors.New("检测指标无效")
		}
		if metric == "speed" && !yes(policy["speed_enabled"]) {
			return nil, errors.New("WARP 速度检测已关闭")
		}
		if metric == "true_latency" && !yes(policy["enabled"]) {
			return nil, errors.New("WARP 真连接检测已关闭")
		}
		if !seen[metric] {
			metrics = append(metrics, metric)
			seen[metric] = true
		}
	}
	if len(metrics) == 0 {
		return nil, errors.New("至少选择一个检测指标")
	}
	return metrics, nil
}

func applyScoutTrueLatency(result, measured M) {
	result["true_latency_attempt_at"], result["true_latency_attempt_ok"] = now(), yes(measured["ok"])
	if yes(measured["ok"]) {
		result["true_latency_ms"], result["true_latency_at"] = measured["latency"], measured["time"]
		if count := num(measured["probe_count"]); count > 0 {
			result["true_latency_success_pct"] = num(measured["probe_successes"]) / count * 100
		}
		delete(result, "true_latency_error")
	} else {
		result["true_latency_error"] = firstNonempty(str(measured["error"]), "真连接检测未通过，保留上次成功结果")
	}
}

func (a *App) scoutTrueLatency(ctx context.Context, protocol, target string, env scoutProbeEnvironment) M {
	policy := a.warpMonitorPolicy()
	return a.scoutSOCKS(ctx, protocol, target, env.binary, env.account, env.uid, env.gid, num(policy["timeout"])*3+2, func(child context.Context, port int) M {
		proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(proxy)
		defer transport.CloseIdleConnections()
		return latencyWithClient(child, &http.Client{Transport: transport, Timeout: time.Duration(num(policy["timeout"])) * time.Second}, str(policy["latency_url"]))
	})
}

func parseWarpPing(output string) M {
	result := M{}
	if match := regexp.MustCompile(`([0-9.]+)% packet loss`).FindStringSubmatch(output); len(match) > 1 {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil && value >= 0 && value <= 100 {
			result["loss_pct"] = value
		}
	}
	if match := regexp.MustCompile(`(?:rtt|round-trip)[^=]*=\s*[0-9.]+/([0-9.]+)/`).FindStringSubmatch(output); len(match) > 1 {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil && value >= 0 {
			result["rtt_ms"] = value
		}
	}
	return result
}

func (a *App) testCurrentWarp(ctx context.Context, job *Job, metrics []string) error {
	a.warpOperate.Lock()
	defer a.warpOperate.Unlock()
	current := a.warp()
	if !yes(current["connected"]) {
		return errors.New("当前 WARP 隧道未连接")
	}
	endpoint := firstNonempty(str(current["endpoint_address"]), str(current["override"]))
	if endpoint == "" {
		return errors.New("实际端点未确认，未执行检测")
	}
	records := M{}
	_ = readJSON(filepath.Join(a.base, "warp-endpoint-results.json"), &records)
	result := clone(obj(records[endpoint]))
	if result == nil {
		result = M{}
	}
	var ping, latencyResult, speedResult M
	passed := 0
	for _, metric := range metrics {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.stage(job, "检测当前隧道 · "+map[string]string{"latency": "RTT", "loss": "丢包", "true_latency": "真延迟", "speed": "速度"}[metric])
		switch metric {
		case "latency", "loss":
			if ping == nil {
				child, cancel := context.WithTimeout(ctx, 12*time.Second)
				output, _ := a.runner.Run(child, []string{"ip", "netns", "exec", "foxray-warp", "ping", "-n", "-I", "CloudflareWARP", "-c", "5", "-W", "2", "-w", "10", "1.1.1.1"}, "")
				cancel()
				ping = parseWarpPing(output)
			}
			value, stamp := "rtt_ms", "rtt_at"
			if metric == "loss" {
				value, stamp = "loss_pct", "loss_at"
			}
			result[metric+"_attempt_at"], result[metric+"_attempt_ok"] = now(), ping[value] != nil
			if ping[value] != nil {
				result[value], result[stamp] = ping[value], now()
			}
		case "true_latency":
			latencyResult = a.warpLatency(ctx)
			applyScoutTrueLatency(result, latencyResult)
		case "speed":
			speedResult = a.warpBandwidth(ctx)
			applyScoutBandwidth(result, speedResult)
		}
		if yes(result[metric+"_attempt_ok"]) {
			passed++
		}
		a.jobDetail(job, scoutResultSummary(endpoint, result, []string{metric}))
	}
	result["origin"] = "official"
	records[endpoint] = result
	if err := writeJSON(filepath.Join(a.base, "warp-endpoint-results.json"), records); err != nil {
		return errors.New("当前隧道检测结果保存失败")
	}
	a.mu.Lock()
	existing := a.results["warp"]
	if existing == nil {
		existing = M{}
	}
	if latencyResult != nil {
		failures := num(existing["failures"])
		for key, value := range latencyResult {
			existing[key] = value
		}
		existing["failures"] = 0
		if !yes(latencyResult["ok"]) {
			existing["failures"] = failures + 1
		}
		history := append(a.history["warp"], M{"time": latencyResult["time"], "ok": latencyResult["ok"], "latency": latencyResult["latency"], "probe_count": latencyResult["probe_count"], "probe_successes": latencyResult["probe_successes"]})
		if len(history) > 120 {
			history = history[len(history)-120:]
		}
		a.history["warp"] = history
	}
	if speedResult != nil {
		existing["speed_time"], existing["speed_ok"], existing["speed_error"] = speedResult["time"], speedResult["ok"], speedResult["error"]
		if yes(speedResult["ok"]) {
			existing["last_speed"], existing["last_speed_time"], existing["last_speed_source"] = speedResult["speed"], speedResult["time"], speedResult["source"]
		}
	}
	a.results["warp"] = existing
	a.mu.Unlock()
	if latencyResult != nil {
		_ = a.storeMeasurement("warp", false, latencyResult)
	}
	a.saveMetrics()
	message := fmt.Sprintf("当前隧道检测完成，成功%d项，失败%d项", passed, len(metrics)-passed)
	a.stage(job, message)
	return taskCompletionError(passed, len(metrics), message)
}
