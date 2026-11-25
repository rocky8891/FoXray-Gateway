package gateway

import (
	"context"
	"strings"
)

func (n *Network) FirewallOrigins(ctx context.Context) M {
	cmd := n.firewallReadCommand(ctx)
	entries := []M{}
	raw, err := cmd(10, "", "nft", "-j", "list", "ruleset")
	complete := err == nil
	if err == nil {
		rows, parseErr := nftablesInventory(raw, nil, nil)
		complete = parseErr == nil
		entries = append(entries, firewallOriginRows(rows)...)
	}
	// The legacy iptables backend is separate from nftables. For iptables-nft
	// the same Docker rules are already in the nft snapshot.
	if version, e := cmd(4, "", "iptables", "--version"); e == nil && strings.Contains(version, "legacy") {
		if raw, e := cmd(6, "", "iptables-save"); e == nil {
			entries = append(entries, firewallOriginRows(iptablesInventory(raw, "ipv4", nil))...)
		} else {
			complete = false
		}
	}
	if str(n.app.service("firewalld")["state"]) == "active" {
		active, activeErr := cmd(8, "", "firewall-cmd", "--get-active-zones")
		if activeErr == nil {
			for _, row := range entries {
				row["ingress_zone"] = firewallActiveInterfaceZone(active, str(row["in_interface"]))
				row["egress_zone"] = firewallActiveInterfaceZone(active, str(row["out_interface"]))
				row["zone"] = row["ingress_zone"]
				if str(row["kind"]) == "masquerade" {
					row["zone"] = row["egress_zone"]
				}
			}
		} else {
			complete = false
		}
		live, saved, _, err := readCrossBundle(cmd)
		if err != nil {
			complete = false
		} else {
			names := map[string]bool{}
			for name := range live {
				names[name] = true
			}
			for name := range saved {
				names[name] = true
			}
			for name := range names {
				a, b := live[name], saved[name]
				if !a.Masquerade && !b.Masquerade {
					continue
				}
				policy := a
				if !a.Masquerade {
					policy = b
				}
				entries = append(entries, M{"id": "policy-masquerade-" + name, "kind": "masquerade", "action": "masquerade", "rule_origin": "firewalld", "name": name, "policy_name": name, "zone": strings.Join(policy.Egress, ", "), "ingress_zone": strings.Join(policy.Ingress, ", "), "egress_zone": strings.Join(policy.Egress, ", "), "runtime": a.Masquerade && !a.Disabled, "permanent": b.Masquerade, "enabled": a.Masquerade && !a.Disabled, "editable": false})
			}
		}
	}
	return M{"entries": entries, "complete": complete}
}

func firewallOriginRows(rows []M) []M {
	result := []M{}
	for _, row := range identifyNativeRows(rows) {
		table, chain := str(row["table"]), str(row["chain"])
		origin := ""
		switch {
		case strings.HasPrefix(table, "foxray_wg_"):
			origin = "wireguard"
		case table == "foxray_vpn_guard" || table == "foxray_warp_nat":
			origin = "warp"
		case chain != "DOCKER-USER" && (strings.HasPrefix(table, "docker-") || strings.HasPrefix(table, "docker_") || strings.HasPrefix(chain, "DOCKER")):
			origin = "docker"
		}
		if origin == "" {
			continue
		}
		switch str(row["action"]) {
		case "masquerade", "snat":
			row["kind"] = "masquerade"
		case "dnat", "redirect":
			row["kind"] = "forward"
		case "accept", "drop", "reject":
			switch {
			case strings.Contains(strings.ToLower(chain), "input"):
				row["kind"] = "ip"
			case strings.Contains(strings.ToLower(chain), "forward"), strings.HasPrefix(chain, "DOCKER"):
				row["kind"] = "cross-zone"
			default:
				continue
			}
		default:
			continue
		}
		row["zone"], row["rule_origin"], row["editable"] = "", origin, false
		result = append(result, row)
	}
	return result
}
