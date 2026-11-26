package gateway

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var firewallDrivers = []string{"firewalld", "nftables", "iptables", "ufw"}

func firewallDriver(item M) string { return firstNonempty(str(item["driver"]), "firewalld") }
func (n *Network) firewallTypes() []M {
	rows := []M{}
	for _, name := range firewallDrivers {
		command := name
		if name == "firewalld" {
			command = "firewall-cmd"
		}
		if name == "nftables" {
			command = "nft"
		}
		_, e := exec.LookPath(command)
		row := M{"id": name, "installed": e == nil, "kind": "tool"}
		if name == "firewalld" || name == "ufw" {
			row["kind"] = "manager"
			row["service"] = n.app.service(name)
		}
		rows = append(rows, row)
	}
	return rows
}
func (n *Network) FirewallView(driver, zone string, contexts ...context.Context) M {
	if driver == "" {
		driver = "firewalld"
	}
	types := n.firewallTypes()
	installed := false
	valid := false
	for _, v := range types {
		if v["id"] == driver {
			valid = true
			installed = yes(v["installed"])
		}
	}
	if !valid {
		return M{"error": "防火墙类型无效", "types": types}
	}
	if driver == "firewalld" {
		var state M
		for _, v := range types {
			if v["id"] == driver {
				state = obj(v["service"])
			}
		}
		r := n.firewallSnapshotWithState(zone, state, contexts...)
		r["driver"], r["types"], r["installed"] = driver, types, installed
		return r
	}
	result := M{"driver": driver, "types": types, "installed": installed, "entries": []M{}, "complete": false, "zones": []string{"host"}, "zone": "host", "firewalld": M{"state": "active", "autostart": true}, "managed": []any{}}
	if !installed {
		result["error"] = "该防火墙工具尚未安装；切换视图不会自动安装或启停服务"
		return result
	}
	var raw, rawV4, rawV6 string
	var e error
	switch driver {
	case "nftables":
		raw, e = n.app.command(6, "", "nft", "-j", "list", "ruleset")
	case "iptables":
		rawV4, e = n.app.command(6, "", "iptables-save")
		raw = rawV4
		v6, er := n.app.command(6, "", "ip6tables-save")
		if er == nil {
			rawV6 = v6
			raw += "\n# IPv6\n" + v6
		}
	case "ufw":
		raw, e = n.app.command(6, "", "ufw", "status", "verbose")
		result["firewalld"] = n.app.service("ufw")
	}
	result["ruleset_raw"], result["complete"] = raw, e == nil
	n.mu.Lock()
	managed := clone(arr(n.managed["firewall"]))
	n.mu.Unlock()
	counters := map[string]M{}
	owned := map[string]bool{}
	for _, value := range managed {
		rule := obj(value)
		if firewallDriver(rule) != driver {
			continue
		}
		if driver == "nftables" {
			owned[nativeRuleName(rule)] = true
		}
		if driver == "iptables" {
			owned[nativeChainName(rule)] = true
		}
	}
	var system []M
	if e == nil {
		switch driver {
		case "nftables":
			system, e = nftablesInventory(raw, owned, counters)
		case "iptables":
			system = append(iptablesInventory(rawV4, "ipv4", owned), iptablesInventory(rawV6, "ipv6", owned)...)
		}
	}
	if e != nil {
		result["complete"] = false
		result["error"] = "系统规则读取失败"
	}
	rows := []M{}
	for _, v := range managed {
		r := obj(v)
		if firewallDriver(r) != driver {
			continue
		}
		r = clone(r)
		r["managed"], r["editable"], r["permanent"] = true, true, true
		if driver == "nftables" {
			r["hook_priority"] = nativeHookPriority(r)
			r["priority"] = r["hook_priority"]
		}
		if c := counters[nativeRuleName(r)]; c != nil {
			r["packets"], r["bytes"] = c["packets"], c["bytes"]
		}
		present := n.nativeFirewallPresent(r)
		r["runtime"] = present
		r["drift"] = present != yes(r["enabled"])
		rows = append(rows, r)
	}
	result["managed"] = rows
	result["entries"] = append(append([]M{}, rows...), identifyNativeRows(system)...)
	result["note"] = "只修改 FoXray 独立规则；完整系统规则在下方只读查看。更换视图不会切换系统防火墙。iptables 可能使用 nft 后端，工具已安装不代表存在多个独立防火墙。"
	return result
}
func nativeRuleName(r M) string { return "foxray_fw_" + strings.ReplaceAll(str(r["id"]), "-", "") }
func (n *Network) nativeFirewallPresent(r M) bool {
	driver := firewallDriver(r)
	name := nativeRuleName(r)
	switch driver {
	case "nftables":
		_, e := n.app.command(4, "", "nft", "list", "table", "inet", name)
		return e == nil
	case "iptables":
		cmd := "iptables"
		if str(r["family"]) == "ipv6" {
			cmd = "ip6tables"
		}
		_, e := n.app.command(4, "", cmd, "-w", "3", "-t", nativeIPTable(r), "-S", nativeChainName(r))
		return e == nil
	case "ufw":
		raw, e := n.app.command(4, "", "ufw", "show", "added")
		return e == nil && strings.Contains(raw, "foxray:"+str(r["id"]))
	}
	return false
}
func nativeIPTable(r M) string {
	if str(r["kind"]) == "forward" || str(r["kind"]) == "masquerade" {
		return "nat"
	}
	return "filter"
}
func nativeChainName(r M) string { return "FX" + fingerprint(str(r["id"]))[:20] }
func nativeHookPriority(r M) int {
	if r["hook_priority"] != nil {
		return int(num(r["hook_priority"]))
	}
	switch str(r["kind"]) {
	case "forward":
		return -110
	case "masquerade":
		return 100
	default:
		return -10
	}
}

func nativeIPParent(r M) string {
	switch str(r["kind"]) {
	case "forward":
		return "PREROUTING"
	case "masquerade":
		return "POSTROUTING"
	default:
		return "INPUT"
	}
}

func (n *Network) nativeIPJumpIndex(r M, remove, replacing bool) (int, error) {
	cmd := "iptables"
	ipv6 := str(r["family"]) == "ipv6"
	if ipv6 {
		cmd = "ip6tables"
	}
	parent, table, chain := nativeIPParent(r), nativeIPTable(r), nativeChainName(r)
	raw, err := n.app.command(5, "", cmd, "-w", "3", "-t", table, "-S", parent)
	if err != nil {
		return 0, errors.New("无法核对 iptables 链顺序")
	}
	n.mu.Lock()
	managed := clone(arr(n.managed["firewall"]))
	n.mu.Unlock()
	priorities := map[string]float64{}
	for _, value := range managed {
		item := obj(value)
		if firewallDriver(item) == "iptables" && nativeIPTable(item) == table && nativeIPParent(item) == parent && (str(item["family"]) == "ipv6") == ipv6 {
			priorities[nativeChainName(item)] = num(item["priority"])
		}
	}
	index, after := 1, 1
	for _, line := range textLines(raw) {
		args := strings.Fields(line)
		if len(args) < 3 || args[0] != "-A" || args[1] != parent {
			continue
		}
		if len(args) == 4 && args[2] == "-j" {
			if args[3] == chain {
				if remove {
					return index, nil
				}
				if replacing {
					continue
				}
			}
			if priority, owned := priorities[args[3]]; owned {
				if !remove && priority > num(r["priority"]) {
					return index, nil
				}
				after = index + 1
			}
		}
		index++
	}
	return after, nil
}

func nativeNFTScript(r M) string {
	name := nativeRuleName(r)
	kind := str(r["kind"])
	chain := "input"
	hook := fmt.Sprintf("type filter hook input priority %d; policy accept;", nativeHookPriority(r))
	if kind == "forward" {
		chain = "prerouting"
		hook = fmt.Sprintf("type nat hook prerouting priority %d; policy accept;", nativeHookPriority(r))
	} else if kind == "masquerade" {
		chain = "postrouting"
		hook = fmt.Sprintf("type nat hook postrouting priority %d; policy accept;", nativeHookPriority(r))
	}
	family := "ip"
	if str(r["family"]) == "ipv6" {
		family = "ip6"
	}
	source := ""
	if value := str(r["source"]); value != "" {
		if strings.Contains(value, ",") {
			value = "{ " + value + " }"
		}
		source = family + " saddr " + value + " "
	}
	if destination := str(r["destination"]); destination != "" {
		source += family + " daddr " + destination + " "
	}
	prefix := ""
	if str(r["family"]) != "" {
		prefix = "meta nfproto " + str(r["family"]) + " "
	}
	rules := []string{}
	protocols := []string{str(r["protocol"])}
	if protocols[0] == "tcpudp" {
		protocols = []string{"tcp", "udp"}
	}
	if kind == "masquerade" {
		action := "masquerade"
		if str(r["action"]) == "snat" {
			action = "snat ip to " + str(r["to_source"])
		}
		rules = append(rules, prefix+source+"oifname \""+str(r["out_interface"])+"\" counter "+action)
	} else if kind == "ip" {
		rules = append(rules, prefix+source+"counter "+str(r["action"]))
	} else {
		for _, proto := range protocols {
			rule := prefix + source
			if proto != "all" {
				rule += proto + " dport " + str(r["port"]) + " "
			}
			if kind == "forward" {
				to := str(r["target_ip"])
				if to == "" {
					rule += "redirect to :" + str(r["target_port"])
				} else {
					rule += "dnat ip to " + to
					if port := str(r["target_port"]); port != "" {
						rule += ":" + port
					}
				}
			} else {
				rule += "counter " + str(r["action"])
			}
			rules = append(rules, rule)
		}
	}
	return "table inet " + name + " { chain " + chain + " { " + hook + "\n" + strings.Join(rules, "\n") + "\n}\n}\n"
}
func nativeIPSpecs(r M) [][]string {
	if str(r["kind"]) == "masquerade" && strings.Contains(str(r["source"]), ",") {
		rows := [][]string{}
		for _, source := range strings.Split(str(r["source"]), ",") {
			child := clone(r)
			child["source"] = strings.TrimSpace(source)
			rows = append(rows, nativeIPSpecs(child)...)
		}
		return rows
	}
	protocols := []string{str(r["protocol"])}
	if protocols[0] == "tcpudp" {
		protocols = []string{"tcp", "udp"}
	}
	if str(r["kind"]) == "ip" || str(r["kind"]) == "masquerade" {
		protocols = []string{""}
	}
	rows := [][]string{}
	for _, proto := range protocols {
		spec := []string{}
		if str(r["source"]) != "" {
			spec = append(spec, "-s", str(r["source"]))
		}
		if destination := str(r["destination"]); destination != "" {
			spec = append(spec, "-d", destination)
		}
		if proto != "" && proto != "all" {
			spec = append(spec, "-p", proto, "--dport", strings.ReplaceAll(str(r["port"]), "-", ":"))
		}
		if str(r["kind"]) == "masquerade" {
			spec = append(spec, "-o", str(r["out_interface"]))
		}
		target := strings.ToUpper(str(r["action"]))
		if str(r["kind"]) == "forward" {
			if str(r["target_ip"]) == "" {
				spec = append(spec, "-j", "REDIRECT", "--to-ports", str(r["target_port"]))
			} else {
				target := str(r["target_ip"])
				if port := str(r["target_port"]); port != "" {
					target += ":" + port
				}
				spec = append(spec, "-j", "DNAT", "--to-destination", target)
			}
		} else {
			spec = append(spec, "-j", target)
			if str(r["kind"]) == "masquerade" && target == "SNAT" {
				spec = append(spec, "--to-source", str(r["to_source"]))
			}
		}
		rows = append(rows, spec)
	}
	return rows
}
func (n *Network) nativeSteps(r M, adding bool, replacing ...bool) ([]Step, []Step, error) {
	driver := firewallDriver(r)
	steps, undo := []Step{}, []Step{}
	pair := func(s, u Step) { steps = append(steps, s); undo = append(undo, u) }
	switch driver {
	case "nftables":
		add := Step{Args: []string{"nft", "-f", "-"}, Input: nativeNFTScript(r)}
		del := Step{Args: []string{"nft", "delete", "table", "inet", nativeRuleName(r)}}
		if adding {
			pair(add, del)
		} else {
			pair(del, add)
		}
	case "iptables":
		cmd := "iptables"
		if str(r["family"]) == "ipv6" {
			cmd = "ip6tables"
		}
		base := []string{cmd, "-w", "3", "-t", nativeIPTable(r)}
		chain := nativeChainName(r)
		parent := nativeIPParent(r)
		position, err := n.nativeIPJumpIndex(r, !adding, len(replacing) > 0 && replacing[0])
		if err != nil {
			return nil, nil, err
		}
		makeStep := func(args ...string) Step { return Step{Args: append(append([]string{}, base...), args...)} }
		pair(makeStep("-N", chain), makeStep("-X", chain))
		for _, spec := range nativeIPSpecs(r) {
			pair(makeStep(append([]string{"-A", chain}, spec...)...), makeStep(append([]string{"-D", chain}, spec...)...))
		}
		pair(makeStep("-I", parent, strconv.Itoa(position), "-j", chain), makeStep("-D", parent, "-j", chain))
		if !adding {
			nextS, nextU := []Step{}, []Step{}
			for i := len(steps) - 1; i >= 0; i-- {
				nextS = append(nextS, undo[i])
				nextU = append(nextU, steps[i])
			}
			steps, undo = nextS, nextU
		}
	case "ufw":
		return nil, nil, errors.New("UFW 当前提供状态与规则查看；避免自动重写其全局规则，请使用 firewalld、nftables 或 iptables 管理独立规则")
	default:
		return nil, nil, errors.New("防火墙类型无效")
	}
	return steps, undo, nil
}
func (n *Network) planNativeFirewall(p *NetworkPlan, d M) error {
	raw := obj(d["item"])
	driver := firewallDriver(raw)
	if str(raw["kind"]) == "service" {
		return errors.New("原生 nftables／iptables 无需切换全局服务；现有规则由原管理器保留")
	}
	item, e := normalizeFirewall(raw)
	if e != nil {
		return e
	}
	item["driver"] = driver
	item["zone"] = "host"
	if str(item["kind"]) == "service-rule" || yes(item["logging"]) {
		return errors.New("预定义服务、规则优先级与限速日志目前由 firewalld 模式提供")
	}
	if str(item["kind"]) == "masquerade" && !yes(d["allow_protected"]) {
		return errors.New("SNAT 会影响转发流量，需要确认本次变更")
	}
	if str(item["kind"]) == "forward" && str(item["family"]) == "ipv6" {
		return errors.New("端口转发目前仅支持 IPv4")
	}
	identity := str(item["id"])
	if identity == "" {
		identity = id()
	}
	item["id"] = identity
	old := n.ownedItem("firewall", identity)
	op := str(d["operation"])
	if op != "add" && op != "replace" && op != "delete" {
		return errors.New("原生防火墙只管理 FoXray 自建规则")
	}
	if op != "add" && old == nil {
		return errors.New("只能修改 FoXray 管理的规则")
	}
	if old != nil && firewallDriver(old) != driver {
		return errors.New("不能跨防火墙类型修改规则")
	}
	appendRule := func(r M, adding bool) error {
		s, u, err := n.nativeSteps(r, adding, adding && old != nil && n.nativeFirewallPresent(old))
		if err != nil {
			return err
		}
		p.steps = append(p.steps, s...)
		p.undo = append(p.undo, u...)
		return nil
	}
	if driver == "iptables" {
		capture := func() (string, error) {
			cmd := "iptables"
			if str(item["family"]) == "ipv6" {
				cmd = "ip6tables"
			}
			return n.app.command(5, "", cmd, "-w", "3", "-t", nativeIPTable(item), "-S", nativeIPParent(item))
		}
		before, err := capture()
		if err != nil {
			return errors.New("无法核对 iptables 链顺序")
		}
		p.precondition = func() error {
			current, err := capture()
			if err != nil || current != before {
				return errors.New("iptables 链顺序已变化，请重新预览")
			}
			return nil
		}
	}
	if old != nil && n.nativeFirewallPresent(old) {
		if e = appendRule(old, false); e != nil {
			return e
		}
	}
	if op != "delete" && yes(item["enabled"]) {
		if old == nil && n.nativeFirewallPresent(item) {
			return errors.New("同名规则已存在，请刷新")
		}
		if e = appendRule(item, true); e != nil {
			return e
		}
	}
	p.persist = M{"operation": op, "item": item}
	p.Summary = driver + " " + op + " · " + str(item["kind"]) + " · " + firstNonempty(str(item["port"]), str(item["source"]))
	p.Warnings = append(p.Warnings, "仅操作 FoXray 自有表／链，声明随管理服务启动恢复；不清空系统规则。其他防火墙的拒绝规则仍可能阻止此处放行的流量")
	return nil
}

// Lightweight service state, independent of zone/rule inventory.
func (n *Network) FirewallServiceState(driver string) M {
	if driver == "" {
		driver = "firewalld"
	}
	result := M{"driver": driver, "installed": false, "checked_at": now()}
	types := []M{}
	for _, name := range firewallDrivers {
		command := map[string]string{"firewalld": "firewall-cmd", "nftables": "nft"}[name]
		if command == "" {
			command = name
		}
		_, err := exec.LookPath(command)
		types = append(types, M{"id": name, "installed": err == nil})
	}
	result["types"] = types

	for _, name := range firewallDrivers {
		if name == driver {
			command := map[string]string{"firewalld": "firewall-cmd", "nftables": "nft"}[driver]
			if command == "" {
				command = driver
			}
			_, err := exec.LookPath(command)
			result["installed"] = err == nil
			if driver == "firewalld" || driver == "ufw" {
				result["firewalld"] = n.app.service(driver)
			}
			return result
		}
	}
	result["error"] = "防火墙类型无效"
	return result
}
