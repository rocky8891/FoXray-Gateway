package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type firewallFlow struct {
	source, destination netip.Addr
	protocol, ingress   string
	port                int
}

func firewallFlowInput(q url.Values) (firewallFlow, error) {
	var f firewallFlow
	var a, b error
	f.source, a = netip.ParseAddr(q.Get("source"))
	f.destination, b = netip.ParseAddr(q.Get("destination"))
	f.protocol, f.ingress = q.Get("protocol"), q.Get("ingress")
	if a != nil || b != nil || f.source.Zone() != "" || f.destination.Zone() != "" || f.source.Is4In6() || f.destination.Is4In6() || f.source.Is4() != f.destination.Is4() || iface(f.ingress) != nil {
		return f, errors.New("请填写同一地址族的源 IP、目标 IP 和有效入口接口")
	}
	switch f.protocol {
	case "tcp", "udp":
		p, err := strconv.Atoi(q.Get("port"))
		if err != nil || p < 1 || p > 65535 {
			return f, errors.New("目标端口须为 1 至 65535")
		}
		f.port = p
	case "icmp":
	default:
		return f, errors.New("请选择 TCP、UDP 或 ICMP")
	}
	return f, nil
}

func firewallAddressMatch(value string, address netip.Addr) bool {
	if value == "" {
		return true
	}
	_, prefix, err := canonicalSource(value)
	return err == nil && prefix.Contains(address)
}

func firewallPortBounds(value string) (int, int, bool) {
	if value == "" {
		return 1, 65535, true
	}
	low, high, rangeValue := strings.Cut(strings.ReplaceAll(value, ":", "-"), "-")
	if !rangeValue {
		high = low
	}
	a, e1 := strconv.Atoi(low)
	b, e2 := strconv.Atoi(high)
	return a, b, e1 == nil && e2 == nil && a > 0 && b >= a && b <= 65535
}

func firewallFlowMatches(r M, f firewallFlow) bool {
	family, proto := str(r["family"]), strings.ToLower(str(r["protocol"]))
	if (family == "ipv4" || family == "ip") && !f.source.Is4() || (family == "ipv6" || family == "ip6") && !f.source.Is6() {
		return false
	}
	if proto != "" && proto != "all" && proto != f.protocol && !(proto == "tcpudp" && (f.protocol == "tcp" || f.protocol == "udp")) {
		return false
	}
	if !firewallAddressMatch(str(r["source"]), f.source) || !firewallAddressMatch(str(r["destination"]), f.destination) {
		return false
	}
	if in := str(r["in_interface"]); in != "" && in != f.ingress {
		return false
	}
	a, b, valid := firewallPortBounds(str(r["port"]))
	return valid && (str(r["port"]) == "" || (f.port >= a && f.port <= b))
}

func firewallStage(r M) int {
	priority := int(num(r["priority"]))
	if priority < 0 {
		return priority - 10
	}
	if priority > 0 {
		return priority + 10
	}
	if action := str(r["action"]); action == "drop" || action == "reject" {
		return -1
	}
	return 0
}

func firewallFlowCandidates(rows []any, f firewallFlow, driver string) ([]M, []string) {
	candidates := []M{}
	warnings := []string{}
	for _, item := range rows {
		r := obj(item)
		if !firewallFlowMatches(r, f) {
			continue
		}
		if !yes(r["runtime"]) && !yes(r["partial_runtime"]) {
			continue
		}
		kind := str(r["kind"])
		evidence := "已匹配可解析字段"
		if yes(r["partial_runtime"]) || r["editable"] == false || (str(r["service"]) != "" && !yes(r["service_verified"])) || str(r["action_rate"]) != "" {
			evidence = "需核对完整条件或运行时副本"
		}
		row := M{"id": r["id"], "name": firstNonempty(str(r["name"]), str(r["service"]), str(r["id"])), "kind": kind, "action": r["action"], "priority": r["priority"], "chain": r["chain"], "table": r["table"], "source": r["source"], "destination": r["destination"], "protocol": r["protocol"], "port": r["port"], "target_ip": r["target_ip"], "target_port": r["target_port"], "to_source": r["to_source"], "out_interface": r["out_interface"], "storage": r["storage"], "raw": r["raw"], "permanent": yes(r["permanent"]), "runtime_only": driver == "firewalld" && !yes(r["permanent"]) && !yes(r["partial_permanent"]), "evidence": evidence, "policy": r["policy_name"], "zone": r["zone"]}
		candidates = append(candidates, row)
	}
	if driver == "firewalld" {
		sort.SliceStable(candidates, func(i, j int) bool { return firewallStage(candidates[i]) < firewallStage(candidates[j]) })
		for i, broad := range candidates {
			if broad["action"] != "accept" || str(broad["source"]) != "" || broad["evidence"] != "已匹配可解析字段" || str(broad["policy"]) != "" {
				continue
			}
			low, high, ok := firewallPortBounds(str(broad["port"]))
			if !ok {
				continue
			}
			for j, narrow := range candidates {
				if str(narrow["source"]) == "" || narrow["evidence"] != "已匹配可解析字段" || str(narrow["policy"]) != "" || broad["destination"] != narrow["destination"] || broad["protocol"] != narrow["protocol"] || firewallStage(broad) > firewallStage(narrow) {
					continue
				}
				a, b, valid := firewallPortBounds(str(narrow["port"]))
				if !valid || low > a || high < b || (low == a && high == b) {
					continue
				}
				if firewallStage(broad) == firewallStage(narrow) {
					broad["comparison"] = "宽放行覆盖较窄规则的匹配范围。同优先级的实际链内顺序需核对。"
				} else if i < j {
					broad["comparison"] = "宽放行先于窄规则所在阶段。"
				}
				if broad["comparison"] != nil {
					broad["overlaps_with"] = narrow["name"]
					break
				}
			}
		}
	} else {
		warnings = append(warnings, "系统规则的链跳转、连接状态和集合匹配需要进一步核对。表中顺序用于定位候选规则。")
	}
	return candidates, warnings
}

// Map candidate rules to uniquely matching live nft rules. Only a shared chain
// establishes a direct order; matches in different chains retain their uncertainty.
func annotateFirewallChainOrder(candidates, live []M) {
	type locatedRule struct {
		index int
		chain string
	}
	locations := map[string]locatedRule{}
	canonical := func(value any) string {
		_, prefix, err := canonicalSource(str(value))
		if err == nil {
			return prefix.String()
		}
		return str(value)
	}
	for _, candidate := range candidates {
		if candidate["evidence"] != "已匹配可解析字段" || str(candidate["policy"]) != "" || str(candidate["protocol"]) == "" || str(candidate["port"]) == "" {
			continue
		}
		index := -1
		for i, row := range live {
			if str(row["table"]) != "firewalld" || row["action"] != candidate["action"] || row["protocol"] != candidate["protocol"] || row["port"] != candidate["port"] || canonical(row["source"]) != canonical(candidate["source"]) || canonical(row["destination"]) != canonical(candidate["destination"]) {
				continue
			}
			if index >= 0 {
				index = -1
				break
			}
			index = i
		}
		if index >= 0 {
			locations[str(candidate["name"])] = locatedRule{index: index, chain: str(live[index]["chain"])}
		}
	}
	for _, row := range candidates {
		left, leftOK := locations[str(row["name"])]
		right, rightOK := locations[str(row["overlaps_with"])]
		if !leftOK || !rightOK || left.chain == "" || left.chain != right.chain {
			continue
		}
		if left.index < right.index {
			row["comparison"] = "运行时 nftables 链 " + left.chain + " 中，宽放行排列在窄规则之前。"
		} else {
			row["comparison"] = "运行时 nftables 链 " + left.chain + " 中，窄规则排列在宽放行之前。"
		}
	}
}

func (n *Network) FirewallDiagnosis(ctx context.Context, q url.Values) (M, error) {
	host := strings.TrimSpace(q.Get("destination"))
	resolved := host
	if _, err := netip.ParseAddr(host); err != nil {
		source, sourceErr := netip.ParseAddr(q.Get("source"))
		if sourceErr != nil || len(host) > 254 || !ruleDomainPattern.MatchString(host) {
			return nil, errors.New("请填写有效源 IP 和目标 IP 或域名")
		}
		family := "ip6"
		if source.Is4() {
			family = "ip4"
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		ips, lookupErr := net.DefaultResolver.LookupNetIP(lookupCtx, family, host)
		cancel()
		if lookupErr != nil {
			return nil, fmt.Errorf("目标域名解析失败：%w", lookupErr)
		}
		if len(ips) == 0 {
			return nil, errors.New("目标域名没有与源 IP 同地址族的记录")
		}
		resolved = ips[0].String()
	}
	flowQuery := url.Values{}
	for _, key := range []string{"source", "ingress", "protocol", "port"} {
		flowQuery.Set(key, q.Get(key))
	}
	flowQuery.Set("destination", resolved)
	f, err := firewallFlowInput(flowQuery)
	if err != nil {
		return nil, err
	}
	driver := firstNonempty(q.Get("driver"), "auto")
	cmd := n.firewallReadCommand(ctx)
	if driver == "auto" {
		if str(n.app.service("firewalld")["state"]) == "active" {
			driver = "firewalld"
		} else if tables, readErr := cmd(6, "", "nft", "list", "tables"); readErr == nil && strings.TrimSpace(tables) != "" {
			driver = "nftables"
		} else {
			driver = "iptables"
		}
	}
	if driver != "firewalld" && driver != "iptables" && driver != "nftables" {
		return nil, errors.New("此工具暂无静态推演")
	}
	warnings := []string{"结果为运行时快照的静态推演。连接跟踪、动态集合、跨链跳转和远端回程需要独立核对。"}
	zone, binding := "host", "系统规则"
	var activeZones string
	var activeZonesErr error
	if driver == "firewalld" {
		raw, e := cmd(3, "", "firewall-cmd", "--get-zone-of-interface="+f.ingress)
		zone = strings.TrimSpace(raw)
		binding = "接口显式绑定"
		if e != nil || !firewallZoneName.MatchString(zone) || zone == "no" {
			raw, e = cmd(3, "", "firewall-cmd", "--get-default-zone")
			zone = strings.TrimSpace(raw)
			binding = "使用默认区域"
			if e != nil || !firewallZoneName.MatchString(zone) {
				return nil, errors.New("无法读取入口区域")
			}
		}
		activeZones, activeZonesErr = cmd(8, "", "firewall-cmd", "--get-active-zones")
		matched := map[string]bool{}
		current := ""
		for _, line := range strings.Split(activeZones, "\n") {
			value := strings.TrimSpace(line)
			if value != "" && value == line {
				current = strings.Fields(value)[0]
				continue
			}
			if sources, ok := strings.CutPrefix(value, "sources:"); ok {
				for _, source := range strings.Fields(sources) {
					if firewallAddressMatch(source, f.source) && firewallZoneName.MatchString(current) {
						matched[current] = true
					}
					if strings.HasPrefix(source, "ipset:") {
						warnings = append(warnings, "入口区域也可能由 IP 集合决定，需核对集合成员。")
					}
				}
			}
		}
		if activeZonesErr != nil || len(matched) > 1 {
			warnings = append(warnings, "源地址归属无法唯一确认，当前以接口区域为依据。")
		}
		if len(matched) == 1 {
			for sourceZone := range matched {
				zone = sourceZone
				binding = "源 IP 显式绑定"
			}
		}
	}
	snapshot := n.FirewallView(driver, zone, ctx)
	if !yes(snapshot["complete"]) {
		return nil, errors.New("规则快照读取不完整，无法进行推演")
	}
	rows := arr(clone(snapshot)["entries"])
	candidates, detail := firewallFlowCandidates(rows, f, driver)
	if driver != "firewalld" {
		warnings = append(warnings, detail...)
	}
	translated := M{"source": f.source.String(), "destination": f.destination.String(), "port": f.port, "status": "未确认地址转换"}
	routeDestination := f.destination.String()
	dnatCount := 0
	translatedDestinationKnown := false
	for _, row := range candidates {
		if row["kind"] == "forward" || row["action"] == "dnat" {
			dnatCount++
		}
	}
	if dnatCount == 1 {
		for _, row := range candidates {
			if row["kind"] != "forward" || row["evidence"] != "已匹配可解析字段" {
				continue
			}
			if target := str(row["target_ip"]); target != "" {
				translated["destination"] = target
				routeDestination = target
				translatedDestinationKnown = true
			}
			if port := str(row["target_port"]); port != "" {
				translated["port"] = port
			}
			translated["status"] = "候选 DNAT 转换"
		}
	}
	family := "-4"
	if f.source.Is6() {
		family = "-6"
	}
	routeRead := func(args ...string) M {
		raw, e := cmd(3, "", append([]string{"ip", "-j", family, "route", "get"}, args...)...)
		var routes []M
		if e != nil || json.Unmarshal([]byte(raw), &routes) != nil || len(routes) == 0 {
			return nil
		}
		return routes[0]
	}
	routeArgs := []string{routeDestination, "from", f.source.String(), "iif", f.ingress}
	if f.protocol == "tcp" || f.protocol == "udp" {
		port := f.port
		if translatedPort, ok := translated["port"].(string); ok {
			port, _ = strconv.Atoi(translatedPort)
		}
		routeArgs = append(routeArgs, "ipproto", f.protocol)
		if port > 0 {
			routeArgs = append(routeArgs, "dport", strconv.Itoa(port))
		}
	}
	var route M
	if dnatCount == 0 || translatedDestinationKnown {
		route = routeRead(routeArgs...)
	} else {
		warnings = append(warnings, "DNAT 转换后的目的地址无法唯一确定，未推断后续出口。")
	}
	reverse := routeRead(f.source.String())

	if reverse == nil || str(reverse["dev"]) != f.ingress {
		warnings = append(warnings, "本机回程路由缺失或出口与入口接口不同，需核对策略路由和远端回程。")
	}
	if route == nil {
		warnings = append(warnings, "未能确认正向路由。")
	}
	egressZone := ""
	pathScope := "unknown"
	if route != nil {
		if str(route["type"]) == "local" {
			pathScope = "input"
		} else if str(route["dev"]) != "" && (str(route["type"]) == "" || str(route["type"]) == "unicast") {
			pathScope = "forward"
		}
	}
	if driver == "firewalld" {
		if pathScope == "forward" {
			if activeZonesErr == nil {
				egressZone = firewallActiveInterfaceZone(activeZones, str(route["dev"]))
				if egressZone == "" {
					raw, err := cmd(8, "", "firewall-cmd", "--get-default-zone")
					if value := strings.TrimSpace(raw); err == nil && firewallZoneName.MatchString(value) {
						egressZone = value
					}
				}
			}
			if egressZone == "" {
				warnings = append(warnings, "未能核实出口区域，无法筛选域间规则。")
			}
		}
		translatedFlow := f
		if address, err := netip.ParseAddr(str(translated["destination"])); err == nil {
			translatedFlow.destination = address
		}
		switch port := translated["port"].(type) {
		case int:
			translatedFlow.port = port
		case string:
			if value, err := strconv.Atoi(port); err == nil {
				translatedFlow.port = value
			} else {
				translatedFlow.port = 0
				warnings = append(warnings, "转换后的端口未唯一确定，后续端口规则无法完整推演。")
			}
		}
		filtered := []any{}
		serviceDefinitions := map[string]M{}
		for _, value := range rows {
			row := obj(value)
			switch str(row["kind"]) {
			case "forward":
				// DNAT matches the original packet, evaluated before the route lookup.
				continue
			case "masquerade":
				// Egress NAT is handled separately from ingress-zone filtering.
				continue
			}
			if pathScope != "input" || (!yes(row["runtime"]) && !yes(row["partial_runtime"])) || !firewallFlowMatches(row, translatedFlow) {
				continue
			}
			if service := str(row["service"]); service != "" {
				details, found := serviceDefinitions[service]
				if !found && firewallZoneName.MatchString(service) {
					raw, err := cmd(8, "", "firewall-cmd", "--info-service="+service)
					if err == nil {
						details = parseZoneDetails(raw)
					}
					serviceDefinitions[service] = details
				}
				match, known := firewallServiceMatch(details, translatedFlow)
				if !known {
					warnings = append(warnings, "无法完整核对服务定义 "+service+"，未列为匹配规则。")
					continue
				}
				if !match {
					continue
				}
				row["service_verified"] = true
			}
			filtered = append(filtered, row)
		}
		inputRules, details := firewallFlowCandidates(filtered, translatedFlow, driver)
		warnings = append(warnings, details...)
		natRules := []M{}
		for _, candidate := range candidates {
			if candidate["kind"] == "forward" {
				natRules = append(natRules, candidate)
			}
			if pathScope == "forward" && candidate["kind"] == "masquerade" && candidate["action"] == "snat" && str(candidate["out_interface"]) == str(route["dev"]) && candidate["evidence"] == "已匹配可解析字段" && firewallFlowMatches(candidate, translatedFlow) {
				candidate["evidence"] = "出口与规则条件相符，实际转换待验证"
				translated["status"] = "存在显式 SNAT 候选，源地址转换未确认"
				natRules = append(natRules, candidate)
			}
		}
		candidates = append(natRules, inputRules...)
		if pathScope == "input" {
			for _, row := range inputRules {
				row["stage"] = "本机入站"
			}
		}
		if pathScope == "forward" {
			warnings = append(warnings, "域内服务放行只用于访问本机。区域地址伪装需核对实际出口及 POSTROUTING 链，本次未据此确认源地址转换。")
			if egressZone != "" {
				cross := n.CrossZoneSnapshot(ctx)
				if yes(cross["complete"]) {
					for _, value := range arr(clone(cross)["items"]) {
						row := obj(value)
						if row["ingress_zone"] != zone || row["egress_zone"] != egressZone || !yes(row["runtime"]) {
							continue
						}
						if !yes(row["enabled"]) {
							continue
						}
						row["kind"] = "cross-zone"
						extra, _ := firewallFlowCandidates([]any{row}, translatedFlow, driver)
						for _, candidate := range extra {
							candidate["stage"] = "转发过滤"
							candidate["ingress_zone"], candidate["egress_zone"] = zone, egressZone
							if str(candidate["action"]) == "" {
								candidate["action"] = str(row["policy_target"])
								candidate["policy_default"] = true
								candidate["raw"] = row["runtime_raw"]
								candidate["evidence"] = "入口与出口区域相符，详见策略默认动作及附加条件"
							}
						}
						candidates = append(candidates, extra...)
					}
				} else {
					warnings = append(warnings, "域间策略读取不完整。")
				}
			}
		}
	}
	verdict, reason := "未确认", "未确认转发或入站链的最终动作"
	if pathScope == "forward" && driver == "firewalld" && dnatCount == 0 && binding == "接口显式绑定" && activeZonesErr == nil && !strings.Contains(strings.Join(warnings, " "), "源地址归属无法唯一确认") && !strings.Contains(strings.Join(warnings, " "), "IP 集合") {
		verdict, reason = firewallInterfaceForwardVerdict(cmd, f, str(route["dev"]), candidates)
	}
	if pathScope == "forward" && driver == "firewalld" {
		if raw, err := cmd(6, "", "nft", "-j", "list", "ruleset"); err == nil {
			moduleRules := moduleSNATCandidates(raw, f, str(route["dev"]))
			for _, row := range moduleRules {
				row["stage"] = "源地址转换"
				if verdict == "预计拒绝" {
					row["evidence"] = "未到达：转发阶段预计拒绝"
				}
			}
			candidates = append(candidates, moduleRules...)
			if verdict == "预计拒绝" {
				translated["status"] = "未到达源地址转换阶段"
			} else if verdict == "预计放行" && len(moduleRules) == 1 && str(moduleRules[0]["evidence"]) == "运行时 POSTROUTING 条件相符，实际转换待验证" {
				translated["status"] = "预计匹配模块伪装，实际源地址转换未观测"
			}
			if len(moduleRules) > 0 && translated["status"] == "未确认地址转换" {
				translated["status"] = "存在运行时模块伪装候选，源地址转换未确认"
			}
		} else {
			warnings = append(warnings, "未能读取内核 NAT 链，模块伪装无法核实。")
		}
	}
	if verdict == "预计拒绝" {
		translated["status"] = "未到达源地址转换阶段"
		candidates = append(candidates, M{"name": "firewalld 转发链拒绝", "stage": "转发过滤", "action": "reject", "ingress_zone": zone, "egress_zone": egressZone, "evidence": reason})
	}
	for _, row := range candidates {
		if row["kind"] == "masquerade" && verdict == "预计拒绝" {
			row["evidence"] = "未到达：转发阶段预计拒绝"
		}
	}
	slices.SortStableFunc(candidates, func(left, right M) int {
		stage := func(row M) int {
			if row["kind"] == "forward" || row["action"] == "dnat" {
				return 0
			}
			if row["kind"] == "masquerade" {
				return 2
			}
			return 1
		}
		return stage(left) - stage(right)
	})
	if driver == "firewalld" {
		hasOverlap := false
		for _, candidate := range candidates {
			hasOverlap = hasOverlap || candidate["overlaps_with"] != nil
		}
		if hasOverlap {
			if raw, err := cmd(4, "", "nft", "-j", "list", "table", "inet", "firewalld"); err == nil {
				if live, err := nftablesInventory(raw, nil, nil); err == nil {
					annotateFirewallChainOrder(candidates, live)
				}
			}
		}
	}

	return M{"host": host, "resolved_ip": resolved, "input_port": f.port, "driver": driver, "zone": zone, "binding": binding, "egress_zone": egressZone, "path_scope": pathScope, "route": route, "reverse_route": reverse, "verdict": verdict, "verdict_reason": reason, "rules": candidates, "nat": translated, "warnings": uniq(warnings), "status": "需结合运行时实际匹配确认"}, nil
}

// Confirm the generated firewalld path only when its first matching dispatch
// and the chains it calls can be understood without packet state or sets.
func firewallInterfaceForwardVerdict(cmd func(int, string, ...string) (string, error), f firewallFlow, output string, candidates []M) (string, string) {
	unknown := "无法确认 firewalld 转发链的最终动作"
	read := func(chain string) ([]string, bool) {
		raw, err := cmd(3, "", "nft", "list", "chain", "inet", "firewalld", chain)
		if err != nil {
			return nil, false
		}
		var rules []string
		for _, line := range strings.Split(raw, "\n") {
			if !strings.HasPrefix(line, "\t\t") {
				continue
			}
			rules = append(rules, strings.TrimSpace(line))
		}
		return rules, true
	}
	base, ok := read("filter_FORWARD")
	if !ok || !slices.Contains(base, "jump filter_FORWARD_POLICIES") {
		return "未确认", unknown
	}
	for _, rule := range base {
		if rule == "jump filter_FORWARD_POLICIES" {
			break
		}
		if strings.HasPrefix(rule, "type filter hook forward ") || rule == "ct state { established, related } accept" || rule == "ct status dnat accept" || rule == "ct state invalid drop" || (rule == `iifname "lo" accept` && f.ingress != "lo") || (f.source.Is4() && strings.HasPrefix(rule, "ip6 ")) {
			continue
		}
		return "未确认", unknown
	}
	dispatch, ok := read("filter_FORWARD_POLICIES")
	if !ok {
		return "未确认", unknown
	}
	for _, rule := range dispatch {
		fields := strings.Fields(rule)
		if len(fields) < 2 {
			return "未确认", unknown
		}
		for len(fields) >= 2 && (fields[0] == "iifname" || fields[0] == "oifname") {
			actual := output
			if fields[0] == "iifname" {
				actual = f.ingress
			}
			if !strings.HasPrefix(fields[1], `"`) || !strings.HasSuffix(fields[1], `"`) {
				return "未确认", unknown
			}
			if fields[1] != strconv.Quote(actual) {
				fields = nil
				break
			}
			fields = fields[2:]
		}
		if fields == nil {
			continue
		}
		if len(fields) == 4 && strings.Join(fields, " ") == "reject with icmpx admin-prohibited" {
			return "预计拒绝", "firewalld 转发链拒绝新请求"
		}
		if len(fields) != 2 || fields[0] != "jump" || !firewallZoneName.MatchString(strings.TrimPrefix(strings.TrimPrefix(fields[1], "filter_FWD_"), "policy_")) {
			return "未确认", unknown
		}
		chain := fields[1]
		rules, ok := read(chain)
		if !ok {
			return "未确认", unknown
		}
		for _, item := range rules {
			if item == "accept" {
				for _, candidate := range candidates {
					if "filter_FWD_policy_"+str(candidate["policy"]) == chain && str(candidate["action"]) == "ACCEPT" {
						return "预计放行", str(candidate["policy"]) + " · 策略默认允许"
					}
				}
				return "预计放行", chain + " · 允许转发"
			}
			sub, ok := strings.CutPrefix(item, "jump ")
			if !ok || !strings.HasPrefix(sub, chain+"_") {
				return "未确认", unknown
			}
			if rows, ok := read(sub); !ok || len(rows) != 0 {
				return "未确认", unknown
			}
		}
	}
	return "未确认", unknown
}
