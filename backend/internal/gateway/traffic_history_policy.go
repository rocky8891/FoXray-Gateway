package gateway

import (
	"errors"
)

func trafficModule(id string) string {
	if id == "warp" {
		return "warp"
	}
	if len(id) > 3 && id[:3] == "wg:" {
		return "wireguard"
	}
	return "xray"
}
func (t *Traffic) historyIntervalLocked(module string) float64 {
	if module == "" {
		v := 3600.
		for _, name := range []string{"xray", "warp", "wireguard"} {
			v = min(v, t.historyIntervalLocked(name))
		}
		return v
	}
	return t.app.historyInterval(module, "traffic")
}
func (t *Traffic) historyPolicySnapshot(module string) M {
	t.mu.Lock()
	defer t.mu.Unlock()
	return M{"storage_interval": t.historyIntervalLocked(module), "sampling_interval": t.modulePolicyLocked(module)["interval"]}
}
func (t *Traffic) saveHistoryPolicy(module string, p M) error {
	if module != "xray" && module != "warp" && module != "wireguard" {
		return errors.New("流量模块无效")
	}
	if len(p) != 1 || p["storage_interval"] == nil {
		return errors.New("仅支持设置历史记录间隔")
	}
	n, err := integer(p["storage_interval"], 1, 3600)
	if err != nil {
		return errors.New("流量历史记录间隔须为1～3600秒")
	}
	return t.app.saveHistoryInterval(module, "traffic", n)
}

func (t *Traffic) samplingIntervalLocked(module string) float64 {
	if module != "" {
		return num(t.modulePolicyLocked(module)["interval"])
	}
	value := 300.
	for _, name := range []string{"xray", "warp", "wireguard"} {
		value = min(value, num(t.modulePolicyLocked(name)["interval"]))
	}
	return max(1., value)
}
