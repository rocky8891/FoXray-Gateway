package gateway

import "errors"

// Status polling sends only rule metadata. Large target lists are fetched on
// demand and unchanged lists round-trip as validated content references.
func compactRuleRows(value any) []any {
	result := []any{}
	for _, value := range arr(value) {
		row := obj(value)
		copy := M{}
		for key, v := range row {
			if key != "targets" {
				copy[key] = clone(v)
			}
		}
		targets := arr(row["targets"])
		copy["targets"] = []any{}
		copy["targets_count"] = len(targets)
		if len(targets) > 0 {
			copy["targets_preview"] = targets[0]
		}
		copy["targets_ref"] = fingerprint(targets)
		result = append(result, copy)
	}
	return result
}

// Caller holds App.mu. Rule bodies are immutable between configuration writes;
// persist invalidates this small projection, so one-second polling does not hash
// hundreds of thousands of unchanged targets on every request.
func (a *App) publicRuleMetadataLocked() M {
	a.publicRulesMu.Lock()
	defer a.publicRulesMu.Unlock()
	if a.publicRules == nil {
		sets := []any{}
		for _, v := range arr(a.cfg["rule_sets"]) {
			set := obj(v)
			copy := M{}
			for k, item := range set {
				if k != "rules" {
					copy[k] = clone(item)
				}
			}
			copy["rules"] = compactRuleRows(set["rules"])
			sets = append(sets, copy)
		}
		a.publicRules = M{"custom_rules": compactRuleRows(a.cfg["custom_rules"]), "rule_sets": sets}
	}
	return clone(a.publicRules)
}
func (a *App) publicConfigLocked() M {
	result := M{}
	rules := a.publicRuleMetadataLocked()
	for key, value := range a.cfg {
		if key == "custom_rules" || key == "rule_sets" {
			result[key] = rules[key]
		} else {
			result[key] = clone(value)
		}
	}
	return result
}
func ruleTargetMap(config M) map[string][]any {
	result := map[string][]any{}
	add := func(value any) {
		for _, v := range arr(value) {
			targets := arr(obj(v)["targets"])
			result[fingerprint(targets)] = targets
		}
	}
	add(config["custom_rules"])
	for _, v := range arr(config["rule_sets"]) {
		add(obj(v)["rules"])
	}
	return result
}
func hydrateRuleTargets(next, old M) error {
	lookup := ruleTargetMap(old)
	resolve := func(value any) error {
		for _, v := range arr(value) {
			row := obj(v)
			if reference := str(row["targets_ref"]); reference != "" {
				targets, exists := lookup[reference]
				if !exists {
					return errors.New("规则目标已更新，请重新加载规则集后重试")
				}
				if len(arr(row["targets"])) > 0 {
					return errors.New("规则目标引用与编辑内容冲突")
				}
				row["targets"] = clone(targets)
			}
			delete(row, "targets_ref")
			delete(row, "targets_count")
			delete(row, "targets_preview")
		}
		return nil
	}
	if err := resolve(next["custom_rules"]); err != nil {
		return err
	}
	for _, set := range arr(next["rule_sets"]) {
		if err := resolve(obj(set)["rules"]); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) ruleTargets(reference string) M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	targets, exists := ruleTargetMap(a.cfg)[reference]
	if !exists {
		return M{"error": "规则目标已更新，请重新打开规则集", "items": []any{}}
	}
	return M{"items": clone(targets), "count": len(targets)}
}
