package gateway

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func routeTable(v any) (int, error) {
	if s := str(v); s != "" {
		switch s {
		case "main":
			return 254, nil
		case "default":
			return 253, nil
		case "local":
			return 255, nil
		}
		n, e := strconv.Atoi(s)
		if e != nil {
			return 0, errors.New("路由表须为数字或 main")
		}
		v = n
	}
	if v == nil || num(v) == 0 {
		return 254, nil
	}
	return integer(v, 1, 2147483647)
}
func familyFor(v M) string {
	if num(v["family"]) == 6 {
		return "-6"
	}
	return "-4"
}
func normalizeRoute(raw M) (M, error) {
	r := clone(raw)
	r["family"] = 4
	if num(raw["family"]) == 6 {
		r["family"] = 6
	}
	table, e := routeTable(raw["table"])
	if e != nil {
		return nil, e
	}
	if table == 253 || table == 255 || table == 65743 || table == 16080 || table == 16082 {
		return nil, errors.New("系统／代理内部路由表不可编辑")
	}
	r["table"] = table
	dst := firstNonempty(str(raw["dst"]), "default")
	if e = cidr(dst); e != nil {
		return nil, e
	}
	if dst == "all" {
		dst = "default"
	}
	r["dst"] = dst
	if dst != "default" {
		host := strings.Split(dst, "/")[0]
		if ip := net.ParseIP(host); ip == nil || ((ip.To4() == nil) != (num(r["family"]) == 6)) {
			return nil, errors.New("目标网段与地址族不一致")
		}
	}
	kind := firstNonempty(str(raw["type"]), "unicast")
	if kind != "unicast" && kind != "blackhole" && kind != "unreachable" && kind != "prohibit" {
		return nil, errors.New("路由类型不可编辑")
	}
	r["type"] = kind
	if kind == "unicast" {
		if e = iface(str(r["dev"])); e != nil {
			return nil, e
		}
	}
	for _, key := range []string{"gateway", "prefsrc"} {
		s := str(r[key])
		if s != "" {
			ip := net.ParseIP(s)
			if ip == nil || ((ip.To4() == nil) != (num(r["family"]) == 6)) {
				return nil, errors.New("地址与所选地址族不一致")
			}
		}
	}
	metric, e := integer(first(r["metric"], 0), 0, 2147483647)
	if e != nil {
		return nil, e
	}
	r["metric"] = metric
	if r["enabled"] == nil {
		r["enabled"] = true
	}
	if scope := str(r["scope"]); scope != "" && scope != "global" && scope != "link" && scope != "host" {
		return nil, errors.New("路由作用域无效")
	}
	if r["mtu"] != nil && num(r["mtu"]) > 0 {
		if _, e = integer(r["mtu"], 576, 65535); e != nil {
			return nil, e
		}
	}
	protocol := firstNonempty(strOrNumber(r["protocol"]), "186")
	if r["protocol"] == nil {
		protocol = "186"
	}
	if protocol != "186" && protocol != "static" && protocol != "boot" {
		return nil, errors.New("此路由由内核或其他网络服务维护")
	}
	r["protocol"] = protocol
	return r, nil
}
func routeSpec(r M) []string {
	args := []string{}
	if kind := str(r["type"]); kind != "" && kind != "unicast" {
		args = append(args, kind)
	}
	args = append(args, str(r["dst"]))
	if str(r["type"]) == "unicast" || str(r["type"]) == "" {
		if str(r["gateway"]) != "" {
			args = append(args, "via", str(r["gateway"]))
		}
		args = append(args, "dev", str(r["dev"]))
		if str(r["prefsrc"]) != "" {
			args = append(args, "src", str(r["prefsrc"]))
		}
		if str(r["scope"]) != "" {
			args = append(args, "scope", str(r["scope"]))
		}
		if yes(r["onlink"]) {
			args = append(args, "onlink")
		}
	}
	args = append(args, "table", intstr(r["table"]), "metric", intstr(r["metric"]), "proto", firstNonempty(str(r["protocol"]), "186"))
	if num(r["mtu"]) > 0 {
		args = append(args, "mtu", intstr(r["mtu"]))
	}
	return args
}
func (n *Network) planRoute(p *NetworkPlan, d M) error {
	item, e := normalizeRoute(obj(d["item"]))
	if e != nil {
		return e
	}
	op := str(d["operation"])
	dev := str(item["dev"])
	if (str(item["dst"]) == "default" && num(item["table"]) == 254 || dev == "wg-home" || dev == "vone0") && !yes(d["allow_protected"]) {
		return errors.New("此路由影响默认出口或受保护 VPN，需要本次明确确认")
	}
	identity := str(item["id"])
	if identity == "" {
		identity = id()
	}
	item["id"] = identity
	old := n.ownedItem("routes", identity)
	if op == "add" {
		old = nil
	}
	if op == "adopt" {
		if dev == "vone0" || strings.HasPrefix(dev, "wg-") || dev == "CloudflareWARP" || dev == "vpm-tun" {
			return errors.New("VPN 或代理接口路由由所属组件维护，不可重复接管")
		}
		rows, e := n.app.commandJSON("ip", "-j", familyFor(item), "route", "show", "table", intstr(item["table"]))
		if e != nil {
			return e
		}
		found := false
		for _, v := range rows {
			r := obj(v)
			if firstNonempty(str(r["dst"]), "default") == str(item["dst"]) && str(r["dev"]) == dev && str(r["gateway"]) == str(item["gateway"]) && num(r["metric"]) == num(item["metric"]) {
				r["family"], r["table"] = item["family"], item["table"]
				normalized, e := normalizeRoute(r)
				if e != nil {
					return e
				}
				item = normalized
				item["id"] = id()
				found = true
				break
			}
		}
		if !found {
			return errors.New("系统路由已变化，请刷新")
		}
		p.Summary = "纳入管理 · 路由 " + str(item["dst"])
		p.persist = M{"operation": "add", "item": item}
		p.Warnings = append(p.Warnings, "请确认此静态路由不由 NetworkManager、VPN 或其他服务维护")
		return nil
	}
	if op != "add" && old == nil {
		return errors.New("请先将外部静态路由纳入管理")
	}
	if old != nil && num(old["family"]) != num(item["family"]) {
		return errors.New("修改地址族请新增独立路由")
	}
	if old != nil {
		old, e = normalizeRoute(old)
		if e != nil {
			return e
		}
		if yes(old["enabled"]) {
			p.steps = append(p.steps, Step{Args: append([]string{"ip", familyFor(old), "route", "del"}, routeSpec(old)...)})
			p.undo = append(p.undo, Step{Args: append([]string{"ip", familyFor(old), "route", "add"}, routeSpec(old)...)})
		}
	}
	if op != "delete" && yes(item["enabled"]) {

		p.steps = append(p.steps, Step{Args: append([]string{"ip", familyFor(item), "route", "add"}, routeSpec(item)...)})
		p.undo = append(p.undo, Step{Args: append([]string{"ip", familyFor(item), "route", "del"}, routeSpec(item)...)})
	}
	p.Summary = fmt.Sprintf("路由 %s · %s → %s", op, item["dst"], dev)
	p.persist = M{"operation": op, "item": item}
	p.Warnings = append(p.Warnings, "仅操作本条路由，保护其他路由表；应用后须确认，超时恢复原条目")
	return nil
}
