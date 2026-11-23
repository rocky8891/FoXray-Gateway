package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var scoutProtocols = map[string][]int{"wg": {2408, 500, 1701, 4500}, "masque": {443, 500, 1701, 4500, 4443, 8443, 8095}, "masque-h2": {443}}

func scoutProtocol(m M) string {
	if str(m["transport"]) == "WireGuard" || str(m["protocol"]) == "WireGuard" {
		return "wg"
	}
	if str(m["transport"]) == "MASQUE TCP" || str(m["masque_options"]) == "h2-only" {
		return "masque-h2"
	}
	return "masque"
}
func (a *App) scoutStatus() M {
	m := M{"collections": M{"wg": []any{}, "masque": []any{}, "masque-h2": []any{}}, "previous": M{}, "job": M{"status": "idle"}}
	_ = readJSON(filepath.Join(a.base, "warp-scout-results.json"), &m)
	current := a.warp()
	protocol := scoutProtocol(current)
	endpoint := firstNonempty(str(current["endpoint_address"]), str(current["override"]))
	if endpoint != "" {
		collection := obj(m["collections"])
		rows := arr(collection[protocol])
		found := false
		legacy := M{}
		_ = readJSON(filepath.Join(a.base, "warp-endpoint-results.json"), &legacy)
		for _, v := range rows {
			r := obj(v)
			r["current"] = false
			if str(r["endpoint"]) == endpoint {
				r["current"] = yes(current["connected"])
				found = true
				if speed := obj(legacy[endpoint]); speed["speed_at"] != nil && num(speed["speed_at"]) > num(r["speed_at"]) {
					r["speed_kib_s"], r["speed_at"], r["speed_origin"] = speed["speed_kib_s"], speed["speed_at"], "official"
				}
			}
		}
		if !found {
			row := M{"endpoint": endpoint, "protocol": protocol, "current": yes(current["connected"]), "pinned": true}
			speed := obj(legacy[endpoint])
			for _, k := range []string{"speed_kib_s", "speed_at", "speed_source"} {
				row[k] = speed[k]
			}
			rows = append([]any{row}, rows...)
		}
		collection[protocol] = rows
		m["collections"] = collection
	}
	records := M{}
	_ = readJSON(filepath.Join(a.base, "warp-endpoint-results.json"), &records)
	m["current_metrics"] = obj(records[endpoint])
	m["current"] = current
	m["installed"] = regular(filepath.Join(a.base, "probe-tools", "warpscout"))
	m["protocols"] = scoutProtocols
	official := M{}
	_ = readJSON(filepath.Join(a.base, "warp-official-results.json"), &official)
	for kind, values := range obj(m["collections"]) {
		for _, value := range arr(values) {
			row := obj(value)
			if result := obj(official[kind+":"+str(row["endpoint"])]); len(result) > 0 {
				row["official_ok"], row["official_at"] = result["connected"], result["checked_at"]
			}
			if yes(row["current"]) {
				row["official_ok"] = true
				row["official_at"] = now()
			}
		}
	}
	policy := M{}
	_ = readJSON(filepath.Join(a.base, "warp-endpoints.json"), &policy)
	m["policy"] = policy
	return m
}
func endpointValid(address string) bool {
	host, p, e := net.SplitHostPort(address)
	if e != nil {
		return false
	}
	ip := net.ParseIP(host)
	port, e := strconv.Atoi(p)
	return e == nil && port >= 1 && port <= 65535 && ip != nil && ip.To4() != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback()
}
func (a *App) warpAction(path string, d M, uiAction ...string) (M, error) {
	if !yes(a.components["warp"]) {
		return nil, errors.New("WARP 未部署")
	}
	if path == "/api/warp/policy" {
		if _, e := integer(d["interval"], 60, 86400); e != nil {
			return nil, e
		}
		return M{"ok": true}, writeJSON(filepath.Join(a.base, "warp-endpoints.json"), d)
	}
	op := str(d["operation"])
	protocol := str(d["protocol"])
	if protocol == "" {
		protocol = "masque-h2"
	}
	if _, ok := scoutProtocols[protocol]; !ok {
		return nil, errors.New("隧道类型无效")
	}
	p := int(num(d["port"]))
	if p == 0 {
		p = scoutProtocols[protocol][0]
	}
	validPort := false
	for _, v := range scoutProtocols[protocol] {
		validPort = validPort || v == p
	}
	if !validPort {
		return nil, errors.New("该协议的端口不支持")
	}
	if op == "discover" || op == "refresh" {
		job, err := a.submitUI(uiActionID(uiAction...), "WARP 优选端点发现", "warp.discover", func(ctx context.Context, j *Job) error {
			return a.discoverWarp(ctx, j, protocol, p)
		})
		return M{"ok": err == nil, "id": job}, err
	}
	if op == "verify-best" {
		if !yes(d["confirmed"]) {
			return nil, errors.New("实际优选会短暂切换 WARP，请先确认")
		}
		job, err := a.submitUI(uiActionID(uiAction...), "WARP 官方端点验证优选", "warp.verify", func(ctx context.Context, j *Job) error { return a.verifyBestWarp(ctx, j, protocol) })
		return M{"ok": err == nil, "id": job}, err
	}
	if op == "restore" {
		if !a.scoutOperate.TryLock() {
			return nil, errors.New("候选检测进行中，请等待完成后恢复候选")
		}
		defer a.scoutOperate.Unlock()
		m := a.scoutStatus()
		collections := obj(m["collections"])
		previous := obj(m["previous"])
		if len(arr(previous[protocol])) == 0 {
			return nil, errors.New("没有上次候选列表")
		}
		collections[protocol], previous[protocol] = previous[protocol], collections[protocol]
		m["collections"], m["previous"] = collections, previous
		return M{"ok": true}, writeJSON(filepath.Join(a.base, "warp-scout-results.json"), m)
	}
	if op == "test-current" {
		metrics, err := validateWarpMetrics(d, a.warpMonitorPolicy())
		if err != nil {
			return nil, err
		}
		job, err := a.submitUI(uiActionID(uiAction...), "WARP 当前隧道检测", "warp.current-test", func(ctx context.Context, j *Job) error { return a.testCurrentWarp(ctx, j, metrics) })
		return M{"ok": err == nil, "id": job}, err
	}
	if op == "current" || op == "speed-current" {
		if !yes(a.warpMonitorPolicy()["speed_enabled"]) {
			return nil, errors.New("WARP 速度检测已关闭")
		}
		j, e := a.submitUI(uiActionID(uiAction...), "WARP 当前隧道测速", "probe.warp_speed", func(ctx context.Context, j *Job) error {
			current := a.warp()
			if !yes(current["connected"]) {
				return errors.New("当前 WARP 隧道未连接，未执行下载")
			}
			result := a.warpBandwidth(ctx)
			if !yes(result["ok"]) {
				return errors.New(str(result["error"]))
			}
			endpoint := firstNonempty(str(current["endpoint_address"]), str(current["override"]))
			if endpoint == "" {
				return errors.New("未确认实际端点，未将下载结果写入候选列表")
			}
			records := M{}
			_ = readJSON(filepath.Join(a.base, "warp-endpoint-results.json"), &records)
			r := obj(records[endpoint])
			r["speed_kib_s"], r["speed_at"], r["speed_source"], r["speed_protocol"], r["tunnel_ok"] = result["speed"], result["time"], result["source"], current["transport"], true
			records[endpoint] = r
			if e := writeJSON(filepath.Join(a.base, "warp-endpoint-results.json"), records); e != nil {
				return e
			}
			a.mu.Lock()
			old := a.results["warp"]
			if old == nil {
				old = M{}
			}
			old["last_speed"], old["last_speed_time"], old["last_speed_source"] = result["speed"], result["time"], result["source"]
			a.results["warp"] = old
			a.mu.Unlock()
			a.saveMetrics()
			a.stage(j, fmt.Sprintf("当前隧道：%.1f KB/s · %s", num(result["speed"])*1024/1000, endpoint))
			return nil
		})
		return M{"ok": e == nil, "id": j}, e
	}
	if op == "activate" {
		endpoint := str(d["endpoint"])
		if endpoint != "" && !endpointValid(endpoint) {
			return nil, errors.New("端点无效")
		}
		j, e := a.submitUI(uiActionID(uiAction...), "WARP 端点切换", "warp.activate", func(ctx context.Context, j *Job) error { return a.activateWarp(ctx, j, protocol, endpoint) })
		return M{"ok": e == nil, "id": j}, e
	}
	if op == "scan" {
		for _, metric := range arr(d["metrics"]) {
			if str(metric) == "speed" && !yes(a.warpMonitorPolicy()["speed_enabled"]) {
				return nil, errors.New("WARP 速度检测已关闭，请取消下载带宽指标")
			}
		}
		targets := []string{}
		for _, v := range arr(d["endpoints"]) {
			target := str(v)
			if !endpointValid(target) {
				return nil, errors.New("候选端点无效")
			}
			targets = append(targets, target)
		}
		if len(targets) < 1 || len(targets) > 10 {
			return nil, errors.New("每次选择1至10个端点")
		}
		metrics, err := validateWarpMetrics(d, a.warpMonitorPolicy())
		if err != nil {
			return nil, err
		}
		j, e := a.submitUI(uiActionID(uiAction...), "WARP 候选端点检测", "warp.scan", func(ctx context.Context, j *Job) error { return a.scanWarp(ctx, j, protocol, targets, metrics) })
		return M{"ok": e == nil, "id": j}, e
	}
	return nil, errors.New("WARP 操作不支持")
}
func (a *App) activateWarp(ctx context.Context, j *Job, protocol, endpoint string) error {
	a.warpOperate.Lock()
	defer a.warpOperate.Unlock()
	return a.activateWarpLocked(ctx, j, protocol, endpoint)
}
func (a *App) activateWarpLocked(ctx context.Context, j *Job, protocol, endpoint string) error {
	if e := a.verifyWarp(); e != nil {
		return e
	}
	a.jobDetail(j, fmt.Sprintf("目标端点：%s；隧道类型：%s", endpoint, protocol))
	if protocol == "masque-h2" && endpoint != "" {
		a.stage(j, "通过独立认证隧道预检候选端点，原隧道保持连接")
		if err := a.preflightWarpCandidate(ctx, endpoint); err != nil {
			return err
		}
		a.jobDetail(j, "独立候选预检通过；官方客户端连接仍需单独确认")
	}
	old := a.warp()
	set := func(proto, option, target string) error {
		_, _ = a.warpCommand(ctx, "disconnect")
		if _, e := a.warpCommand(ctx, "tunnel", "protocol", "set", proto); e != nil {
			return e
		}
		if proto == "MASQUE" {
			if _, e := a.warpCommand(ctx, "tunnel", "masque-options", "set", option); e != nil {
				return e
			}
		}
		if target != "" {
			if _, e := a.warpCommand(ctx, "tunnel", "endpoint", "set", target); e != nil {
				return e
			}
		} else {
			if _, e := a.warpCommand(ctx, "tunnel", "endpoint", "reset"); e != nil {
				return e
			}
		}
		_, e := a.warpCommand(ctx, "connect")
		return e
	}
	want, option := "MASQUE", "h3-only"
	if protocol == "wg" {
		want = "WireGuard"
	} else if protocol == "masque-h2" {
		option = "h2-only"
	}
	a.stage(j, "切换隔离 WARP 隧道；Xray 与 VPN 路由保持")
	e := set(want, option, endpoint)
	if e == nil {
		e = a.waitWarp(ctx, j, 45)
	}
	if e == nil {
		actual := a.warp()
		if endpoint != "" && !warpEndpointConfirmed(actual, endpoint) {
			e = errors.New("客户端未确认目标端点")
		}
	}
	if e != nil {
		a.recordWarpOfficial(protocol, endpoint, false)
		a.jobDetail(j, "官方客户端连接未通过："+e.Error())
		restoreCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if restoreErr := a.restoreWarpConnection(restoreCtx, j, old); restoreErr != nil {
			a.jobDetail(j, restoreErr.Error())
			return errors.New(e.Error() + "；回退未完成，请检查 WARP 状态")
		}
		if yes(old["connected"]) {
			a.jobDetail(j, "已确认原隧道恢复连接")
			return errors.New(e.Error() + "；已恢复原端点并确认原隧道连接")
		}
		return errors.New(e.Error() + "；已恢复原端点设置，原隧道保持断开")
	}
	policy := M{}
	_ = readJSON(filepath.Join(a.base, "warp-endpoints.json"), &policy)
	if j.Source != "automatic" && j.Group != "warp.verify" {
		policy["auto"] = false
	}
	if err := writeJSON(filepath.Join(a.base, "warp-endpoints.json"), policy); err != nil {
		return errors.New("目标隧道已连接，但自动选择策略保存失败，请核对设置")
	}
	a.stage(j, "已确认目标隧道连接")
	a.recordWarpOfficial(protocol, endpoint, true)
	return nil
}
func durationMS(s string) any {
	units := []struct {
		suffix     string
		multiplier float64
	}{{"ms", 1}, {"µs", .001}, {"us", .001}, {"s", 1000}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, e := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if e == nil {
				return n * u.multiplier
			}
		}
	}
	return nil
}
func parseScoutReport(raw, protocol, target string) M {
	r := M{"endpoint": target, "protocol": protocol, "tested": true, "tested_at": now(), "durable": false}
	torn := strings.Contains(raw, "torn down")
	for _, line := range textLines(raw) {
		parts := strings.Fields(line)
		if len(parts) < 4 || parts[0] != target {
			continue
		}
		r["handshake"], r["durable"] = true, !torn
		if strings.HasSuffix(parts[3], "%") {
			loss, e := strconv.ParseFloat(strings.TrimSuffix(parts[3], "%"), 64)
			if e == nil {
				r["loss_pct"] = loss
				r["rtt_ms"] = durationMS(parts[2])
			}
		}
		break
	}
	return r
}
func (a *App) scanWarp(ctx context.Context, j *Job, protocol string, targets, metrics []string) error {
	if !a.scoutOperate.TryLock() {
		return errors.New("已有独立端点检测进行中")
	}
	defer a.scoutOperate.Unlock()
	env, cleanup, err := a.prepareScoutProbe()
	if err != nil {
		return err
	}
	defer cleanup()
	speed, latency, trueLatency := false, false, false
	for _, m := range metrics {
		speed = speed || m == "speed"
		trueLatency = trueLatency || m == "true_latency"
		latency = latency || m == "latency" || m == "loss"
	}
	state := a.scoutStatus()
	collections := obj(state["collections"])
	rows := arr(collections[protocol])
	passed, speedFailed, successfulMetrics := 0, 0, 0
	for index, target := range targets {
		if ctx.Err() != nil {
			return errors.New("检测已取消")
		}
		a.stage(j, fmt.Sprintf("检测端点 %d / %d · %s", index+1, len(targets), target))
		r := a.probeScoutCandidate(ctx, env, protocol, target, latency)
		var old M
		for _, v := range rows {
			if obj(v)["endpoint"] == target {
				old = obj(v)
			}
		}
		if yes(r["durable"]) {
			passed++
		}
		r = mergeScoutMetrics(old, r, metrics)
		if trueLatency {
			measured := M{"ok": false, "error": "候选隧道未通过，未发起真连接"}
			if yes(r["durable"]) {
				measured = a.scoutTrueLatency(ctx, protocol, target, env)
			}
			applyScoutTrueLatency(r, measured)
		}
		if speed {
			bandwidth := M{"ok": false, "error": "候选隧道未通过，未执行下载"}
			if yes(r["durable"]) {
				a.stage(j, fmt.Sprintf("下载测速 %d / %d · %s", index+1, len(targets), target))
				bandwidth = a.scoutBandwidth(ctx, protocol, target, env.binary, env.account, env.uid, env.gid)
			}
			applyScoutBandwidth(r, bandwidth)
			if !yes(bandwidth["ok"]) {
				speedFailed++
			}
		}
		for _, metric := range metrics {
			if yes(r[metric+"_attempt_ok"]) {
				successfulMetrics++
			}
		}
		next := []any{r}
		for _, v := range rows {
			if obj(v)["endpoint"] != target {
				next = append(next, v)
			}
		}
		rows = next
		collections[protocol] = rows
		state["collections"] = collections
		state["items"] = rows
		state["job"] = M{"status": "running", "started": j.Started, "completed": index + 1, "total": len(targets), "metrics": metrics}
		if err := writeJSON(filepath.Join(a.base, "warp-scout-results.json"), state); err != nil {
			return errors.New("候选检测结果保存失败")
		}
		a.jobDetail(j, scoutResultSummary(target, r, metrics))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		l, r := obj(rows[i]), obj(rows[j])
		if yes(l["durable"]) != yes(r["durable"]) {
			return yes(l["durable"])
		}
		if l["loss_pct"] == nil {
			return false
		}
		if r["loss_pct"] == nil {
			return true
		}
		if num(l["loss_pct"]) != num(r["loss_pct"]) {
			return num(l["loss_pct"]) < num(r["loss_pct"])
		}
		return num(l["rtt_ms"]) < num(r["rtt_ms"])
	})
	collections[protocol] = rows
	status := "success"
	resultErr := taskCompletionError(successfulMetrics, len(targets)*len(metrics), fmt.Sprintf("完成%d个端点，检测成功%d项／共%d项；请查看逐项结果", len(targets), successfulMetrics, len(targets)*len(metrics)))
	if resultErr != nil {
		status = "error"
		if successfulMetrics > 0 {
			status = "partial"
		}
	}
	state["job"] = M{"status": status, "finished": now(), "completed": len(targets), "total": len(targets), "metrics": metrics, "speed_failed": speedFailed}
	if err := writeJSON(filepath.Join(a.base, "warp-scout-results.json"), state); err != nil {
		return errors.New("候选检测结果保存失败")
	}
	a.stage(j, fmt.Sprintf("完成%d个端点，独立隧道通过%d个；官方端点未切换", len(targets), passed))
	return resultErr
}

func (a *App) scoutBandwidth(ctx context.Context, protocol, target, binary, account, uid, gid string) M {
	policy := a.warpMonitorPolicy()
	return a.scoutSOCKS(ctx, protocol, target, binary, account, uid, gid, num(policy["speed_timeout"]), func(child context.Context, p int) M { return bandwidthWithPolicy(child, p, "socks5", policy) })
}

func (a *App) scoutSOCKS(ctx context.Context, protocol, target, binary, account, uid, gid string, seconds float64, probe func(context.Context, int) M) M {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return M{"ok": false, "error": "无法分配探测端口"}
	}
	p := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	args := []string{"setpriv", "--reuid", uid, "--regid", gid, "--clear-groups", "--inh-caps=+net_raw", "--ambient-caps=+net_raw", binary, "socks", "--proto", protocol, "--endpoint", target, "--listen", "127.0.0.1", "--port", strconv.Itoa(p), "--interface", uplink(), "--account", account, "--timeout", "4"}
	args = append(args, scoutMASQUEArgs(protocol)...)
	child, cancel := context.WithTimeout(ctx, time.Duration(seconds+20)*time.Second)
	done := make(chan struct{})
	go func() { _, _ = a.runner.Run(child, args, ""); close(done) }()
	defer func() { cancel(); <-done }()
	for i := 0; i < 100; i++ {
		select {
		case <-child.Done():
			return M{"ok": false, "error": "已取消或超时"}
		case <-done:
			return M{"ok": false, "error": "独立隧道启动失败"}
		default:
		}
		c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), 100*time.Millisecond)
		if e == nil {
			_ = c.Close()
			return probe(child, p)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return M{"ok": false, "error": "独立 SOCKS 入口未就绪"}
}
