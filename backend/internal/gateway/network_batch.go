package gateway

import (
	"errors"
	"fmt"
	"strings"
)

func (n *Network) batchPreview(data M) (*NetworkPlan, error) {
	kind, operation := str(data["kind"]), str(data["operation"])
	if kind != "firewall" && kind != "routes" && kind != "rules" && kind != "interfaces" {
		return nil, errors.New("当前模块不支持批量操作")
	}
	if kind == "interfaces" && operation != "start" && operation != "stop" {
		return nil, errors.New("接口批量操作仅支持启用和停用")
	}
	if (kind == "routes" || kind == "rules") && operation != "enable" && operation != "disable" {
		return nil, errors.New("路由批量操作仅支持启用和停用")
	}
	items := arr(data["items"])
	if len(items) < 1 || len(items) > 50 {
		return nil, errors.New("每批选择1至50项")
	}
	if kind == "firewall" && str(data["scope"]) == "cross-zone" {
		return n.batchCrossZonePreview(data, items)
	}
	n.mu.Lock()
	revision := fingerprint(n.managed)
	n.mu.Unlock()
	p := &NetworkPlan{ID: id(), Kind: kind, Created: now(), Status: "preview", Warnings: []string{}, Preview: []string{}, revision: revision}
	changes := []any{}
	seen := map[string]bool{}
	for _, value := range items {
		item := obj(value)
		if kind == "firewall" && (str(item["kind"]) == "zone-policy" || str(item["kind"]) == "cross-zone") {
			return nil, errors.New("区域策略须单独预览和确认，不支持批量变更")
		}
		if kind == "firewall" {
			if owned := n.ownedItem(kind, str(item["id"])); owned != nil && str(owned["kind"]) == "cross-zone" {
				return nil, errors.New("跨区域规则须单独预览和确认")
			}
		}
		identity := str(item["id"])
		if kind == "interfaces" {
			identity = str(item["name"])
		}
		if identity == "" || seen[identity] {
			return nil, errors.New("规则选择包含空值或重复项")
		}
		seen[identity] = true
		if operation == "enable" || operation == "disable" {
			item = n.ownedItem(kind, str(item["id"]))
			if item == nil {
				return nil, errors.New("所选规则不属于 FoXray")
			}
			if str(item["kind"]) == "cross-zone" {
				return nil, errors.New("跨区域规则须单独预览和确认")
			}
			item["enabled"] = operation == "enable"
		}
		op := operation
		if op == "enable" || op == "disable" {
			op = "replace"
		}
		child, e := n.Preview(M{"kind": kind, "operation": op, "item": item, "allow_protected": data["allow_protected"]})
		if e != nil {
			return nil, e
		}
		n.mu.Lock()
		delete(n.plans, child.ID)
		n.mu.Unlock()
		p.steps = append(p.steps, child.steps...)
		p.undo = append(p.undo, child.undo...)
		p.Preview = append(p.Preview, child.Preview...)
		p.Warnings = append(p.Warnings, child.Warnings...)
		if child.persist != nil {
			changes = append(changes, child.persist)
		}
	}
	names := map[string]string{"adopt": "纳入管理", "enable": "启用", "disable": "停用", "delete": "删除", "start": "启用", "stop": "停用"}
	p.Summary = fmt.Sprintf("%s批量%s · %d 项", map[string]string{"firewall": "防火墙", "interfaces": "接口", "routes": "路由", "rules": "策略路由"}[kind], names[operation], len(items))
	p.Warnings = uniq(p.Warnings)
	if len(changes) > 0 {
		p.persist = M{"changes": changes}
	}
	for i := len(p.undo) - 1; i >= 0; i-- {
		p.Rollback = append(p.Rollback, printableCommand(p.undo[i].Args))
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if revision != fingerprint(n.managed) {
		return nil, errors.New("规则已变化，请重新预览")
	}
	n.plans[p.ID] = p
	return p, nil
}
func printableCommand(args []string) string {
	quoted := []string{}
	for _, s := range args {
		if strings.ContainsAny(s, " \t'\";()$") {
			s = "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
		}
		quoted = append(quoted, s)
	}
	return strings.Join(quoted, " ")
}
