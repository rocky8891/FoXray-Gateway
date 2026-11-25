package gateway

import (
	"encoding/json"
	"sort"
	"strings"
)

// Evaluate the simple source-prefix and output-device rules used by FoXray's
// WireGuard and WARP helpers. Report partial evidence for other expressions.
func moduleSNATCandidates(raw string, flow firewallFlow, device string) []M {
	var snapshot struct {
		Entries []map[string]json.RawMessage `json:"nftables"`
	}
	if json.Unmarshal([]byte(raw), &snapshot) != nil {
		return nil
	}
	// Reuse the rule identities in the read-only inventory so the report
	// can link to the exact row, including rules with identical conditions.
	links := map[string][]M{}
	if rows, err := nftablesInventory(raw, nil, nil); err == nil {
		for _, row := range firewallOriginRows(rows) {
			key := str(row["raw"])
			links[key] = append(links[key], row)
		}
	}
	priorities := map[[3]string]any{}
	for _, entry := range snapshot.Entries {
		var chain M
		if json.Unmarshal(entry["chain"], &chain) != nil {
			continue
		}
		if str(chain["hook"]) == "postrouting" && str(chain["type"]) == "nat" {
			priorities[[3]string{str(chain["family"]), str(chain["table"]), str(chain["name"])}] = chain["prio"]
		}
	}
	var result []M
	for _, entry := range snapshot.Entries {
		ruleJSON := entry["rule"]
		var rule M
		if json.Unmarshal(ruleJSON, &rule) != nil {
			continue
		}
		family, table, chain := str(rule["family"]), str(rule["table"]), str(rule["chain"])
		priority, present := priorities[[3]string{family, table, chain}]
		if !present || !flow.source.Is4() || (family != "ip" && family != "inet") {
			continue
		}
		origin := ""
		if strings.HasPrefix(table, "foxray_wg_") {
			origin = "WireGuard"
		}
		if table == "foxray_vpn_guard" {
			origin = "WARP"
		}
		if origin == "" {
			continue
		}
		matches, known, nat, outputKnown := true, true, false, false
		for _, item := range arr(rule["expr"]) {
			expr := obj(item)
			if _, ok := expr["masquerade"]; ok {
				nat = true
				continue
			}
			if _, ok := expr["counter"]; ok {
				continue
			}
			match := obj(expr["match"])
			if str(match["op"]) != "==" && str(match["op"]) != "in" {
				known = false
				continue
			}
			left := obj(match["left"])
			if str(obj(left["meta"])["key"]) == "oifname" && str(match["right"]) != "" {
				outputKnown = true
				matches = matches && str(match["right"]) == device
				continue
			}
			field := obj(left["payload"])
			if str(field["protocol"]) != "ip" || str(field["field"]) != "saddr" {
				known = false
				continue
			}
			values := arr(obj(match["right"])["set"])
			if len(values) == 0 {
				values = []any{match["right"]}
			}
			found, parsed := false, true
			for _, value := range values {
				text := str(value)
				if p := obj(obj(value)["prefix"]); len(p) > 0 {
					text = str(p["addr"]) + "/" + strOrNumber(p["len"])
				}
				_, prefix, err := canonicalSource(text)
				if err != nil {
					parsed = false
					continue
				}
				found = found || prefix.Contains(flow.source)
			}
			if !parsed && !found {
				known = false
			} else {
				matches = matches && found
			}
		}
		if !nat || !matches {
			continue
		}
		evidence := "需核对完整匹配条件"
		if known && outputKnown {
			evidence = "运行时 POSTROUTING 条件相符，实际转换待验证"
		}
		data, _ := json.Marshal(rule)
		candidate := M{"name": origin + " · " + table, "kind": "masquerade", "action": "masquerade", "out_interface": device, "priority": priority, "chain": chain, "table": table, "raw": string(data), "evidence": evidence}
		key := string(ruleJSON)
		if matching := links[key]; len(matching) > 0 {
			candidate["id"] = matching[0]["id"]
			candidate["rule_origin"] = matching[0]["rule_origin"]
			links[key] = matching[1:]
		}
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool { return num(result[i]["priority"]) < num(result[j]["priority"]) })
	return result
}
