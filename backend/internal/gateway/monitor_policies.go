package gateway

import (
	"errors"
	"net/url"
	"path/filepath"
	"time"
)

func defaultTrafficPolicy() M {
	return M{"interval": 2, "retention_days": 30, "max_storage_mb": 256}
}

func validateTrafficPolicy(policy M) error {
	for _, field := range []struct {
		key      string
		min, max int
	}{{"interval", 1, 300}, {"retention_days", 1, 365}, {"max_storage_mb", 1, 10240}} {
		if _, err := integer(policy[field.key], field.min, field.max); err != nil {
			return err
		}
	}
	return nil
}

func (t *Traffic) storageLocked() (int64, string, error) {
	var bytes int64
	err := t.app.histdb.QueryRow("SELECT COALESCE(SUM(pgsize),0) FROM dbstat WHERE name IN (SELECT name FROM sqlite_schema WHERE tbl_name='traffic_samples' AND type IN ('table','index'))").Scan(&bytes)
	if err == nil {
		return bytes, "table_pages", nil
	}
	// Some SQLite distributions omit dbstat; expose the fallback explicitly.
	var count int64
	if err = t.app.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples").Scan(&count); err != nil {
		return 0, "", err
	}
	return count * 256, "conservative_estimate", nil
}

func (t *Traffic) pruneLocked() error {
	err := t.pruneModulesLocked()
	t.storageBytes, t.storageMethod, _ = t.storageLocked()
	t.storageChecked = now()
	return err
}

func (t *Traffic) configure(policy M) error {
	for key := range policy {
		if key != "interval" && key != "retention_days" && key != "max_storage_mb" {
			return errors.New("流量监控设置包含未知字段")
		}
	}
	if err := validateTrafficPolicy(policy); err != nil {
		return err
	}
	for _, module := range []string{"xray", "warp", "wireguard"} {
		p := clone(policy)
		p["independent"] = true
		if err := t.app.saveMonitorData(module, "traffic", p); err != nil {
			return err
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := writeJSON(filepath.Join(t.app.base, "traffic-monitor-policy.json"), policy); err != nil {
		return err
	}
	t.policy = clone(policy)
	t.cleanupError = ""
	if err := t.pruneLocked(); err != nil {
		t.cleanupError = err.Error()
	}
	select {
	case t.wake <- struct{}{}:
	default:
	}
	return nil
}

func (t *Traffic) policySnapshot() M {
	t.mu.Lock()
	defer t.mu.Unlock()
	return M{"policy": clone(t.policy), "used_bytes": t.storageBytes, "usage_method": t.storageMethod, "checked_at": t.storageChecked, "cleanup_error": t.cleanupError, "storage_interval": 60}
}

func (a *App) probePolicySnapshot() M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return M{"latency_enabled": latencyDetectionEnabled(a.cfg), "interval": a.cfg["interval"], "timeout": a.cfg["timeout"], "latency_url": a.cfg["latency_url"], "retention_days": a.settings["measurement_retention_days"]}
}

func latencyDetectionEnabled(c M) bool {
	enabled, present := c["latency_enabled"].(bool)
	return !present || enabled
}

func (a *App) configureProbePolicy(data M) error {
	if value, exists := data["latency_enabled"]; exists {
		if _, ok := value.(bool); !ok {
			return errors.New("真连接检测开关必须为布尔值")
		}
	}
	for key := range data {
		if key != "latency_enabled" && key != "interval" && key != "timeout" && key != "latency_url" && key != "retention_days" && key != "icmp_policy" {
			return errors.New("节点检测设置包含未知字段")
		}
	}
	for _, field := range []struct {
		key      string
		min, max int
	}{{"interval", 0, 3600}, {"timeout", 1, 60}, {"retention_days", 1, 365}} {
		if _, err := integer(data[field.key], field.min, field.max); err != nil {
			return err
		}
	}
	address, err := url.Parse(str(data["latency_url"]))
	if err != nil || address.Host == "" || (address.Scheme != "https" && address.Scheme != "http") || address.User != nil {
		return errors.New("真连接检测地址须为不含认证信息的 HTTP 或 HTTPS 地址")
	}
	if policy, ok := data["icmp_policy"]; ok {
		if err := validateICMPPolicy(obj(policy)); err != nil {
			return err
		}
	}
	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	oldConfig, oldSettings, oldICMP := clone(a.cfg), clone(a.settings), clone(a.icmp)
	for _, key := range []string{"interval", "timeout", "latency_url"} {
		a.cfg[key] = data[key]
	}
	if value, exists := data["latency_enabled"]; exists {
		a.cfg["latency_enabled"] = value
	}
	a.settings["measurement_retention_days"] = data["retention_days"]
	if policy, ok := data["icmp_policy"]; ok {
		a.icmp = clone(obj(policy))
	}
	if err = a.persist(); err == nil {
		err = writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings)
	}
	if err == nil {
		err = writeJSON(filepath.Join(a.base, "icmp-policy.json"), a.icmp)
	}
	if err != nil {
		a.cfg, a.settings, a.icmp = oldConfig, oldSettings, oldICMP
		_ = a.persist()
		_ = writeJSON(filepath.Join(a.base, "manager-settings.json"), oldSettings)
		_ = writeJSON(filepath.Join(a.base, "icmp-policy.json"), oldICMP)
		return err
	}
	return nil
}

func (t *Traffic) intervalLocked() time.Duration {
	interval := 300.
	for _, module := range []string{"xray", "warp", "wireguard"} {
		interval = min(interval, num(t.modulePolicyLocked(module)["interval"]))
	}
	return time.Duration(max(1., interval)) * time.Second
}
