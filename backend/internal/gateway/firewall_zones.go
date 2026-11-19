package gateway

import (
	"errors"
	"strings"
)

func (n *Network) planZoneInterface(p *NetworkPlan, d M) error {
	item := obj(d["item"])
	zone, name := str(item["zone"]), str(item["interface"])
	if !firewallZoneName.MatchString(zone) {
		return errors.New("区域无效")
	}
	if e := iface(name); e != nil {
		return e
	}
	if !yes(d["allow_protected"]) {
		return errors.New("更改区域归属可能影响接口访问，需要确认本次变更")
	}
	if _, e := n.app.command(4, "", "ip", "link", "show", "dev", name); e != nil {
		return errors.New("接口不存在")
	}
	for _, permanent := range []bool{false, true} {
		args := []string{"firewall-cmd"}
		if permanent {
			args = append(args, "--permanent")
		}
		old, _ := n.app.command(4, "", append(args, "--get-zone-of-interface="+name)...)
		old = strings.TrimSpace(old)
		if old == zone {
			continue
		}
		if old != "" && !firewallZoneName.MatchString(old) {
			return errors.New("原接口区域无法确认")
		}
		forward := append(append([]string{}, args...), "--zone="+zone, "--change-interface="+name)
		reverse := append(append([]string{}, args...), "--zone="+zone, "--remove-interface="+name)
		if old != "" {
			reverse = append(append([]string{}, args...), "--zone="+old, "--change-interface="+name)
		}
		p.steps = append(p.steps, Step{Args: forward})
		p.undo = append(p.undo, Step{Args: reverse})
	}
	p.Summary = "防火墙区域 · " + name + " → " + zone
	p.Warnings = append(p.Warnings, "仅变更该接口的运行与永久区域归属，不全局 reload；未确认将恢复原区域。接口若由 NetworkManager 管理，还应核对其连接配置")
	return nil
}
