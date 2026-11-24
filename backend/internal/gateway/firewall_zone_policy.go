package gateway

import (
	"errors"
	"net/netip"
	"sort"
	"strings"
)

type firewallZonePolicy struct {
	Forward       *bool    `json:"forward,omitempty"`
	Target        string   `json:"target"`
	Masquerade    bool     `json:"masquerade"`
	ICMPBlocks    []string `json:"icmp_blocks"`
	ICMPInversion bool     `json:"icmp_inversion"`
}

func zoneArgs(zone string, permanent bool) []string {
	args := []string{"firewall-cmd"}
	if permanent {
		args = append(args, "--permanent")
	}
	return append(args, "--zone="+zone)
}
func parseZoneDetails(raw string) M {
	out := M{}
	for _, line := range textLines(raw) {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 {
			out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return out
}
func (n *Network) readZonePolicy(zone string, permanent bool) (firewallZonePolicy, error) {
	raw, err := n.app.command(6, "", append(zoneArgs(zone, permanent), "--list-all")...)
	if err != nil {
		return firewallZonePolicy{}, errors.New("无法读取区域策略")
	}
	return parseZonePolicy(raw)
}
func parseZonePolicy(raw string) (firewallZonePolicy, error) {
	result := firewallZonePolicy{ICMPBlocks: []string{}}
	data := parseZoneDetails(raw)
	for _, key := range []string{"target", "masquerade", "icmp-blocks", "icmp-block-inversion"} {
		if _, ok := data[key]; !ok {
			return result, errors.New("区域策略读取不完整，未生成变更")
		}
	}
	result.Target = canonicalZoneTarget(str(data["target"]))
	if !validZoneTarget(result.Target) {
		return result, errors.New("区域默认行为无法识别，未生成变更")
	}
	if value := str(data["forward"]); value == "yes" || value == "no" {
		enabled := value == "yes"
		result.Forward = &enabled
	}
	result.Masquerade = str(data["masquerade"]) == "yes"
	result.ICMPInversion = str(data["icmp-block-inversion"]) == "yes"
	validSwitch := func(value any) bool { return str(value) == "yes" || str(value) == "no" }
	if !validSwitch(data["masquerade"]) || !validSwitch(data["icmp-block-inversion"]) {
		return result, errors.New("区域开关状态无效")
	}
	result.ICMPBlocks = strings.Fields(str(data["icmp-blocks"]))
	sort.Strings(result.ICMPBlocks)
	return result, nil
}
func (n *Network) zonePolicySnapshot(zone string) M {
	result := M{"complete": false, "runtime": M{}, "permanent": M{}, "icmp_types": []string{}}
	live, e1 := n.readZonePolicy(zone, false)
	saved, e2 := n.readZonePolicy(zone, true)
	raw, e3 := n.app.command(5, "", "firewall-cmd", "--get-icmptypes")
	if defaultZone, err := n.app.command(5, "", "firewall-cmd", "--get-default-zone"); err == nil {
		result["default_zone"] = strings.TrimSpace(defaultZone)
	}
	if e1 != nil || e2 != nil || e3 != nil {
		result["error"] = "区域运行时、永久策略或ICMP类型未完整读取"
		return result
	}
	result["runtime"], result["permanent"], result["icmp_types"], result["complete"] = live, saved, strings.Fields(raw), true
	result["different"] = fingerprint(live) != fingerprint(saved)
	return result
}
func (n *Network) planZonePolicy(p *NetworkPlan, d M) error {
	item := obj(d["item"])
	zone := str(item["zone"])
	if !firewallZoneName.MatchString(zone) || str(d["operation"]) != "add" {
		return errors.New("区域策略请求无效")
	}
	if !yes(d["allow_protected"]) {
		return errors.New("区域策略变更需要确认管理连接及转发影响")
	}
	desiredMasq, ok := item["masquerade"].(bool)
	if !ok {
		return errors.New("地址伪装必须为开关值")
	}
	desiredInversion, ok := item["icmp_inversion"].(bool)
	if !ok {
		return errors.New("ICMP反向匹配必须为开关值")
	}
	live, err := n.readZonePolicy(zone, false)
	if err != nil {
		return err
	}
	saved, err := n.readZonePolicy(zone, true)
	if err != nil {
		return err
	}
	desiredTarget := saved.Target
	if item["target"] != nil {
		value, valid := item["target"].(string)
		if !valid || !validZoneTarget(value) {
			return errors.New("默认行为须为default、ACCEPT、DROP或REJECT")
		}
		desiredTarget = value
	}
	targetApply := firstNonempty(str(item["target_apply"]), "permanent")
	if targetApply != "permanent" && targetApply != "immediate" {
		return errors.New("默认行为生效方式无效")
	}
	targetChanged := desiredTarget != saved.Target || targetApply == "immediate" && desiredTarget != live.Target
	reload := targetChanged && targetApply == "immediate"
	if targetChanged && !yes(item["target_ack"]) {
		return errors.New("请明确确认默认行为可能影响管理连接和转发")
	}
	reloadBaseline := ""
	if reload {
		if !yes(item["reload_ack"]) {
			return errors.New("立即生效需要明确确认全局重新加载影响")
		}
		reloadBaseline, err = n.firewallReloadBaseline()
		if err != nil {
			return err
		}
	}
	raw, err := n.app.command(5, "", "firewall-cmd", "--get-icmptypes")
	if err != nil {
		return errors.New("无法核对ICMP类型")
	}
	allowed := map[string]bool{}
	for _, value := range strings.Fields(raw) {
		allowed[value] = true
	}
	values, ok := item["icmp_blocks"].([]any)
	if !ok || len(values) > 256 {
		return errors.New("ICMP阻断类型列表无效")
	}
	desired := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		name, valid := v.(string)
		if !valid || !allowed[name] {
			return errors.New("不支持的ICMP类型")
		}
		if !seen[name] {
			seen[name] = true
			desired = append(desired, name)
		}
	}
	sort.Strings(desired)
	changes := []any{}
	appendChange := func(args, reverse []string) {
		p.steps = append(p.steps, Step{Args: args})
		p.undo = append(p.undo, Step{Args: reverse})
		p.undoEnds = append(p.undoEnds, len(p.undo))
	}
	for i, before := range []firewallZonePolicy{live, saved} {
		permanent := i == 1
		args := zoneArgs(zone, permanent)
		addBool := func(name string, old, value bool) {
			if old == value {
				return
			}
			verb, undo := "--add-", "--remove-"
			if !value {
				verb, undo = undo, verb
			}
			appendChange(append(append([]string{}, args...), verb+name), append(append([]string{}, args...), undo+name))
		}
		addBool("masquerade", before.Masquerade, desiredMasq)
		afterForward := before.Forward
		if forward, ok := item["forward"].(bool); ok {
			if before.Forward == nil {
				return errors.New("此 firewalld 未提供区域转发状态")
			}
			addBool("forward", *before.Forward, forward)
			afterForward = &forward
		}
		// Remove inversion first before changing its list; apply desired inversion last.
		icmpChanged := fingerprint(before.ICMPBlocks) != fingerprint(desired) || before.ICMPInversion != desiredInversion
		if icmpChanged && before.ICMPInversion {
			addBool("icmp-block-inversion", true, false)
		}
		oldSet := map[string]bool{}
		for _, name := range before.ICMPBlocks {
			oldSet[name] = true
			if !seen[name] {
				appendChange(append(append([]string{}, args...), "--remove-icmp-block="+name), append(append([]string{}, args...), "--add-icmp-block="+name))
			}
		}
		for _, name := range desired {
			if !oldSet[name] {
				appendChange(append(append([]string{}, args...), "--add-icmp-block="+name), append(append([]string{}, args...), "--remove-icmp-block="+name))
			}
		}
		if icmpChanged && desiredInversion {
			addBool("icmp-block-inversion", false, true)
		}
		afterTarget := before.Target
		if permanent || reload {
			afterTarget = desiredTarget
		}
		changes = append(changes, M{"scope": map[bool]string{true: "永久配置", false: "运行时配置"}[permanent], "before": before, "after": firewallZonePolicy{Forward: afterForward, Target: afterTarget, Masquerade: desiredMasq, ICMPBlocks: desired, ICMPInversion: desiredInversion}})
	}
	if targetChanged {
		args := zoneArgs(zone, true)
		appendChange(append(append([]string{}, args...), "--set-target="+desiredTarget), append(append([]string{}, args...), "--set-target="+saved.Target))
		if reload {
			p.steps = append(p.steps, Step{Args: []string{"firewall-cmd", "--reload"}})
			// Reverse order: restore saved target, reload the original runtime target,
			// then undo the first permanent setter. Both original targets are equal
			// because the full runtime/permanent guard passed before this plan.
			p.undo = append(p.undo, Step{Args: []string{"firewall-cmd", "--reload"}}, Step{Args: append(append([]string{}, args...), "--set-target="+saved.Target)})
			p.undoEnds = append(p.undoEnds, len(p.undo))
		}
		p.postcondition = func() error {
			actual, e := n.readZonePolicy(zone, true)
			if e != nil {
				return e
			}
			if actual.Target != desiredTarget {
				return errors.New("默认行为写入后核对失败，已触发回退")
			}
			if reload {
				actual, e = n.readZonePolicy(zone, false)
				if e != nil {
					return e
				}
				if actual.Target != desiredTarget {
					return errors.New("运行时默认行为未生效，已触发回退")
				}
			}
			return nil
		}
	}
	// A globally managed masquerade is the same switch, not a second configuration source.
	n.mu.Lock()
	owned := clone(arr(n.managed["firewall"]))
	n.mu.Unlock()
	for _, v := range owned {
		r := obj(v)
		if firewallDriver(r) == "firewalld" && str(r["zone"]) == zone && str(r["kind"]) == "masquerade" && str(r["source"]) == "" && num(r["priority"]) == 0 && str(r["action"]) != "snat" {
			if p.persist != nil {
				return errors.New("存在重复的区域地址伪装管理声明，请先核对SNAT规则")
			}
			next := clone(r)
			next["enabled"] = desiredMasq
			p.persist = M{"operation": "replace", "item": next}
		}
	}
	if len(p.steps) == 0 && p.persist == nil {
		return errors.New("区域策略没有变化")
	}
	p.precondition = func() error {
		a, e := n.readZonePolicy(zone, false)
		if e != nil {
			return e
		}
		b, e := n.readZonePolicy(zone, true)
		if e != nil {
			return e
		}
		if fingerprint(a) != fingerprint(live) || fingerprint(b) != fingerprint(saved) {
			return errors.New("区域策略已变化，请重新预览")
		}
		if reload {
			current, e := n.firewallReloadBaseline()
			if e != nil {
				return e
			}
			if current != reloadBaseline {
				return errors.New("全局防火墙配置已变化，请重新预览")
			}
		}
		return nil
	}
	p.Details = M{"policy_changes": changes, "target_changed": targetChanged, "target_apply": targetApply, "reload_required": reload}
	p.Summary = "编辑区域策略 · " + zone
	p.Warnings = append(p.Warnings, "运行时和永久配置分别保留原值；应用后120秒内确认，未确认或中途失败由独立回退恢复。")
	if targetChanged && !reload {
		p.Warnings = append(p.Warnings, "默认行为仅写入永久配置：本次不重新加载，运行时默认行为保持原值；下次重新加载或启动firewalld后生效。")
	}
	if reload {
		p.Warnings = append(p.Warnings, "立即生效将重新加载整个firewalld。已核对区域、策略、直接规则、IP集合及引用服务的运行时与永久配置；应用前再次核对，有差异则拒绝重载。期间勿并发修改防火墙。")
	}
	if targetChanged {
		p.Warnings = append(p.Warnings, "ACCEPT会放宽区域默认处理，DROP／REJECT可能阻断未被具体允许的管理连接或转发；default不等于全部允许。请核对该区域及管理入口的放行规则。")
	}
	if desiredInversion {
		p.Warnings = append(p.Warnings, "已开启ICMP反向匹配：列表中的类型成为例外，其余ICMP类型会被阻断；空列表可能阻断所有ICMP。")
	}
	return nil
}

func (n *Network) planDefaultZone(plan *NetworkPlan, data M) error {
	zone := str(obj(data["item"])["zone"])
	if !yes(data["allow_protected"]) || !firewallZoneName.MatchString(zone) || str(data["operation"]) != "add" {
		return errors.New("需要选择有效的默认区域并确认影响")
	}
	read := func() (string, error) {
		raw, err := n.app.command(5, "", "firewall-cmd", "--get-default-zone")
		return strings.TrimSpace(raw), err
	}
	before, err := read()
	if err != nil || !firewallZoneName.MatchString(before) || before == zone {
		return errors.New("无法读取默认区域或区域未变化")
	}
	zones, err := n.app.command(5, "", "firewall-cmd", "--permanent", "--get-zones")
	if err != nil || !strings.Contains(" "+strings.Join(strings.Fields(zones), " ")+" ", " "+zone+" ") {
		return errors.New("目标区域不存在")
	}
	plan.steps = []Step{{Args: []string{"firewall-cmd", "--set-default-zone=" + zone}}}
	plan.undo = []Step{{Args: []string{"firewall-cmd", "--set-default-zone=" + before}}}
	plan.precondition = func() error {
		value, err := read()
		if err != nil || value != before {
			return errors.New("默认区域已变化，请重新预览")
		}
		return nil
	}
	plan.postcondition = func() error {
		value, err := read()
		if err != nil || value != zone {
			return errors.New("默认区域应用后核对失败")
		}
		return nil
	}
	plan.Summary = "修改默认区域 · " + before + " → " + zone
	plan.Warnings = append(plan.Warnings, "会影响使用默认区域的流量。应用后 120 秒内确认，否则回退。")
	return nil
}

func canonicalSource(raw string) (string, netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if address, err := netip.ParseAddr(raw); err == nil && !address.Is4In6() {
		return address.String(), netip.PrefixFrom(address, address.BitLen()), nil
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil && !prefix.Addr().Is4In6() {
		prefix = prefix.Masked()
		return prefix.String(), prefix, nil
	}
	return "", netip.Prefix{}, errors.New("来源须为有效IPv4／IPv6地址或网段")
}

type sourceBinding struct {
	Zone      string `json:"zone"`
	Source    string `json:"source"`
	Permanent bool   `json:"permanent"`
}

func (n *Network) sourceBindings() ([]sourceBinding, error) {
	out := []sourceBinding{}
	for _, permanent := range []bool{false, true} {
		args := []string{"firewall-cmd"}
		if permanent {
			args = append(args, "--permanent")
		}
		raw, err := n.app.command(5, "", append(args, "--get-zones")...)
		if err != nil {
			return nil, errors.New("无法读取区域列表")
		}
		zones := strings.Fields(raw)
		if len(zones) == 0 {
			return nil, errors.New("区域列表为空")
		}
		sort.Strings(zones)
		for _, zone := range zones {
			if !firewallZoneName.MatchString(zone) {
				return nil, errors.New("区域列表包含无效名称")
			}
			raw, e := n.app.command(5, "", append(zoneArgs(zone, permanent), "--list-sources")...)
			if e != nil {
				return nil, errors.New("无法完整读取来源归属")
			}
			for _, source := range strings.Fields(raw) {
				out = append(out, sourceBinding{Zone: zone, Source: source, Permanent: permanent})
			}
		}
	}
	return out, nil
}
func (n *Network) planZoneSource(p *NetworkPlan, d M) error {
	item := obj(d["item"])
	zone := str(item["zone"])
	action := str(item["binding_action"])
	if str(d["operation"]) != "add" || !firewallZoneName.MatchString(zone) || (action != "bind" && action != "unbind") {
		return errors.New("来源归属请求无效")
	}
	if !yes(d["allow_protected"]) {
		return errors.New("来源归属会改变整套区域策略，需要确认本次变更")
	}
	source, prefix, err := canonicalSource(str(item["source"]))
	if err != nil {
		return err
	}
	before, err := n.sourceBindings()
	if err != nil {
		return err
	}
	// Check the selected target exists in both live and permanent inventory.
	for _, permanent := range []bool{false, true} {
		args := []string{"firewall-cmd"}
		if permanent {
			args = append(args, "--permanent")
		}
		raw, e := n.app.command(5, "", append(args, "--get-zones")...)
		if e != nil {
			return errors.New("无法核对目标区域")
		}
		found := false
		for _, z := range strings.Fields(raw) {
			found = found || z == zone
		}
		if !found {
			return errors.New("目标区域不存在于运行时或永久配置")
		}
	}
	changes := []any{}
	for _, permanent := range []bool{false, true} {
		var old *sourceBinding
		for _, binding := range before {
			if binding.Permanent != permanent {
				continue
			}
			_, other, e := canonicalSource(binding.Source)
			if e != nil {
				continue
			}
			if prefix == other {
				if old != nil {
					return errors.New("来源存在重复归属，请先核对")
				}
				copy := binding
				old = &copy
			} else if action == "bind" && prefix.Overlaps(other) {
				return errors.New("来源与现有地址网段重叠，请明确范围后再绑定")
			}
		}
		if action == "unbind" && (old == nil || old.Zone != zone) {
			continue
		}
		if action == "bind" && old != nil && old.Zone == zone {
			continue
		}
		appendChange := func(args, undo []string) {
			p.steps = append(p.steps, Step{Args: args})
			p.undo = append(p.undo, Step{Args: undo})
		}
		if old != nil {
			args := zoneArgs(old.Zone, permanent)
			appendChange(append(append([]string{}, args...), "--remove-source="+old.Source), append(append([]string{}, args...), "--add-source="+old.Source))
		}
		target := "未绑定"
		if action == "bind" {
			args := zoneArgs(zone, permanent)
			appendChange(append(append([]string{}, args...), "--add-source="+source), append(append([]string{}, args...), "--remove-source="+source))
			target = zone
		}
		previous := "未绑定"
		if old != nil {
			previous = old.Zone
		}
		changes = append(changes, M{"scope": map[bool]string{true: "永久配置", false: "运行时配置"}[permanent], "source": source, "before": previous, "after": target})
	}
	if len(p.steps) == 0 {
		return errors.New("来源归属没有变化")
	}
	p.precondition = func() error {
		current, e := n.sourceBindings()
		if e != nil {
			return e
		}
		if fingerprint(current) != fingerprint(before) {
			return errors.New("来源归属已变化，请重新预览")
		}
		return nil
	}
	p.Details = M{"source_changes": changes}
	p.Summary = "来源地址归属 · " + source + " → " + map[bool]string{true: "解除绑定", false: zone}[action == "unbind"]
	p.Warnings = append(p.Warnings, "来源归属决定适用区域，不是访问白名单；本次同时处理运行时和永久配置，移入前解除原区域绑定，失败或未确认按各自原归属回退。不修改其他来源、接口或默认区域。")
	return nil
}
