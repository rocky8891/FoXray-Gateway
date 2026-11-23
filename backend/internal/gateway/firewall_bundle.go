package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Reuse one firewalld client instead of starting four Python command processes.
// Keep the CLI path as a fallback for distributions with a different client API.
const firewallBundleScript = `import json,sys
from firewall.client import FirewallClient
f=FirewallClient();c=f.config();z=sys.argv[1]
a=f.getZoneSettings(z).getSettingsDict();b=c.getZoneByName(z).getSettings().getSettingsDict()
for d in [a,b]:
 for key in ['masquerade','icmp_block_inversion','forward']:d[key]=bool(d.get(key,False))
print(json.dumps({'zones':list(f.getZones()),'runtime':a,'permanent':b,'icmp_types':list(c.getIcmpTypeNames())}))`

type firewallBundle struct {
	Zones     []string `json:"zones"`
	Runtime   M        `json:"runtime"`
	Permanent M        `json:"permanent"`
	ICMPTypes []string `json:"icmp_types"`
}

func readFirewallBundle(command func(int, string, ...string) (string, error), zone string) (firewallBundle, error) {
	var result firewallBundle
	raw, err := command(20, "", "/usr/bin/python3", "-c", firewallBundleScript, zone)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		return result, err
	}
	if len(result.Zones) == 0 || result.Runtime["target"] == nil || result.Permanent["target"] == nil {
		return result, fmt.Errorf("防火墙批量快照不完整")
	}
	for _, settings := range []M{result.Runtime, result.Permanent} {
		for _, key := range []string{"services", "ports", "forward_ports", "rules_str", "masquerade"} {
			if _, ok := settings[key]; !ok {
				return result, fmt.Errorf("防火墙批量快照缺少字段 %s", key)
			}
		}
	}
	return result, nil
}
func firewallZoneText(zone string, settings M) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", zone)
	fields := []struct{ label, key string }{{"target", "target"}, {"interfaces", "interfaces"}, {"sources", "sources"}, {"services", "services"}, {"protocols", "protocols"}, {"icmp-blocks", "icmp_blocks"}}
	for _, field := range fields {
		value := str(settings[field.key])
		if values, ok := settings[field.key].([]any); ok {
			parts := []string{}
			for _, v := range values {
				parts = append(parts, str(v))
			}
			value = strings.Join(parts, " ")
		}
		fmt.Fprintf(&out, "  %s: %s\n", field.label, value)
	}
	for _, key := range []string{"ports", "source_ports"} {
		parts := []string{}
		for _, value := range arr(settings[key]) {
			pair := arr(value)
			if len(pair) == 2 {
				parts = append(parts, str(pair[0])+"/"+str(pair[1]))
			}
		}
		fmt.Fprintf(&out, "  %s: %s\n", strings.ReplaceAll(key, "_", "-"), strings.Join(parts, " "))
	}
	forwards := []string{}
	for _, value := range arr(settings["forward_ports"]) {
		v := arr(value)
		if len(v) == 4 {
			forwards = append(forwards, fmt.Sprintf("port=%s:proto=%s:toport=%s:toaddr=%s", str(v[0]), str(v[1]), str(v[2]), str(v[3])))
		}
	}
	fmt.Fprintf(&out, "  forward-ports: %s\n", strings.Join(forwards, " "))
	for _, key := range []string{"masquerade", "icmp_block_inversion", "forward"} {
		value := "no"
		if yes(settings[key]) {
			value = "yes"
		}
		fmt.Fprintf(&out, "  %s: %s\n", strings.ReplaceAll(key, "_", "-"), value)
	}
	out.WriteString("  rich rules:\n")
	for _, value := range arr(settings["rules_str"]) {
		fmt.Fprintf(&out, "    %s\n", str(value))
	}
	return out.String()
}
