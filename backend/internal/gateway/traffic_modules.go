package gateway

import (
	"errors"
	"path/filepath"
	"strings"
	"time"
)

func (t *Traffic) modulePolicyLocked(module string) M {
	return t.app.monitorDataPolicy(monitorModule(module), "traffic")
}
func (t *Traffic) configureModule(module string, p M) error {
	if module != "xray" && module != "warp" && module != "wireguard" {
		return errors.New("监控模块无效")
	}
	for k := range p {
		if k != "interval" && k != "retention_days" && k != "max_storage_mb" {
			return errors.New("流量设置字段无效")
		}
	}
	if err := validateTrafficPolicy(p); err != nil {
		return err
	}
	newPolicy := clone(p)
	newPolicy["independent"] = true
	if err := t.app.saveMonitorData(module, "traffic", newPolicy); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	next := clone(t.modulePolicies)
	next[module] = clone(p)
	if e := writeJSON(filepath.Join(t.app.base, "traffic-monitor-modules.json"), next); e != nil {
		return e
	}
	t.modulePolicies = next
	t.recomputeIntervalLocked()
	select {
	case t.wake <- struct{}{}:
	default:
	}
	return t.pruneModulesLocked()
}
func (t *Traffic) moduleSnapshot(module string) M {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := monitorModule(module)
	var count int
	_ = t.app.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples WHERE " + monitorScope(s)).Scan(&count)
	return M{"policy": t.modulePolicyLocked(s), "used_bytes": count * 256, "usage_method": "conservative_estimate", "checked_at": now(), "storage_interval": t.historyIntervalLocked(s), "effective_interval": num(t.modulePolicyLocked(s)["interval"])}
}
func (t *Traffic) pruneModulesLocked() error {
	for _, s := range []string{"xray", "warp", "wireguard"} {
		p := t.modulePolicyLocked(s)
		scope := monitorScope(s)
		if _, e := t.app.histdb.Exec("DELETE FROM traffic_samples WHERE ("+scope+") AND t<?", now()-num(p["retention_days"])*86400); e != nil {
			return e
		}
		var count int
		_ = t.app.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples WHERE " + scope).Scan(&count)
		excess := count - int(num(p["max_storage_mb"])*1024*1024/256)
		if excess > 0 {
			if _, e := t.app.histdb.Exec("DELETE FROM traffic_samples WHERE rowid IN (SELECT rowid FROM traffic_samples WHERE "+scope+" ORDER BY t LIMIT ?)", excess); e != nil {
				return e
			}
		}
	}
	return nil
}
func (t *Traffic) wireguardCounters(counters map[string]trafficCounter, meta map[string]M) {
	if !t.app.moduleEnabled("wireguard") || !t.app.monitorModuleEnabled("wireguard") {
		return
	}
	if time.Since(t.wgNamesAt) > 30*time.Second {
		raw, _ := t.app.command(3, "", "wg", "show", "interfaces")
		t.wgNames = strings.Fields(raw)
		t.wgNamesAt = time.Now()
	}
	rows, err := t.app.interfaceCounters()
	byName := map[string]M{}
	for _, value := range rows {
		row := obj(value)
		byName[str(row["name"])] = row
	}
	for _, name := range t.wgNames {
		if iface(name) != nil {
			continue
		}
		row, found := byName[name]
		id := "wg:" + name
		meta[id] = M{"name": name, "source": "WireGuard"}
		status := "共享接口流量计数"
		if err != nil || !found {
			status = "WireGuard 接口计数不可用"
		}
		counters[id] = trafficCounter{Up: uint64(num(row["send_total"])), Down: uint64(num(row["receive_total"])), Available: err == nil && found, Status: status}
	}
}

func (t *Traffic) recomputeIntervalLocked() {
	interval := 300.
	for _, module := range []string{"xray", "warp", "wireguard"} {
		interval = min(interval, num(t.modulePolicyLocked(module)["interval"]))
	}
	t.policy["interval"] = interval
}
func trafficModuleMatches(module, id string) bool {
	switch module {
	case "":
		return true
	case "warp":
		return id == "warp"
	case "wireguard":
		return strings.HasPrefix(id, "wg:")
	default:
		return id != "warp" && !strings.HasPrefix(id, "wg:")
	}
}
