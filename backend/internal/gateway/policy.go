package gateway

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

func normalizePolicy(raw M) (M, error) {
	r := M{}
	for _, k := range []string{"id", "family", "priority", "from", "to", "fwmark", "table", "iif", "oif", "uidrange", "invert", "enabled"} {
		if v, ok := raw[k]; ok {
			r[k] = v
		}
	}
	r["family"] = 4
	if num(raw["family"]) == 6 {
		r["family"] = 6
	}
	pref, e := integer(r["priority"], 1, 32765)
	if e != nil {
		return nil, e
	}
	if pref >= 16060 && pref <= 16100 {
		return nil, errors.New("该优先级为代理与 VPN 隔离保留")
	}
	r["priority"] = pref
	table, e := routeTable(r["table"])
	if e != nil {
		return nil, e
	}
	if table == 253 || table == 255 || table == 16080 || table == 16082 || table == 65743 {
		return nil, errors.New("不能引用系统或代理内部路由表")
	}
	r["table"] = table
	for _, key := range []string{"from", "to"} {
		v := str(r[key])
		if v == "" || v == "all" {
			r[key] = "all"
			continue
		}
		if e = cidr(v); e != nil {
			return nil, e
		}
		ip := net.ParseIP(strings.Split(v, "/")[0])
		if ip == nil || ((ip.To4() == nil) != (num(r["family"]) == 6)) {
			return nil, errors.New("匹配地址与地址族不一致")
		}
	}
	for _, key := range []string{"iif", "oif"} {
		if v := str(r[key]); v != "" {
			if e = iface(v); e != nil {
				return nil, e
			}
		}
	}
	if mark := str(r["fwmark"]); mark != "" {
		parts := strings.Split(mark, "/")
		if len(parts) > 2 {
			return nil, errors.New("fwmark 掩码无效")
		}
		values := []uint64{}
		for _, p := range parts {
			v, err := strconv.ParseUint(p, 0, 32)
			if err != nil {
				return nil, errors.New("标记与掩码须为十进制或带 0x 的十六进制")
			}
			values = append(values, v)
		}
		mask := uint64(0xffffffff)
		if len(values) == 2 {
			mask = values[1]
		}
		if values[0]&mask == uint64(0x100cf)&mask {
			return nil, errors.New("标记匹配范围包含代理／VPN 保留标记")
		}
	}
	if value := str(r["uidrange"]); value != "" {
		parts := strings.Split(value, "-")
		if len(parts) != 2 {
			return nil, errors.New("UID 范围格式为起始UID-结束UID")
		}
		a, e1 := strconv.ParseUint(parts[0], 10, 32)
		b, e2 := strconv.ParseUint(parts[1], 10, 32)
		if e1 != nil || e2 != nil || a > b {
			return nil, errors.New("UID 范围无效")
		}
	}
	if r["enabled"] == nil {
		r["enabled"] = true
	}
	return r, nil
}
func policySpec(r M) []string {
	args := []string{"pref", intstr(r["priority"])}
	if yes(r["invert"]) {
		args = append(args, "not")
	}
	for _, key := range []string{"from", "to", "fwmark", "iif", "oif", "uidrange"} {
		if v := str(r[key]); v != "" {
			args = append(args, key, v)
		}
	}
	return append(args, "lookup", intstr(r["table"]))
}
func (n *Network) planRule(p *NetworkPlan, d M) error {
	if str(d["operation"]) == "adopt" {
		return n.adoptPolicy(p, obj(d["item"]))
	}
	item, e := normalizePolicy(obj(d["item"]))
	if e != nil {
		return e
	}
	op := str(d["operation"])
	if op != "add" && op != "replace" && op != "delete" {
		return errors.New("策略路由仅支持新增、编辑和删除；现有外部策略保持只读")
	}
	identity := str(item["id"])
	if identity == "" {
		identity = id()
	}
	item["id"] = identity
	old := n.ownedItem("rules", identity)
	if op == "add" {
		old = nil
	}
	if op != "add" && old == nil {
		return errors.New("只能修改 FoXray 管理的策略")
	}
	if old != nil {
		old, e = normalizePolicy(old)
		if e != nil {
			return e
		}
		if num(old["family"]) != num(item["family"]) {
			return errors.New("修改地址族请新增独立策略")
		}
	}
	if op != "delete" && yes(item["enabled"]) {
		rows, err := n.app.commandJSON("ip", "-j", familyFor(item), "rule", "show")
		if err != nil {
			return errors.New("无法检查策略优先级冲突")
		}
		for _, v := range rows {
			if num(obj(v)["priority"]) == num(item["priority"]) && (old == nil || !yes(old["enabled"]) || num(old["priority"]) != num(item["priority"])) {
				return errors.New("该优先级已被占用")
			}
		}
	}
	if old != nil && yes(old["enabled"]) {
		p.steps = append(p.steps, Step{Args: append([]string{"ip", familyFor(old), "rule", "del"}, policySpec(old)...)})
		p.undo = append(p.undo, Step{Args: append([]string{"ip", familyFor(old), "rule", "add"}, policySpec(old)...)})
	}
	if op != "delete" && yes(item["enabled"]) {
		p.steps = append(p.steps, Step{Args: append([]string{"ip", familyFor(item), "rule", "add"}, policySpec(item)...)})
		p.undo = append(p.undo, Step{Args: append([]string{"ip", familyFor(item), "rule", "del"}, policySpec(item)...)})
	}
	if num(item["priority"]) < 16070 {
		p.Warnings = append(p.Warnings, "该策略优先于 VPN 路由选择，请验证公司与回家 VPN")
	}
	p.Summary = "策略路由 " + op + " · 优先级 " + intstr(item["priority"])
	p.persist = M{"operation": op, "item": item}
	return nil
}

func (n *Network) adoptPolicy(p *NetworkPlan, input M) error {
	input = clone(input)
	input["from"], input["to"] = firstNonempty(str(input["from"]), str(input["src"])), firstNonempty(str(input["to"]), str(input["dst"]))
	item, err := normalizePolicy(input)
	if err != nil {
		return err
	}
	n.mu.Lock()
	ownedRules := clone(n.managed["rules"])
	n.mu.Unlock()
	for _, value := range arr(ownedRules) {
		owned := obj(value)
		if yes(owned["enabled"]) && num(owned["family"]) == num(item["family"]) && num(owned["priority"]) == num(item["priority"]) {
			return errors.New("该优先级的策略已纳管")
		}
	}
	wanted := strings.Join(policySpec(item), " ")
	check := func() error {
		rows, e := n.app.commandJSON("ip", "-j", familyFor(item), "rule", "show")
		if e != nil {
			return errors.New("无法核验系统策略")
		}
		matches := 0
		for _, value := range rows {
			raw := obj(value)
			if num(raw["priority"]) != num(item["priority"]) {
				continue
			}
			if value, exists := raw["fwmark"]; exists {
				if _, ok := value.(string); !ok {
					return errors.New("该策略标记不能等价导入，保留原归属")
				}
			}
			for key := range raw {
				switch key {
				case "priority", "src", "dst", "from", "to", "table", "family", "fwmark", "iif", "oif", "uidrange", "invert", "protocol":
				default:
					return errors.New("该系统策略包含不能等价管理的条件，保留原归属")
				}
			}
			candidate := clone(raw)
			candidate["family"] = item["family"]
			candidate["from"], candidate["to"] = firstNonempty(str(raw["from"]), str(raw["src"])), firstNonempty(str(raw["to"]), str(raw["dst"]))
			normalized, e := normalizePolicy(candidate)
			if e != nil {
				return e
			}
			if strings.Join(policySpec(normalized), " ") != wanted {
				return errors.New("同优先级系统策略与预览不一致")
			}
			matches++
		}
		if matches != 1 {
			return errors.New("系统策略不存在或优先级不唯一")
		}
		return nil
	}
	if err = check(); err != nil {
		return err
	}
	item["id"] = id()
	p.precondition = check
	p.persist = M{"operation": "add", "item": item}
	p.Summary = "纳管策略路由 · 优先级 " + intstr(item["priority"])
	p.Warnings = append(p.Warnings, "本次仅记录当前系统策略的归属，不删除或重建内核策略。")
	return nil
}
