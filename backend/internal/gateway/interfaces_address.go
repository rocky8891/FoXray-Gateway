package gateway

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

func interfaceRouteKey(route M) string {
	r := clone(route)
	delete(r, "key")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fingerprint(r))))
}
func interfacePrefix(address string, prefix any) (string, *net.IPNet, int, error) {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return "", nil, 0, errors.New("IP地址须为有效单播地址")
	}
	bits, family := 128, 6
	if ip.To4() != nil {
		ip = ip.To4()
		bits, family = 32, 4
	}
	value := prefix
	if raw := strings.TrimSpace(str(prefix)); raw != "" {
		if strings.Contains(raw, ".") {
			mask := net.ParseIP(raw)
			if mask == nil || mask.To4() == nil || family != 4 {
				return "", nil, 0, errors.New("IPv4掩码无效")
			}
			ones, total := net.IPMask(mask.To4()).Size()
			if total != 32 {
				return "", nil, 0, errors.New("子网掩码须为连续的1")
			}
			value = ones
		} else {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return "", nil, 0, errors.New("前缀长度无效")
			}
			value = parsed
		}
	}
	length, err := integer(value, 0, bits)
	if err != nil {
		return "", nil, 0, errors.New("前缀长度超出地址族范围")
	}
	network := &net.IPNet{IP: ip.Mask(net.CIDRMask(length, bits)), Mask: net.CIDRMask(length, bits)}
	return fmt.Sprintf("%s/%d", ip, length), network, family, nil
}
func interfaceAddressSignature(link M) string {
	addresses := []string{}
	for _, raw := range arr(link["addr_info"]) {
		r := obj(raw)
		addresses = append(addresses, fingerprint(M{"local": r["local"], "prefixlen": r["prefixlen"], "scope": r["scope"], "label": r["label"], "dynamic": r["dynamic"], "temporary": r["temporary"], "noprefixroute": r["noprefixroute"]}))
	}
	sort.Strings(addresses)
	return fingerprint(M{"ifindex": link["ifindex"], "ifname": link["ifname"], "addresses": addresses, "flags": link["flags"]})
}
func interfaceRouteArgs(route M) ([]string, error) {
	for k, v := range route {
		switch k {
		case "key", "family", "table", "dst", "gateway", "dev", "protocol", "scope", "prefsrc", "metric", "flags", "pref", "type":
		default:
			if v != nil {
				return nil, errors.New("此默认路由包含复杂属性，请在路由页面管理")
			}
		}
	}
	if str(route["type"]) != "" && str(route["type"]) != "unicast" {
		return nil, errors.New("默认路由类型不可在接口页面编辑")
	}
	if err := iface(str(route["dev"])); err != nil {
		return nil, err
	}
	metric, err := integer(first(route["metric"], 0), 0, 2147483647)
	if err != nil {
		return nil, err
	}
	r := clone(route)
	r["dst"], r["table"], r["metric"] = "default", 254, metric
	protocol := firstNonempty(strOrNumber(route["protocol"]), "boot")
	if !regexpInterfaceProtocol(protocol) {
		return nil, errors.New("路由协议无效")
	}
	r["protocol"] = protocol
	for _, raw := range arr(route["flags"]) {
		if str(raw) == "onlink" {
			r["onlink"] = true
		} else if str(raw) != "linkdown" {
			return nil, errors.New("此默认路由标志不可在接口页面编辑")
		}
	}
	args := routeSpec(r)
	if pref := str(route["pref"]); pref != "" {
		if pref != "low" && pref != "medium" && pref != "high" {
			return nil, errors.New("IPv6路由优先级无效")
		}
		args = append(args, "pref", pref)
	}
	return args, nil
}
func regexpInterfaceProtocol(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
func (n *Network) planInterfaceAddress(p *NetworkPlan, item M, creating bool) error {
	name := str(item["name"])
	if err := iface(name); err != nil {
		return err
	}
	if name == "lo" || name == "vone0" || name == "CloudflareWARP" || name == "vpm-tun" || name == "fw-uplink" || strings.HasPrefix(name, "wg-") {
		return errors.New("此接口由系统或VPN维护，请在对应模块修改地址")
	}
	address := strings.TrimSpace(str(item["address"]))
	original := str(item["original_address"])
	gateway := strings.TrimSpace(str(item["gateway"]))
	action := firstNonempty(str(item["gateway_action"]), "keep")
	if action != "keep" && action != "set" && action != "remove" {
		return errors.New("网关操作无效")
	}
	if creating && gateway != "" && item["gateway_action"] == nil {
		action = "set"
	}
	var desired string
	var network *net.IPNet
	family := 0
	if address != "" {
		var err error
		desired, network, family, err = interfacePrefix(address, item["prefix"])
		if err != nil {
			return err
		}
	}
	if creating && original != "" {
		return errors.New("新接口不能替换已有地址")
	}
	if !creating && address == "" && original != "" {
		return errors.New("编辑地址时须填写新IP和掩码")
	}
	if action == "set" {
		ip := net.ParseIP(gateway)
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
			return errors.New("网关须为有效单播IP")
		}
		gf := 6
		if ip.To4() != nil {
			gf = 4
		}
		if family != 0 && family != gf {
			return errors.New("IP地址与网关地址族不一致")
		}
		family = gf
		gateway = ip.String()
	}
	if creating && action != "keep" && desired == "" {
		return errors.New("新接口配置网关前须填写IP地址和掩码")
	}
	var link M
	var old M
	if !creating {
		rows, err := n.app.commandJSON("ip", "-j", "-d", "address", "show", "dev", name)
		if err != nil || len(rows) != 1 {
			return errors.New("接口地址读取失败，请刷新后重试")
		}
		link = obj(rows[0])
		for _, raw := range arr(link["addr_info"]) {
			r := obj(raw)
			cidr := fmt.Sprintf("%s/%v", str(r["local"]), r["prefixlen"])
			if cidr == original {
				old = r
			}
			if cidr == desired && cidr != original {
				return errors.New("此地址已存在，请选择该地址进行修改")
			}
		}
		if original != "" && old == nil {
			return errors.New("原地址已变化，请刷新后重试")
		}
		if old != nil && desired != original {
			if (net.ParseIP(str(old["local"])).To4() != nil) != (family == 4) {
				return errors.New("修改地址族请新增独立静态地址")
			}
			if yes(old["dynamic"]) || yes(old["temporary"]) || str(old["scope"]) != "global" {
				return errors.New("自动获取或非全局地址请在所属网络管理器中修改；可新增独立静态地址")
			}
			_, oldNet, err := net.ParseCIDR(original)
			if err != nil {
				return errors.New("原地址无效")
			}
			for _, raw := range arr(link["addr_info"]) {
				r := obj(raw)
				if str(r["local"]) != str(old["local"]) && oldNet.Contains(net.ParseIP(str(r["local"]))) {
					return errors.New("此地址存在同网段别名，删除主地址可能连带影响别名，请先在网络管理器中调整")
				}
			}
		}
	}
	up := false
	for _, flag := range arr(link["flags"]) {
		if str(flag) == "UP" {
			up = true
		}
	}
	if action == "set" && !up && !yes(item["activate"]) {
		return errors.New("配置网关需要启用接口，请勾选启用")
	}
	if family == 0 && action != "keep" {
		if num(item["family"]) == 6 {
			family = 6
		} else {
			family = 4
		}
	}
	routes := []any{}
	if family != 0 {
		var err error
		routes, err = n.app.commandJSON("ip", "-j", fmt.Sprint("-", family), "route", "show", "table", "main", "default")
		if err != nil || routes == nil {
			return errors.New("默认路由读取失败，不能生成完整回退计划")
		}
	}

	if old != nil && desired != original {
		for _, raw := range routes {
			route := obj(raw)
			gatewayIP := net.ParseIP(str(route["gateway"]))
			if str(route["dev"]) != name || gatewayIP == nil {
				continue
			}
			keyed := clone(route)
			keyed["family"], keyed["table"] = family, 254
			if action != "keep" && interfaceRouteKey(keyed) == str(item["gateway_key"]) {
				continue
			}
			reachable := network != nil && network.Contains(gatewayIP) || gatewayIP.To4() == nil && gatewayIP.IsLinkLocalUnicast()
			for _, rawAddress := range arr(link["addr_info"]) {
				address := obj(rawAddress)
				if str(address["local"]) == str(old["local"]) {
					continue
				}
				_, subnet, err := net.ParseCIDR(fmt.Sprintf("%s/%v", str(address["local"]), address["prefixlen"]))
				if err == nil && subnet.Contains(gatewayIP) {
					reachable = true
				}
			}
			if !reachable {
				return errors.New("修改后原默认网关不可达，请同时修改或删除该网关")
			}
		}
	}
	// Preserve selected-address ownership and the original default route before applying.
	priorCheck := p.precondition
	linkSig := interfaceAddressSignature(link)
	routeSig := fingerprint(routes)
	p.precondition = func() error {
		if priorCheck != nil {
			if err := priorCheck(); err != nil {
				return err
			}
		}
		if !creating {
			rows, err := n.app.commandJSON("ip", "-j", "-d", "address", "show", "dev", name)
			if err != nil || len(rows) != 1 || interfaceAddressSignature(obj(rows[0])) != linkSig {
				return errors.New("接口地址或状态已变化，请重新预览")
			}
		}
		if family != 0 {
			rows, err := n.app.commandJSON("ip", "-j", fmt.Sprint("-", family), "route", "show", "table", "main", "default")
			if err != nil || fingerprint(rows) != routeSig {
				return errors.New("默认路由已变化，请重新预览")
			}
		}
		return nil
	}
	appendStep := func(args, undo []string) {
		p.steps = append(p.steps, Step{Args: args})
		p.undo = append(p.undo, Step{Args: undo})
	}
	if old != nil && desired != original {
		for _, raw := range routes {
			if str(obj(raw)["prefsrc"]) == str(old["local"]) {
				return errors.New("默认路由固定使用原IP作为来源，请先在路由页面调整来源地址")
			}
		}
		restore := []string{"ip", "address", "add", original, "dev", name}
		if broadcast := str(old["broadcast"]); broadcast != "" {
			restore = append(restore, "broadcast", broadcast)
		}
		if label := str(old["label"]); label != "" && label != name {
			restore = append(restore, "label", label)
		}
		if yes(old["noprefixroute"]) {
			restore = append(restore, "noprefixroute")
		}
		appendStep([]string{"ip", "address", "del", original, "dev", name}, restore)
	}
	if desired != "" && desired != original {
		add := []string{"ip", "address", "add", desired, "dev", name}
		if family == 4 {
			ones, _ := network.Mask.Size()
			if ones < 31 {
				add = append(add, "broadcast", "+")
			}
		}
		appendStep(add, []string{"ip", "address", "del", desired, "dev", name})
	}
	if yes(item["activate"]) && !up {
		appendStep([]string{"ip", "link", "set", "dev", name, "up"}, []string{"ip", "link", "set", "dev", name, "down"})
	}
	var selected M
	for _, raw := range routes {
		r := clone(obj(raw))
		r["family"], r["table"] = family, 254
		if action != "keep" && interfaceRouteKey(r) == str(item["gateway_key"]) {
			selected = r
		}
	}
	if action != "keep" && str(item["gateway_key"]) != "" && selected == nil {
		return errors.New("所选默认网关已变化，请刷新后重试")
	}
	if selected != nil && str(selected["dev"]) != name {
		return errors.New("只能修改当前接口的默认网关")
	}
	if action == "remove" && selected == nil {
		return errors.New("请选择要删除的默认网关")
	}
	if action == "set" {
		reachable := network != nil && network.Contains(net.ParseIP(gateway))
		if net.ParseIP(gateway).To4() == nil && net.ParseIP(gateway).IsLinkLocalUnicast() {
			reachable = true
		}
		for _, raw := range arr(link["addr_info"]) {
			r := obj(raw)
			cidr := fmt.Sprintf("%s/%v", str(r["local"]), r["prefixlen"])
			if old != nil && cidr == original && desired != original {
				continue
			}
			_, subnet, err := net.ParseCIDR(cidr)
			if err == nil && subnet.Contains(net.ParseIP(gateway)) {
				reachable = true
			}
		}
		if !reachable {
			return errors.New("网关不在接口可达网段内；特殊onlink路由请在路由页面配置")
		}
	}
	if action != "keep" {
		var oldArgs []string
		var err error
		if selected != nil {
			oldArgs, err = interfaceRouteArgs(selected)
			if err != nil {
				return err
			}
		}
		if action == "remove" {
			appendStep(append([]string{"ip", fmt.Sprint("-", family), "route", "del"}, oldArgs...), append([]string{"ip", fmt.Sprint("-", family), "route", "add"}, oldArgs...))
		} else {
			metric := 600
			if selected != nil {
				metric = int(num(selected["metric"]))
			} else {
				var err error
				metric, err = integer(first(item["gateway_metric"], 600), 0, 2147483647)
				if err != nil {
					return errors.New("网关优先级无效")
				}
			}
			for _, raw := range routes {
				r := obj(raw)
				if int(num(r["metric"])) == metric && (selected == nil || str(r["dev"]) != name || str(r["gateway"]) != str(selected["gateway"])) {
					return errors.New("已有相同优先级的默认路由，请选择其他优先级或在路由页面管理")
				}
			}
			next := M{"dst": "default", "gateway": gateway, "dev": name, "family": family, "table": 254, "metric": metric, "protocol": "static"}
			if selected != nil {
				next = clone(selected)
				next["gateway"] = gateway
			}
			args, err := interfaceRouteArgs(next)
			if err != nil {
				return err
			}
			if selected != nil {
				appendStep(append([]string{"ip", fmt.Sprint("-", family), "route", "replace"}, args...), append([]string{"ip", fmt.Sprint("-", family), "route", "replace"}, oldArgs...))
			} else {
				appendStep(append([]string{"ip", fmt.Sprint("-", family), "route", "add"}, args...), append([]string{"ip", fmt.Sprint("-", family), "route", "del"}, args...))
			}
		}
	}
	if !creating {
		if len(p.steps) == 0 {
			return errors.New("没有需要应用的地址或网关变更")
		}
		p.Summary = "接口 " + name + " · 地址与网关"
	}
	if address != "" || action != "keep" {
		p.Warnings = append(p.Warnings, "仅修改所选静态地址和main表的指定默认网关；其他地址和路由保留。仅运行期有效，网络管理器或重启可能覆盖。应用后120秒内未确认自动回退。")
	}
	return nil
}
