package gateway

import (
	"errors"
	"path/filepath"
)

func (a *App) probeIntervals(module string) M {
	switch module {
	case "xray":
		a.mu.RLock()
		defer a.mu.RUnlock()
		return M{"interval": a.cfg["interval"], "enabled": latencyDetectionEnabled(a.cfg), "icmp_interval": a.icmp["interval"], "icmp_enabled": a.icmp["enabled"], "min": 0, "max": 3600}
	case "warp":
		p := a.warpMonitorPolicy()
		return M{"interval": p["interval"], "enabled": p["enabled"], "min": 10, "max": 3600}
	case "wireguard":
		p := a.wgMonitorPolicy()
		return M{"interval": p["interval"], "enabled": p["enabled"], "min": 10, "max": 3600}
	}
	return M{"error": "检测模块无效"}
}

func (a *App) saveProbeIntervals(module string, data M, uiAction ...string) error {
	minValue := 10
	if module == "xray" {
		minValue = 0
	} else if module != "warp" && module != "wireguard" {
		return errors.New("检测模块无效")
	}
	for key := range data {
		if key != "interval" && !(module == "xray" && key == "icmp_interval") {
			return errors.New("检测间隔包含未知字段")
		}
	}
	if _, err := integer(data["interval"], minValue, 3600); err != nil {
		if minValue == 0 {
			return errors.New("检测间隔允许范围：0～3600秒，0表示暂停周期检测")
		}
		return errors.New("检测间隔允许范围：10～3600秒")
	}
	if module == "xray" {
		if _, err := integer(data["icmp_interval"], 0, 3600); err != nil {
			return errors.New("ICMP检测间隔允许范围：0～3600秒，0表示暂停周期检测")
		}
		a.mutate.Lock()
		defer a.mutate.Unlock()
		a.mu.RLock()
		icmp := clone(a.icmp)
		a.mu.RUnlock()
		icmp["interval"] = data["icmp_interval"]
		return a.saveXraySettingsLocked(M{"interval": data["interval"], "icmp_policy": icmp}, uiAction...)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := clone(a.settings)
	key := module + "_monitor"
	p := clone(obj(next[key]))
	if p == nil {
		p = M{}
	}
	p["interval"] = data["interval"]
	next[key] = p
	if err := writeJSON(filepath.Join(a.base, "manager-settings.json"), next); err != nil {
		return err
	}
	a.settings = next
	return nil
}
