package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (a *App) refreshWGGuard() error {
	ports := []string{}
	paths, _ := filepath.Glob("/etc/wireguard/*.conf")
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".conf")
		profile, e := readWG(name)
		if e == nil && profile.ListenPort > 0 {
			ports = append(ports, strconv.Itoa(profile.ListenPort))
		}
	}
	names, _ := a.command(4, "", "wg", "show", "interfaces")
	for _, name := range strings.Fields(names) {
		raw, _ := a.command(3, "", "wg", "show", name, "listen-port")
		port, e := strconv.Atoi(strings.TrimSpace(raw))
		if e == nil && port > 0 {
			ports = append(ports, strconv.Itoa(port))
		}
	}
	ports = uniq(ports)
	sort.Strings(ports)
	if len(ports) == 0 || uplink() == "" {
		return nil
	}
	text := "table inet foxray_wireguard_guard {\n chain output {\n type route hook output priority -166; policy accept;\n udp sport { " + strings.Join(ports, ", ") + " } meta mark set 0x100cf ct mark set 0x100cf\n }\n chain prerouting {\n type filter hook prerouting priority -166; policy accept;\n iifname " + strconv.Quote(uplink()) + " udp dport { " + strings.Join(ports, ", ") + " } meta mark set 0x100cf ct mark set 0x100cf\n }\n}\n"
	if _, e := a.command(3, "", "nft", "list", "table", "inet", "foxray_wireguard_guard"); e == nil {
		text = "delete table inet foxray_wireguard_guard\n" + text
	}
	if _, e := a.command(5, text, "nft", "--check", "-f", "-"); e != nil {
		return e
	}
	_, e := a.command(5, text, "nft", "-f", "-")
	return e
}
func (n *Network) reconcile() error {
	n.mu.Lock()
	managed := clone(n.managed)
	n.mu.Unlock()
	for _, v := range arr(managed["routes"]) {
		item := obj(v)
		if item["enabled"] != nil && !yes(item["enabled"]) {
			continue
		}
		plan := &NetworkPlan{Kind: "routes", Warnings: []string{}}
		if e := n.planRoute(plan, M{"operation": "add", "item": item, "allow_protected": false}); e != nil {
			continue
		}
		family := "-4"
		if num(item["family"]) == 6 {
			family = "-6"
		}
		rows, e := n.app.commandJSON("ip", "-j", family, "route", "show", "table", intstr(item["table"]))
		if e != nil {
			continue
		}
		present := false
		for _, v := range rows {
			r := obj(v)
			if firstNonempty(str(r["dst"]), "default") == str(item["dst"]) && num(r["metric"]) == num(item["metric"]) {
				present = true
				if str(r["dev"]) != str(item["dev"]) || str(r["gateway"]) != str(item["gateway"]) {
					n.app.audit("network.conflict", "持久路由与外部现状冲突，保持现状", false, M{"id": item["id"]})
				}
			}
		}
		if present {
			continue
		}
		for _, step := range plan.steps {
			if _, e := n.app.command(8, step.Input, step.Args...); e != nil {
				return e
			}
		}

	}
	for _, v := range arr(managed["rules"]) {
		item := obj(v)
		if item["enabled"] != nil && !yes(item["enabled"]) {
			continue
		}
		family := "-4"
		if num(item["family"]) == 6 {
			family = "-6"
		}
		rows, e := n.app.commandJSON("ip", "-j", family, "rule", "show")
		if e != nil {
			continue
		}
		occupied := false
		for _, r := range rows {
			if num(obj(r)["priority"]) == num(item["priority"]) {
				occupied = true
			}
		}
		if occupied {
			continue
		}
		plan := &NetworkPlan{Kind: "rules", Warnings: []string{}}
		if n.planRule(plan, M{"operation": "add", "item": item}) == nil {
			for _, step := range plan.steps {
				if _, e = n.app.command(8, step.Input, step.Args...); e != nil {
					return e
				}
			}
		}
	}
	for _, v := range arr(managed["firewall"]) {
		item := obj(v)
		if firewallDriver(item) == "firewalld" || firewallDriver(item) == "ufw" || !yes(item["enabled"]) || n.nativeFirewallPresent(item) {
			continue
		}
		steps, _, e := n.nativeSteps(item, true)
		if e != nil {
			return e
		}
		for _, step := range steps {
			if _, e = n.app.command(8, step.Input, step.Args...); e != nil {
				return e
			}
		}
	}
	return nil
}
func (n *Network) safeOwnedInput(kind, op string, item M) (M, error) {
	if op == "add" {
		copy := clone(item)
		delete(copy, "id")
		return copy, nil
	}
	old := n.ownedItem(kind, str(item["id"]))
	if old == nil {
		return nil, errors.New("受管对象不存在")
	}
	if op == "delete" {
		return old, nil
	}
	if num(old["family"]) != num(item["family"]) && item["family"] != nil {
		return nil, errors.New("编辑不能改变地址族，请新增独立对象")
	}
	return mergeMasked(old, item), nil
}
func (a *App) setWarpDisabled(disabled bool) {
	a.mu.Lock()
	a.warpDisabled = disabled
	a.mu.Unlock()
	preferences := M{}
	path := filepath.Join(a.base, "service-preferences.json")
	_ = readJSON(path, &preferences)
	preferences["warp_manual_stop"] = disabled
	_ = writeJSON(path, preferences)
}
func (a *App) maintainControlState() {
	if a.readonly || a.ctx.Err() != nil {
		return
	}
	a.mu.RLock()
	active, applied, last, issue := a.active, a.applied, a.lastSwitch, a.recoveryIssue
	a.mu.RUnlock()
	if issue != "" {
		return
	}
	pid, err := a.command(4, "", "systemctl", "show", "foxray-engine", "-p", "MainPID", "--value")
	n, parseErr := strconv.Atoi(strings.TrimSpace(pid))
	if err != nil || parseErr != nil || n <= 0 {
		return
	}
	cfg, cfgErr := os.ReadFile(filepath.Join(a.base, "config.json"))
	engine, engineErr := os.ReadFile(filepath.Join(a.base, "engine.json"))
	if cfgErr != nil || engineErr != nil || a.ctx.Err() != nil {
		return
	}
	_ = writeJSON(filepath.Join(a.base, "private", "control-state.json"), M{"time": now(), "active": active, "applied": applied, "last_switch": last, "engine_pid": strings.TrimSpace(pid), "config_hash": fingerprint(string(cfg)), "engine_hash": fingerprint(string(engine))})
}

// Engine ExecStopPost removes the capture table, so every node-driven reload
// must restore it before reporting success or committing new node metadata.
func (a *App) reloadNodeEngine(c, native M, active string, applied bool) error {
	a.mu.RLock()
	oldConfig, oldNative, oldActive := clone(a.cfg), clone(a.native), a.active
	a.mu.RUnlock()
	err := a.startEngine(c, native, active)
	if err == nil && applied {
		err = a.attachWithNative(c, native)
	}
	if err == nil {
		return nil
	}
	rollback := a.startEngine(oldConfig, oldNative, oldActive)
	if rollback == nil && applied {
		rollback = a.attachWithNative(oldConfig, oldNative)
	}
	if rollback != nil {
		return errors.New("Xray 重载或接管规则恢复失败；原配置恢复未确认，请检查 Xray 状态")
	}
	return errors.New("Xray 重载或接管规则恢复失败；已恢复原配置，未提交节点变更")
}

func (a *App) adoptControl(record M) (bool, error) {
	if len(record) == 0 || !yes(record["applied"]) || !yes(a.cfg["enabled"]) {
		return false, nil
	}
	if str(record["active"]) != str(a.cfg["selected"]) {
		return false, errors.New("已保存的出口与接管快照不一致")
	}
	pid, err := a.command(4, "", "systemctl", "show", "foxray-engine", "-p", "MainPID", "--value")
	if err != nil || strings.TrimSpace(pid) == "0" || strings.TrimSpace(pid) != str(record["engine_pid"]) {
		return false, errors.New("Xray 核心进程与接管快照不一致")
	}
	if _, err = a.command(4, "", "nft", "list", "table", "inet", "vpm"); err != nil {
		if a.readonly {
			return false, errors.New("Xray 接管规则缺失，只读模式未恢复")
		}
		if err = a.attach(a.cfg); err != nil {
			return false, errors.New("Xray 接管规则缺失且恢复失败")
		}
	}
	a.active, a.applied, a.lastSwitch = str(record["active"]), true, num(record["last_switch"])
	return true, nil
}
