package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// InterfacePreviewFromSnapshot runs the production planner against public GET
// snapshots. Its runner has no system-command implementation and no write path.
func InterfacePreviewFromSnapshot(request, snapshot M) (M, error) {
	if str(request["kind"]) != "interfaces" || len(arr(request["items"])) > 0 {
		return nil, errors.New("只支持单接口只读预览")
	}
	operation := str(request["operation"])
	if operation != "snapshot" && operation != "add" && operation != "replace" {
		return nil, errors.New("只读接口预览操作无效")
	}
	a := &App{ctx: context.Background(), runner: interfacePreviewRunner{snapshot: snapshot}}
	n := &Network{app: a, plans: map[string]*NetworkPlan{}, managed: M{}}
	if operation == "snapshot" {
		return n.InterfaceSnapshot(), nil
	}
	p, err := n.Preview(request)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	out := M{}
	if err = json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	out["id"], out["preview_only"] = "preview-only", true
	return out, nil
}

type interfacePreviewRunner struct{ snapshot M }

func (r interfacePreviewRunner) Run(_ context.Context, args []string, _ string) (string, error) {
	if !yes(r.snapshot["complete"]) {
		return "", errors.New("现有接口读取不完整")
	}
	var rows []any
	switch strings.Join(args, " ") {
	case "ip -j -d link show", "ip -j -s -d link show", "ip -j -d address show":
		rows = arr(r.snapshot["items"])
	case "ip -j -4 route show table main default", "ip -j -6 route show table main default":
		if !yes(r.snapshot["routes_complete"]) {
			return "", errors.New("默认路由读取不完整")
		}
		family := "4"
		if args[2] == "-6" {
			family = "6"
		}
		rows = arr(obj(r.snapshot["default_routes"])[family])
	default:
		if len(args) == 7 && strings.Join(args[:6], " ") == "ip -j -d address show dev" {
			rows = []any{}
			for _, raw := range arr(r.snapshot["items"]) {
				if str(obj(raw)["ifname"]) == args[6] {
					rows = append(rows, raw)
				}
			}
		} else {
			return "", errors.New("只读预览不执行系统命令")
		}
	}
	if rows == nil {
		rows = []any{}
	}
	data, err := json.Marshal(rows)
	return string(data), err
}
