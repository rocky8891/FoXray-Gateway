package gateway

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

type crossZoneProtection struct {
	Zone string `json:"zone"`
	Rule string `json:"rule"`
}
type firewallReloadOptions struct {
	policy     string
	protection []crossZoneProtection
}

// firewalld does not preserve display ordering of zone members or rich rules.
// Actual precedence remains encoded by the rule's explicit priority and action.
func reloadObjects(raw string) map[string][]string {
	objects := map[string][]string{}
	name := ""
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			name = strings.Fields(line)[0]
			objects[name] = []string{}
			continue
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "rule ") {
			line = compactRule(line)
		} else if key, value, ok := strings.Cut(line, ":"); ok && slices.Contains([]string{"interfaces", "sources", "services", "ports", "protocols", "source-ports", "forward-ports", "icmp-blocks", "includes", "helpers"}, key) {
			values := strings.Fields(value)
			sort.Strings(values)
			line = key + ": " + strings.Join(values, " ")
		}
		objects[name] = append(objects[name], line)
	}
	for name, lines := range objects {
		sort.Strings(lines)
		objects[name] = lines
	}
	return objects
}
func reloadObjectFingerprint(raw, omit string) string {
	objects := reloadObjects(raw)
	if omit != "" {
		delete(objects, omit)
	}
	return fingerprint(objects)
}
func protectableZoneRules(liveRaw, savedRaw string) ([]crossZoneProtection, error) {
	live, saved := reloadObjects(liveRaw), reloadObjects(savedRaw)
	if len(live) != len(saved) {
		return nil, errors.New("区域集合存在差异，不能安全加载新策略")
	}
	protection := []crossZoneProtection{}
	for zone, lines := range live {
		other, ok := saved[zone]
		if !ok || !firewallZoneName.MatchString(zone) {
			return nil, errors.New("区域集合存在差异，不能安全加载新策略")
		}
		split := func(lines []string) ([]string, []string) {
			base, rules := []string{}, []string{}
			for _, line := range lines {
				if strings.HasPrefix(line, "rule ") {
					rules = append(rules, line)
				} else {
					base = append(base, line)
				}
			}
			return base, rules
		}
		base, rules := split(lines)
		savedBase, savedRules := split(other)
		if !slices.Equal(base, savedBase) {
			return nil, fmt.Errorf("区域 %s 的接口、来源或其他配置存在差异，不能自动重载", zone)
		}
		for _, rule := range savedRules {
			if !slices.Contains(rules, rule) {
				return nil, fmt.Errorf("区域 %s 存在仅永久启用的规则，不能自动重载", zone)
			}
		}
		for _, rule := range rules {
			if !slices.Contains(savedRules, rule) {
				protection = append(protection, crossZoneProtection{Zone: zone, Rule: rule})
			}
		}
	}
	if len(protection) > 64 {
		return nil, errors.New("运行时规则差异超过保护范围，不能自动重载")
	}
	sort.Slice(protection, func(i, j int) bool {
		if protection[i].Zone != protection[j].Zone {
			return protection[i].Zone < protection[j].Zone
		}
		return protection[i].Rule < protection[j].Rule
	})
	return protection, nil
}
func protectionCommand(rule crossZoneProtection, add bool) []string {
	option := "--remove-rich-rule="
	if add {
		option = "--add-rich-rule="
	}
	return []string{"firewall-cmd", "--permanent", "--zone=" + rule.Zone, option + rule.Rule}
}

func (n *Network) crossZoneReloadBaseline(name string) (string, []crossZoneProtection, error) {
	options := &firewallReloadOptions{policy: name}
	baseline, err := n.firewallReloadBaselineWithOptions(options)
	return baseline, options.protection, err
}

// Rollback after a reload must reload with the original runtime-only rules
// present, then remove their temporary permanent copies. A policy that was
// originally permanent-only must remain inactive during that rollback reload.
func crossReloadUndo(policyUndo []Step, protection []crossZoneProtection, name string, old crossZonePolicy, savedExists bool) []Step {
	execution := []Step{}
	for _, rule := range protection {
		execution = append(execution, Step{Args: protectionCommand(rule, true)})
	}
	for i := len(policyUndo) - 1; i >= 0; i-- {
		execution = append(execution, policyUndo[i])
	}
	if savedExists {
		for _, zone := range old.Ingress {
			execution = append(execution, Step{Args: crossCommand(name, true, "--remove-ingress-zone="+zone)})
		}
		for _, zone := range old.Egress {
			execution = append(execution, Step{Args: crossCommand(name, true, "--remove-egress-zone="+zone)})
		}
	}
	execution = append(execution, Step{Args: []string{"firewall-cmd", "--reload"}})
	if savedExists {
		for _, zone := range old.Ingress {
			execution = append(execution, Step{Args: crossCommand(name, true, "--add-ingress-zone="+zone)})
		}
		for _, zone := range old.Egress {
			execution = append(execution, Step{Args: crossCommand(name, true, "--add-egress-zone="+zone)})
		}
	}
	for _, rule := range protection {
		execution = append(execution, Step{Args: protectionCommand(rule, false)})
	}
	slices.Reverse(execution)
	return execution
}
