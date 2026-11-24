package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const firewallCrossBundleScript = `import json
from firewall.client import FirewallClient
f=FirewallClient();c=f.config()
def policy(settings):
 d=settings.getSettingsDict();d['masquerade']=bool(d.get('masquerade',False));d['disable']=bool(d.get('disable',False));return d
print(json.dumps({'zones':list(f.getZones()),'runtime':{n:policy(f.getPolicySettings(n)) for n in f.getPolicies()},'permanent':{n:policy(c.getPolicyByName(n).getSettings()) for n in c.getPolicyNames()}}))`

func (n *Network) firewallReadCommand(contexts ...context.Context) func(int, string, ...string) (string, error) {
	parent := n.app.ctx
	if len(contexts) > 0 && contexts[0] != nil {
		parent = contexts[0]
	}
	return func(seconds int, input string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(parent, time.Duration(seconds)*time.Second)
		defer cancel()
		return n.app.runner.Run(ctx, args, input)
	}
}
func crossPolicyFromSettings(settings M) (crossZonePolicy, error) {
	p := crossZonePolicy{Target: str(settings["target"]), Disabled: yes(settings["disable"])}
	for _, key := range []string{"target", "priority", "ingress_zones", "egress_zones", "rich_rules"} {
		if _, ok := settings[key]; !ok {
			return p, errors.New("策略批量快照不完整")
		}
	}
	for key, values := range map[string]*[]string{"ingress_zones": &p.Ingress, "egress_zones": &p.Egress, "rich_rules": &p.Rules} {
		for _, value := range arr(settings[key]) {
			text := str(value)
			if key == "rich_rules" {
				text = compactRule(text)
			}
			*values = append(*values, text)
		}
		sort.Strings(*values)
	}
	p.Priority = int(num(settings["priority"]))
	for key, value := range settings {
		switch key {
		case "description", "short", "version", "target", "priority", "ingress_zones", "egress_zones", "rich_rules", "disable":
			continue
		}
		present := false
		switch v := value.(type) {
		case bool:
			present = v
		case string:
			present = strings.TrimSpace(v) != ""
		case float64:
			present = v != 0
		case []any:
			present = len(v) > 0
		case map[string]any:
			present = len(v) > 0
		default:
			present = value != nil
		}
		p.Extra = p.Extra || present
	}
	return p, nil
}
func readCrossBundle(command func(int, string, ...string) (string, error)) (map[string]crossZonePolicy, map[string]crossZonePolicy, []string, error) {
	var bundle struct {
		Zones     []string     `json:"zones"`
		Runtime   map[string]M `json:"runtime"`
		Permanent map[string]M `json:"permanent"`
	}
	raw, err := command(20, "", "/usr/bin/python3", "-c", firewallCrossBundleScript)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = json.Unmarshal([]byte(raw), &bundle); err != nil {
		return nil, nil, nil, err
	}
	if len(bundle.Zones) == 0 || bundle.Runtime == nil || bundle.Permanent == nil {
		return nil, nil, nil, errors.New("策略批量快照不完整")
	}
	decode := func(settings map[string]M) (map[string]crossZonePolicy, error) {
		result := map[string]crossZonePolicy{}
		for name, value := range settings {
			if !firewallZoneName.MatchString(name) {
				return nil, errors.New("策略名称无效")
			}
			p, err := crossPolicyFromSettings(value)
			if err != nil {
				return nil, err
			}
			result[name] = p
		}
		return result, nil
	}
	live, err := decode(bundle.Runtime)
	if err != nil {
		return nil, nil, nil, err
	}
	saved, err := decode(bundle.Permanent)
	return live, saved, bundle.Zones, err
}
