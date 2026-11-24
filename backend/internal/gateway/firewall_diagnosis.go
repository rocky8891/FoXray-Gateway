package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
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
		if yes(r["partial_runtime"]) || r["editable"] == false || kind == "service-rule" || str(r["rich_match"]) == "service" || str(r["action_rate"]) != "" {
			evidence = "需核对完整条件或运行时副本"
		}
		row := M{"id": r["id"], "name": firstNonempty(str(r["name"]), str(r["service"]), str(r["id"])), "kind": kind, "action": r["action"], "priority": r["priority"], "chain": r["chain"], "table": r["table"], "source": r["source"], "destination": r["destination"], "protocol": r["protocol"], "port": r["port"], "target_ip": r["target_ip"], "target_port": r["target_port"], "to_source": r["to_source"], "out_interface": r["out_interface"], "raw": r["raw"], "permanent": yes(r["permanent"]), "runtime_only": driver == "firewalld" && !yes(r["permanent"]) && !yes(r["partial_permanent"]), "evidence": evidence, "policy": r["policy_name"]}
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
	driver := firstNonempty(q.Get("driver"), "firewalld")
	if driver != "firewalld" && driver != "iptables" && driver != "nftables" {
		return nil, errors.New("此工具暂无静态推演")
	}
	cmd := n.firewallReadCommand(ctx)
	warnings := []string{"结果为运行时快照的静态推演。连接跟踪、动态集合、跨链跳转和远端回程需要独立核对。"}
	zone, binding := "host", "系统规则"
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
		active, e := cmd(3, "", "firewall-cmd", "--get-active-zones")
		matched := map[string]bool{}
		current := ""
		for _, line := range strings.Split(active, "\n") {
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
		if e != nil || len(matched) > 1 {
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
	warnings = append(warnings, detail...)
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
	translated := M{"source": f.source.String(), "destination": f.destination.String(), "port": f.port, "status": "需结合实际链顺序"}
	routeDestination := f.destination.String()
	dnatCount := 0
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
	route := routeRead(routeDestination, "from", f.source.String(), "iif", f.ingress)
	reverse := routeRead(f.source.String())
	if route != nil && str(route["dev"]) != "" {
		for _, candidate := range candidates {
			if candidate["kind"] == "masquerade" && candidate["action"] == "snat" && str(candidate["out_interface"]) == str(route["dev"]) && str(candidate["to_source"]) != "" && candidate["evidence"] == "已匹配可解析字段" {
				if translated["source"] != f.source.String() && translated["source"] != candidate["to_source"] {
					translated["source"] = f.source.String()
					break
				}
				translated["source"] = candidate["to_source"]
				translated["status"] = "候选 NAT 转换"
			}
		}
	}
	if reverse == nil || str(reverse["dev"]) != f.ingress {
		warnings = append(warnings, "本机回程路由缺失或出口与入口接口不同，需核对策略路由和远端回程。")
	}
	if route == nil {
		warnings = append(warnings, "未能确认正向路由。")
	}
	egressZone := ""
	if driver == "firewalld" && str(route["dev"]) != "" {
		raw, e := cmd(3, "", "firewall-cmd", "--get-zone-of-interface="+str(route["dev"]))
		egressZone = strings.TrimSpace(raw)
		if e != nil || !firewallZoneName.MatchString(egressZone) || egressZone == "no" {
			raw, _ = cmd(3, "", "firewall-cmd", "--get-default-zone")
			egressZone = strings.TrimSpace(raw)
		}
		cross := n.CrossZoneSnapshot(ctx)
		if yes(cross["complete"]) {
			for _, value := range arr(clone(cross)["items"]) {
				row := obj(value)
				if row["ingress_zone"] != zone || row["egress_zone"] != egressZone || !yes(row["runtime"]) {
					continue
				}
				row["kind"] = "cross-zone"
				extra, _ := firewallFlowCandidates([]any{row}, f, driver)
				candidates = append(candidates, extra...)
			}
		} else {
			warnings = append(warnings, "域间策略读取不完整。")
		}
	}
	return M{"host": host, "resolved_ip": resolved, "input_port": f.port, "driver": driver, "zone": zone, "binding": binding, "egress_zone": egressZone, "route": route, "reverse_route": reverse, "rules": candidates, "nat": translated, "warnings": warnings, "status": "需结合运行时实际匹配确认"}, nil
}
