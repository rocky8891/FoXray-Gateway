package gateway

import (
	"errors"
	"fmt"
	"strings"
)

func (n *Network) batchPreview(data M) (*NetworkPlan, error) {
	kind, operation := str(data["kind"]), str(data["operation"])
	if kind != "firewall" {
		return nil, errors.New("当前批量操作只用于防火墙规则")
	}
	items := arr(data["items"])
	if len(items) < 1 || len(items) > 50 {
		return nil, errors.New("每批选择1至50条规则")
	}
	n.mu.Lock()
	revision := fingerprint(n.managed)
	n.mu.Unlock()
	p := &NetworkPlan{ID: id(), Kind: kind, Created: now(), Status: "preview", Warnings: []string{}, Preview: []string{}, revision: revision}
	changes := []any{}
	seen := map[string]bool{}
	for _, value := range items {
		item := obj(value)
		identity := str(item["id"])
		if identity == "" || seen[identity] {
			return nil, errors.New("规则选择包含空值或重复项")
		}
		seen[identity] = true
		if operation == "enable" || operation == "disable" {
			item = n.ownedItem(kind, str(item["id"]))
			if item == nil {
				return nil, errors.New("所选规则不属于 FoXray")
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
		changes = append(changes, child.persist)
	}
	names := map[string]string{"adopt": "纳入管理", "enable": "启用", "disable": "停用", "delete": "删除"}
	p.Summary = fmt.Sprintf("防火墙批量%s · %d 条规则", names[operation], len(items))
	p.Warnings = uniq(p.Warnings)
	p.persist = M{"changes": changes}
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
