package gateway

import (
	"encoding/json"
	"errors"
	"regexp"
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
		for _, expression := range arr(rule["expr"]) {
			expr := obj(expression)
			if counter := obj(expr["counter"]); len(counter) > 0 {
				row["packets"], row["bytes"] = counter["packets"], counter["bytes"]
				if owned[table] {
					if counters[table] == nil {
						counters[table] = M{}
					}
					c := counters[table]
					c["packets"], c["bytes"] = num(c["packets"])+num(counter["packets"]), num(c["bytes"])+num(counter["bytes"])
				}
			}
			for _, action := range []string{"accept", "drop", "reject", "jump", "goto", "return", "masquerade", "snat", "dnat", "redirect"} {
				if _, ok := expr[action]; ok {
					row["action"] = action
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
			if right == "" {
				continue
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
		if !owned[table] {
			rows = append(rows, row)
		}
	}
	return rows, nil
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
