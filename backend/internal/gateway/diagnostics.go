package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) bypassStatus() M {
	a.mu.RLock()
	nodes := clone(arr(a.native["nodes"]))
	manual := clone(arr(a.cfg["bypass_ips"]))
	a.mu.RUnlock()
	rows := []M{}
	seen := map[string]int{}
	for _, v := range nodes {
		host := str(obj(obj(v)["outbound"])["server"])
		if host != "" {
			seen[host]++
		}
	}
	for host, count := range seen {
		row := M{"host": host, "references": count, "ips": []string{}}
		if ip, e := a.resolveEntry(host); e == nil {
			row["ips"] = []string{ip + "/32"}
			row["status"] = "resolved"
		} else {
			row["status"] = "unresolved"
		}
		rows = append(rows, row)
	}
	return M{"targets": rows, "manual": manual, "refresh_seconds": 300}
}
func (a *App) diagnose(ctx context.Context, j *Job, data M) (diagnosticError error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := M{"parameters": clone(data), "job_id": j.ID, "ok": false}
	defer func() {
		if diagnosticError != nil {
			result["error"] = diagnosticError.Error()
		}
		a.jobs.mu.RLock()
		result["steps"] = append([]string{}, j.Details...)
		a.jobs.mu.RUnlock()
		if err := a.audit("diagnostic.result", diagnosticResultSummary(result), diagnosticError == nil && yes(result["ok"]), result); err != nil {
			diagnosticError = fmt.Errorf("诊断已执行，但结果保存失败：%w", err)
		}
	}()
	host := str(data["target"])
	if host == "" {
		host = str(data["host"])
	}
	if host == "" || strings.ContainsAny(host, "\r\n\t /;") {
		return errors.New("目标 IP 或域名无效")
	}
	protocol := str(data["protocol"])
	if protocol != "icmp" && protocol != "tcp" && protocol != "tls" {
		protocol = "tls"
	}
	p := int(num(data["port"]))
	if p == 0 {
		p = 443
	}
	if p < 1 || p > 65535 {
		return errors.New("端口无效")
	}
	scope := str(data["scope"])
	source := str(data["source"])
	if source != "" && net.ParseIP(source) == nil {
		return errors.New("来源 IP 无效")
	}
	a.stage(j, "解析目标并查询当前内核路径")
	dnsStarted := time.Now()
	ips, e := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	result["dns_ms"] = float64(time.Since(dnsStarted)) / float64(time.Millisecond)
	if e != nil || len(ips) == 0 {
		if e != nil {
			return fmt.Errorf("目标解析失败：%w", e)
		}
		return errors.New("目标没有可用IPv4地址")
	}
	target := ips[0].String()
	args := []string{"ip", "-j", "-4", "route", "get", target}
	if scope == "lan" && source != "" {
		args = append(args, "from", source, "iif", "ens16")
	}
	raw, _ := a.command(5, "", args...)
	a.jobDetail(j, "目标："+host+" → "+target)
	a.jobDetail(j, "内核原始路由："+strings.TrimSpace(raw))
	a.mu.RLock()
	enabled, active := a.applied, a.active
	capture := yes(a.cfg["capture_host"])
	profile := str(a.cfg["route_profile"])
	exclusions := M{"vpn_exclusions": clone(a.cfg["vpn_exclusions"])}
	if scope == "lan" {
		capture = yes(a.cfg["capture_lan"])
	}
	a.mu.RUnlock()
	expected := "系统原路径"
	if enabled && capture && !defaultCaptureBypass(target) && protocol != "icmp" {
		expected = "Xray → " + active + "（需结合分流模式 " + profile + " 与排除规则判断）"
	}
	if vpnExcludedTarget(exclusions, host, target) {
		expected = "VPN自定义排除 → 系统直连"
	}
	a.jobDetail(j, "预计路径："+expected)
	if scope == "lan" {
		a.jobDetail(j, "LAN 为规则模拟；以下实际连接由网关发起，不能代替客户端实测")
	}
	result["parameters"] = M{"target": host, "port": p, "scope": scope, "source": source, "protocol": protocol}
	result["resolved_ip"], result["route"], result["expected"] = target, raw, expected
	ok := false
	if protocol == "icmp" {
		child, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		text, err := a.runner.Run(child, []string{"ping", "-4", "-n", "-q", "-c", "3", "-s", "32", "-W", "2", target}, "")
		if matches := pingCount.FindStringSubmatch(text); len(matches) == 3 {
			sent, _ := strconv.Atoi(matches[1])
			received, _ := strconv.Atoi(matches[2])
			result["icmp_success_pct"] = float64(received) / float64(sent) * 100
			result["icmp_average_ms"] = -1.
			if m := pingAverage.FindStringSubmatch(text); len(m) == 2 {
				result["icmp_average_ms"], _ = strconv.ParseFloat(m[1], 64)
			}
			a.jobDetail(j, fmt.Sprintf("ICMP：成功率 %v%% · 平均 %v ms", result["icmp_success_pct"], result["icmp_average_ms"]))
			ok = received > 0
		}
		if err != nil && !ok {
			a.jobDetail(j, "ICMP 无有效回应，不等于 HTTPS 不可用")
		}
	} else if protocol == "tls" {
		result["route_observer"] = a.routingWebhookStatus()
		started := time.Now()
		measured, err := measureDiagnosticHTTPS(ctx, host, target, p, nil)
		for key, value := range measured {
			result[key] = value
		}
		ok = err == nil
		if err != nil {
			result["test_error"] = err.Error()
			a.jobDetail(j, err.Error())
		}
		if result["tcp_ms"] != nil {
			a.jobDetail(j, fmt.Sprintf("本地TCP接管／建连：%.2f ms（不作为目标访问延迟）", num(result["tcp_ms"])))
		}
		if yes(result["tls_verified"]) {
			a.jobDetail(j, fmt.Sprintf("目标TLS证书校验通过；握手 %.2f ms", num(result["tls_handshake_ms"])))
		}
		if yes(result["http_response_received"]) {
			a.jobDetail(j, fmt.Sprintf("目标HTTPS首字节：%.2f ms；HTTP %v", num(result["http_ttfb_ms"]), result["http_status"]))
		}
		node := M{"status": "unconfirmed"}
		if source := str(result["local_source"]); source != "" {
			if event, found := a.diagnosticRouteEvent(ctx, source, net.JoinHostPort(target, strconv.Itoa(p)), started); found {
				node = a.observedDiagnosticNode(event)
			}
		}
		result["actual_node"] = node
		result["observed_path"] = diagnosticObservedPath(node, host, p)
		result["path_status"] = node["status"]
		a.jobDetail(j, "本次连接路径："+str(result["observed_path"]))
	} else {
		start := time.Now()
		dial := net.Dialer{Timeout: 8 * time.Second}
		conn, err := dial.DialContext(ctx, "tcp", net.JoinHostPort(target, strconv.Itoa(p)))
		if err == nil {
			defer conn.Close()
			result["tcp_ms"] = float64(time.Since(start)) / float64(time.Millisecond)
			result["tcp_connected"] = true
			ok = true
			a.jobDetail(j, fmt.Sprintf("TCP建连：%.2f ms；此项不证明目标TLS或HTTP可用", num(result["tcp_ms"])))
		} else {
			result["test_error"] = err.Error()
			a.jobDetail(j, "TCP连接未完成："+err.Error())
		}
	}

	result["ok"] = ok

	if !ok {
		return errors.New(firstNonempty(str(result["test_error"]), "ICMP未获得有效回应"))
	}
	a.stage(j, diagnosticResultSummary(result)+"；结果与日志已显示")
	return nil
}

var _ = filepath.Separator

func diagnosticResultSummary(result M) string {
	p := obj(result["parameters"])
	target := str(p["target"])
	if target == "" {
		target = str(p["host"])
	}
	outcome := "未通过"
	if yes(result["ok"]) {
		outcome = "通过"
	}
	if yes(result["http_response_received"]) && yes(result["tls_verified"]) {
		return fmt.Sprintf("%s · HTTPS首字节 %.2f ms · %s", target, num(result["http_ttfb_ms"]), outcome)
	}
	if str(p["protocol"]) == "tls" {
		return fmt.Sprintf("%s · HTTPS未通过 · %s", target, firstNonempty(str(result["test_error"]), str(result["error"])))
	}
	if result["tcp_ms"] != nil {
		return fmt.Sprintf("%s · 仅TCP建连 %.2f ms · %s", target, num(result["tcp_ms"]), outcome)
	}
	if result["icmp_success_pct"] != nil {
		return fmt.Sprintf("%s · ICMP成功率 %v%% · %s", target, result["icmp_success_pct"], outcome)
	}
	if e := str(result["error"]); e != "" {
		return label(target) + " · " + e
	}
	return label(target) + " · " + outcome
}
func (a *App) diagnosticResult(jobID string) M {
	var identity string
	_ = a.logdb.QueryRow("SELECT id FROM operations WHERE action='diagnostic.result' AND json_extract(details,'$.job_id')=? ORDER BY t DESC LIMIT 1", jobID).Scan(&identity)
	result := M{"result": a.operation(identity)}
	a.jobs.mu.RLock()
	defer a.jobs.mu.RUnlock()
	for _, j := range a.jobs.items {
		if j.ID == jobID {
			result["task"] = M{"id": j.ID, "status": j.Status, "message": j.Message, "started": j.Started, "finished": j.Finished}
			break
		}
	}
	return result
}
