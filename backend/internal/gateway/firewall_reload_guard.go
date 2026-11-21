package gateway

import (
	"errors"
	"strings"
	"time"
)

func validZoneTarget(value string) bool {
	return value == "default" || value == "ACCEPT" || value == "DROP" || value == "REJECT"
}
func canonicalZoneTarget(value string) string {
	if value == "%%REJECT%%" {
		return "REJECT"
	}
	return value
}

// Keep rule ordering and object membership intact; ignore only display active flags.
func canonicalFirewallObjects(raw string) string {
	objects := map[string][]string{}
	key := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r\t ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			key = strings.TrimSuffix(strings.TrimSpace(line), " (active)")
			objects[key] = []string{}
		} else {
			objects[key] = append(objects[key], strings.TrimSpace(line))
		}
	}
	return fingerprint(objects)
}

// A reload must not discard runtime-only configuration. Query all traffic-related
// objects, including referenced service definitions, before allowing it.
func (n *Network) firewallReloadBaseline() (string, error) {
	baseline := M{}
	started := time.Now()
	read := func(permanent bool, args ...string) (string, error) {
		if time.Since(started) > 40*time.Second {
			return "", errors.New("重载核对超时")
		}
		command := []string{"firewall-cmd"}
		if permanent {
			command = append(command, "--permanent")
		}
		return n.app.command(8, "", append(command, args...)...)
	}
	compare := func(label string, objects bool, args ...string) (string, error) {
		live, e1 := read(false, args...)
		saved, e2 := read(true, args...)
		if e1 != nil || e2 != nil {
			return "", errors.New("无法完整核对" + label + "，请选择仅保存永久配置")
		}
		a, b := live, saved
		if objects {
			if len(args) > 0 && strings.HasPrefix(args[0], "--info-") && (strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "") {
				return "", errors.New(label + "定义未完整读取，不能立即重载")
			}
			a, b = canonicalFirewallObjects(live), canonicalFirewallObjects(saved)
		} else {
			a, b = strings.TrimSpace(live), strings.TrimSpace(saved)
		}
		if a != b {
			return "", errors.New(label + "的运行时与永久配置存在差异，立即重载可能丢失运行时规则；请选择仅保存永久配置")
		}
		baseline[label] = a
		return live, nil
	}
	zones, err := compare("全部区域", true, "--list-all-zones")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(zones) == "" {
		return "", errors.New("区域快照为空，不能立即重载")
	}
	policies, err := compare("全部策略", true, "--list-all-policies")
	if err != nil {
		return "", err
	}
	for _, key := range []string{"rules", "chains", "passthroughs"} {
		if _, err = compare("直接规则"+key, false, "--direct", "--get-all-"+key); err != nil {
			return "", err
		}
	}
	ipsets, err := compare("IP集合", false, "--get-ipsets")
	if err != nil {
		return "", err
	}
	ipsetNames := strings.Fields(ipsets)
	if len(ipsetNames) > 64 {
		return "", errors.New("IP集合超出可核对范围，请仅保存永久配置")
	}
	for _, name := range ipsetNames {
		if !firewallZoneName.MatchString(name) {
			return "", errors.New("IP集合名称无法安全核对")
		}
		if _, err = compare("IP集合"+name, true, "--info-ipset="+name); err != nil {
			return "", err
		}
	}
	// Service includes can introduce extra ports or helpers; inspect dependencies too.
	queue := []string{}
	icmpTypes := map[string]bool{}
	for _, line := range strings.Split(zones+"\n"+policies, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "services:") {
			queue = append(queue, strings.Fields(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))...)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "icmp-blocks:") {
			for _, name := range strings.Fields(strings.SplitN(line, ":", 2)[1]) {
				icmpTypes[name] = true
			}
		}
		if match := richService.FindStringSubmatch(line); len(match) > 1 {
			queue = append(queue, match[1])
		}
	}
	for name := range icmpTypes {
		if !firewallZoneName.MatchString(name) {
			return "", errors.New("ICMP类型无法核对")
		}
		if _, err = compare("ICMP定义"+name, true, "--info-icmptype="+name); err != nil {
			return "", err
		}
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		if len(seen) > 64 || !firewallZoneName.MatchString(name) {
			return "", errors.New("服务依赖超出可核对范围，请仅保存永久配置")
		}
		raw, e := compare("服务定义"+name, true, "--info-service="+name)
		if e != nil {
			return "", e
		}
		info := parseZoneDetails(raw)
		queue = append(queue, strings.Fields(str(info["includes"]))...)
		for _, helper := range strings.Fields(str(info["helpers"])) {
			if !firewallZoneName.MatchString(helper) {
				return "", errors.New("连接跟踪助手名称无法核对")
			}
			if _, e = compare("连接跟踪助手"+helper, true, "--info-helper="+helper); e != nil {
				return "", e
			}
		}
	}
	if _, err = n.app.command(10, "", "firewall-cmd", "--check-config"); err != nil {
		return "", errors.New("永久配置校验失败，不能立即重载")
	}
	return fingerprint(baseline), nil
}
