package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
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
		if err := a.audit("diagnostic.result", diagnosticResultSummary(result), diagnosticError == nil && yes(result["ok"]), result, uiActionFromContext(ctx)); err != nil {
			diagnosticError = fmt.Errorf("诊断已执行，但结果保存失败：%w", err)
		}
	}()
	var firewallResult M
	if str(data["kind"]) == "firewall" {
		params := M{"kind": "firewall", "probe": yes(data["probe"]) && str(data["protocol"]) == "tcp"}
		query := url.Values{}
		for _, key := range []string{"driver", "source", "ingress", "protocol", "destination", "port"} {
			value := strings.TrimSpace(str(data[key]))
			query.Set(key, value)
			params[key] = value
		}
		data = params
		result["parameters"] = params
		a.stage(j, "解析目标并读取防火墙与内核路由")
		var err error
		firewallResult, err = a.network.FirewallDiagnosis(ctx, query)
		if err != nil {
			return err
		}
		a.jobDetail(j, "目标："+query.Get("destination")+" → "+str(firewallResult["resolved_ip"]))
		a.jobDetail(j, "入口："+query.Get("ingress")+" · "+str(firewallResult["zone"]))
		a.jobDetail(j, "内核路由查询："+firstNonempty(str(obj(firewallResult["route"])["dev"]), "未取得"))
		result["firewall"] = firewallResult
		result["resolved_ip"] = firewallResult["resolved_ip"]
		expected, _ := a.expectedDiagnosticPath("lan", query.Get("protocol"), query.Get("destination"), str(firewallResult["resolved_ip"]))
		result["expected"] = expected
		if !yes(params["probe"]) || query.Get("protocol") != "tcp" || net.ParseIP(str(firewallResult["resolved_ip"])).To4() == nil {
			a.jobDetail(j, "FoXray 配置预计路径："+str(result["expected"]))
			a.jobDetail(j, "客户端实际出口与 NAT 结果：本次未观测")
			result["ok"] = true
			a.stage(j, diagnosticResultSummary(result))
			return nil
		}
		result["gateway_probe"] = true
		data = M{"target": query.Get("destination"), "protocol": "tcp", "port": firewallResult["input_port"], "scope": "lan", "source": query.Get("source")}
	}
	host := str(data["target"])
	if host == "" {
		host = str(data["host"])
	}
	if host == "" || strings.ContainsAny(host, "\r\n\t /;") {
		return errors.New("目标 IP 或域名无效")
	}
	protocol := str(data["protocol"])
	if protocol != "icmp" && protocol != "tcp" && protocol != "tls" && protocol != "dns" && protocol != "dns_tcp" {
		protocol = "tls"
	}
	p := int(num(data["port"]))
	if p == 0 {
		p = 443
		if protocol == "dns" || protocol == "dns_tcp" {
			p = 53
		}
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
	var target string
	if firewallResult != nil {
		target = str(firewallResult["resolved_ip"])
	} else {
		dnsStarted := time.Now()
		ips, e := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		result["dns_ms"] = float64(time.Since(dnsStarted)) / float64(time.Millisecond)
		result["dns_lookup_performed"] = net.ParseIP(host) == nil
		if e != nil || len(ips) == 0 {
			if e != nil {
				return fmt.Errorf("目标解析失败：%w", e)
			}
			return errors.New("目标没有可用IPv4地址")
		}
		target = ips[0].String()
	}
	args := []string{"ip", "-j", "-4", "route", "get", target}
	if scope == "lan" && source != "" {
		args = append(args, "from", source, "iif", uplink())
	}
	var raw string
	if firewallResult != nil {
		encoded, _ := json.Marshal(firewallResult["route"])
		raw = string(encoded)
	} else {
		raw, _ = a.command(5, "", args...)
	}
	a.jobDetail(j, "目标："+host+" → "+target)
	a.jobDetail(j, "内核原始路由："+strings.TrimSpace(raw))
	expected, smartLANDNS := a.expectedDiagnosticPath(scope, protocol, host, target)
	a.jobDetail(j, "预计路径："+expected)
	if scope == "lan" {
		a.jobDetail(j, "LAN 为规则模拟；以下实际连接由网关发起，不能代替客户端实测")
	}
	if firewallResult == nil {
		result["parameters"] = M{"target": host, "port": p, "scope": scope, "source": source, "protocol": protocol}
	}
	result["resolved_ip"], result["route"], result["expected"] = target, raw, expected
	ok := false
	started := time.Now()
	if protocol != "icmp" {
		result["route_observer"] = a.routingWebhookStatus()
	}
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
	} else if protocol == "dns" || protocol == "dns_tcp" {
		query := firstNonempty(str(data["query"]), "www.google.com")
		obj(result["parameters"])["query"] = query
		var measured M
		var err error
		if smartLANDNS {
			measured, err = a.measureDiagnosticLANDNS(ctx, target, p, query, protocol == "dns_tcp")
			if scope != "lan" {
				measured["dns_routing_mode"] = "本机智能DNS；实际经过网关解析器与上游"
			}
		} else {
			measured, err = measureDiagnosticDNS(ctx, target, p, query, protocol == "dns_tcp")
		}
		for key, value := range measured {
			result[key] = value
		}
		ok = err == nil
		if err != nil {
			result["test_error"] = err.Error()
			a.jobDetail(j, err.Error())
		} else {
			a.jobDetail(j, fmt.Sprintf("DNS应答验证：%s；查询 %s；%.2f ms", str(result["dns_server"]), query, num(result["dns_query_ms"])))
		}
	} else if protocol == "tls" {
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

	} else {
		start := time.Now()
		dial := net.Dialer{Timeout: 8 * time.Second}
		conn, err := dial.DialContext(ctx, "tcp", net.JoinHostPort(target, strconv.Itoa(p)))
		if err == nil {
			defer conn.Close()
			result["tcp_ms"] = float64(time.Since(start)) / float64(time.Millisecond)
			result["tcp_connected"] = true
			result["local_source"] = conn.LocalAddr().String()
			result["verification_scope"] = "tcp_connect_only"
			result["target_response_verified"] = false
			ok = true
			a.jobDetail(j, fmt.Sprintf("TCP建连：%.2f ms；此项不证明目标TLS或HTTP可用", num(result["tcp_ms"])))
		} else {
			result["test_error"] = err.Error()
			a.jobDetail(j, "TCP连接未完成："+err.Error())
		}
	}

	if protocol != "icmp" {
		node := M{"status": "unconfirmed", "note": "未收到本次连接匹配的核心事件；不推断实际出站"}
		if protocol == "tcp" {
			node["note"] = "纯TCP测试不发送应用数据，可能尚未触发TUN后的核心路由；未观测到事件时不推断实际出站"
		}
		candidates := arr(result["probe_connections"])
		if source := str(result["local_source"]); source != "" {
			candidates = append(candidates, M{"network": "tcp", "source": source, "target": net.JoinHostPort(target, strconv.Itoa(p))})
		}
		if event, found := a.diagnosticRouteCandidates(ctx, candidates, started); found {
			node = a.observedDiagnosticNode(event)
		}

		node["diagnostic_scope"] = scope
		result["actual_node"] = node
		pathHost, pathPort := host, p
		if address := str(result["dns_actual_upstream"]); address != "" {
			if h, port, err := net.SplitHostPort(address); err == nil {
				if number, e := strconv.Atoi(port); e == nil {
					pathHost, pathPort = h, number
				}
			}
		}
		result["observed_path"] = diagnosticObservedPath(node, pathHost, pathPort)
		result["path_status"] = node["status"]
		a.jobDetail(j, "本次连接路径："+str(result["observed_path"]))
	}
	result["ok"] = ok

	if !ok {
		return errors.New(firstNonempty(str(result["test_error"]), "ICMP未获得有效回应"))
	}
	a.stage(j, diagnosticResultSummary(result)+"；结果与日志已显示")
	return nil
}

func (a *App) expectedDiagnosticPath(scope, protocol, host, target string) (string, bool) {
	a.mu.RLock()
	enabled, active := a.applied, a.active
	capture := yes(a.cfg["capture_host"])
	profile := str(a.cfg["route_profile"])
	smartLAN := yes(a.cfg["lan_dns_intercept"])
	exclusions := M{"vpn_exclusions": clone(a.cfg["vpn_exclusions"])}
	if scope == "lan" {
		capture = yes(a.cfg["capture_lan"])
	}
	a.mu.RUnlock()
	expected := "系统原路径"
	if enabled && capture && !defaultCaptureBypass(target) && protocol != "icmp" {
		expected = "Xray → " + active + "（需结合分流模式 " + profile + " 与排除规则判断）"
	}
	smartLANDNS := scope == "lan" && capture && enabled && smartLAN
	if smartLANDNS && (protocol == "dns" || protocol == "dns_tcp") {
		expected = "LAN智能DNS接管：自定义规则优先，上游由DNS策略选择；PC填写的DNS地址不决定实际上游。本次仍为网关发起的规则模拟测试"
	}
	if vpnExcludedTarget(exclusions, host, target) {
		expected = "VPN自定义排除 → 系统直连"
	}
	return expected, smartLANDNS
}

var _ = filepath.Separator

func diagnosticResultSummary(result M) string {
	p := obj(result["parameters"])
	if str(p["kind"]) == "firewall" {
		port := "—"
		if str(p["port"]) != "" {
			port = str(p["port"])
		}
		summary := strings.Join([]string{str(p["source"]), str(p["destination"]), strings.ToUpper(str(p["protocol"])), port}, " · ")
		if err := str(result["error"]); err != "" {
			return summary + " · " + err
		}
		if yes(result["gateway_probe"]) {
			return summary + " · 网关 TCP 建连完成"
		}
		return summary + " · 完成"
	}
	target := str(p["target"])
	if target == "" {
		target = str(p["host"])
	}
	outcome := "未通过"
	if yes(result["ok"]) {
		outcome = "通过"
	}
	if str(p["protocol"]) == "dns" || str(p["protocol"]) == "dns_tcp" {
		if yes(result["dns_response_verified"]) {
			return fmt.Sprintf("%s · DNS有效应答 %.2f ms · %s", target, num(result["dns_query_ms"]), outcome)
		}
		return fmt.Sprintf("%s · DNS查询未通过 · %s", target, firstNonempty(str(result["test_error"]), str(result["error"])))
	}
	if yes(result["http_response_received"]) && yes(result["tls_verified"]) {
		return fmt.Sprintf("%s · HTTPS首字节 %.2f ms · %s", target, num(result["http_ttfb_ms"]), outcome)
	}
	if str(p["protocol"]) == "tls" {
		return fmt.Sprintf("%s · HTTPS未通过 · %s", target, firstNonempty(str(result["test_error"]), str(result["error"])))
	}
	if result["tcp_ms"] != nil {
		observation := "实际出站未确认"
		if str(obj(result["actual_node"])["status"]) == "observed" {
			observation = "实际出站已确认"
		}
		return fmt.Sprintf("%s · TCP建连完成 %.2f ms · 目标应用未验证；%s", target, num(result["tcp_ms"]), observation)
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
