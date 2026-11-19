package gateway

import (
	"errors"
	"path/filepath"
)

func (a *App) versionCheckPolicy(service string) M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p := M{"enabled": true, "interval_hours": 24}
	for key, value := range obj(obj(a.settings["version_check_policies"])[service]) {
		if key == "enabled" || key == "interval_hours" {
			p[key] = value
		}
	}
	return p
}
func (a *App) saveVersionCheckPolicy(data M) error {
	service := str(data["service"])
	if !validUpdateService(service) {
		return errors.New("版本检查模块无效")
	}
	if len(data) != 3 {
		return errors.New("版本检查设置包含未知或缺失字段")
	}
	enabled, ok := data["enabled"].(bool)
	if !ok {
		return errors.New("自动检查开关无效")
	}
	hours, err := integer(data["interval_hours"], 1, 168)
	if err != nil {
		return errors.New("检查间隔须为1～168小时")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	previous := clone(a.settings)
	policies := clone(obj(a.settings["version_check_policies"]))
	policies[service] = M{"enabled": enabled, "interval_hours": hours}
	a.settings["version_check_policies"] = policies
	if err = writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings); err != nil {
		a.settings = previous
	}
	return err
}
func versionCheckDue(policy, previous M, stamp float64) bool {
	return yes(policy["enabled"]) && stamp-num(previous["checked_at"]) >= num(policy["interval_hours"])*3600
}
