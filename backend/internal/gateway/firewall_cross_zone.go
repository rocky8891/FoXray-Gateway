package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type crossZonePolicy struct {
	Priority               int
	Target                 string
	Ingress, Egress, Rules []string
	Extra                  bool
	Disabled               bool
	Masquerade             bool
}

func parseCrossZonePolicies(raw string) (map[string]crossZonePolicy, error) {
	result := map[string]crossZonePolicy{}
	name := ""
	rich := false
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			name = strings.Fields(line)[0]
			if !firewallZoneName.MatchString(name) {
				return nil, errors.New("跨区域策略名称无法解析")
			}
			result[name] = crossZonePolicy{Priority: -1}
			rich = false
			continue
		}
		if name == "" {
			return nil, errors.New("跨区域策略快照缺少名称")
		}
		p := result[name]
		text := strings.TrimSpace(line)
		if rich && strings.HasPrefix(text, "rule ") {
			p.Rules = append(p.Rules, compactRule(text))
		} else {
			rich = false
			key, value, ok := strings.Cut(text, ":")
			if !ok {
				p.Extra = true
			} else {
				value = strings.TrimSpace(value)
				switch key {
				case "target":
					p.Target = value
				case "ingress-zones":
					p.Ingress = strings.Fields(value)
				case "egress-zones":
					p.Egress = strings.Fields(value)
				case "rich rules":
					rich = true
					if value != "" {
						p.Extra = true
					}
				case "priority":
					priority, err := strconv.Atoi(value)
					if err != nil {
						return nil, errors.New("跨区域策略优先级无法解析")
					}
					p.Priority = priority
				case "ports", "services", "protocols", "source-ports", "forward-ports", "icmp-blocks", "helpers":
					if value != "" {
						p.Extra = true
					}
				case "disable":
					p.Disabled = value == "yes"
				case "masquerade", "icmp-block-inversion":
					if key == "masquerade" {
						p.Masquerade = value == "yes"
					}
					if value != "no" {
						p.Extra = true
					}
				case "description", "short", "active", "intra-zone-forwarding":
				default:
					if value != "" {
						p.Extra = true
					}
				}
			}
		}
		result[name] = p
	}
	for name, p := range result {
		if p.Target == "" {
			return nil, errors.New("跨区域策略快照不完整")
		}
		if len(p.Ingress) == 0 {
			p.Ingress = nil
		}
		if len(p.Egress) == 0 {
			p.Egress = nil
		}
		if len(p.Rules) == 0 {
			p.Rules = nil
		}
		sort.Strings(p.Ingress)
		sort.Strings(p.Egress)
		sort.Strings(p.Rules)
		result[name] = p
	}
	return result, nil
}

func crossRuntimeMatchesBaseline(runtime crossZonePolicy, baseline string) bool {
	if fingerprint(runtime) == baseline {
		return true
	}
	if runtime.Priority != -1 {
		return false
	}
	legacy := struct {
		Target                 string
		Ingress, Egress, Rules []string
		Extra, Disabled        bool
	}{runtime.Target, runtime.Ingress, runtime.Egress, runtime.Rules, runtime.Extra, runtime.Disabled}
	return fingerprint(legacy) == baseline
}

func normalizeCrossZone(item M) (M, error) {
	r := M{"kind": "cross-zone", "driver": "firewalld", "enabled": true}
	for _, key := range []string{"id", "name", "ingress_zone", "egress_zone", "source", "destination", "family", "protocol", "port", "action", "enabled", "apply_mode", "runtime_baseline", "priority"} {
		if value, ok := item[key]; ok {
			r[key] = value
		}
	}
	priority, err := integer(first(r["priority"], -1), -32768, 32767)
	if err != nil || priority == 0 {
		return nil, errors.New("跨区域优先级须为 -32768 至 32767 的非零整数")
	}
	r["priority"] = priority
	if firewallDriver(item) != "firewalld" {
		return nil, errors.New("跨区域规则仅支持firewalld")
	}
	for _, key := range []string{"ingress_zone", "egress_zone"} {
		zone := str(r[key])
		if !firewallZoneName.MatchString(zone) || zone == "ANY" || zone == "HOST" {
			return nil, errors.New("请选择具体入口、出口区域，不支持ANY或HOST")
		}
	}
	if r["ingress_zone"] == r["egress_zone"] {
		return nil, errors.New("入口区域和出口区域必须不同")
	}
	for _, key := range []string{"source", "destination"} {
		_, prefix, err := canonicalSource(str(r[key]))
		if err != nil {
			return nil, errors.New("来源和目标必须填写有效IP或网段")
		}
		r[key] = prefix.String()
	}
	family := "ipv4"
	if strings.Contains(str(r["source"]), ":") {
		family = "ipv6"
	}
	if strings.Contains(str(r["source"]), ":") != strings.Contains(str(r["destination"]), ":") {
		return nil, errors.New("来源与目标地址族必须一致")
	}
	if str(r["family"]) != "" && str(r["family"]) != family {
		return nil, errors.New("所选地址族与IP不一致")
	}
	r["family"] = family
	r["protocol"] = firstNonempty(str(r["protocol"]), "all")
	if !slices.Contains([]string{"all", "tcp", "udp", "tcpudp", "icmp"}, str(r["protocol"])) {
		return nil, errors.New("跨区域协议无效")
	}
	if slices.Contains([]string{"tcp", "udp", "tcpudp"}, str(r["protocol"])) && str(r["port"]) != "" {
		port, err := portRange(r["port"])
		if err != nil {
			return nil, err
		}
		r["port"] = port
	} else {
		r["port"] = ""
	}
	r["action"] = firstNonempty(str(r["action"]), "accept")
	if !slices.Contains([]string{"accept", "drop", "reject"}, str(r["action"])) {
		return nil, errors.New("跨区域动作无效")
	}
	if _, ok := r["enabled"].(bool); !ok {
		return nil, errors.New("规则状态必须为布尔值")
	}
	r["apply_mode"] = firstNonempty(str(r["apply_mode"]), "permanent")
	if !slices.Contains([]string{"permanent", "immediate"}, str(r["apply_mode"])) {
		return nil, errors.New("生效方式无效")
	}
	r["name"] = label(str(r["name"]))
	if len(str(r["name"])) > 240 {
		return nil, errors.New("规则名称过长")
	}
	if str(r["id"]) != "" && !regexpID(str(r["id"])) {
		return nil, errors.New("规则标识无效")
	}
	r["zone"] = "" // Policies are not zone INPUT rules.
	return r, nil
}

func crossZoneName(item M) string { return "fxc-" + str(item["id"])[:14] }
func crossZoneRules(item M) []string {
	if !yes(item["enabled"]) {
		return nil
	}
	prefix := fmt.Sprintf("rule family=\"%s\" source address=\"%s\" destination address=\"%s\"", str(item["family"]), str(item["source"]), str(item["destination"]))
	protocols := []string{str(item["protocol"])}
	if str(item["protocol"]) == "tcpudp" {
		protocols = []string{"tcp", "udp"}
	}
	rules := []string{}
	for _, protocol := range protocols {
		rule := prefix
		if protocol == "icmp" && item["family"] == "ipv6" {
			protocol = "ipv6-icmp"
		}
		if protocol != "all" {
			if str(item["port"]) != "" {
				rule += fmt.Sprintf(" port port=\"%s\" protocol=\"%s\"", str(item["port"]), protocol)
			} else {
				rule += " protocol value=\"" + protocol + "\""
			}
		}
		rules = append(rules, compactRule(rule+" "+str(item["action"])))
	}
	sort.Strings(rules)
	return rules
}

func (n *Network) readCrossZonePolicies(permanent bool, contexts ...context.Context) (map[string]crossZonePolicy, error) {
	args := []string{"firewall-cmd"}
	if permanent {
		args = append(args, "--permanent")
	}
	raw, err := n.firewallReadCommand(contexts...)(20, "", append(args, "--list-all-policies")...)
	if err != nil {
		return nil, errors.New("跨区域规则未完整读取；请确认firewalld支持Policy且正在运行")
	}
	return parseCrossZonePolicies(raw)
}

func (n *Network) CrossZoneSnapshot(contexts ...context.Context) M {
	result := M{"items": []M{}, "zones": []string{}, "complete": false}
	if str(n.app.service("firewalld")["state"]) != "active" {
		result["error"] = "firewalld未运行，无法管理跨区域规则"
		return result
	}
	command := n.firewallReadCommand(contexts...)
	live, saved, zoneNames, err := readCrossBundle(command)
	if err != nil {
		if len(contexts) > 0 && contexts[0] != nil && contexts[0].Err() != nil {
			result["error"] = "跨区域规则读取已取消"
			return result
		}
		var a, b, c error
		var zones string
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); live, a = n.readCrossZonePolicies(false, contexts...) }()
		go func() { defer wg.Done(); saved, b = n.readCrossZonePolicies(true, contexts...) }()
		go func() { defer wg.Done(); zones, c = command(20, "", "firewall-cmd", "--get-zones") }()
		wg.Wait()
		if a != nil || b != nil || c != nil {
			result["error"] = "跨区域规则或区域列表读取失败，请刷新重试"
			return result
		}
		zoneNames = strings.Fields(zones)
	}
	result["zones"] = zoneNames

	n.mu.Lock()
	managed := clone(arr(n.managed["firewall"]))
	n.mu.Unlock()
	rows := []M{}
	owned := map[string]bool{}
	for _, value := range managed {
		item := obj(value)
		if str(item["kind"]) != "cross-zone" {
			continue
		}
		row, err := normalizeCrossZone(item)
		if err != nil {
			result["error"] = "受管跨区域规则格式异常"
			return result
		}
		if !regexpID(str(row["id"])) {
			result["error"] = "受管规则缺少有效标识"
			return result
		}
		name := crossZoneName(row)
		owned[name] = true
		runtime, runtimeExists := live[name]
		permanent, savedExists := saved[name]
		expected := crossZonePolicy{Priority: int(num(row["priority"])), Target: "CONTINUE", Ingress: []string{str(row["ingress_zone"])}, Egress: []string{str(row["egress_zone"])}, Rules: crossZoneRules(row)}
		row["policy_name"], row["managed"], row["runtime"], row["permanent"] = name, true, runtimeExists && fingerprint(runtime) == fingerprint(expected), savedExists && fingerprint(permanent) == fingerprint(expected)
		row["drift"] = !runtimeExists || !savedExists || fingerprint(runtime) != fingerprint(expected) || fingerprint(permanent) != fingerprint(expected)
		row["runtime_raw"], row["permanent_raw"] = runtime, permanent
		rows = append(rows, row)
	}
	runtimeNames := map[string]bool{}
	for name := range live {
		runtimeNames[name] = true
	}
	for name, p := range saved {
		if _, exists := live[name]; !exists {
			live[name] = p
		}
	}
	for name, p := range live {
		if !owned[name] {
			_, permanent := saved[name]
			rows = append(rows, M{"id": "external-" + name, "policy_name": name, "name": name, "managed": false, "priority": p.Priority, "policy_target": p.Target, "policy_rules": p.Rules, "masquerade": p.Masquerade, "ingress_zone": strings.Join(p.Ingress, ", "), "egress_zone": strings.Join(p.Egress, ", "), "runtime": runtimeNames[name], "permanent": permanent, "runtime_raw": live[name], "permanent_raw": saved[name], "enabled": !p.Disabled && (len(p.Rules) > 0 || p.Target != "CONTINUE" || p.Extra)})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return str(rows[i]["policy_name"]) < str(rows[j]["policy_name"]) })
	result["items"], result["complete"] = rows, true
	return result
}

func crossCommand(name string, permanent bool, option string) []string {
	args := []string{"firewall-cmd"}
	if permanent {
		args = append(args, "--permanent")
	}
	return append(args, "--policy="+name, option)
}

type crossZonePlanSnapshot struct {
	live, saved map[string]crossZonePolicy
	zones       []string
}

func (n *Network) crossZonePlanSnapshot() (*crossZonePlanSnapshot, error) {
	live, err := n.readCrossZonePolicies(false)
	if err != nil {
		return nil, err
	}
	saved, err := n.readCrossZonePolicies(true)
	if err != nil {
		return nil, err
	}
	zones, err := n.app.command(20, "", "firewall-cmd", "--get-zones")
	if err != nil {
		return nil, errors.New("区域列表读取失败")
	}
	return &crossZonePlanSnapshot{live: live, saved: saved, zones: strings.Fields(zones)}, nil
}

func (n *Network) planCrossZone(p *NetworkPlan, d M) error {
	return n.planCrossZoneWithSnapshot(p, d, nil)
}

func (n *Network) planCrossZoneWithSnapshot(p *NetworkPlan, d M, snapshot *crossZonePlanSnapshot) error {
	if !yes(d["allow_protected"]) {
		return errors.New("请确认跨区域规则对管理连接和转发的影响")
	}
	if snapshot == nil && str(n.app.service("firewalld")["state"]) != "active" {
		return errors.New("firewalld未运行")
	}
	item, err := normalizeCrossZone(obj(d["item"]))
	if err != nil {
		return err
	}
	op := str(d["operation"])
	if !slices.Contains([]string{"add", "replace", "delete"}, op) {
		return errors.New("跨区域规则只支持新增、修改、删除")
	}
	if str(item["id"]) == "" {
		item["id"] = id()
	}
	old := n.ownedItem("firewall", str(item["id"]))
	if op != "add" && (old == nil || str(old["kind"]) != "cross-zone") {
		return errors.New("只能修改本应用管理的跨区域规则")
	}
	name := crossZoneName(item)
	if snapshot == nil {
		snapshot, err = n.crossZonePlanSnapshot()
		if err != nil {
			return err
		}
	}
	live, saved := snapshot.live, snapshot.saved
	runtime, runtimeExists := live[name]
	permanent, savedExists := saved[name]
	if op == "add" && (runtimeExists || savedExists) {
		return errors.New("同名策略已存在，请重新预览")
	}
	// A managed policy is dedicated to one rule. Never erase external edits.
	if old != nil {
		previous, e := normalizeCrossZone(old)
		if e != nil {
			return e
		}
		expected := crossZonePolicy{Priority: int(num(previous["priority"])), Target: "CONTINUE", Ingress: []string{str(previous["ingress_zone"])}, Egress: []string{str(previous["egress_zone"])}, Rules: crossZoneRules(previous)}
		if savedExists && fingerprint(permanent) != fingerprint(expected) {
			return errors.New("策略永久配置被外部修改，请先核对详情，不覆盖外部规则")
		}
		shell := expected
		shell.Ingress = nil
		shell.Egress = nil
		if runtimeExists && fingerprint(runtime) != fingerprint(expected) && !crossRuntimeMatchesBaseline(runtime, str(previous["runtime_baseline"])) && fingerprint(runtime) != fingerprint(shell) {
			return errors.New("运行时策略包含表单以外内容，不支持覆盖")
		}
	}
	for _, zone := range []string{str(item["ingress_zone"]), str(item["egress_zone"])} {
		if !slices.Contains(snapshot.zones, zone) {
			return errors.New("入口或出口区域不存在")
		}
	}
	immediate := item["apply_mode"] == "immediate"
	priorityReload := runtimeExists && runtime.Priority != int(num(item["priority"]))
	reload := immediate && (!runtimeExists || priorityReload) && op != "delete"
	if reload && priorityReload && (!savedExists || fingerprint(runtime) != fingerprint(permanent)) {
		return errors.New("策略运行时与永久配置不同，请先同步或仅保存永久配置")
	}
	baseline := ""
	var protection []crossZoneProtection
	if reload {
		if !yes(obj(d["item"])["reload_ack"]) {
			return errors.New("新增策略或修改优先级立即生效需要确认全局重载；也可仅保存永久配置")
		}
		baseline, protection, err = n.crossZoneReloadBaseline(name)
		if err != nil {
			return err
		}
	}
	appendStep := func(args, undo []string) {
		p.steps = append(p.steps, Step{Args: args})
		p.undo = append(p.undo, Step{Args: undo})
		p.undoEnds = append(p.undoEnds, len(p.undo))
	}
	// Permanent creation is safe until a checked reload activates the new policy.
	if !savedExists && op != "delete" {
		appendStep([]string{"firewall-cmd", "--permanent", "--new-policy=" + name}, []string{"firewall-cmd", "--permanent", "--delete-policy=" + name})
		permanent = crossZonePolicy{Target: "CONTINUE", Priority: -1}
	}
	desired := crossZonePolicy{Priority: int(num(item["priority"])), Target: "CONTINUE", Ingress: []string{str(item["ingress_zone"])}, Egress: []string{str(item["egress_zone"])}, Rules: crossZoneRules(item)}
	reconcile := func(before crossZonePolicy, perm bool, deleting bool) {
		if perm && !deleting && before.Priority != desired.Priority {
			appendStep(crossCommand(name, true, "--set-priority="+strconv.Itoa(desired.Priority)), crossCommand(name, true, "--set-priority="+strconv.Itoa(before.Priority)))
		}
		for _, rule := range before.Rules {
			if deleting || !slices.Contains(desired.Rules, rule) {
				appendStep(crossCommand(name, perm, "--remove-rich-rule="+rule), crossCommand(name, perm, "--add-rich-rule="+rule))
			}
		}
		if deleting {
			return
		}
		for _, pair := range []struct {
			key      string
			old, new []string
		}{{"ingress-zone", before.Ingress, desired.Ingress}, {"egress-zone", before.Egress, desired.Egress}} {
			for _, zone := range pair.old {
				if !slices.Contains(pair.new, zone) {
					appendStep(crossCommand(name, perm, "--remove-"+pair.key+"="+zone), crossCommand(name, perm, "--add-"+pair.key+"="+zone))
				}
			}
			for _, zone := range pair.new {
				if !slices.Contains(pair.old, zone) {
					appendStep(crossCommand(name, perm, "--add-"+pair.key+"="+zone), crossCommand(name, perm, "--remove-"+pair.key+"="+zone))
				}
			}
		}
		for _, rule := range desired.Rules {
			if !slices.Contains(before.Rules, rule) {
				appendStep(crossCommand(name, perm, "--add-rich-rule="+rule), crossCommand(name, perm, "--remove-rich-rule="+rule))
			}
		}
	}
	if immediate && runtimeExists && !reload {
		reconcile(runtime, false, op == "delete")
	}
	if savedExists || op != "delete" {
		reconcile(permanent, true, op == "delete")
	}
	if op == "delete" && savedExists {
		p.steps = append(p.steps, Step{Args: []string{"firewall-cmd", "--permanent", "--delete-policy=" + name}})
		// Stored reversed: recreate the shell before earlier inverse rule steps.
		for _, zone := range permanent.Egress {
			p.undo = append(p.undo, Step{Args: crossCommand(name, true, "--add-egress-zone="+zone)})
		}
		for _, zone := range permanent.Ingress {
			p.undo = append(p.undo, Step{Args: crossCommand(name, true, "--add-ingress-zone="+zone)})
		}
		if permanent.Priority != -1 {
			p.undo = append(p.undo, Step{Args: crossCommand(name, true, "--set-priority="+strconv.Itoa(permanent.Priority))})
		}
		p.undo = append(p.undo, Step{Args: []string{"firewall-cmd", "--permanent", "--new-policy=" + name}})
		p.undoEnds = append(p.undoEnds, len(p.undo))
	}
	if reload {
		policyUndo := append([]Step(nil), p.undo...)
		for _, rule := range protection {
			appendStep(protectionCommand(rule, true), protectionCommand(rule, false))
		}
		for _, end := range p.undoEnds {
			p.undoStates = append(p.undoStates, append([]Step(nil), p.undo[:end]...))
		}
		p.steps = append(p.steps, Step{Args: []string{"firewall-cmd", "--reload"}})
		p.undo = crossReloadUndo(policyUndo, protection, name, permanent, savedExists && !runtimeExists)
		p.undoStates = append(p.undoStates, append([]Step(nil), p.undo...))
		for _, rule := range protection {
			p.steps = append(p.steps, Step{Args: protectionCommand(rule, false)})
			p.undoStates = append(p.undoStates, append([]Step(nil), p.undo...))
		}
	}

	p.precondition = func() error {
		a, e := n.readCrossZonePolicies(false)
		if e != nil {
			return e
		}
		b, e := n.readCrossZonePolicies(true)
		if e != nil {
			return e
		}
		if fingerprint(a) != fingerprint(live) || fingerprint(b) != fingerprint(saved) {
			return errors.New("跨区域策略已变化，请重新预览")
		}
		if reload {
			current, _, e := n.crossZoneReloadBaseline(name)
			if e != nil {
				return e
			}
			if current != baseline {
				return errors.New("全局规则已变化，拒绝重载")
			}
		}
		return nil
	}

	item["runtime_baseline"] = ""
	if immediate && op != "delete" {
		item["runtime_baseline"] = fingerprint(desired)
	} else if runtimeExists {
		item["runtime_baseline"] = fingerprint(runtime)
	}
	p.postcondition = func() error {
		actualLive, e := n.readCrossZonePolicies(false)
		if e != nil {
			return e
		}
		actualSaved, e := n.readCrossZonePolicies(true)
		if e != nil {
			return e
		}
		expectedLive := map[string]crossZonePolicy{}
		for key, value := range live {
			expectedLive[key] = value
		}
		expectedSaved := map[string]crossZonePolicy{}
		for key, value := range saved {
			expectedSaved[key] = value
		}
		if op == "delete" {
			delete(expectedSaved, name)
			if immediate && runtimeExists {
				empty := runtime
				empty.Rules = nil
				expectedLive[name] = empty
			}
		} else {
			expectedSaved[name] = desired
			if immediate {
				expectedLive[name] = desired
			}
		}
		if fingerprint(actualSaved) != fingerprint(expectedSaved) || fingerprint(actualLive) != fingerprint(expectedLive) {
			return errors.New("跨区域规则应用后核对失败，已请求回退")
		}
		if reload {
			current, _, err := n.crossZoneReloadBaseline(name)
			if err != nil {
				return err
			}
			if current != baseline {
				return errors.New("重载后其他区域或策略发生变化，已请求回退")
			}
		}
		return nil
	}
	p.persist = M{"operation": op, "item": item}
	p.Summary = "跨区域规则 " + op + " · " + str(item["ingress_zone"]) + " → " + str(item["egress_zone"]) + " · " + str(item["source"]) + " → " + str(item["destination"])
	p.Details = M{"cross_zone": item, "policy_name": name, "reload_required": reload, "apply_mode": item["apply_mode"], "runtime_protection": protection}
	p.Warnings = append(p.Warnings, "规则只匹配入口→出口方向；不自动启用IP转发，不修改路由、NAT或WireGuard。管理连接和业务回程须单独核对。应用后120秒内确认，未确认自动回退。")
	if !immediate {
		p.Warnings = append(p.Warnings, "本次仅修改永久配置，运行时保持原状；下次重载或启动firewalld后生效。")
	}
	if reload {
		p.Warnings = append(p.Warnings, "新策略通过全局重载载入；会保留已核对的运行时富规则，完成后恢复原永久配置。其他配置差异仍阻止重载。")
	}
	if reload && len(protection) > 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("本次临时保护 %d 条仅运行时存在的区域规则；不会把这些临时规则永久保存。", len(protection)))
	}
	if reload && savedExists {
		p.Warnings = append(p.Warnings, "如发生回退，原永久配置将恢复；运行时可能保留未绑定入口、出口区域的无效策略外壳，不放行流量。")
	}
	if op == "delete" && immediate && runtimeExists {
		p.Warnings = append(p.Warnings, "删除后运行时空Policy保留至下次重载，已移除其所有匹配规则，默认CONTINUE不放行流量。")
	}
	return nil
}
