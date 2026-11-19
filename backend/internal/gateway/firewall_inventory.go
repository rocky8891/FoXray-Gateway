package gateway

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var richPriority = regexp.MustCompile(`priority="?(-?[0-9]+)"?`)
var richLogRate = regexp.MustCompile(`limit value="?([1-9][0-9]*/[smhd])"?`)
var richFamily = regexp.MustCompile(`family="?(ipv[46])"?`)

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
		} else if r["source"] != nil {
			r["kind"] = "ip"
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
func (n *Network) FirewallSnapshot(zone string) M {
	state := n.app.service("firewalld")
	result := M{"plans": n.Plans(), "firewalld": state, "complete": false, "entries": []M{}, "zones": []string{}, "managed": []any{}}
	n.mu.Lock()
	managed := clone(arr(n.managed["firewall"]))
	n.mu.Unlock()
	result["managed"] = managed
	if str(state["state"]) != "active" {
		result["zone"] = "public"
		result["error"] = "firewalld 未运行；系统 nftables 规则仍可查看"
		raw, e := n.app.command(5, "", "nft", "-j", "list", "ruleset")
		if e == nil {
			result["ruleset_raw"] = raw
		}
		return result
	}
	zonesRaw, e := n.app.command(5, "", "firewall-cmd", "--get-zones")
	if e != nil {
		return result
	}
	zones := strings.Fields(zonesRaw)
	result["zones"] = zones
	if zone == "" {
		raw, _ := n.app.command(5, "", "firewall-cmd", "--get-default-zone")
		zone = strings.TrimSpace(raw)
	}
	valid := false
	for _, v := range zones {
		valid = valid || zone == v
	}
	if !valid || !firewallZoneName.MatchString(zone) {
		result["error"] = "请选择有效区域"
		return result
	}
	result["zone"] = zone
	entries := map[string]M{}
	var mutex sync.Mutex
	var wg sync.WaitGroup
	gate := make(chan struct{}, 4)
	complete := true
	for _, permanent := range []bool{false, true} {
		for _, kind := range []string{"port", "rich-rule", "forward-port", "service", "masquerade"} {
			wg.Add(1)
			go func(storage string, permanent bool) {
				defer wg.Done()
				gate <- struct{}{}
				defer func() { <-gate }()
				args := []string{"firewall-cmd", "--zone=" + zone}
				if permanent {
					args = append(args, "--permanent")
				}
				flag := map[string]string{"port": "--list-ports", "rich-rule": "--list-rich-rules", "forward-port": "--list-forward-ports", "service": "--list-services", "masquerade": "--query-masquerade"}[storage]
				raw, e := n.app.command(6, "", append(args, flag)...)
				mutex.Lock()
				defer mutex.Unlock()
				if e != nil && !(storage == "masquerade" && strings.TrimSpace(raw) == "no") {
					complete = false
					return
				}
				lines := textLines(raw)
				if storage == "port" || storage == "service" {
					lines = strings.Fields(raw)
				}
				if storage == "masquerade" {
					lines = nil
					if strings.TrimSpace(raw) == "yes" {
						lines = []string{""}
					}
				}
				for _, line := range lines {
					row := parseFirewallEntry(zone, storage, line)
					if row == nil {
						continue
					}
					key := storage + "|" + compactRule(line)
					stored := entries[key]
					if stored == nil {
						stored = row
						stored["id"] = "external-" + fingerprint(key)[:16]
						stored["managed"] = false
						entries[key] = stored
					}
					if permanent {
						stored["permanent"] = true
					} else {
						stored["runtime"] = true
					}
				}
			}(kind, permanent)
		}
	}
	wg.Wait()
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
				var liveErr, savedErr error
				live, liveErr = n.queryFirewall(zone, spec, false)
				saved, savedErr = n.queryFirewall(zone, spec, true)
				complete = complete && liveErr == nil && savedErr == nil
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
	masq, _ := n.queryFirewall(zone, [2]string{"masquerade", ""}, false)
	result["masquerade"] = masq
	services, _ := n.app.command(5, "", "firewall-cmd", "--zone="+zone, "--list-services")
	result["allowed_services"] = strings.Fields(services)
	details, _ := n.app.command(5, "", "firewall-cmd", "--zone="+zone, "--list-all")
	zoneInfo := M{}
	for _, line := range textLines(details) {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 {
			zoneInfo[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	result["zone_info"] = zoneInfo
	installedServices := []string{}
	paths, _ := filepath.Glob("/usr/lib/firewalld/services/*.xml")
	custom, _ := filepath.Glob("/etc/firewalld/services/*.xml")
	for _, path := range append(paths, custom...) {
		installedServices = append(installedServices, strings.TrimSuffix(filepath.Base(path), ".xml"))
	}
	result["available_services"] = uniq(installedServices)
	return result
}
