package gateway

import (
	"errors"
	"fmt"
	"strings"
)

func (n *Network) InterfaceSnapshot() M {
	addresses, e := n.app.commandJSON("ip", "-j", "-d", "address", "show")
	links, er := n.app.commandJSON("ip", "-j", "-s", "-d", "link", "show")
	byName := map[string]M{}
	for _, v := range links {
		r := obj(v)
		byName[str(r["ifname"])] = r
	}

	gateways, gatewayOK := []any{}, true
	for _, family := range []int{4, 6} {
		routes, err := n.app.commandJSON("ip", "-j", fmt.Sprint("-", family), "route", "show", "table", "main", "default")
		if err != nil {
			gatewayOK = false
			continue
		}
		for _, raw := range routes {
			route := clone(obj(raw))
			route["family"] = family
			route["table"] = 254
			route["key"] = interfaceRouteKey(route)
			gateways = append(gateways, route)
		}
	}
	rows := []M{}
	for _, v := range addresses {
		r := obj(v)
		name := str(r["ifname"])
		link := byName[name]
		for k, value := range link {
			if r[k] == nil {
				r[k] = value
			}
		}
		r["type"] = firstNonempty(str(obj(r["linkinfo"])["info_kind"]), str(r["link_type"]))
		r["admin_up"] = false
		for _, flag := range arr(r["flags"]) {
			if str(flag) == "UP" {
				r["admin_up"] = true
			}
		}
		r["protected"] = name == uplink() || name == "lo" || name == "vone0" || strings.HasPrefix(name, "wg-") || name == "CloudflareWARP" || name == "vpm-tun"
		r["managed_by_vpn"] = name == "vone0" || strings.HasPrefix(name, "wg-") || name == "CloudflareWARP" || name == "vpm-tun"
		r["managed"] = n.ownedItem("interfaces", name) != nil
		r["can_delete"] = yes(r["managed"]) && !yes(r["protected"]) && !yes(r["admin_up"]) && len(arr(r["addr_info"])) == 0
		r["gateways"] = []any{}
		for _, raw := range gateways {
			route := obj(raw)
			if str(route["dev"]) == name {
				r["gateways"] = append(arr(r["gateways"]), clone(route))
			}
		}
		rows = append(rows, r)
	}
	return M{"items": rows, "complete": e == nil && er == nil, "gateways_complete": gatewayOK}
}
func (n *Network) planInterface(p *NetworkPlan, d M) error {
	if str(d["operation"]) == "delete" {
		return n.planDeleteInterface(p, d)
	}
	if str(d["operation"]) == "replace" {
		if !yes(d["allow_protected"]) {
			return errors.New("修改接口地址或网关需要确认本次影响")
		}
		return n.planInterfaceAddress(p, obj(d["item"]), false)
	}
	if str(d["operation"]) == "add" {
		return n.planNewInterface(p, obj(d["item"]))
	}
	name := str(obj(d["item"])["name"])
	if e := iface(name); e != nil {
		return e
	}
	if name == "lo" {
		return errors.New("回环接口不允许关闭")
	}
	if name == "vone0" || strings.HasPrefix(name, "wg-") || name == "CloudflareWARP" || name == "vpm-tun" || name == "fw-uplink" {
		return errors.New("该接口由 VPN／代理服务维护，请在对应模块管理，避免状态不一致")
	}
	op := str(d["operation"])
	if op != "start" && op != "stop" {
		return errors.New("接口仅支持启用和停用")
	}
	if !yes(d["allow_protected"]) {
		return errors.New("接口状态变更可能影响现有连接，需要确认本次影响")
	}
	rows, e := n.app.commandJSON("ip", "-j", "link", "show", "dev", name)
	if e != nil || len(rows) != 1 {
		return errors.New("接口不存在或状态无法读取")
	}
	previous := "down"
	for _, flag := range arr(obj(rows[0])["flags"]) {
		if str(flag) == "UP" {
			previous = "up"
		}
	}
	desired := "up"
	if op == "stop" {
		desired = "down"
	}
	p.Summary = "接口 " + name + " · " + desired
	if desired != previous {
		p.steps = []Step{{Args: []string{"ip", "link", "set", "dev", name, desired}}}
		p.undo = []Step{{Args: []string{"ip", "link", "set", "dev", name, previous}}}
	}
	p.Warnings = append(p.Warnings, "仅更改当前接口状态，不修改 IP、MAC 或开机配置；若影响管理连接且未确认，将自动恢复原状态")
	return nil
}

func (n *Network) planNewInterface(p *NetworkPlan, item M) error {
	name, kind := str(item["name"]), str(item["type"])
	safeNewName := func(value string) error {
		if err := iface(value); err != nil {
			return err
		}
		if value == "lo" || value == uplink() || value == "vone0" || value == "CloudflareWARP" || value == "vpm-tun" || value == "fw-uplink" || strings.HasPrefix(value, "wg-") {
			return errors.New("名称由系统或VPN保留")
		}
		return nil
	}
	if err := safeNewName(name); err != nil {
		return err
	}
	names := []string{name}
	args := []string{"ip", "link", "add", "name", name, "type", kind}
	parent := str(item["parent"])
	switch kind {
	case "veth":
		peer := str(item["peer"])
		if err := safeNewName(peer); err != nil {
			return err
		}
		if peer == name {
			return errors.New("veth两端名称不能相同")
		}
		names = append(names, peer)
		args = append(args, "peer", "name", peer)
	case "dummy":
	case "bridge":
		// No existing interface is enslaved and STP/IP policy remains untouched.
	case "vlan":
		if err := iface(parent); err != nil {
			return err
		}
		if parent == name {
			return errors.New("父接口不能与新增接口同名")
		}
		tag, err := integer(item["vlan_id"], 1, 4094)
		if err != nil {
			return errors.New("VLAN ID须为1～4094的整数")
		}
		args = []string{"ip", "link", "add", "link", parent, "name", name, "type", "vlan", "id", fmt.Sprint(tag)}
	default:
		return errors.New("支持veth、虚拟回环dummy、VLAN子接口和网桥")
	}
	// A successful full snapshot is required; a failed per-device lookup cannot prove absence.
	parentIdentity := ""
	check := func() error {
		rows, err := n.app.commandJSON("ip", "-j", "-d", "link", "show")
		if err != nil || rows == nil {
			return errors.New("无法读取现有接口，不能确认名称是否可用")
		}
		foundParent := kind != "vlan"
		for _, raw := range rows {
			row := obj(raw)
			existing := str(row["ifname"])
			for _, candidate := range names {
				if existing == candidate {
					return errors.New("接口名称已存在，请重新选择")
				}
			}
			if existing == parent {
				if existing == "lo" || str(obj(row["linkinfo"])["info_kind"]) == "wireguard" || existing == "vone0" || existing == "CloudflareWARP" || existing == "vpm-tun" || existing == "fw-uplink" {
					return errors.New("不能使用回环或VPN受管接口作为VLAN父接口")
				}
				identity := fingerprint(M{"ifindex": row["ifindex"], "type": row["linkinfo"], "address": row["address"]})
				if parentIdentity != "" && parentIdentity != identity {
					return errors.New("VLAN父接口已变化，请重新预览")
				}
				parentIdentity = identity
				foundParent = true
			}
		}
		if !foundParent {
			return errors.New("VLAN父接口不存在")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	p.precondition = check
	p.Summary = "新增 " + kind + " 接口 " + name
	p.steps = []Step{{Args: args}}
	p.undo = []Step{{Args: []string{"ip", "link", "delete", "dev", name}}}
	if err := n.planInterfaceAddress(p, item, true); err != nil {
		return err
	}
	owned := clone(item)
	owned["id"] = name
	p.persist = M{"operation": "add", "item": owned}
	p.Warnings = append(p.Warnings, "按预览创建接口；未选择启用时保持DOWN，仅当前运行期有效，不改变现有网桥成员。应用后120秒内未确认会回退本次新增接口。")
	if kind == "dummy" {
		p.Warnings = append(p.Warnings, "Linux原生lo不能重复创建；此选项创建Dummy虚拟回环设备。")
	}
	return nil
}
