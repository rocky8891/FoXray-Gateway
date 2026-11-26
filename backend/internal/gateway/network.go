package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Step struct {
	Args  []string `json:"args"`
	Input string   `json:"input,omitempty"`
}
type NetworkPlan struct {
	ID                          string                `json:"id"`
	Kind                        string                `json:"kind"`
	Summary                     string                `json:"summary"`
	Status                      string                `json:"status"`
	Created                     float64               `json:"created"`
	Expires                     float64               `json:"expires"`
	Warnings                    []string              `json:"warnings"`
	Preview                     []string              `json:"preview"`
	ZoneChanges                 []ZoneInterfaceChange `json:"zone_changes,omitempty"`
	Details                     M                     `json:"details,omitempty"`
	Rollback                    []string              `json:"rollback,omitempty"`
	steps, undo                 []Step
	undoEnds                    []int
	undoStates                  [][]Step
	applyFiles                  func() error
	undoFiles                   func() error
	persist                     M
	restoreFile                 *FileRestore
	restoreFiles                []*FileRestore
	beforeRestore, afterRestore []Step
	precondition                func() error
	postcondition               func() error
	revision                    string
}
type Network struct {
	app          *App
	mu           sync.Mutex
	plans        map[string]*NetworkPlan
	managed      M
	peerHealthMu sync.RWMutex
	peerHealth   map[string]wgPeerHealth
}

func NewNetwork(a *App) *Network {
	n := &Network{app: a, plans: map[string]*NetworkPlan{}, managed: M{"routes": []any{}, "rules": []any{}, "firewall": []any{}}}
	_ = readJSON(filepath.Join(a.base, "private", "network-managed.json"), &n.managed)
	n.recoverPlans()
	return n
}
func (n *Network) rollbackPending() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.plans {
		if p.Status == "awaiting_confirmation" {
			n.rollback(p)
		}
	}
}
func (n *Network) rollback(p *NetworkPlan, uiAction ...string) {
	_ = rollbackFile(n.specPath(p), n.app.runner)
	n.refreshPlan(p)
	n.app.audit("network.rollback", p.Summary+"："+p.Status, p.Status == "rolled_back", M{"plan_id": p.ID}, uiAction...)
}
func (n *Network) Apply(identity string) error { return n.ApplyProgress(n.app.ctx, identity, nil) }
func (n *Network) ApplyProgress(ctx context.Context, identity string, progress func(string)) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.plans[identity]
	if p == nil || p.Status != "preview" {
		return errors.New("变更预览不存在或已使用")
	}
	if progress != nil {
		progress("操作 · " + p.Summary)
	}
	if now()-p.Created > 600 {
		return errors.New("请重新生成变更预览")
	}
	for _, v := range n.plans {
		n.refreshPlan(v)
		if v.Status == "awaiting_confirmation" || v.Status == "armed" {
			return errors.New("请先确认或回退上一项网络变更")
		}
	}
	n.app.mutate.Lock()
	defer n.app.mutate.Unlock()
	if p.revision != "" && p.revision != fingerprint(n.managed) {
		return errors.New("规则已变化，请重新预览")
	}
	if p.precondition != nil {
		if e := p.precondition(); e != nil {
			return e
		}
	}
	if e := n.arm(p); e != nil {
		return e
	}
	deadline := time.UnixMilli(int64((p.Expires - 5) * 1000))
	ctx, cancelBudget := context.WithDeadline(ctx, deadline)
	defer cancelBudget()
	defer func() { p.applyFiles = nil; p.undoFiles = nil; p.precondition = nil; p.postcondition = nil }()
	if p.applyFiles != nil {
		if e := p.applyFiles(); e != nil {
			n.rollback(p, uiActionFromContext(ctx))
			return e
		}
	}
	done := 0
	for index, s := range p.steps {
		if ctx.Err() != nil {
			n.rollback(p, uiActionFromContext(ctx))
			return errors.New("已取消并回退本次变更")
		}
		description := s.Args[0]
		wgService := len(s.Args) == 3 && s.Args[0] == "systemctl" && strings.HasPrefix(s.Args[2], "wg-quick@")
		if wgService {
			description = printableCommand(s.Args)
		}
		if progress != nil {
			progress(fmt.Sprintf("应用步骤 %d / %d · %s", index+1, len(p.steps), description))
		}
		child, cancel := context.WithTimeout(ctx, 25*time.Second)
		_, e := n.app.runner.Run(child, s.Args, s.Input)
		cancel()
		if e != nil {
			if len(s.Args) > 0 && (s.Args[0] == "systemctl" || s.Args[0] == "firewall-cmd" || s.Args[0] == "nmcli") {
				_ = n.writeSpec(p, done+1)
			}
			n.rollback(p, uiActionFromContext(ctx))
			if wgService {
				return n.app.wireGuardServiceStepError(description, s.Args[2], e)
			}
			return fmt.Errorf("%s · 步骤 %d/%d（%s）：%w", p.Summary, index+1, len(p.steps), description, e)
		}
		done++
		if e := n.writeSpec(p, done); e != nil {
			n.rollback(p, uiActionFromContext(ctx))
			return e
		}
	}
	if ctx.Err() != nil {
		n.rollback(p, uiActionFromContext(ctx))
		return errors.New("已取消并回退本次变更")
	}
	if p.postcondition != nil {
		if e := p.postcondition(); e != nil {
			n.rollback(p, uiActionFromContext(ctx))
			return e
		}
	}
	p.Status = "awaiting_confirmation"
	if e := n.writeSpec(p, done); e != nil {
		n.rollback(p, uiActionFromContext(ctx))
		return e
	}
	n.app.audit("network.apply", p.Summary+"：等待确认", true, M{"plan_id": p.ID, "expires": p.Expires}, uiActionFromContext(ctx))
	return nil
}
func (n *Network) Confirm(identity string, uiAction ...string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.plans[identity]
	if p == nil {
		return errors.New("没有待确认变更")
	}
	path := n.specPath(p)
	unlock, e := lockPlan(path + ".lock")
	if e != nil {
		return e
	}
	defer unlock()
	var spec rollbackSpec
	if readJSON(path, &spec) != nil {
		return errors.New("回退计划无法读取")
	}
	if spec.Status != "awaiting_confirmation" || spec.Expires-now() < 5 {
		return errors.New("确认期限已到或即将到，请等待回退后重新应用")
	}
	oldManaged := clone(n.managed)
	candidate := clone(n.managed)
	if p.persist != nil {
		changes := arr(p.persist["changes"])
		if len(changes) == 0 {
			changes = []any{p.persist}
		}
		for _, change := range changes {
			c := obj(change)
			key := p.Kind
			item := obj(c["item"])
			next := []any{}
			for _, v := range arr(candidate[key]) {
				if obj(v)["id"] != item["id"] {
					next = append(next, v)
				}
			}
			if str(c["operation"]) != "delete" {
				next = append(next, item)
			}
			candidate[key] = next
		}
		if e = writeJSON(filepath.Join(n.app.base, "private", "network-managed.json"), candidate); e != nil {
			return e
		}
	}

	spec.Status = "confirmed"
	if e = writeJSON(path, spec); e != nil {
		_ = writeJSON(filepath.Join(n.app.base, "private", "network-managed.json"), oldManaged)
		return e
	}
	n.managed = candidate
	p.Status = "confirmed"
	n.app.audit("network.confirm", p.Summary+"：已确认", true, M{"plan_id": p.ID}, uiAction...)
	return nil
}
func (n *Network) Plans() []NetworkPlan {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := []NetworkPlan{}
	for _, p := range n.plans {
		n.refreshPlan(p)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}
func (n *Network) Preview(data M) (*NetworkPlan, error) {
	if len(arr(data["items"])) > 0 {
		return n.batchPreview(data)
	}
	kind, op := str(data["kind"]), str(data["operation"])
	n.mu.Lock()
	sourceRevision := fingerprint(n.managed)
	n.mu.Unlock()
	if op != "add" && op != "replace" && op != "delete" && op != "start" && op != "stop" && op != "restart" && op != "enable" && op != "disable" && op != "adopt" && !((op == "remark" || op == "peer-enable" || op == "peer-disable") && kind == "wireguard") {
		return nil, errors.New("网络操作无效")
	}
	if kind == "routes" || kind == "rules" || kind == "firewall" {
		item := clone(obj(data["item"]))
		if op == "add" {
			delete(item, "id")
		} else if op == "delete" || op == "replace" {
			old := n.ownedItem(kind, str(item["id"]))
			if old == nil {
				return nil, errors.New("只能修改 FoXray 管理的对象")
			}
			if kind == "firewall" && op == "replace" && (str(old["kind"]) == "cross-zone" || str(item["kind"]) == "cross-zone") && str(old["kind"]) != str(item["kind"]) {
				return nil, errors.New("跨区域规则不能转换为区域入站规则")
			}
			if kind == "routes" && (str(old["dev"]) == "wg-home" || str(old["dev"]) == "vone0") && !yes(data["allow_protected"]) {
				return nil, errors.New("原路由属于受保护 VPN，需要本次明确确认")
			}
			if op == "delete" {
				item = old
			} else {
				for k, v := range item {
					old[k] = v
				}
				item = old
			}
		}
		data = clone(data)
		data["item"] = item
	}
	p := &NetworkPlan{ID: id(), Kind: kind, Status: "preview", Created: now(), Warnings: []string{}, Preview: []string{}}
	var e error
	switch kind {
	case "dns":
		e = n.planDNS(p, data)
	case "interfaces":
		e = n.planInterface(p, data)
	case "routes":
		e = n.planRoute(p, data)
	case "rules":
		e = n.planRule(p, data)
	case "firewall":
		e = n.planFirewall(p, data)
	case "wireguard":
		e = n.planWireGuard(p, data)
	default:
		e = errors.New("网络模块无效")
	}
	if e != nil {
		return nil, e
	}
	for _, s := range p.steps {
		p.Preview = append(p.Preview, printableCommand(s.Args))
		if s.Input != "" {
			p.Preview = append(p.Preview, s.Input)
		}
	}
	for _, step := range p.beforeRestore {
		p.Rollback = append(p.Rollback, printableCommand(step.Args))
	}
	files := append([]*FileRestore(nil), p.restoreFiles...)
	if p.restoreFile != nil {
		files = append(files, p.restoreFile)
	}
	for _, file := range files {
		if file.Existed {
			p.Rollback = append(p.Rollback, "从受限备份恢复 "+file.Target)
		} else {
			p.Rollback = append(p.Rollback, "移除本次创建的配置 "+file.Target)
		}
	}
	for i := len(p.undo) - 1; i >= 0; i-- {
		p.Rollback = append(p.Rollback, printableCommand(p.undo[i].Args))
	}
	for _, step := range p.afterRestore {
		p.Rollback = append(p.Rollback, printableCommand(step.Args))
	}
	n.mu.Lock()
	if sourceRevision != fingerprint(n.managed) {
		n.mu.Unlock()
		return nil, errors.New("规则已变化，请重新预览")
	}
	p.revision = sourceRevision
	n.plans[p.ID] = p
	n.mu.Unlock()
	return p, nil
}
func (n *Network) Snapshot(kind string) M {
	a := n.app
	out := M{"plans": n.Plans()}
	switch kind {
	case "wireguard":
		out["items"] = n.wireguard()
	case "routes":
		r4, e4 := a.commandJSON("ip", "-j", "-4", "route", "show", "table", "all")
		r6, e6 := a.commandJSON("ip", "-j", "-6", "route", "show", "table", "all")
		out["ipv4"], out["ipv6"] = r4, r6
		out["complete"] = e4 == nil && e6 == nil
	case "rules":
		r4, e4 := a.commandJSON("ip", "-j", "-4", "rule", "show")
		r6, e6 := a.commandJSON("ip", "-j", "-6", "rule", "show")
		out["ipv4"], out["ipv6"] = r4, r6
		out["complete"] = e4 == nil && e6 == nil
	case "firewall":
		raw, e := a.command(8, "", "nft", "-j", "list", "ruleset")
		rules := M{}
		if e == nil {
			_ = json.Unmarshal([]byte(raw), &rules)
		}
		out["ruleset"] = rules
		out["firewalld"] = a.service("firewalld")
		out["complete"] = e == nil
		zones, _ := a.command(5, "", "firewall-cmd", "--get-active-zones")
		out["zones"] = zones
	}
	n.mu.Lock()
	out["managed"] = clone(n.managed[kind])
	n.mu.Unlock()
	return out
}
func (n *Network) Interfaces() []any {
	rows, _ := n.app.commandJSON("ip", "-j", "address", "show")
	return rows
}

var ruleToken = regexp.MustCompile(`^[A-Za-z0-9_./:-]+$`)

func (n *Network) wireguard() []M {
	a := n.app
	names := []string{}
	running, _ := a.command(5, "", "wg", "show", "interfaces")
	names = append(names, strings.Fields(running)...)
	paths, _ := filepath.Glob("/etc/wireguard/*.conf")
	for _, p := range paths {
		names = append(names, strings.TrimSuffix(filepath.Base(p), ".conf"))
	}
	rows := []M{}
	routeInventory := map[string][]M{}
	for _, name := range uniq(names) {
		active := false
		for _, r := range strings.Fields(running) {
			active = active || r == name
		}
		row := M{"name": name, "active": active, "configured": regular("/etc/wireguard/" + name + ".conf"), "service": a.service("wg-quick@" + name), "protected": name == "wg-home", "peers": []M{}}
		var profile *WGProfile
		if yes(row["configured"]) {
			var err error
			if profile, err = readWG(name); err == nil {
				row["remark"] = profile.Remark
				row["listen_port"] = profile.ListenPort
				row["configured_addresses"] = profile.Addresses
				row["peers"] = profile.Peers
				allowed := []string{}
				for _, peer := range profile.Peers {
					allowed = append(allowed, peer.AllowedIPs...)
				}
				candidates := []any{}
				for _, ip := range wgProbeCandidates(allowed, profile.Addresses) {
					candidates = append(candidates, ip)
				}
				row["probe_candidates"] = candidates
			}
		}
		if active {
			listen, _ := a.command(5, "", "wg", "show", name, "listen-port")
			row["listen_port"] = strings.TrimSpace(listen)
			h, _ := a.command(5, "", "wg", "show", name, "latest-handshakes")
			transfers, _ := a.command(5, "", "wg", "show", name, "transfer")
			endpoints, _ := a.command(5, "", "wg", "show", name, "endpoints")
			allowed, _ := a.command(5, "", "wg", "show", name, "allowed-ips")
			peers := map[string]M{}
			for _, entry := range []struct{ raw, key string }{{h, "handshake"}, {transfers, "transfer"}, {endpoints, "endpoint"}, {allowed, "allowed_ips"}} {
				for _, line := range textLines(entry.raw) {
					v := strings.Fields(line)
					if len(v) < 2 {
						continue
					}
					peer := peers[v[0]]
					if peer == nil {
						peer = M{"public_key": v[0]}
						peers[v[0]] = peer
					}
					peer[entry.key] = strings.Join(v[1:], " ")
				}
			}
			values := []M{}
			for _, v := range peers {
				values = append(values, v)
			}
			row["peers"] = mergeWGPeerRows(row["peers"], values)
			addresses, _ := a.commandJSON("ip", "-j", "address", "show", "dev", name)
			row["addresses"] = addresses
			local := []string{}
			for _, v := range addresses {
				for _, info := range arr(obj(v)["addr_info"]) {
					local = append(local, str(obj(info)["local"]))
				}
			}
			routes := []string{}
			for _, peer := range values {
				routes = append(routes, strings.Fields(str(peer["allowed_ips"]))...)
			}
			candidates := []any{}
			for _, ip := range wgProbeCandidates(routes, local) {
				candidates = append(candidates, ip)
			}
			row["probe_candidates"] = candidates
		}
		row["peers"] = n.decorateWGPeers(name, yes(row["active"]), profile, row["peers"], routeInventory)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return str(rows[i]["name"]) < str(rows[j]["name"]) })
	return rows
}

func (n *Network) ownedItem(kind, identity string) M {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, v := range arr(n.managed[kind]) {
		if str(obj(v)["id"]) == identity {
			return clone(obj(v))
		}
	}
	return nil
}
