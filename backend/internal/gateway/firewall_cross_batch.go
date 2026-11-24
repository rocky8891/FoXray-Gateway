package gateway

import (
	"errors"
	"fmt"
	"maps"
)

// Batch changes only existing FoXray policies, without any global reload.
func (n *Network) batchCrossZonePreview(data M, items []any) (*NetworkPlan, error) {
	operation := str(data["operation"])
	labels := map[string]string{"enable": "启用", "disable": "停用", "delete": "删除"}
	if labels[operation] == "" || !yes(data["allow_protected"]) {
		return nil, errors.New("请确认域间规则对转发和管理连接的影响")
	}
	if str(n.app.service("firewalld")["state"]) != "active" {
		return nil, errors.New("firewalld 未运行")
	}
	n.mu.Lock()
	revision := fingerprint(n.managed)
	n.mu.Unlock()
	snapshot, err := n.crossZonePlanSnapshot()
	if err != nil {
		return nil, err
	}
	expectedLive, expectedSaved := maps.Clone(snapshot.live), maps.Clone(snapshot.saved)
	plan := &NetworkPlan{ID: id(), Kind: "firewall", Status: "preview", Created: now(), revision: revision, Warnings: []string{}, Preview: []string{}}
	seen := map[string]bool{}
	changes := []any{}
	mode := firstNonempty(str(data["apply_mode"]), "permanent")
	if mode != "immediate" && mode != "permanent" {
		return nil, errors.New("域间规则生效方式无效")
	}
	for _, value := range items {
		identity := str(obj(value)["id"])
		item := n.ownedItem("firewall", identity)
		if identity == "" || seen[identity] || str(item["kind"]) != "cross-zone" {
			return nil, errors.New("请选择互不重复的 FoXray 纳管域间规则")
		}
		seen[identity] = true
		name := crossZoneName(item)
		if mode == "immediate" && operation != "delete" {
			live, exists := snapshot.live[name]
			if !exists || live.Priority != int(num(first(item["priority"], -1))) || len(live.Ingress) == 0 || len(live.Egress) == 0 {
				return nil, errors.New("所选策略需要全局重载，请选择仅保存永久配置或逐条预览")
			}
		}
		op := "replace"
		if operation == "delete" {
			op = "delete"
		} else {
			item["enabled"] = operation == "enable"
		}
		item["apply_mode"], item["reload_ack"] = mode, false
		child := &NetworkPlan{Kind: "firewall"}
		err = n.planCrossZoneWithSnapshot(child, M{"operation": op, "item": item, "allow_protected": true}, snapshot)
		if err != nil {
			return nil, err
		}
		if len(child.undoEnds) != len(child.steps) || yes(child.Details["reload_required"]) {
			return nil, errors.New("所选规则无法安全批量变更")
		}
		for _, end := range child.undoEnds {
			plan.undoEnds = append(plan.undoEnds, len(plan.undo)+end)
		}
		plan.steps = append(plan.steps, child.steps...)
		plan.undo = append(plan.undo, child.undo...)
		changes = append(changes, child.persist)
		plan.Warnings = append(plan.Warnings, child.Warnings...)
		if operation == "delete" {
			delete(expectedSaved, name)
			if current, exists := expectedLive[name]; mode == "immediate" && exists {
				current.Rules = nil
				expectedLive[name] = current
			}
		} else {
			next := obj(child.persist["item"])
			desired := crossZonePolicy{Priority: int(num(next["priority"])), Target: "CONTINUE", Ingress: []string{str(next["ingress_zone"])}, Egress: []string{str(next["egress_zone"])}, Rules: crossZoneRules(next)}
			expectedSaved[name] = desired
			if mode == "immediate" {
				expectedLive[name] = desired
			}
		}
	}
	compare := func(live, saved map[string]crossZonePolicy) error {
		currentLive, err := n.readCrossZonePolicies(false)
		if err != nil {
			return err
		}
		currentSaved, err := n.readCrossZonePolicies(true)
		if err != nil {
			return err
		}
		if fingerprint(currentLive) != fingerprint(live) || fingerprint(currentSaved) != fingerprint(saved) {
			return errors.New("域间规则快照不一致，请核对当前配置")
		}
		return nil
	}
	plan.precondition = func() error { return compare(snapshot.live, snapshot.saved) }
	plan.postcondition = func() error { return compare(expectedLive, expectedSaved) }
	plan.Summary = fmt.Sprintf("域间规则批量%s · %d 项", labels[operation], len(items))
	plan.Warnings = uniq(plan.Warnings)
	plan.persist = M{"changes": changes}
	for _, step := range plan.steps {
		plan.Preview = append(plan.Preview, printableCommand(step.Args))
	}
	for i := len(plan.undo) - 1; i >= 0; i-- {
		plan.Rollback = append(plan.Rollback, printableCommand(plan.undo[i].Args))
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if fingerprint(n.managed) != revision {
		return nil, errors.New("规则已变化，请重新预览")
	}
	n.plans[plan.ID] = plan
	return plan, nil
}
