package gateway

import (
	"errors"
	"path/filepath"
)

var monitorItems = map[string][]string{
	"host":      {"load", "cpu", "memory", "network", "disk", "connections"},
	"xray":      {"traffic", "latency", "connections"},
	"warp":      {"traffic", "latency", "connections"},
	"wireguard": {"traffic", "latency", "connections"},
}

func defaultMonitorItems() M {
	result := M{}
	for module, keys := range monitorItems {
		item := M{}
		for _, key := range keys {
			item[key] = true
		}
		if module == "host" {
			item["root"] = true
			item["layout"] = "single"
		}
		result[module] = item
	}
	return result
}
func (a *App) monitorItemsSnapshot() M {
	a.monitorMu.RLock()
	defer a.monitorMu.RUnlock()
	return clone(a.monitoringItems)
}
func (a *App) monitorEnabled(module, item string) bool {
	a.monitorMu.RLock()
	defer a.monitorMu.RUnlock()
	if enabled, exists := obj(a.monitoringItems["_modules"])[module].(bool); exists && !enabled {
		return false
	}
	value, exists := obj(a.monitoringItems[module])[item].(bool)
	return !exists || value
}

func (a *App) monitorModuleEnabled(module string) bool {
	a.monitorMu.RLock()
	defer a.monitorMu.RUnlock()
	value, exists := obj(a.monitoringItems["_modules"])[module].(bool)
	return !exists || value
}
func (a *App) monitorModules() M {
	a.monitorMu.RLock()
	defer a.monitorMu.RUnlock()
	values := M{}
	for module := range monitorItems {
		value, exists := obj(a.monitoringItems["_modules"])[module].(bool)
		values[module] = !exists || value
	}
	return values
}
func (a *App) saveMonitorModules(input M) error {
	if len(input) != len(monitorItems) {
		return errors.New("请提交服务器、Xray、WARP、WireGuard四个监控总开关")
	}
	for module := range monitorItems {
		if _, ok := input[module].(bool); !ok {
			return errors.New("监控总开关必须是开关值")
		}
	}
	a.monitorMu.Lock()
	defer a.monitorMu.Unlock()
	next := clone(a.monitoringItems)
	next["_modules"] = clone(input)
	if err := writeJSON(filepath.Join(a.base, "monitoring-items.json"), next); err != nil {
		return err
	}
	a.monitoringItems = next
	return nil
}

// Accept an old client's retired partition field without restoring it.
func canonicalHostMonitorItems(input M) M {
	out := clone(input)
	delete(out, "partition")
	if order, ok := out["order"].([]any); ok {
		next := []any{}
		for _, value := range order {
			if str(value) != "partition" {
				next = append(next, value)
			}
		}
		out["order"] = next
	}
	return out
}
func (a *App) saveMonitorItems(module string, input M) error {
	if module == "host" {
		input = canonicalHostMonitorItems(input)
		if input["root"] == nil {
			input["root"] = a.monitorEnabled("host", "root")
		}
	}
	keys, ok := monitorItems[module]
	if !ok {
		return errors.New("监控模块无效")
	}
	expected := len(keys)
	if module == "host" {
		expected++
		if _, ok := input["root"].(bool); !ok {
			return errors.New("根分区使用率必须为开关值")
		}
	}
	if input["runtime_order"] != nil {
		expected++
		if module != "host" || !validOrder(input["runtime_order"], []string{"load", "cpu", "memory", "root"}, true) {
			return errors.New("运行状态顺序须包含负载、CPU、内存、根分区且不能重复")
		}
	}
	if input["layout"] != nil {
		expected++
		if module != "host" || (str(input["layout"]) != "single" && str(input["layout"]) != "double") {
			return errors.New("服务器布局须为单排或双排")
		}
	}
	if input["order"] != nil {
		expected++
		if !validOrder(input["order"], keys, true) {
			return errors.New("监控项顺序须包含每个指标且不能重复")
		}
	}
	if len(input) != expected {
		return errors.New("请提交完整监控项开关")
	}
	for _, key := range keys {
		if _, ok := input[key].(bool); !ok {
			return errors.New("监控项必须是开关值：" + key)
		}
	}
	a.monitorMu.Lock()
	defer a.monitorMu.Unlock()
	next := clone(a.monitoringItems)
	saved := clone(input)
	if module == "host" && saved["runtime_order"] == nil && obj(a.monitoringItems[module])["runtime_order"] != nil {
		saved["runtime_order"] = clone(obj(a.monitoringItems[module])["runtime_order"])
	}
	next[module] = saved
	if err := writeJSON(filepath.Join(a.base, "monitoring-items.json"), next); err != nil {
		return err
	}
	a.monitoringItems = next
	return nil
}
