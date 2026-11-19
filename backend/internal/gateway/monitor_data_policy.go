package gateway

import (
	"errors"
	"fmt"
	"path/filepath"
)

func standardMonitorData() M { return M{"interval": 2, "retention_days": 30, "max_storage_mb": 256} }
func validMonitorData(module, kind string) bool {
	return module == "host" && (kind == "metrics" || kind == "connections") || (module == "xray" || module == "warp" || module == "wireguard") && (kind == "traffic" || kind == "latency" || kind == "connections")
}
func (a *App) loadMonitorData() {
	defaults := standardMonitorData()
	if n := num(a.settings["live_sampling_seconds"]); n >= 1 && n <= 300 {
		defaults["interval"] = n
	}
	if n := num(a.settings["measurement_retention_days"]); n >= 1 && n <= 365 {
		defaults["retention_days"] = n
	}
	overrides := M{}
	add := func(key string, legacy M) {
		row := M{}
		for _, field := range []string{"interval", "retention_days", "max_storage_mb"} {
			if v, exists := legacy[field]; exists && num(v) != num(standardMonitorData()[field]) {
				row[field] = v
			}
		}
		if len(row) > 0 {
			overrides[key] = row
		}
	}
	host := M{}
	_ = readJSON(filepath.Join(a.base, "host-monitor-policy.json"), &host)
	add("host:metrics", host)
	legacyTraffic := M{}
	_ = readJSON(filepath.Join(a.base, "traffic-monitor-policy.json"), &legacyTraffic)
	traffic := M{}
	_ = readJSON(filepath.Join(a.base, "traffic-monitor-modules.json"), &traffic)
	for _, module := range []string{"xray", "warp", "wireguard"} {
		pTraffic := obj(traffic[module])
		if len(pTraffic) == 0 {
			pTraffic = legacyTraffic
		}
		add(module+":traffic", pTraffic)
		p := M{}
		if v, ok := a.settings["measurement_"+module+"_days"]; ok {
			p["retention_days"] = v
		}
		if v, ok := a.settings["measurement_"+module+"_mb"]; ok {
			p["max_storage_mb"] = v
		}
		add(module+":latency", p)
	}
	legacyHistory := M{}
	_ = readJSON(filepath.Join(a.base, "traffic-history-policy.json"), &legacyHistory)
	history := M{}
	for module, v := range legacyHistory {
		if num(v) >= 1 && num(v) <= 3600 {
			history[module+":traffic"] = v
		}
	}
	a.monitorData = M{"defaults": defaults, "overrides": overrides, "history_intervals": history}
	_ = readJSON(filepath.Join(a.base, "monitor-data-policy.json"), &a.monitorData)
}
func (a *App) monitorDataPolicy(module, kind string) M {
	a.dataMu.RLock()
	defer a.dataMu.RUnlock()
	p := standardMonitorData()
	for key, value := range obj(a.monitorData["defaults"]) {
		p[key] = value
	}
	for key, value := range obj(obj(a.monitorData["overrides"])[module+":"+kind]) {
		p[key] = value
	}
	return p
}
func (a *App) monitorDataView(module, kind string) M {
	policy := a.monitorDataPolicy(module, kind)
	a.dataMu.RLock()
	defer a.dataMu.RUnlock()
	if module == "all" {
		return M{"policy": clone(obj(a.monitorData["defaults"]))}
	}
	return M{"policy": policy, "independent": len(obj(obj(a.monitorData["overrides"])[module+":"+kind])) > 0, "global": clone(obj(a.monitorData["defaults"])), "storage_interval": a.historyIntervalLocked(module, kind)}
}
func (a *App) saveMonitorData(module, kind string, data M) error {
	if module != "all" && !validMonitorData(module, kind) {
		return errors.New("监控数据类型无效")
	}
	allowed := map[string]bool{"interval": true, "retention_days": true, "max_storage_mb": true, "independent": true, "storage_interval": true, "history_only": true}
	for key := range data {
		if !allowed[key] {
			return errors.New("监控数据设置包含未知字段")
		}
	}
	if value, exists := data["storage_interval"]; exists {
		if module == "all" || kind == "metrics" {
			return errors.New("请在对应服务的监控项中设置历史记录间隔")
		}
		if _, err := integer(value, 1, 3600); err != nil {
			return errors.New("历史记录间隔（秒）允许范围：1～3600")
		}
	}
	historyOnly := yes(data["history_only"])
	if historyOnly && data["storage_interval"] == nil {
		return errors.New("历史记录间隔不能为空")
	}
	independent := true
	if module != "all" {
		var ok bool
		independent, ok = data["independent"].(bool)
		if !ok {
			return errors.New("请选择跟随全局或独立设置")
		}
	}
	if independent && !historyOnly {
		for key, bounds := range map[string][2]int{"interval": {1, 300}, "retention_days": {1, 365}, "max_storage_mb": {1, 10240}} {
			if _, err := integer(data[key], bounds[0], bounds[1]); err != nil {
				return fmt.Errorf("%s允许范围：%d～%d", map[string]string{"interval": "采集间隔（秒）", "retention_days": "保留周期（天）", "max_storage_mb": "保留大小（MiB）"}[key], bounds[0], bounds[1])
			}
		}
	}
	a.dataMu.Lock()
	defer a.dataMu.Unlock()
	next := clone(a.monitorData)
	policy := M{"interval": data["interval"], "retention_days": data["retention_days"], "max_storage_mb": data["max_storage_mb"]}
	if historyOnly {
	} else if module == "all" {
		next["defaults"] = policy
	} else {
		overrides := obj(next["overrides"])
		if independent {
			overrides[module+":"+kind] = policy
		} else {
			delete(overrides, module+":"+kind)
		}
		next["overrides"] = overrides
	}
	if value, exists := data["storage_interval"]; exists {
		history := obj(next["history_intervals"])
		history[module+":"+kind] = value
		next["history_intervals"] = history
	}
	if err := writeJSON(filepath.Join(a.base, "monitor-data-policy.json"), next); err != nil {
		return err
	}
	a.monitorData = next
	wakes := []chan struct{}{}
	if a.host != nil {
		wakes = append(wakes, a.host.wake)
	}
	if a.traffic != nil {
		wakes = append(wakes, a.traffic.wake)
	}
	if a.connections != nil {
		wakes = append(wakes, a.connections.wake)
	}
	for _, wake := range wakes {
		select {
		case wake <- struct{}{}:
		default:
		}
	}

	return nil
}
