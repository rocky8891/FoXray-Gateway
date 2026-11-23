package gateway

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var firewallZoneName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)
var richSource = regexp.MustCompile(`source address="?([^" ]+)"?`)
var richPort = regexp.MustCompile(`(?:^| )port port="?([0-9-]+)"? protocol="?(tcp|udp)"?`)
var richForward = regexp.MustCompile(`forward-port port="?([0-9-]+)"? protocol="?(tcp|udp)"? to-port="?([0-9-]+)"?(?: to-addr="?([^" ]+)"?)?`)

func portRange(value any) (string, error) {
	s := str(value)
	if s == "" {
		s = intstr(value)
	}
	parts := strings.Split(s, "-")
	if len(parts) > 2 {
		return "", errors.New("端口范围无效")
	}
	first, last := 0, 0
	for i, p := range parts {
		v, e := strconv.Atoi(p)
		if e != nil || v < 1 || v > 65535 {
			return "", errors.New("端口须为1至65535或有效范围")
		}
		if i == 0 {
			first = v
			last = v
		} else {
			last = v
		}
	}
	if last < first {
		return "", errors.New("结束端口小于起始端口")
	}
	if first == last {
		return strconv.Itoa(first), nil
	}
	return strconv.Itoa(first) + "-" + strconv.Itoa(last), nil
}
func normalizeFirewall(item M) (M, error) {
	if str(item["kind"]) == "cross-zone" {
		return normalizeCrossZone(item)
	}
	r := M{}
	for _, key := range []string{"id", "driver", "name", "kind", "zone", "port", "protocol", "source", "action", "target_ip", "target_port", "enabled", "family", "storage", "priority", "logging", "log_rate", "service", "to_source", "out_interface", "rich_match"} {
		if value, ok := item[key]; ok {
			r[key] = value
		}
	}
	zone := str(r["zone"])
	if !firewallZoneName.MatchString(zone) {
		return nil, errors.New("防火墙区域无效")
	}
	kind := firstNonempty(str(r["kind"]), "port")
	r["kind"] = kind
	priority, e := integer(first(r["priority"], 0), -32768, 32767)
	if e != nil {
		return nil, e
	}
	r["priority"] = priority
	if yes(r["logging"]) {
		rate := firstNonempty(str(r["log_rate"]), "5/m")
		if !regexp.MustCompile(`^[1-9][0-9]{0,4}/[smhd]$`).MatchString(rate) {
			return nil, errors.New("日志频率格式为 5/m、10/s 等")
		}
		r["log_rate"] = rate
	}
	if kind == "service-rule" {
		if !firewallZoneName.MatchString(str(r["service"])) {
			return nil, errors.New("服务名称无效")
		}
		if r["enabled"] == nil {
			r["enabled"] = true
		}
		return r, nil
	}

	if r["enabled"] == nil {
		r["enabled"] = true
	}
	if source := str(r["source"]); source != "" {
		ip, subnet, e := net.ParseCIDR(source)
		if e != nil {
			ip = net.ParseIP(source)
			if ip == nil {
				return nil, errors.New("来源 IP／CIDR 无效")
			}
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			subnet = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
		}
		r["source"] = subnet.String()
	}
	if family := str(r["family"]); family != "" && family != "ipv4" && family != "ipv6" {
		return nil, errors.New("地址族无效")
	}
	if str(r["source"]) != "" {
		family := "ipv4"
		if strings.Contains(str(r["source"]), ":") {
			family = "ipv6"
		}
		if str(r["family"]) != "" && str(r["family"]) != family {
			return nil, errors.New("来源与地址族不匹配")
		}
		r["family"] = family
	}
	if kind == "masquerade" {
		action := strings.ToLower(firstNonempty(str(r["action"]), "masquerade"))
		if action == "accept" {
			action = "masquerade"
		} // Existing saved rules used accept.
		if action != "masquerade" && action != "snat" {
			return nil, errors.New("SNAT 动作须为 MASQUERADE 或 SNAT")
		}
		if str(r["family"]) == "ipv6" {
			return nil, errors.New("SNAT 规则目前仅支持 IPv4")
		}
		r["action"], r["family"] = action, "ipv4"
		if action == "snat" {
			ip := net.ParseIP(str(r["to_source"]))
			if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() {
				return nil, errors.New("SNAT 须指定有效的转换后源 IPv4 地址")
			}
			r["to_source"] = ip.To4().String()
		} else {
			delete(r, "to_source")
		}
		if action == "snat" || firewallDriver(r) != "firewalld" {
			if str(r["source"]) == "" {
				return nil, errors.New("请指定需要源地址转换的来源 IPv4／网段")
			}
			if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,14}$`).MatchString(str(r["out_interface"])) {
				return nil, errors.New("请指定有效出接口")
			}
		} else {
			delete(r, "out_interface")
		}
		return r, nil
	}
	action := firstNonempty(str(r["action"]), "accept")
	if action != "accept" && action != "drop" && action != "reject" {
		return nil, errors.New("规则策略无效")
	}
	r["action"] = action
	if kind == "rich" {
		if firewallDriver(r) != "firewalld" {
			return nil, errors.New("高级规则仅支持firewalld视图")
		}
		r["storage"] = "rich-rule"
		match := str(r["rich_match"])
		if match != "port" && match != "service" && match != "all" {
			return nil, errors.New("高级规则匹配类型无效")
		}
		if str(r["family"]) == "" {
			return nil, errors.New("高级规则须明确IPv4或IPv6地址族")
		}
		if match == "service" && !firewallZoneName.MatchString(str(r["service"])) {
			return nil, errors.New("服务名称无效")
		}
		if match != "service" {
			delete(r, "service")
		}
		if match != "port" {
			delete(r, "port")
			delete(r, "protocol")
		}
		if match == "port" {
			proto := str(r["protocol"])
			if proto != "tcp" && proto != "udp" && proto != "tcpudp" {
				return nil, errors.New("协议须为TCP、UDP或TCP+UDP")
			}
			port, e := portRange(r["port"])
			if e != nil {
				return nil, e
			}
			r["port"] = port
		}
		return r, nil
	}
	if kind == "ip" {
		if str(r["source"]) == "" {
			return nil, errors.New("IP 规则须填写来源 IP 或网段")
		}
		return r, nil
	}
	if kind != "port" && kind != "forward" {
		return nil, errors.New("防火墙规则类型无效")
	}
	proto := str(r["protocol"])
	if proto != "tcp" && proto != "udp" && proto != "tcpudp" {
		return nil, errors.New("协议须为 TCP、UDP 或 TCP+UDP")
	}
	p, e := portRange(r["port"])
	if e != nil {
		return nil, e
	}
	r["port"] = p
	if kind == "forward" {
		if str(r["family"]) == "ipv6" {
			return nil, errors.New("端口转发目前仅支持 IPv4")
		}
		to, e := portRange(r["target_port"])
		if e != nil {
			return nil, e
		}
		r["target_port"] = to
		if target := str(r["target_ip"]); target != "" {
			ip := net.ParseIP(target)
			if ip == nil || ip.To4() == nil {
				return nil, errors.New("端口转发的目标须为 IPv4 地址")
			}
		}
	}
	return r, nil
}
func firewallSpecs(item M) [][2]string {
	kind := str(item["kind"])
	if kind == "service-rule" {
		return [][2]string{{"service", str(item["service"])}}
	}
	if kind == "masquerade" && str(item["action"]) == "snat" {
		spec := "ipv4 nat POSTROUTING " + intstr(item["priority"]) + " -s " + str(item["source"]) + " -o " + str(item["out_interface"]) + " -j SNAT --to-source " + str(item["to_source"])
		return [][2]string{{"direct-rule", spec}}
	}
	source := str(item["source"])
	family := str(item["family"])
	prefix := "rule"
	if num(item["priority"]) != 0 {
		prefix += " priority=\"" + intstr(item["priority"]) + "\""
	}
	if family != "" {
		prefix += " family=\"" + family + "\""
	}
	if source != "" {
		prefix += " source address=\"" + source + "\""
	}
	logging := ""
	if yes(item["logging"]) {
		logging = " log prefix=\"foxray \" level=\"warning\" limit value=\"" + str(item["log_rate"]) + "\""
	}
	if kind == "masquerade" {
		if source == "" && num(item["priority"]) == 0 {
			return [][2]string{{"masquerade", ""}}
		}
		return [][2]string{{"rich-rule", prefix + " masquerade"}}
	}
	if kind == "ip" {
		return [][2]string{{"rich-rule", prefix + logging + " " + str(item["action"])}}
	}
	if kind == "rich" && str(item["rich_match"]) == "all" {
		return [][2]string{{"rich-rule", prefix + logging + " " + str(item["action"])}}
	}
	if kind == "rich" && str(item["rich_match"]) == "service" {
		return [][2]string{{"rich-rule", prefix + " service name=\"" + str(item["service"]) + "\"" + logging + " " + str(item["action"])}}
	}
	protocols := []string{str(item["protocol"])}
	if protocols[0] == "tcpudp" {
		protocols = []string{"tcp", "udp"}
	}
	out := [][2]string{}
	for _, proto := range protocols {
		port := str(item["port"])
		if kind == "port" && source == "" && str(item["action"]) == "accept" && str(item["storage"]) != "rich-rule" && str(item["family"]) == "" && num(item["priority"]) == 0 && !yes(item["logging"]) {
			out = append(out, [2]string{"port", port + "/" + proto})
		} else if kind == "port" || kind == "rich" {
			out = append(out, [2]string{"rich-rule", prefix + " port port=\"" + port + "\" protocol=\"" + proto + "\"" + logging + " " + str(item["action"])})
		} else {
			value := "port=" + port + ":proto=" + proto + ":toport=" + str(item["target_port"])
			if target := str(item["target_ip"]); target != "" {
				value += ":toaddr=" + target
			}
			if source == "" && num(item["priority"]) == 0 && str(item["family"]) == "" {
				out = append(out, [2]string{"forward-port", value})
			} else {
				rule := prefix + " forward-port port=\"" + port + "\" protocol=\"" + proto + "\" to-port=\"" + str(item["target_port"]) + "\""
				if target := str(item["target_ip"]); target != "" {
					rule += " to-addr=\"" + target + "\""
				}
				out = append(out, [2]string{"rich-rule", rule})
			}
		}
	}
	return out
}
func firewallOption(verb string, spec [2]string) string {
	value := "--" + verb + "-" + spec[0]
	if spec[1] != "" {
		value += "=" + spec[1]
	}
	return value
}
func firewallCommand(zone, verb string, spec [2]string, permanent bool) []string {
	args := []string{"firewall-cmd"}
	if spec[0] != "direct-rule" {
		args = append(args, "--zone="+zone)
	}
	if permanent {
		args = append(args, "--permanent")
	}
	if spec[0] == "direct-rule" {
		args = append(args, "--direct", "--"+verb+"-rule")
		return append(args, strings.Fields(spec[1])...)
	}
	return append(args, firewallOption(verb, spec))
}
func (n *Network) queryFirewall(zone string, spec [2]string, permanent bool) (bool, error) {
	args := firewallCommand(zone, "query", spec, permanent)
	output, e := n.app.command(6, "", args...)
	value := strings.TrimSpace(output)
	if value == "yes" {
		return true, nil
	}
	if value == "no" {
		return false, nil
	}
	if e != nil {
		return false, errors.New("无法确认现有防火墙规则")
	}
	return false, nil
}
func (n *Network) planFirewall(p *NetworkPlan, d M) error {
	if firewallDriver(obj(d["item"])) != "firewalld" {
		return n.planNativeFirewall(p, d)
	}
	raw := obj(d["item"])
	if str(raw["kind"]) == "cross-zone" {
		return n.planCrossZone(p, d)
	}
	if str(raw["kind"]) == "zone-interface" {
		return n.planZoneInterface(p, d)
	}
	if str(raw["kind"]) == "zone-source" || str(raw["kind"]) == "zone-policy" {
		if str(n.app.service("firewalld")["state"]) != "active" {
			return errors.New("firewalld未运行，不能变更区域配置")
		}
		if str(raw["kind"]) == "zone-source" {
			return n.planZoneSource(p, d)
		}
		return n.planZonePolicy(p, d)
	}
	op := str(d["operation"])
	if str(raw["kind"]) == "service" {
		if !yes(d["allow_protected"]) {
			return errors.New("防火墙服务操作需要明确确认入站连接风险")
		}
		if op != "start" && op != "stop" && op != "restart" && op != "enable" && op != "disable" {
			return errors.New("服务操作无效")
		}
		state := n.app.service("firewalld")
		undo := "stop"
		if str(state["state"]) == "active" {
			undo = "start"
		}
		if op == "enable" || op == "disable" {
			undo = "disable"
			if yes(state["autostart"]) {
				undo = "enable"
			}
		}
		p.steps = []Step{{Args: []string{"systemctl", op, "firewalld"}}}
		p.undo = []Step{{Args: []string{"systemctl", undo, "firewalld"}}}
		p.Summary = "firewalld 服务 · " + op
		p.Warnings = append(p.Warnings, "请确认管理端口已放行；未在期限内确认将恢复原服务状态")
		return nil
	}
	if str(n.app.service("firewalld")["state"]) != "active" {
		return errors.New("firewalld 未运行，请先查看永久规则并启动服务")
	}
	item, e := normalizeFirewall(raw)
	if e != nil {
		return e
	}
	if str(item["kind"]) == "masquerade" && !yes(d["allow_protected"]) {
		return errors.New("SNAT 会影响转发流量，需要确认本次变更")
	}
	if str(item["action"]) == "snat" && op != "delete" && yes(item["enabled"]) {
		zone, err := n.app.command(6, "", "firewall-cmd", "--get-zone-of-interface="+str(item["out_interface"]))
		if err != nil || strings.TrimSpace(zone) != str(item["zone"]) {
			return errors.New("SNAT 出接口必须属于所选 firewalld 区域")
		}
	}
	identity := str(item["id"])
	if identity == "" {
		identity = id()
	}
	item["id"] = identity
	old := n.ownedItem("firewall", identity)
	if op == "adopt" {
		item["id"] = id()
		found := false
		for _, spec := range firewallSpecs(item) {
			for _, perm := range []bool{false, true} {
				present, err := n.queryFirewall(str(item["zone"]), spec, perm)
				if err != nil {
					return err
				}
				found = found || present
			}
		}
		if !found {
			return errors.New("当前系统中未找到此规则")
		}
		p.Summary = "纳入管理 · " + str(item["kind"]) + " · " + str(item["zone"])
		p.persist = M{"operation": "add", "item": item}
		p.Warnings = append(p.Warnings, "不重复添加规则；确认后才允许 FoXray 编辑该规则")
		return nil
	}
	if op != "add" && op != "replace" && op != "delete" {
		return errors.New("规则操作无效")
	}
	if op != "add" && old == nil {
		return errors.New("请先将外部规则纳入管理")
	}
	if old != nil {
		old, e = normalizeFirewall(old)
		if e != nil {
			return e
		}
	}
	type desiredSpec struct {
		zone string
		spec [2]string
	}
	oldSpecs, newSpecs := map[string]desiredSpec{}, map[string]desiredSpec{}
	collect := func(rule M, dst map[string]desiredSpec) {
		for _, spec := range firewallSpecs(rule) {
			key := str(rule["zone"]) + "|" + spec[0] + "|" + compactRule(spec[1])
			dst[key] = desiredSpec{str(rule["zone"]), spec}
		}
	}
	if old != nil && yes(old["enabled"]) {
		collect(old, oldSpecs)
	}
	if op != "delete" && yes(item["enabled"]) {
		collect(item, newSpecs)
	}
	change := func(entry desiredSpec, adding bool) error {
		for _, perm := range []bool{false, true} {
			present, err := n.queryFirewall(entry.zone, entry.spec, perm)
			if err != nil {
				return err
			}
			if adding && present {
				if op == "add" {
					return errors.New("目标规则已存在，请查看或纳入管理")
				}
				continue
			}
			if !adding && !present {
				continue
			}
			verb, reverse := "add", "remove"
			if !adding {
				verb, reverse = reverse, verb
			}
			p.steps = append(p.steps, Step{Args: firewallCommand(entry.zone, verb, entry.spec, perm)})
			p.undo = append(p.undo, Step{Args: firewallCommand(entry.zone, reverse, entry.spec, perm)})
		}
		return nil
	}
	// Reconcile only differences: preserve overlapping rules and repair missing copies.
	for key, entry := range oldSpecs {
		if _, keep := newSpecs[key]; !keep {
			if e = change(entry, false); e != nil {
				return e
			}
		}
	}
	for _, entry := range newSpecs {
		if e = change(entry, true); e != nil {
			return e
		}
	}

	p.persist = M{"operation": op, "item": item}
	p.Summary = "防火墙 " + op + " · " + str(item["kind"]) + " · " + str(item["zone"]) + " · " + firstNonempty(str(item["port"]), str(item["source"]))
	p.Warnings = append(p.Warnings, "运行与永久规则同时处理；不清空其他规则，不执行全局 reload")
	if str(item["kind"]) == "forward" {
		p.Warnings = append(p.Warnings, "目标主机须具备正确回程路由；区域地址伪装需单独明确配置")
	}
	return nil
}
