package gateway

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Only empty, down virtual interfaces created and recorded by this manager can
// be deleted. Recreating arbitrary system or container interfaces is not safe.
func (n *Network) planDeleteInterface(p *NetworkPlan, data M) error {
	name := str(obj(data["item"])["name"])
	if err := iface(name); err != nil {
		return err
	}
	owned := n.ownedItem("interfaces", name)
	if owned == nil {
		return errors.New("只允许删除 FoXray 创建并记录的虚拟接口")
	}
	if !yes(data["allow_protected"]) {
		return errors.New("请确认删除接口及其对端的影响")
	}
	if name == "ens16" || name == "lo" || strings.HasPrefix(name, "wg-") {
		return errors.New("系统或 VPN 接口不能删除")
	}
	kind := str(owned["type"])
	args := []string{"ip", "link", "add", "name", name, "type", kind}
	names := []string{name}
	switch kind {
	case "dummy", "bridge":
	case "veth":
		peer := str(owned["peer"])
		if iface(peer) != nil || peer == name {
			return errors.New("对端信息无效")
		}
		names = append(names, peer)
		args = append(args, "peer", "name", peer)
	case "vlan":
		parent := str(owned["parent"])
		tag, err := integer(owned["vlan_id"], 1, 4094)
		if err != nil || iface(parent) != nil {
			return errors.New("VLAN 恢复参数无效")
		}
		args = []string{"ip", "link", "add", "link", parent, "name", name, "type", "vlan", "id", fmt.Sprint(tag)}
	default:
		return errors.New("接口类型不能安全删除和恢复")
	}
	var baseline string
	var restore []Step
	check := func() error {
		rows, err := n.app.commandJSON("ip", "-j", "-d", "address", "show")
		if err != nil || rows == nil {
			return errors.New("无法核对接口引用")
		}
		found := []any{}
		for _, wanted := range names {
			var row M
			for _, raw := range rows {
				if str(obj(raw)["ifname"]) == wanted {
					row = obj(raw)
					break
				}
			}
			if row != nil && str(obj(row["linkinfo"])["info_kind"]) != kind {
				return errors.New("接口类型与创建记录不一致")
			}
			if row == nil || len(arr(row["addr_info"])) > 0 || row["master"] != nil {
				return errors.New("接口不存在、有地址或已加入网桥，请先清理并停用")
			}
			for _, flag := range arr(row["flags"]) {
				if str(flag) == "UP" {
					return errors.New("请先停用所选接口及其对端")
				}
			}
			for _, raw := range rows {
				other := obj(raw)
				if other["master"] != nil && (str(other["master"]) == wanted || num(other["master"]) == num(row["ifindex"])) {
					return errors.New("接口仍有网桥成员，拒绝删除")
				}
			}
			for _, family := range []string{"-4", "-6"} {
				routes, e := n.app.commandJSON("ip", "-j", family, "route", "show", "table", "all", "dev", wanted)
				if e != nil || len(routes) > 0 {
					return errors.New("接口仍有关联路由或无法核对")
				}
			}
			found = append(found, M{"name": wanted, "ifindex": row["ifindex"], "address": row["address"], "mtu": row["mtu"], "linkinfo": row["linkinfo"]})
		}
		identity := fingerprint(found)
		if baseline != "" && baseline != identity {
			return errors.New("接口在预览后发生变化")
		}
		baseline = identity
		restore = []Step{{Args: args}}
		for _, raw := range found {
			row := obj(raw)
			if address := str(row["address"]); address != "" {
				if _, err := net.ParseMAC(address); err != nil {
					return errors.New("接口 MAC 无法安全恢复")
				}
				restore = append(restore, Step{Args: []string{"ip", "link", "set", "dev", str(row["name"]), "address", address}})
			}
			if num(row["mtu"]) > 0 {
				restore = append(restore, Step{Args: []string{"ip", "link", "set", "dev", str(row["name"]), "mtu", intstr(row["mtu"])}})
			}
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	p.precondition = check
	p.steps = []Step{{Args: []string{"ip", "link", "delete", "dev", name}}}
	commands := []string{}
	for _, step := range restore {
		commands = append(commands, strings.Join(step.Args[1:], " "))
	}
	p.undo = []Step{{Args: []string{"ip", "-batch", "-"}, Input: strings.Join(commands, "\n") + "\n"}}
	p.persist = M{"operation": "delete", "item": owned}
	p.Summary = "删除虚拟接口 · " + name
	p.Warnings = append(p.Warnings, "仅删除已停用且无地址、路由、网桥成员的受管理虚拟接口；veth 会同时删除已核验的对端。未确认时按记录重建。")
	return nil
}
