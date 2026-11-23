package gateway

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var richPriority = regexp.MustCompile(`priority="?(-?[0-9]+)"?`)
var richLogRate = regexp.MustCompile(`limit value="?([1-9][0-9]*/[smhd])"?`)
var richFamily = regexp.MustCompile(`family="?(ipv[46])"?`)
var richService = regexp.MustCompile(`service name="?([a-zA-Z][a-zA-Z0-9_-]{0,31})"?`)

func compactRule(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\"", "")), " ")
}
func parseFirewallEntry(zone, storage, raw string) M {
	r := M{"zone": zone, "storage": storage, "raw": raw, "enabled": true, "action": "accept", "editable": true}
	switch storage {
	case "service":
		r["kind"], r["service"] = "service-rule", raw
	case "masquerade":
		r["kind"] = "masquerade"
	case "port":
		parts := strings.Split(raw, "/")
		if len(parts) != 2 {
			return nil
		}
		r["kind"], r["port"], r["protocol"] = "port", parts[0], parts[1]
	case "forward-port":
		r["kind"] = "forward"
		for _, part := range strings.Split(raw, ":") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "port":
				r["port"] = kv[1]
			case "proto":
				r["protocol"] = kv[1]
			case "toport":
				r["target_port"] = kv[1]
			case "toaddr":
				r["target_ip"] = kv[1]
			}
		}
	case "rich-rule":
		if m := richPriority.FindStringSubmatch(raw); len(m) > 1 {
			r["priority"], _ = strconv.Atoi(m[1])
		}
		if strings.Contains(raw, `log prefix="foxray "`) {
			r["logging"] = true
			if m := richLogRate.FindStringSubmatch(raw); len(m) > 1 {
				r["log_rate"] = m[1]
			}
		}
		if m := richSource.FindStringSubmatch(raw); len(m) > 1 {
			r["source"] = m[1]
		}
		if m := richFamily.FindStringSubmatch(raw); len(m) > 1 {
			r["family"] = m[1]
		}
		if strings.HasSuffix(strings.TrimSpace(raw), "masquerade") {
			r["kind"] = "masquerade"
		} else if m := richForward.FindStringSubmatch(raw); len(m) > 3 {
			r["kind"], r["port"], r["protocol"], r["target_port"] = "forward", m[1], m[2], m[3]
			if len(m) > 4 {
				r["target_ip"] = m[4]
			}
		} else if m := richPort.FindStringSubmatch(raw); len(m) > 2 {
			r["kind"], r["port"], r["protocol"] = "port", m[1], m[2]
		} else if m := richService.FindStringSubmatch(raw); len(m) > 1 {
			r["kind"], r["rich_match"], r["service"] = "rich", "service", m[1]
		} else if r["source"] != nil {
			r["kind"] = "ip"
		} else if str(r["family"]) != "" && (strings.HasSuffix(strings.TrimSpace(raw), " accept") || strings.HasSuffix(strings.TrimSpace(raw), " drop") || strings.HasSuffix(strings.TrimSpace(raw), " reject")) {
			r["kind"], r["rich_match"] = "rich", "all"
		} else {
			r["kind"], r["editable"] = "advanced", false
		}
		for _, action := range []string{"accept", "drop", "reject"} {
			if strings.HasSuffix(strings.TrimSpace(raw), action) {
				r["action"] = action
			}
		}
	default:
		return nil
	}
	normalized, e := normalizeFirewall(r)
	if e != nil {
		r["editable"] = false
		return r
	}
	normalized["raw"] = raw
	normalized["editable"] = true
	specs := firewallSpecs(normalized)
	if len(specs) != 1 || storage == "rich-rule" && compactRule(specs[0][1]) != compactRule(raw) {
		normalized["editable"] = false
	}
	return normalized
}

// firewall-cmd prints rich rules on separate indented lines under their heading.
func addZoneInventory(entries map[string]M, zone, scope, state string) bool {
	details := parseZoneDetails(scope)
	richRules := []string{}
	inRichRules := false
	for _, line := range strings.Split(scope, "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "rich rules:"); ok {
			inRichRules = true
			if value = strings.TrimSpace(value); value != "" {
				richRules = append(richRules, value)
			}
			continue
		}
		if inRichRules && strings.HasPrefix(line, "rule ") {
			richRules = append(richRules, line)
		} else if line != "" {
			inRichRules = false
		}
	}
	inventory := map[string][]string{
		"port":         strings.Fields(str(details["ports"])),
		"service":      strings.Fields(str(details["services"])),
		"forward-port": strings.Fields(str(details["forward-ports"])),
		"rich-rule":    richRules,
	}
	if str(details["masquerade"]) == "yes" {
		inventory["masquerade"] = []string{""}
	}
	for storage, lines := range inventory {
		for _, line := range lines {
			key := storage + "|" + compactRule(line)
			row := entries[key]
			if row == nil {
				row = parseFirewallEntry(zone, storage, line)
				if row == nil {
					continue
				}
				row["id"], row["managed"] = "external-"+fingerprint(key)[:16], false
				entries[key] = row
			}
			row[state] = true
		}
	}
	for _, key := range []string{"ports", "services", "forward-ports", "rich rules", "masquerade"} {
		if _, ok := details[key]; !ok {
			return false
		}
	}
	return str(details["masquerade"]) == "yes" || str(details["masquerade"]) == "no"
}

func (n *Network) FirewallSnapshot(zone string) M {
	return n.firewallSnapshotWithState(zone, n.app.service("firewalld"))
}
func (n *Network) firewallSnapshotWithState(zone string, state M, contexts ...context.Context) M {
	command := n.app.command
	if len(contexts) > 0 && contexts[0] != nil {
		command = func(seconds int, input string, args ...string) (string, error) {
			ctx, cancel := context.WithTimeout(contexts[0], time.Duration(seconds)*time.Second)
			defer cancel()
			return n.app.runner.Run(ctx, args, input)
		}
	}
	result := M{"plans": n.Plans(), "firewalld": state, "complete": false, "entries": []M{}, "zones": []string{}, "managed": []any{}}
	n.mu.Lock()
	managed := clone(arr(n.managed["firewall"]))
	n.mu.Unlock()
	result["managed"] = managed
	if str(state["state"]) != "active" {
		result["zone"] = "public"
		result["error"] = "firewalld 未运行；系统 nftables 规则仍可查看"
		raw, e := command(5, "", "nft", "-j", "list", "ruleset")
		if e == nil {
			result["ruleset_raw"] = raw
		}
		return result
	}
	if zone == "" {
		raw, err := command(8, "", "firewall-cmd", "--get-default-zone")
		if err != nil {
			result["error"] = "无法读取默认防火墙区域"
			return result
		}
		zone = strings.TrimSpace(raw)
	}
	if !firewallZoneName.MatchString(zone) {
		result["error"] = "请选择有效区域"
		return result
	}
	result["zone"] = zone
	entries := map[string]M{}
	var wg sync.WaitGroup
	var runtimeDetails, permanentDetails, icmpTypes, zonesRaw string
	var runtimeErr, permanentErr, icmpErr, zonesErr error
	read := func(output *string, err *error, args ...string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			*output, *err = command(20, "", args...)
		}()
	}
	if bundle, err := readFirewallBundle(command, zone); err == nil {
		zonesRaw = strings.Join(bundle.Zones, " ")
		runtimeDetails = firewallZoneText(zone, bundle.Runtime)
		permanentDetails = firewallZoneText(zone, bundle.Permanent)
		icmpTypes = strings.Join(bundle.ICMPTypes, " ")
	} else {
		read(&zonesRaw, &zonesErr, "firewall-cmd", "--get-zones")
		read(&runtimeDetails, &runtimeErr, append(zoneArgs(zone, false), "--list-all")...)
		read(&permanentDetails, &permanentErr, append(zoneArgs(zone, true), "--list-all")...)
		read(&icmpTypes, &icmpErr, "firewall-cmd", "--get-icmptypes")
	}
	var runtimeDirect, permanentDirect string
	var runtimeDirectErr, permanentDirectErr error
	for _, value := range managed {
		rule, err := normalizeFirewall(obj(value))
		if err == nil && firewallDriver(rule) == "firewalld" && str(rule["zone"]) == zone && str(rule["kind"]) == "masquerade" && str(rule["action"]) == "snat" {
			read(&runtimeDirect, &runtimeDirectErr, "firewall-cmd", "--direct", "--get-all-rules")
			read(&permanentDirect, &permanentDirectErr, "firewall-cmd", "--permanent", "--direct", "--get-all-rules")
			break
		}
	}
	wg.Wait()
	if zonesErr == nil {
		zones := strings.Fields(zonesRaw)
		result["zones"] = zones
		if !slices.Contains(zones, zone) {
			result["error"] = "请选择有效区域"
			return result
		}
	} else {
		result["zones"] = []string{zone}
	}
	if runtimeErr != nil && permanentErr != nil {
		result["error"] = "防火墙运行时与永久规则读取失败，请刷新重试"
	}
	complete := runtimeErr == nil && permanentErr == nil && runtimeDirectErr == nil && permanentDirectErr == nil
	if runtimeErr == nil {
		complete = addZoneInventory(entries, zone, runtimeDetails, "runtime") && complete
	}
	if permanentErr == nil {
		complete = addZoneInventory(entries, zone, permanentDetails, "permanent") && complete
	}
	directRules := func(raw string) map[string]bool {
		rules := map[string]bool{}
		for _, line := range textLines(raw) {
			rules[compactRule(line)] = true
		}
		return rules
	}
	liveDirect, savedDirect := directRules(runtimeDirect), directRules(permanentDirect)
	for _, value := range managed {
		rule, e := normalizeFirewall(obj(value))
		if e != nil || firewallDriver(rule) != "firewalld" || str(rule["zone"]) != zone {
			continue
		}
		row := clone(rule)
		row["managed"], row["editable"] = true, true
		allRuntime, allPermanent := true, true
		anyRuntime, anyPermanent := false, false
		for _, spec := range firewallSpecs(rule) {
			key := spec[0] + "|" + compactRule(spec[1])
			match := entries[key]
			live, saved := match != nil && yes(match["runtime"]), match != nil && yes(match["permanent"])
			if spec[0] == "direct-rule" {
				live = runtimeDirectErr == nil && liveDirect[compactRule(spec[1])]
				saved = permanentDirectErr == nil && savedDirect[compactRule(spec[1])]
			}
			allRuntime, allPermanent = allRuntime && live, allPermanent && saved
			anyRuntime, anyPermanent = anyRuntime || live, anyPermanent || saved
			delete(entries, key)
		}
		row["runtime"], row["permanent"] = allRuntime, allPermanent
		row["partial_runtime"], row["partial_permanent"] = anyRuntime && !allRuntime, anyPermanent && !allPermanent
		row["drift"] = yes(rule["enabled"]) && (!allRuntime || !allPermanent) || !yes(rule["enabled"]) && (anyRuntime || anyPermanent)
		entries["managed|"+str(rule["id"])] = row

	}
	rows := []M{}
	for _, row := range entries {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if str(rows[i]["kind"]) != str(rows[j]["kind"]) {
			return str(rows[i]["kind"]) < str(rows[j]["kind"])
		}
		return str(rows[i]["port"])+str(rows[i]["source"]) < str(rows[j]["port"])+str(rows[j]["source"])
	})
	result["entries"], result["complete"] = rows, complete
	zoneInfo := parseZoneDetails(runtimeDetails)
	result["zone_info"] = zoneInfo
	result["allowed_services"] = strings.Fields(str(zoneInfo["services"]))
	if str(zoneInfo["masquerade"]) == "yes" || str(zoneInfo["masquerade"]) == "no" {
		result["masquerade"] = str(zoneInfo["masquerade"]) == "yes"
	}
	live, e1 := parseZonePolicy(runtimeDetails)
	saved, e2 := parseZonePolicy(permanentDetails)
	policy := M{"complete": false, "runtime": M{}, "permanent": M{}, "icmp_types": []string{}}
	if runtimeErr == nil && permanentErr == nil && icmpErr == nil && e1 == nil && e2 == nil {
		policy["runtime"], policy["permanent"], policy["icmp_types"], policy["complete"] = live, saved, strings.Fields(icmpTypes), true
		policy["different"] = fingerprint(live) != fingerprint(saved)
	} else {
		policy["error"] = "区域运行时、永久策略或ICMP类型未完整读取"
	}
	result["zone_policy"] = policy
	installedServices := []string{}
	paths, _ := filepath.Glob("/usr/lib/firewalld/services/*.xml")
	custom, _ := filepath.Glob("/etc/firewalld/services/*.xml")
	for _, path := range append(paths, custom...) {
		installedServices = append(installedServices, strings.TrimSuffix(filepath.Base(path), ".xml"))
	}
	result["available_services"] = uniq(installedServices)
	return result
}
