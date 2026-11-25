package gateway

import (
	"sort"
	"strings"
	"sync"
)

// splitFirewallZones parses firewall-cmd --list-all-zones without treating
// indented rich rules and their contents as zone headers.
func splitFirewallZones(raw string) map[string]string {
	values := map[string]string{}
	var name string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.TrimLeft(line, " \t") == line {
			fields := strings.Fields(line)
			name = fields[0]
			if !firewallZoneName.MatchString(name) {
				name = ""
			}
		}
		if name != "" {
			values[name] += line + "\n"
		}
	}
	return values
}

func firewallDirectSet(raw string) map[string]bool {
	rules := map[string]bool{}
	for _, line := range textLines(raw) {
		rules[compactRule(line)] = true
	}
	return rules
}

func mergeManagedZoneRows(entries map[string]M, zone string, managed []any, liveDirect, savedDirect map[string]bool, liveDirectKnown, savedDirectKnown bool) {
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
				live = liveDirectKnown && liveDirect[compactRule(spec[1])]
				saved = savedDirectKnown && savedDirect[compactRule(spec[1])]
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
}

func (n *Network) firewallAllZoneSnapshot(result M, managed []any, command func(int, string, ...string) (string, error)) M {
	result["zone"] = "*"
	var runtime, permanent, runtimeDirect, permanentDirect, defaultZone string
	var runtimeErr, permanentErr, runtimeDirectErr, permanentDirectErr, defaultZoneErr error
	var wg sync.WaitGroup
	read := func(out *string, failure *error, args ...string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			*out, *failure = command(20, "", args...)
		}()
	}
	read(&runtime, &runtimeErr, "firewall-cmd", "--list-all-zones")
	read(&permanent, &permanentErr, "firewall-cmd", "--permanent", "--list-all-zones")
	read(&defaultZone, &defaultZoneErr, "firewall-cmd", "--get-default-zone")
	for _, item := range managed {
		rule := obj(item)
		if firewallDriver(rule) == "firewalld" && str(rule["kind"]) == "masquerade" && str(rule["action"]) == "snat" {
			read(&runtimeDirect, &runtimeDirectErr, "firewall-cmd", "--direct", "--get-all-rules")
			read(&permanentDirect, &permanentDirectErr, "firewall-cmd", "--permanent", "--direct", "--get-all-rules")
			break
		}
	}
	wg.Wait()
	live, saved := splitFirewallZones(runtime), splitFirewallZones(permanent)
	if runtimeErr != nil {
		live = nil
	}
	if permanentErr != nil {
		saved = nil
	}
	if len(live) == 0 && len(saved) == 0 {
		result["error"] = "无法读取所有区域的运行时及永久配置"
		return result
	}
	zoneSet := map[string]bool{}
	for zone := range live {
		zoneSet[zone] = true
	}
	for zone := range saved {
		zoneSet[zone] = true
	}
	zones := make([]string, 0, len(zoneSet))
	for zone := range zoneSet {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	rows := []M{}
	info := map[string]M{}
	ports := map[string][]string{}
	complete := len(live) > 0 && len(saved) > 0 && runtimeDirectErr == nil && permanentDirectErr == nil
	liveDirect, savedDirect := firewallDirectSet(runtimeDirect), firewallDirectSet(permanentDirect)
	for _, zone := range zones {
		entries := map[string]M{}
		if raw, ok := live[zone]; ok {
			complete = addZoneInventory(entries, zone, raw, "runtime") && complete
			info[zone] = parseZoneDetails(raw)
		} else {
			// Permanent bindings do not establish a runtime interface association.
			info[zone] = M{}
		}
		if raw, ok := saved[zone]; ok {
			complete = addZoneInventory(entries, zone, raw, "permanent") && complete
		}
		mergeManagedZoneRows(entries, zone, managed, liveDirect, savedDirect, runtimeDirectErr == nil, permanentDirectErr == nil)
		for _, row := range entries {
			if service := str(row["service"]); service != "" {
				values, found := ports[service]
				if !found {
					values = firewallServiceDisplayPorts(service, map[string]bool{})
					ports[service] = values
				}
				row["service_ports"] = values
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i]["zone"] != rows[j]["zone"] {
			return str(rows[i]["zone"]) < str(rows[j]["zone"])
		}
		return str(rows[i]["id"]) < str(rows[j]["id"])
	})
	result["entries"], result["zones"], result["zone_bindings"], result["complete"] = rows, zones, info, complete
	if value := strings.TrimSpace(defaultZone); defaultZoneErr == nil && zoneSet[value] {
		result["zone_policy"] = M{"default_zone": value}
	}
	result["available_services"] = firewallAvailableServices()
	return result
}
