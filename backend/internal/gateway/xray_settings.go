package gateway

import (
	"errors"
	"path/filepath"
)

func validateICMPPolicy(policy M) error {
	for _, field := range []struct {
		key      string
		min, max int
	}{{"interval", 0, 3600}, {"count", 1, 20}, {"size", 0, 1400}, {"timeout", 1, 5}} {
		if _, err := integer(policy[field.key], field.min, field.max); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) saveXraySettings(data M) error {
	a.mutate.Lock()
	defer a.mutate.Unlock()
	return a.saveXraySettingsLocked(data)
}
func (a *App) saveXraySettingsLocked(data M) error {
	a.mu.RLock()
	oldConfig, oldICMP, wasApplied := clone(a.cfg), clone(a.icmp), a.applied
	a.mu.RUnlock()
	next := clone(oldConfig)
	newICMP := clone(oldICMP)
	for key, value := range data {
		if key == "endpoints" || key == "fallback_active" {
			continue
		}
		if key == "icmp_policy" {
			newICMP = obj(value)
			if err := validateICMPPolicy(newICMP); err != nil {
				return err
			}
			continue
		}
		if _, ok := next[key]; !ok {
			return errors.New("配置包含未知字段")
		}
		next[key] = value
	}
	policy, err := normalizeVPNExclusions(next["vpn_exclusions"])
	if err != nil {
		return err
	}
	next["vpn_exclusions"] = policy
	if err := hydrateRuleTargets(next, oldConfig); err != nil {
		return err
	}
	for _, v := range arr(next["rule_sets"]) {
		r := obj(v)
		if str(r["id"]) == str(next["active_rule_set"]) {
			next["route_profile"] = r["profile"]
		}
	}
	if err := validateConfig(next); err != nil {
		return err
	}
	icmpChanged := fingerprint(newICMP) != fingerprint(oldICMP)
	if icmpChanged {
		if err := writeJSON(filepath.Join(a.base, "icmp-policy.json"), newICMP); err != nil {
			return err
		}
	}
	networkConfig := clone(next)
	for _, key := range []string{"speed_enabled", "speed_url", "speed_timeout", "speed_size_mb", "speed_concurrency", "latency_concurrency", "latency_enabled", "interval", "timeout", "latency_url", "mode", "failures", "hold", "improvement", "confirmations", "score_metrics", "score_weights", "score_latency_window_minutes", "score_latency_min_samples"} {
		networkConfig[key] = oldConfig[key]
	}
	routingChanged := fingerprint(routingComparable(networkConfig)) != fingerprint(routingComparable(oldConfig))
	if routingChanged && (yes(next["enabled"]) || wasApplied) {
		if err := a.proxyControlConfigLocked(next, next); err != nil {
			if icmpChanged {
				_ = writeJSON(filepath.Join(a.base, "icmp-policy.json"), oldICMP)
			}
			return err
		}
	}
	a.mu.Lock()
	a.cfg, a.icmp = next, newICMP
	err = a.persist()
	if err != nil {
		a.cfg, a.icmp = oldConfig, oldICMP
	}
	a.mu.Unlock()
	if err != nil {
		if routingChanged && (wasApplied || yes(next["enabled"])) {
			_ = a.proxyControlConfigLocked(oldConfig, oldConfig)
		}
		_ = writeJSON(filepath.Join(a.base, "config.json"), oldConfig)
		if icmpChanged {
			_ = writeJSON(filepath.Join(a.base, "icmp-policy.json"), oldICMP)
		}
		return err
	}
	a.lanDNS.invalidate()
	a.routes.setDomainCollection(yes(next["domain_identification"]))
	a.audit("policy.save", "Xray 配置与 ICMP 设置已统一保存", true, M{"routing_changed": routingChanged, "icmp_changed": icmpChanged})
	return nil
}
