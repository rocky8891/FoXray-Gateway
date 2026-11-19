package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

const communityDirect = "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/direct-list.txt"
const communityProxy = "https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/proxy-list.txt"

func migratePresetSources(c M) {
	for _, v := range arr(c["rule_sets"]) {
		r := obj(v)
		old := str(r["source"])
		if !yes(r["predefined"]) || !strings.HasPrefix(old, "https://raw.githubusercontent.com/rocky8891/FoXray-Gateway/main/rules/sets/") {
			continue
		}
		switch str(r["profile"]) {
		case "bypass_cn":
			r["source"] = communityDirect
			r["source_format"] = "domain-list"
		case "gfwlist":
			r["source"] = communityProxy
			r["source_format"] = "domain-list"
		case "global":
			r["source"] = ""
			r["auto_update"] = false
		}
	}
}
func validateRuleSetRules(r M) error {
	rules := arr(r["rules"])
	if str(r["source_format"]) != "domain-list" {
		return validateCustomRules(M{"custom_rules": rules})
	}
	if len(rules) > 500 {
		return errors.New("公共域名列表最多500条分组规则")
	}
	for start := 0; start < len(rules); start += 100 {
		if err := validateCustomRules(M{"custom_rules": rules[start:min(start+100, len(rules))]}); err != nil {
			return err
		}
	}
	return nil
}
func decodeRuleSource(raw []byte, previous M) ([]any, error) {
	if str(previous["source_format"]) == "domain-list" {
		action := "proxy"
		if str(previous["profile"]) == "bypass_cn" {
			action = "direct"
		}
		targets := []any{}
		seen := map[string]bool{}
		for _, line := range strings.Split(string(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})), "\n") {
			value := strings.TrimSpace(line)
			if value == "" || strings.HasPrefix(value, "#") {
				continue
			}
			if !strings.Contains(value, ":") {
				value = "domain:" + value
			}
			if !seen[value] {
				seen[value] = true
				targets = append(targets, value)
			}
			if len(targets) > 250000 {
				return nil, errors.New("公共列表目标超过250000条，保留原规则")
			}
		}
		if len(targets) == 0 {
			return nil, errors.New("公共域名列表为空，保留原规则")
		}
		rules := []any{}
		for start := 0; start < len(targets); start += 500 {
			rules = append(rules, M{"name": "公共域名列表", "enabled": true, "action": action, "type": "domain", "targets": targets[start:min(start+500, len(targets))], "network": "tcp,udp", "port": ""})
		}
		r := clone(previous)
		r["rules"] = rules
		if err := validateRuleSetRules(r); err != nil {
			return nil, err
		}
		return rules, nil
	}
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return nil, errors.New("规则源须为JSON，或选择公共域名列表格式")
	}
	var rules []any
	switch v := doc.(type) {
	case []any:
		rules = v
	case map[string]any:
		var ok bool
		rules, ok = v["rules"].([]any)
		if !ok {
			return nil, errors.New("规则源缺少rules数组")
		}
	default:
		return nil, errors.New("规则源结构无效")
	}
	if err := validateCustomRules(M{"custom_rules": rules}); err != nil {
		return nil, err
	}
	return rules, nil
}
