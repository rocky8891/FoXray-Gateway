package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var latencyTargets = map[string]bool{"https://www.google.com/generate_204": true, "https://www.gstatic.com/generate_204": true, "https://cp.cloudflare.com/generate_204": true}

func (a *App) probePortLocked(identity string) int {
	if identity == "warp" {
		return 0
	}
	enabled := false
	for _, v := range arr(a.cfg["endpoints"]) {
		e := obj(v)
		enabled = enabled || (str(e["id"]) == identity && yes(e["enabled"]))
	}
	if !enabled {
		return 0
	}
	var inbounds []any
	if readEngineField(filepath.Join(a.base, "engine.json"), "inbounds", &inbounds) == nil {
		for _, v := range inbounds {
			r := obj(v)
			if str(r["tag"]) == "probe-"+identity && str(r["listen"]) == "127.0.0.1" {
				return int(num(r["port"]))
			}
		}
	}
	i := 0
	for _, v := range arr(a.cfg["endpoints"]) {
		e := obj(v)
		if yes(e["enabled"]) && str(e["id"]) != "warp" {
			if str(e["id"]) == identity {
				return 16100 + i
			}
			i++
		}
	}
	return 0
}
func (a *App) probePort(identity string) (int, func(), error) {
	a.mu.RLock()
	p := a.probePortLocked(identity)
	var node M
	for _, v := range arr(a.native["nodes"]) {
		if obj(v)["id"] == identity {
			node = clone(obj(obj(v)["outbound"]))
		}
	}
	a.mu.RUnlock()
	if p > 0 {
		return p, func() {}, nil
	}
	if node == nil {
		return 0, nil, errors.New("节点没有可用的独立探测入口")
	}
	out, e := canonicalOutbound(node, "manual-probe")
	if e != nil {
		return 0, nil, e
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return 0, nil, e
	}
	p = listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	root := filepath.Join(a.base, "probe-runtime")
	_ = os.MkdirAll(root, 0711)
	_ = os.Chmod(root, 0711)
	dir, e := os.MkdirTemp(root, "go-manual-")
	if e != nil {
		return 0, nil, e
	}
	u, e := user.Lookup("vpm-engine")
	if e != nil {
		return 0, nil, errors.New("探测用户不存在")
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	_ = os.Chown(dir, uid, gid)
	data := M{"log": M{"loglevel": "none", "access": "none", "error": "none"}, "inbounds": []any{M{"tag": "probe", "listen": "127.0.0.1", "port": p, "protocol": "http", "settings": M{}}}, "outbounds": []any{out}}
	path := filepath.Join(dir, "probe.json")
	if e = writeJSON(path, data); e != nil {
		return 0, nil, e
	}
	_ = os.Chown(path, uid, gid)
	cmd := exec.CommandContext(a.ctx, "setpriv", "--reuid", u.Uid, "--regid", u.Gid, "--clear-groups", "--inh-caps=+net_admin,+net_raw", "--ambient-caps=+net_admin,+net_raw", filepath.Join(a.base, "xray"), "run", "-c", path)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if e = cmd.Start(); e != nil {
		_ = os.RemoveAll(dir)
		return 0, nil, errors.New("独立探测核心启动失败")
	}
	cleanup := func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = os.RemoveAll(dir) }
	for i := 0; i < 40; i++ {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return p, cleanup, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	cleanup()
	return 0, nil, errors.New("独立探测核心未就绪")
}
func measureLatency(ctx context.Context, p int, target string, timeout float64) M {
	u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", p))
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyURL(u)
	t.MaxIdleConnsPerHost = 1
	t.DisableCompression = true
	defer t.CloseIdleConnections()
	client := &http.Client{Transport: t, Timeout: time.Duration(timeout) * time.Second}
	return latencyWithClient(ctx, client, target)
}
func latencyWithClient(ctx context.Context, client *http.Client, target string) M {
	result := M{"ok": false, "latency": -1, "time": now(), "latency_probe_count": 3, "probe_count": 3}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		result["error"] = "真连接检测地址无效"
		return result
	}
	samples := []int{}
	minValue, success := math.MaxInt, 0
	for i := 0; i < 3; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				result["error"] = "已取消"
				return result
			case <-time.After(100 * time.Millisecond):
			}
		}
		started := time.Now()
		req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
		req.Header.Set("User-Agent", "FoXrayGateway/"+Version)
		res, e := client.Do(req)
		value := -1
		if e == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
			_ = res.Body.Close()
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				value = max(1, int(time.Since(started).Milliseconds()))
				success++
				if value < minValue {
					minValue = value
				}
			}
		}
		samples = append(samples, value)
	}
	result["latency_probe_samples"] = samples
	result["latency_probe_successes"], result["probe_successes"] = success, success
	if success > 0 {
		result["ok"], result["latency"] = true, minValue
	} else {
		result["error"] = "HTTPS 三次检测均失败"
	}
	result["time"] = now()
	return result
}
func measureBandwidth(ctx context.Context, p int) M { return measureBandwidthScheme(ctx, p, "http") }
func measureBandwidthScheme(ctx context.Context, p int, scheme string) M {
	return bandwidthWithPolicy(ctx, p, scheme, M{})
}
func (a *App) bandwidth(ctx context.Context, p int, scheme string) M {
	a.mu.RLock()
	c := a.probeConfigLocked()
	a.mu.RUnlock()
	return bandwidthWithPolicy(ctx, p, scheme, c)
}
func bandwidthWithPolicy(ctx context.Context, p int, scheme string, c M) M {
	timeout := num(c["speed_timeout"])
	if timeout < 1 {
		timeout = 15
	}
	limit := int64(num(c["speed_size_mb"])) * 1024 * 1024
	if limit < 1 {
		limit = 50 * 1024 * 1024
	}

	sources := []struct{ name, url string }{{"Cloudflare", "https://speed.cloudflare.com/__down?bytes=5000000"}, {"OVH", "https://proof.ovh.net/files/10Mb.dat"}}
	if custom := str(c["speed_url"]); custom != "" {
		sources = []struct{ name, url string }{{"自定义测速地址", custom}}
	}
	lastFailure := "下载数据不足"
	for _, source := range sources {
		proxy, _ := url.Parse(fmt.Sprintf("%s://127.0.0.1:%d", scheme, p))
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = http.ProxyURL(proxy)
		if scheme == "warp" {
			t.Proxy = nil
			t.DialContext = warpDialer().DialContext
		}
		client := &http.Client{Transport: t, Timeout: time.Duration(timeout) * time.Second}
		req, _ := http.NewRequestWithContext(ctx, "GET", source.url, nil)
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
		start := time.Now()
		res, e := client.Do(req)
		if e != nil {
			lastFailure = "下载连接失败"
			var networkError net.Error
			if errors.As(e, &networkError) && networkError.Timeout() {
				lastFailure = "下载连接超时"
			}
			t.CloseIdleConnections()
			continue
		}
		if res.StatusCode != 200 && res.StatusCode != 206 || strings.Contains(res.Header.Get("Content-Type"), "text/html") || strings.Contains(res.Header.Get("Content-Type"), "application/json") {
			lastFailure = fmt.Sprintf("测速地址返回 HTTP %d", res.StatusCode)
			if strings.Contains(res.Header.Get("Content-Type"), "text/html") {
				lastFailure = "测速地址返回 HTML 页面"
			}
			if strings.Contains(res.Header.Get("Content-Type"), "application/json") {
				lastFailure = "测速地址返回 JSON 数据"
			}
			_ = res.Body.Close()
			t.CloseIdleConnections()
			continue
		}
		size, er := io.Copy(io.Discard, io.LimitReader(res.Body, limit))
		_ = res.Body.Close()
		t.CloseIdleConnections()
		elapsed := math.Max(time.Since(start).Seconds(), 0.000001)
		if size >= 65536 && elapsed > 0 {
			return M{"ok": true, "speed": float64(size) / elapsed / 1024, "time": now(), "source": source.name, "bytes": size, "duration": elapsed, "limited": er != nil}
		}
	}
	return M{"ok": false, "speed": 0, "time": now(), "error": lastFailure + "，保留上次成功带宽"}
}

var pingCount = regexp.MustCompile(`(\d+) packets transmitted,\s*(\d+) (?:packets )?received`)
var pingAverage = regexp.MustCompile(`(?:rtt|round-trip)[^=]*=\s*[\d.]+/([\d.]+)/`)

func (a *App) measureICMP(ctx context.Context, host string) M {
	a.mu.RLock()
	policy := clone(a.icmp)
	a.mu.RUnlock()
	if !yes(policy["enabled"]) {
		return M{"packet_loss_status": "disabled"}
	}
	ip := net.ParseIP(host)
	if ip == nil {
		resolved, e := a.resolveEntry(host)
		if e != nil {
			return M{"packet_loss_status": "error"}
		}
		ip = net.ParseIP(resolved)
	}
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return M{"packet_loss_status": "unavailable"}
	}
	count := int(num(policy["count"]))
	seconds := count*int(num(policy["timeout"])) + 5
	child, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	args := []string{"ping", "-4", "-n", "-q", "-c", intstr(policy["count"]), "-s", intstr(policy["size"]), "-W", intstr(policy["timeout"]), "-m", "65743", "-I", uplink(), ip.String()}
	raw, _ := a.runner.Run(child, args, "")
	m := pingCount.FindStringSubmatch(raw)
	if len(m) != 3 {
		return M{"packet_loss_status": "error", "packet_loss_at": now()}
	}
	sent, _ := strconv.Atoi(m[1])
	received, _ := strconv.Atoi(m[2])
	if sent <= 0 {
		return M{"packet_loss_status": "error"}
	}
	r := M{"packet_loss_status": "measured", "packet_loss_at": now(), "packet_loss_ttl": max(600, num(policy["interval"])*2), "icmp_sent": sent, "icmp_received": received, "icmp_success_pct": float64(received) / float64(sent) * 100, "packet_loss_pct": float64(sent-received) / float64(sent) * 100}
	if received == 0 {
		r["packet_loss_status"] = "unresponsive"
		r["packet_loss_pct"] = nil
	}
	if values := pingAverage.FindStringSubmatch(raw); len(values) == 2 {
		r["icmp_average_ms"], _ = strconv.ParseFloat(values[1], 64)
	}
	return r
}
func (a *App) detect(ctx context.Context, j *Job, speed bool, ids []string) error {
	a.mu.RLock()
	c, native := a.probeConfigLocked(), clone(a.native)
	a.mu.RUnlock()
	eps := []M{}
	selected := map[string]bool{}
	for _, identity := range ids {
		selected[identity] = true
	}
	for _, v := range arr(c["endpoints"]) {
		e := obj(v)
		if str(e["id"]) == "warp" && !a.moduleEnabled("warp") || str(e["id"]) != "warp" && !a.moduleEnabled("xray") {
			continue
		}
		if len(ids) > 0 {
			if selected[str(e["id"])] {
				eps = append(eps, e)
			}
		} else if yes(e["enabled"]) && str(e["id"]) != "warp" {
			eps = append(eps, e)
		}
	}
	if selected["warp"] && a.moduleEnabled("warp") {
		found := false
		for _, e := range eps {
			found = found || str(e["id"]) == "warp"
		}
		if !found {
			eps = append(eps, M{"id": "warp", "name": "WARP"})
		}
	}
	if len(eps) == 0 {
		return errors.New("没有可检测节点")
	}
	// Xray batches serialize with each other; WARP uses its own probe lock.
	for _, e := range eps {
		if str(e["id"]) != "warp" {
			if !a.probeMu.TryLock() {
				return errors.New("已有 Xray 节点检测进行中")
			}
			defer a.probeMu.Unlock()
			break
		}
	}
	if speed {
		if err := validateSpeedTargets(c, a.warpMonitorPolicy(), eps); err != nil {
			return err
		}
	}
	parallel := int(num(c["latency_concurrency"]))
	if parallel < 1 {
		parallel = 4
	}
	if speed {
		parallel = int(num(c["speed_concurrency"]))
		if parallel < 1 {
			parallel = 1
		}
	}
	gate := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	passed := 0
	var countMu sync.Mutex
	for _, endpoint := range eps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case gate <- struct{}{}:
		}
		wg.Add(1)
		go func(e M) {
			defer wg.Done()
			defer func() { <-gate }()
			identity := str(e["id"])
			a.stage(j, "正在检测："+label(str(e["name"])))
			p, cleanup, er := a.probePort(identity)
			if identity == "warp" {
				p = 0
				cleanup = func() {}
				er = nil
			}
			result := M{"ok": false, "latency": -1, "time": now(), "error": "独立探测入口未就绪"}
			if er == nil {
				defer cleanup()
				if speed {
					if identity == "warp" {
						result = a.warpBandwidth(ctx)
					} else {
						result = a.bandwidth(ctx, p, "http")
					}
				} else {
					if identity == "warp" {
						result = a.warpLatency(ctx)
					} else {
						result = measureLatency(ctx, p, str(c["latency_url"]), num(c["timeout"]))
					}
					var host string
					for _, v := range arr(native["nodes"]) {
						if obj(v)["id"] == identity {
							host = str(obj(obj(v)["outbound"])["server"])
						}
					}
					if identity == "warp" {
						host = str(a.warp()["endpoint"])
					}
					if host != "" {
						a.mu.RLock()
						oldICMP := clone(a.results[identity])
						policy := clone(a.icmp)
						a.mu.RUnlock()
						if len(ids) > 0 || num(policy["interval"]) > 0 && now()-num(oldICMP["packet_loss_at"]) >= num(policy["interval"]) {
							for k, v := range a.measureICMP(ctx, host) {
								result[k] = v
							}
						} else {
							for _, k := range []string{"packet_loss_status", "packet_loss_at", "packet_loss_ttl", "packet_loss_pct", "icmp_success_pct", "icmp_average_ms", "icmp_sent", "icmp_received"} {
								if oldICMP[k] != nil {
									result[k] = oldICMP[k]
								}
							}
						}
					}
				}
			}
			a.mu.Lock()
			old := a.results[identity]
			if old == nil {
				old = M{}
			}
			if speed {
				old["speed_time"], old["speed_ok"], old["speed_error"] = result["time"], result["ok"], result["error"]
				if yes(result["ok"]) {
					old["last_speed"], old["last_speed_time"], old["last_speed_source"] = result["speed"], result["time"], result["source"]
				}
			} else {
				for _, k := range []string{"last_speed", "last_speed_time", "last_speed_source", "speed", "speed_time", "speed_ok"} {
					if old[k] != nil {
						result[k] = old[k]
					}
				}
				result["failures"] = 0
				if !yes(result["ok"]) {
					result["failures"] = num(old["failures"]) + 1
				}
				old = result
				h := a.history[identity]
				h = append(h, M{"time": result["time"], "ok": result["ok"], "latency": result["latency"], "probe_count": result["probe_count"], "probe_successes": result["probe_successes"]})
				if len(h) > 120 {
					h = h[len(h)-120:]
				}
				a.history[identity] = h
			}
			a.results[identity] = old
			a.mu.Unlock()
			if !speed {
				_ = a.storeMeasurement(identity, false, result)
				if result["packet_loss_at"] != nil {
					_ = a.storeMeasurement(identity, true, result)
				}
			}
			if yes(result["ok"]) {
				countMu.Lock()
				passed++
				countMu.Unlock()
			}
			message := label(str(e["name"])) + "："
			if speed && yes(result["ok"]) {
				message += fmt.Sprintf("%.1f KB/s", num(result["speed"])*1024/1000)
			} else {
				message += fmt.Sprintf("%v ms", result["latency"])
			}
			if !yes(result["ok"]) {
				message += " · " + str(result["error"])
			}
			a.jobDetail(j, message)
		}(endpoint)
	}
	wg.Wait()
	a.saveMetrics()
	a.stage(j, fmt.Sprintf("已完成 %d / %d 项，成功 %d 项", len(eps), len(eps), passed))
	if !speed && len(ids) == 0 {
		a.autoSelect()
	}
	if passed == 0 {
		return errors.New("所有所选节点检测未成功，请查看详细结果")
	}
	return taskCompletionError(passed, len(eps), fmt.Sprintf("检测完成，成功%d项，失败%d项", passed, len(eps)-passed))
}
func (a *App) periodic() {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	next := time.Now().Add(5 * time.Second)
	lastInterval := -1.
	subDue := map[string]time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
			a.mu.RLock()
			c := M{"interval": a.cfg["interval"], "latency_enabled": a.cfg["latency_enabled"]}
			native := M{"subscriptions": clone(a.native["subscriptions"])}
			a.mu.RUnlock()
			interval := num(c["interval"])
			if lastInterval >= 0 && lastInterval != interval {
				next = time.Now().Add(time.Duration(max(1., interval)) * time.Second)
			}
			lastInterval = interval
			if latencyDetectionEnabled(c) && interval > 0 && (a.moduleEnabled("xray") || a.moduleEnabled("warp")) && time.Now().After(next) {
				next = time.Now().Add(time.Duration(interval) * time.Second)
				if a.probeMu.TryLock() {
					a.probeMu.Unlock()
					_, _ = a.submitAutomatic("周期节点检测", "probe.latency", func(ctx context.Context, j *Job) error { return a.detect(ctx, j, false, nil) })
				}
			}
			for _, v := range arr(native["subscriptions"]) {
				s := obj(v)
				identity := str(s["id"])
				if a.moduleEnabled("xray") && s["enabled"] != false && yes(s["auto_update"]) && time.Now().After(subDue[identity]) {
					subDue[identity] = time.Now().Add(time.Duration(max(300, num(s["interval"]))) * time.Second)
					if num(s["updated"])+num(s["interval"]) < now() {
						_, _ = a.submitAutomatic("自动更新订阅", "subscription.update", func(ctx context.Context, j *Job) error { return a.updateSubscription(ctx, j, identity, "skip") })
					}
				}
			}
		}
	}
}
func (a *App) autoSelect() {
	a.mu.Lock()
	if !yes(a.components["xray"]) || (!a.applied && !yes(a.cfg["fallback_active"])) || !latencyDetectionEnabled(a.cfg) || num(a.cfg["interval"]) == 0 {
		a.mu.Unlock()
		return
	}
	scores := a.scores()
	sort.Slice(scores, func(i, j int) bool { return num(scores[i]["score"]) > num(scores[j]["score"]) })
	current := M{}
	for _, s := range scores {
		if s["id"] == a.active {
			current = s
		}
	}
	allFailed, count := true, 0
	for _, v := range arr(a.cfg["endpoints"]) {
		e := obj(v)
		if str(e["id"]) != "warp" && yes(e["enabled"]) {
			count++
			if num(a.results[str(e["id"])]["failures"]) < num(a.cfg["failures"]) {
				allFailed = false
			}
		}
	}
	allFailed = allFailed && count > 0
	if str(a.cfg["mode"]) != "auto" && !yes(a.cfg["fallback_active"]) && !allFailed {
		a.mu.Unlock()
		return
	}
	target := ""
	for _, s := range scores {
		if str(s["id"]) != "warp" && yes(s["eligible"]) {
			if str(s["id"]) != a.active {
				emergency := num(a.results[a.active]["failures"]) >= num(a.cfg["failures"])
				if yes(a.cfg["fallback_active"]) || emergency || now()-a.lastSwitch >= num(a.cfg["hold"]) && num(s["score"])-num(current["score"]) >= num(a.cfg["improvement"]) {
					target = str(s["id"])
				}
			}
			break
		}
	}
	if (a.active != "warp" || a.applied) && allFailed && yes(a.components["warp"]) && !a.warpDisabled && yes(a.results["warp"]["ok"]) && now()-num(a.results["warp"]["time"]) < 180 {
		target = "warp"
	}
	if target == a.lastCandidate {
		a.candidateStreak++
	} else {
		a.lastCandidate = target
		a.candidateStreak = 1
	}
	confirm := target == "warp" || a.candidateStreak >= int(num(a.cfg["confirmations"])) || num(a.results[a.active]["failures"]) >= num(a.cfg["failures"])
	a.mu.Unlock()
	if target != "" && confirm {
		_, _ = a.submitAutomatic("自动切换出口", "node.switch", func(ctx context.Context, j *Job) error {
			a.mutate.Lock()
			defer a.mutate.Unlock()
			a.mu.RLock()
			allowed := a.applied || yes(a.cfg["fallback_active"])
			if target == "warp" {
				enabled, failed := 0, true
				for _, v := range arr(a.cfg["endpoints"]) {
					e := obj(v)
					if str(e["id"]) != "warp" && yes(e["enabled"]) {
						enabled++
						failed = failed && num(a.results[str(e["id"])]["failures"]) >= num(a.cfg["failures"])
					}
				}
				allowed = allowed && enabled > 0 && failed && (a.active != "warp" || a.applied)
			} else {
				allowed = allowed && (str(a.cfg["mode"]) == "auto" || yes(a.cfg["fallback_active"]))
			}
			a.mu.RUnlock()
			if !allowed {
				return errors.New("出口状态已改变，取消过期的自动切换")
			}
			if target == "warp" {
				if !yes(a.warp()["connected"]) {
					return errors.New("WARP 未连接，保留现有出口")
				}
				if err := a.proxyControlLocked(M{"enabled": false, "method": "auto"}); err != nil {
					return err
				}
				a.mu.Lock()
				a.cfg["fallback_active"] = true
				err := a.persist()
				a.mu.Unlock()
				return err
			}
			return a.proxyControlLocked(M{"selected": target, "enabled": true, "method": "auto"})
		})
	}
}

func validateSpeedTargets(config, warpPolicy M, endpoints []M) error {
	for _, endpoint := range endpoints {
		if str(endpoint["id"]) == "warp" {
			if !yes(warpPolicy["speed_enabled"]) {
				return errors.New("WARP 速度检测已关闭")
			}
		} else if value, ok := config["speed_enabled"].(bool); ok && !value {
			return errors.New("速度检测已关闭，请在 Xray 设置中启用")
		}
	}
	return nil
}
