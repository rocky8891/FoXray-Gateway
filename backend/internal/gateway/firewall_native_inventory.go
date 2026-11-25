package gateway

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
)

var iptablesToken = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|[^\s"]+`)

func nativeInventoryRow(driver, family, table, chain, raw string) M {
	return M{"driver": driver, "zone": "host", "family": family, "table": table, "chain": chain, "raw": raw, "kind": "system", "enabled": true, "runtime": true, "editable": false, "managed": false}
}

func iptablesInventory(raw, family string, owned map[string]bool) []M {
	rows := []M{}
	table := ""
	positions := map[string]int{}
	for _, line := range textLines(raw) {
		if strings.HasPrefix(line, "*") {
			table = strings.TrimPrefix(line, "*")
			continue
		}
		if line == "COMMIT" {
			table = ""
			continue
		}
		if table == "" || !strings.HasPrefix(line, "-A ") {
			continue
		}
		fields := iptablesToken.FindAllString(line, -1)
		if len(fields) < 3 {
			continue
		}
		chainKey := table + "|" + fields[1]
		positions[chainKey]++
		if owned[fields[1]] {
			continue
		}
		row := nativeInventoryRow("iptables", family, table, fields[1], line)
		row["priority"] = positions[chainKey]
		row["native_summary"] = strings.Join(fields[2:], " ")
		negate := false
		for i := 2; i < len(fields); i++ {
			if fields[i] == "!" {
				negate = true
				continue
			}
			key := ""
			switch fields[i] {
			case "-s", "--source":
				key = "source"
			case "-d", "--destination":
				key = "destination"
			case "-i", "--in-interface":
				key = "in_interface"
			case "-o", "--out-interface":
				key = "out_interface"
			case "-p", "--protocol":
				key = "protocol"
			case "--dport", "--destination-port", "--dports":
				key = "port"
			case "-j", "--jump", "-g", "--goto":
				key = "action"
			}
			if key != "" && i+1 < len(fields) {
				i++
				value := fields[i]
				if key == "action" {
					value = strings.ToLower(value)
				}
				if negate {
					value = "! " + value
				}
				row[key] = value
			}
			negate = false
		}
		if !owned[strings.ToUpper(str(row["action"]))] {
			rows = append(rows, row)
		}
	}
	return rows
}

func nftablesInventory(raw string, owned map[string]bool, counters map[string]M) ([]M, error) {
	var data struct {
		Entries []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	if data.Entries == nil {
		return nil, errors.New("缺少 nftables 规则列表")
	}
	chainPriorities := map[[3]string]any{}
	for _, entry := range data.Entries {
		var chain M
		if raw, found := entry["chain"]; found && json.Unmarshal(raw, &chain) == nil && chain["prio"] != nil {
			chainPriorities[[3]string{str(chain["family"]), str(chain["table"]), str(chain["name"])}] = chain["prio"]
		}
	}
	rows := []M{}
	for _, entry := range data.Entries {
		ruleJSON, ok := entry["rule"]
		if !ok {
			continue
		}
		var rule M
		if err := json.Unmarshal(ruleJSON, &rule); err != nil {
			return nil, err
		}
		table := str(rule["table"])
		row := nativeInventoryRow("nftables", str(rule["family"]), table, str(rule["chain"]), string(ruleJSON))
		row["handle"] = rule["handle"]
		row["priority"] = chainPriorities[[3]string{str(rule["family"]), table, str(rule["chain"])}]
		summary := []string{}
		for _, expression := range arr(rule["expr"]) {
			expr := obj(expression)
			if counter := obj(expr["counter"]); len(counter) > 0 {
				row["packets"], row["bytes"] = counter["packets"], counter["bytes"]
				if owned[table] && counters != nil {
					if counters[table] == nil {
						counters[table] = M{}
					}
					c := counters[table]
					c["packets"], c["bytes"] = num(c["packets"])+num(counter["packets"]), num(c["bytes"])+num(counter["bytes"])
				}
			}
			summary = append(summary, nftInventorySummary(expr)...)
			for _, action := range []string{"accept", "drop", "reject", "jump", "goto", "return", "masquerade", "snat", "dnat", "redirect"} {
				if _, ok := expr[action]; ok {
					row["action"] = action
					if action == "dnat" || action == "redirect" || action == "snat" {
						nat := obj(expr[action])
						if addr := str(nat["addr"]); addr != "" {
							if action == "snat" {
								row["to_source"] = addr
							} else {
								row["target_ip"] = addr
							}
						}
						if nat["port"] != nil && (action == "dnat" || action == "redirect") {
							switch nat["port"].(type) {
							case string, float64:
								row["target_port"] = strOrNumber(nat["port"])
							}
						}
					}
					if target := str(obj(expr[action])["target"]); target != "" {
						row["action"] = action + " " + target
					}
				}
			}
			match := obj(expr["match"])
			if str(match["op"]) != "==" {
				continue
			}
			payload := obj(obj(match["left"])["payload"])
			right := ""
			switch value := match["right"].(type) {
			case string:
				right = value
			case float64:
				right = intstr(value)
			}
			if prefix := obj(obj(match["right"])["prefix"]); len(prefix) > 0 {
				right = str(prefix["addr"]) + "/" + strOrNumber(prefix["len"])
			}
			if values := arr(obj(match["right"])["range"]); len(values) == 2 && str(payload["field"]) == "dport" {
				right = strOrNumber(values[0]) + "-" + strOrNumber(values[1])
			}
			if right == "" {
				continue
			}
			switch str(obj(obj(match["left"])["meta"])["key"]) {
			case "iifname":
				row["in_interface"] = right
			case "oifname":
				row["out_interface"] = right
			}
			switch str(payload["field"]) {
			case "saddr", "daddr":
				if str(payload["protocol"]) != "ip" && str(payload["protocol"]) != "ip6" {
					continue
				}
				key := "source"
				if str(payload["field"]) == "daddr" {
					key = "destination"
				}
				row[key] = right
			case "dport":
				row["port"], row["protocol"] = right, payload["protocol"]
			}
		}
		if comment := str(rule["comment"]); comment != "" {
			summary = append(summary, "comment "+comment)
		}
		row["native_summary"] = strings.Join(summary, " · ")
		if !owned[table] {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// Keep nft expressions visible even when the structured columns cannot
// represent their semantics (for example named sets or verdict maps).
func nftInventorySummary(expr M) []string {
	keys := make([]string, 0, len(expr))
	for key := range expr {
		if key != "counter" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		value := expr[key]
		if match := obj(value); key == "match" && len(match) > 0 {
			result = append(result, strings.Join([]string{nftInventoryValue(match["left"]), str(match["op"]), nftInventoryValue(match["right"])}, " "))
		} else if value == nil {
			result = append(result, key)
		} else if target := str(obj(value)["target"]); (key == "jump" || key == "goto") && target != "" {
			result = append(result, key+" "+target)
		} else {
			result = append(result, key+" "+nftInventoryValue(value))
		}
	}
	if len(result) == 0 && expr["counter"] != nil {
		result = append(result, "counter")
	}
	return result
}

func nftInventoryValue(value any) string {
	switch typed := value.(type) {
	case string, float64:
		return strOrNumber(typed)
	case map[string]any:
		if port := obj(typed["payload"]); str(port["protocol"]) != "" && str(port["field"]) != "" {
			return str(port["protocol"]) + " " + str(port["field"])
		}
		for _, object := range []string{"meta", "ct"} {
			if fields := obj(typed[object]); len(fields) == 1 && str(fields["key"]) != "" {
				return object + " " + str(fields["key"])
			}
		}
		if prefix := obj(typed["prefix"]); str(prefix["addr"]) != "" && prefix["len"] != nil {
			return str(prefix["addr"]) + "/" + strOrNumber(prefix["len"])
		}
		if items := arr(typed["range"]); len(items) == 2 {
			return nftInventoryValue(items[0]) + "–" + nftInventoryValue(items[1])
		}
		if items, ok := typed["set"].([]any); ok {
			values := make([]string, 0, len(items))
			for _, item := range items {
				values = append(values, nftInventoryValue(item))
			}
			return "{" + strings.Join(values, ", ") + "}"
		}
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func identifyNativeRows(rows []M) []M {
	seen := map[string]int{}
	for _, row := range rows {
		identity := first(row["handle"], row["raw"])
		key := fingerprint([]any{row["driver"], row["family"], row["table"], row["chain"], identity})[:16]
		seen[key]++
		row["id"] = "system-" + key + "-" + intstr(seen[key])
	}
	return rows
}
