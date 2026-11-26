package gateway

import (
	"errors"
	"path/filepath"
	"strings"
)

const wgProtectionKey = "wireguard_protection"

type wgProtectionRename struct {
	From, To  string
	Protected bool
}

// Call with n.mu held after replacing the managed metadata snapshot.
func (n *Network) syncWGProtectionLocked() {
	n.wgProtectionMu.Lock()
	defer n.wgProtectionMu.Unlock()
	n.wgProtection = clone(obj(n.managed[wgProtectionKey]))
}
func (n *Network) wireguardProtected(name string) bool {
	n.wgProtectionMu.RLock()
	defer n.wgProtectionMu.RUnlock()
	if value, found := n.wgProtection[name]; found {
		protected, valid := value.(bool)
		return !valid || protected
	}
	return name == "wg-home"
}
func (n *Network) wireguardProtectionSource(name string) string {
	n.wgProtectionMu.RLock()
	defer n.wgProtectionMu.RUnlock()
	if _, found := n.wgProtection[name]; found {
		return "user"
	}
	return "default"
}
func renameWireGuardProtection(candidate M, change *wgProtectionRename) {
	if change == nil {
		return
	}
	flags := clone(obj(candidate[wgProtectionKey]))
	delete(flags, change.From)
	flags[change.To] = change.Protected
	candidate[wgProtectionKey] = flags
}

// Update metadata only. No wg, wg-quick, routing or service mutations are performed.
func (n *Network) setWireGuardProtection(input M) (M, error) {
	enabled, valid := input["protected"].(bool)
	if !valid {
		return nil, errors.New("接口保护状态须为布尔值")
	}
	selected := arr(input["interfaces"])
	if len(selected) < 1 || len(selected) > 50 {
		return nil, errors.New("每次请选择1～50个接口")
	}
	names := []string{}
	seen := map[string]bool{}
	for _, raw := range selected {
		name, valid := raw.(string)
		if !valid || iface(name) != nil || seen[name] {
			return nil, errors.New("接口选择包含无效名称或重复项")
		}
		names = append(names, name)
		seen[name] = true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.app.mutate.Lock()
	defer n.app.mutate.Unlock()
	for _, plan := range n.plans {
		n.refreshPlan(plan)
		if plan.Status == "awaiting_confirmation" || plan.Status == "armed" {
			return nil, errors.New("请先确认或回退当前网络变更，再修改接口保护")
		}
	}
	if value := n.managed[wgProtectionKey]; value != nil {
		flags, valid := value.(map[string]any)
		if !valid {
			return nil, errors.New("接口保护配置无法读取，请先检查管理服务")
		}
		for _, flag := range flags {
			if _, valid := flag.(bool); !valid {
				return nil, errors.New("接口保护配置包含无效状态")
			}
		}
	}
	known := map[string]bool{}
	running, _ := n.app.command(5, "", "wg", "show", "interfaces")
	for _, name := range strings.Fields(running) {
		known[name] = true
	}
	paths, _ := filepath.Glob("/etc/wireguard/*.conf")
	for _, path := range paths {
		if regular(path) {
			known[strings.TrimSuffix(filepath.Base(path), ".conf")] = true
		}
	}
	for _, name := range names {
		if !known[name] {
			return nil, errors.New("所选 WireGuard 接口已不存在，请刷新后重试")
		}
	}
	next := clone(n.managed)
	flags := clone(obj(next[wgProtectionKey]))
	changes := []M{}
	for _, name := range names {
		changes = append(changes, M{"name": name, "before": n.wireguardProtected(name), "protected": enabled})
		flags[name] = enabled
	}
	next[wgProtectionKey] = flags
	if err := writeJSON(filepath.Join(n.app.base, "private", "network-managed.json"), next); err != nil {
		return nil, errors.New("接口保护保存失败，原状态保持不变")
	}
	n.managed = next
	n.syncWGProtectionLocked()
	return M{"ok": true, "items": changes, "protected": enabled, "count": len(names)}, nil
}
