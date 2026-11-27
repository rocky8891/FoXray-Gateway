package gateway

import (
	"errors"
	"fmt"
	"path/filepath"
)

func (a *App) realtimeInterval(fallback float64) float64 {
	a.mu.RLock()
	value := num(a.settings["live_sampling_seconds"])
	a.mu.RUnlock()
	if value >= 1 && value <= 300 {
		return value
	}
	return fallback
}
func (a *App) displayPolicy() M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return M{"status_refresh_seconds": first(a.settings["status_refresh_seconds"], 5), "chart_refresh_seconds": first(a.settings["chart_refresh_seconds"], 15), "live_refresh_seconds": first(a.settings["live_refresh_seconds"], 5), "live_sampling_seconds": first(a.settings["live_sampling_seconds"], 0), "traffic_chart_window_seconds": first(a.settings["traffic_chart_window_seconds"], 30)}
}
func (a *App) saveDisplayPolicy(data M) error {
	bounds := map[string][2]int{"status_refresh_seconds": {1, 3600}, "chart_refresh_seconds": {1, 3600}, "live_refresh_seconds": {1, 3600}, "live_sampling_seconds": {0, 300}, "traffic_chart_window_seconds": {1, 3600}}
	if len(data) == 0 {
		return errors.New("刷新设置不能为空")
	}
	for key, value := range data {
		bound, ok := bounds[key]
		if !ok {
			return errors.New("刷新设置包含未知字段")
		}
		if _, err := integer(value, bound[0], bound[1]); err != nil {
			labels := map[string]string{"status_refresh_seconds": "状态更新间隔（秒）", "chart_refresh_seconds": "历史图更新间隔（秒）", "live_refresh_seconds": "实时指标更新间隔（秒）", "live_sampling_seconds": "后台实时采集间隔（秒，0 表示跟随模块）", "traffic_chart_window_seconds": "流量曲线汇总间隔（秒）"}
			return fmt.Errorf("%s：%w", labels[key], err)
		}
	}
	a.mu.Lock()
	old := clone(a.settings)
	for key, value := range data {
		a.settings[key] = value
	}
	err := writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings)
	if err != nil {
		a.settings = old
	}
	a.mu.Unlock()
	if err == nil && num(data["live_sampling_seconds"]) > 0 {
		p := a.monitorDataPolicy("all", "")
		p["interval"] = data["live_sampling_seconds"]
		if err = a.saveMonitorData("all", "", p); err != nil {
			return err
		}
	}
	if err == nil && data["live_sampling_seconds"] != nil && a.traffic != nil {
		select {
		case a.traffic.wake <- struct{}{}:
		default:
		}
	}
	if err == nil && data["live_sampling_seconds"] != nil && a.host != nil {
		select {
		case a.host.wake <- struct{}{}:
		default:
		}
	}
	return err
}
