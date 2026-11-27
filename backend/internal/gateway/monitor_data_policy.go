package gateway

import (
	"errors"
	"fmt"
	"path/filepath"
)

func standardMonitorData() M { return M{"interval": 10, "retention_days": 30, "max_storage_mb": 256} }
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
	overrides := M{"host:metrics": M{"interval": 5}, "xray:traffic": M{"max_storage_mb": 1024}}
	add := func(key string, legacy M) {
		row := clone(obj(overrides[key]))
		for _, field := range []string{"interval", "retention_days", "max_storage_mb"} {
			if v, exists := legacy[field]; exists && num(v) != num(M{"interval": 2, "retention_days": 30, "max_storage_mb": 256}[field]) {
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
	history := M{"xray:traffic": 10, "warp:traffic": 60, "wireguard:traffic": 60}
	for module, v := range legacyHistory {
		if num(v) >= 1 && num(v) <= 3600 {
			history[module+":traffic"] = v
		}
	}
	a.monitorData = M{"defaults": defaults, "overrides": overrides, "history_intervals": history, "history_default": 30}
	var persisted M
	if readJSON(filepath.Join(a.base, "monitor-data-policy.json"), &persisted) == nil && persisted != nil {
		a.monitorData = persisted
	}
	stored := obj(a.monitorData["history_intervals"])
	if _, exists := stored["host:metrics"]; !exists {
		if value, err := integer(host["storage_interval"], 10, 3600); err == nil {
			stored["host:metrics"] = value
			a.monitorData["history_intervals"] = stored
		}
	}
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
		return M{"policy": clone(obj(a.monitorData["defaults"])), "storage_interval": a.monitorData["history_default"]}
	}
	return M{"policy": policy, "independent": len(obj(obj(a.monitorData["overrides"])[module+":"+kind])) > 0, "global": clone(obj(a.monitorData["defaults"])), "storage_interval": a.historyIntervalLocked(module, kind), "storage_override": num(obj(a.monitorData["history_intervals"])[module+":"+kind]) > 0, "storage_default": a.defaultHistoryIntervalLocked(kind)}
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
	historyOnly := yes(data["history_only"])
	storage, hasStorage := data["storage_interval"]
	if historyOnly && (!hasStorage || module == "all") {
		return errors.New("历史记录间隔设置无效")
	}
	if hasStorage && storage != nil {
		minimum := 1
		if kind == "metrics" {
			minimum = 10
		}
		if _, err := integer(storage, minimum, 3600); err != nil {
			return fmt.Errorf("历史记录间隔（秒）允许范围：%d～3600", minimum)
		}
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
	if hasStorage {
		if module == "all" {
			next["history_default"] = storage
		} else {
			history := obj(next["history_intervals"])
			if storage == nil {
				// Zero preserves an explicit choice to inherit over any legacy host setting.
				storage = 0
			}
			history[module+":"+kind] = storage
			next["history_intervals"] = history
		}
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
