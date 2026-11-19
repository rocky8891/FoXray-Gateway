package gateway

import (
	"errors"
	"fmt"
	"strings"
)

type ZoneInterfaceChange struct {
	Interface     string   `json:"interface"`
	TargetZone    string   `json:"target_zone"`
	RuntimeZone   string   `json:"runtime_zone"`
	PermanentZone string   `json:"permanent_zone"`
	Commands      []string `json:"commands"`
	Rollback      []string `json:"rollback"`
}

func (n *Network) interfaceZone(name string, permanent bool) (string, error) {
	args := []string{"firewall-cmd"}
	if permanent {
		args = append(args, "--permanent")
	}
	raw, err := n.app.command(4, "", append(args, "--get-zone-of-interface="+name)...)
	zone := strings.TrimSpace(raw)
	// firewall-cmd reports an unbound interface as "no zone" with exit status 1.
	if zone == "no zone" {
		return "", nil
	}
	if err != nil {
		return "", errors.New("无法读取接口原区域：" + name)
	}
	if zone != "" && !firewallZoneName.MatchString(zone) {
		return "", errors.New("原接口区域无法确认：" + name)
	}
	return zone, nil
}

func (n *Network) planZoneInterface(p *NetworkPlan, d M) error {
	item := obj(d["item"])
	zone := str(item["zone"])
	if !firewallZoneName.MatchString(zone) {
		return errors.New("区域无效")
	}
	var names []string
	if raw, provided := item["interfaces"]; provided {
		values := arr(raw)
		if len(values) < 1 || len(values) > 50 {
			return errors.New("每批选择1至50个接口")
		}
		seen := map[string]bool{}
		for _, value := range values {
			name, ok := value.(string)
			if !ok {
				return errors.New("接口名称无效")
			}
			if err := iface(name); err != nil {
				return err
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	} else {
		name := str(item["interface"])
		if err := iface(name); err != nil {
			return err
		}
		names = []string{name}
	}
	if !yes(d["allow_protected"]) {
		return errors.New("更改区域归属可能影响接口访问，需要确认本次变更")
	}
	for _, name := range names {
		if _, e := n.app.command(4, "", "ip", "link", "show", "dev", name); e != nil {
			return errors.New("接口不存在：" + name)
		}
		change := ZoneInterfaceChange{Interface: name, TargetZone: zone, Commands: []string{}, Rollback: []string{}}
		for _, permanent := range []bool{false, true} {
			args := []string{"firewall-cmd"}
			if permanent {
				args = append(args, "--permanent")
			}
			old, err := n.interfaceZone(name, permanent)
			if err != nil {
				return err
			}
			if permanent {
				change.PermanentZone = old
			} else {
				change.RuntimeZone = old
			}
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
			change.Commands = append(change.Commands, printableCommand(forward))
			change.Rollback = append([]string{printableCommand(reverse)}, change.Rollback...)
		}
		p.ZoneChanges = append(p.ZoneChanges, change)
	}
	p.precondition = func() error {
		for _, change := range p.ZoneChanges {
			if _, err := n.app.command(4, "", "ip", "link", "show", "dev", change.Interface); err != nil {
				return errors.New("接口已变化，请重新预览")
			}
			for _, permanent := range []bool{false, true} {
				current, err := n.interfaceZone(change.Interface, permanent)
				if err != nil {
					return err
				}
				expected := change.RuntimeZone
				if permanent {
					expected = change.PermanentZone
				}
				if current != expected {
					return errors.New("接口区域已变化，请重新预览：" + change.Interface)
				}
			}
		}
		return nil
	}
	p.Summary = fmt.Sprintf("防火墙区域 · %d 个接口 → %s", len(names), zone)
	p.Warnings = append(p.Warnings, "统一变更所选接口的运行与永久区域归属，不全局 reload；任一步失败或超时未确认时，按每个接口原区域回退。接口若由 NetworkManager 管理，还应核对其连接配置")
	return nil
}
