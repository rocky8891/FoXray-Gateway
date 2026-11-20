package gateway

import (
	"net"
	"regexp"
	"strings"
	"time"
)

// Observe actual access-log decisions only when the complete IP tuple matches.
// Domain-only records are not sufficient to join an IP flow to an outbound.
var accessOutbound = regexp.MustCompile(`\[tun\s+(?:->|>>)\s+([^\s\]]+)\]`)

func parseAccessRoutes(raw string, stamp float64) map[string]string {
	result, ambiguous := map[string]string{}, map[string]bool{}
	for _, line := range textLines(raw) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		at, err := time.ParseInLocation("2006/01/02 15:04:05", strings.Split(fields[0]+" "+fields[1], ".")[0], time.Local)
		if err != nil || stamp-float64(at.Unix()) > 60 || float64(at.Unix()) > stamp+2 {
			continue
		}
		accepted, outbound := accessDomain.FindStringSubmatch(line), accessOutbound.FindStringSubmatch(line)
		if len(accepted) != 4 || len(outbound) != 2 {
			continue
		}
		source := strings.TrimPrefix(strings.TrimPrefix(accepted[1], "tcp:"), "udp:")
		host, _, err := net.SplitHostPort(accepted[3])
		if err != nil || net.ParseIP(host) == nil {
			continue
		}
		key := socketMetadataKey(accepted[2], source, accepted[3])
		if old := result[key]; old != "" && old != outbound[1] {
			ambiguous[key] = true
		}
		result[key] = outbound[1]
	}
	for key := range ambiguous {
		delete(result, key)
	}
	return result
}

func outboundClass(tag string, outbounds map[string]M) string {
	out := outbounds[tag]
	if out == nil {
		return "unconfirmed"
	}
	if tag == "out-direct" || str(out["protocol"]) == "freedom" && str(obj(obj(out["streamSettings"])["sockopt"])["interface"]) != "CloudflareWARP" {
		return "direct"
	}
	if str(out["protocol"]) == "blackhole" {
		return "blocked"
	}
	return "proxy"
}
func classifyXrayRoute(row, engine M, hints map[string]string) (string, string, string) {
	outbounds := map[string]M{}
	for _, v := range arr(engine["outbounds"]) {
		o := obj(v)
		tag := str(o["tag"])
		outbounds[tag] = o
	}
	if len(outbounds) == 0 {
		return "unconfirmed", "", "未读取到当前核心出站配置"
	}
	if tag := hints[socketMetadataKey(str(row["protocol"]), str(row["source"]), str(row["destination"]))]; outbounds[tag] != nil {
		return outboundClass(tag, outbounds), tag, "已有访问日志的完整IP连接元组与实际出站标签匹配"
	}
	return "unconfirmed", "", "未收到核心Webhook或完整IP元组访问日志证据；不使用配置推断出站"
}
func filterXrayBusiness(rows []M, engine M, hints map[string]string) ([]M, M) {
	result := []M{}
	stats := M{"direct_excluded": 0, "blocked_excluded": 0, "unclassified_count": 0, "count_scope": "confirmed-proxy-v2"}
	for _, row := range rows {
		if row["module"] != "xray" {
			result = append(result, row)
			continue
		}
		class, tag, note := classifyXrayRoute(row, engine, hints)
		row["routing_classification"], row["outbound_tag"], row["routing_note"] = class, tag, note
		row["proxy_counted"] = class == "proxy"
		if class == "direct" || class == "blocked" {
			if yes(row["active"]) {
				key := "direct_excluded"
				if class == "blocked" {
					key = "blocked_excluded"
				}
				stats[key] = num(stats[key]) + 1
			}
			continue
		}
		if class == "unconfirmed" && yes(row["active"]) {
			stats["unclassified_count"] = num(stats["unclassified_count"]) + 1
		}
		result = append(result, row)
	}
	return result, stats
}

func proxyConnectionCounted(row M) bool {
	return row["module"] != "xray" || row["proxy_counted"] != false
}
func businessSampleModule(module string) string {
	if module == "xray" {
		return "xray-proxy-v2"
	}
	return module
}
