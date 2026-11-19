package gateway

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// This table only observes packets. It never changes marks, verdicts or routes.
const businessTable = "foxray_business_v1"
const businessOwner = "FoXray passive business monitor v1"
const businessTimeout = 600

type businessTarget struct{ module, namespace, device string }

func (t businessTarget) prefix() string { return "f" + fingerprint(t.module + "|" + t.device)[:12] }
func (t businessTarget) args(args ...string) []string {
	if t.namespace != "" {
		return append([]string{"ip", "netns", "exec", t.namespace}, args...)
	}
	return args
}

func businessRules(targets []businessTarget, existing map[string]bool, create bool) string {
	var s strings.Builder
	if create {
		fmt.Fprintf(&s, "add table inet %s { comment %q; }\n", businessTable, businessOwner)
		for _, hook := range []string{"prerouting", "postrouting"} {
			fmt.Fprintf(&s, "add chain inet %s %s { type filter hook %s priority 150; policy accept; }\n", businessTable, hook, hook)
		}
	}
	for _, t := range targets {
		for _, family := range []string{"ip", "ip6"} {
			addressType, nfproto := "ipv4_addr", "ipv4"
			if family == "ip6" {
				addressType, nfproto = "ipv6_addr", "ipv6"
			}
			for _, proto := range []string{"tcp", "udp"} {
				for _, direction := range []string{"up", "down"} {
					name := t.prefix() + "_" + family + "_" + proto + "_" + direction
					if existing[name] {
						continue
					}
					fmt.Fprintf(&s, "add set inet %s %s { type %s . inet_service . %s . inet_service; flags dynamic,timeout; timeout 600s; size 4096; }\n", businessTable, name, addressType, addressType)
					hook, dev := "postrouting", "oifname"
					if direction == "down" {
						hook, dev = "prerouting", "iifname"
					}
					fmt.Fprintf(&s, "add rule inet %s %s meta nfproto %s meta l4proto %s %s %q update @%s { ct original %s saddr . ct original proto-src . ct original %s daddr . ct original proto-dst counter }\n", businessTable, hook, nfproto, proto, dev, t.device, name, family, family)
				}
			}
		}
	}
	return s.String()
}

func parseBusinessMeters(raw string, targets []businessTarget, stamp float64) ([]M, error) {
	var doc struct {
		NFTables []M `json:"nftables"`
	}
	if err := jsonDecode(raw, &doc); err != nil {
		return nil, err
	}
	return parseBusinessMeterItems(doc.NFTables, targets, stamp)
}
func parseBusinessMeterItems(items []M, targets []businessTarget, stamp float64) ([]M, error) {
	sets := map[string]struct {
		target           businessTarget
		proto, direction string
	}{}
	for _, t := range targets {
		for _, family := range []string{"ip", "ip6"} {
			for _, proto := range []string{"tcp", "udp"} {
				for _, dir := range []string{"up", "down"} {
					sets[t.prefix()+"_"+family+"_"+proto+"_"+dir] = struct {
						target           businessTarget
						proto, direction string
					}{t, proto, dir}
				}
			}
		}
	}
	flows := map[string]M{}
	for _, item := range items {
		set := obj(item["set"])
		meta, ok := sets[str(set["name"])]
		if !ok {
			continue
		}
		for _, value := range arr(set["elem"]) {
			e := obj(obj(value)["elem"])
			tuple := arr(obj(e["val"])["concat"])
			if len(tuple) != 4 || net.ParseIP(str(tuple[0])) == nil || net.ParseIP(str(tuple[2])) == nil {
				return nil, fmt.Errorf("业务连接计数元组无效，已停止展示错误数据")
			}
			port := func(v any) (string, bool) { n, err := integer(v, 0, 65535); return strconv.Itoa(n), err == nil }
			sp, a := port(tuple[1])
			dp, b := port(tuple[3])
			if !a || !b {
				return nil, fmt.Errorf("业务连接端口无效")
			}
			src, dst := net.JoinHostPort(str(tuple[0]), sp), net.JoinHostPort(str(tuple[2]), dp)
			t := meta.target
			id := fingerprint("business|" + t.module + "|" + t.device + "|" + meta.proto + "|" + src + "|" + dst)
			r := flows[id]
			if r == nil {
				node := ""
				if t.module == "warp" {
					node = "warp"
				}
				if t.module == "wireguard" {
					node = "wg:" + t.device
				}
				r = M{"id": id, "module": t.module, "node_id": node, "protocol": meta.proto, "source": src, "destination": dst, "interface": t.device, "kind": "业务连接", "state": "近期有流量", "counter_scope": "独立被动计数器观测到的IP字节（不含隧道封装）；上传为发往隧道，下载为隧道返回", "attribution": "实际经过隧道接口；Xray无法可靠识别具体出站节点", "last_packet": 0.}
				flows[id] = r
			}
			direction := "upload"
			if meta.direction == "down" {
				direction = "download"
			}
			if e["counter"] == nil {
				return nil, fmt.Errorf("业务连接计数器不可读取")
			}
			r[direction] = num(obj(e["counter"])["bytes"])
			r["_observed_"+direction] = true
			r["last_packet"] = max(num(r["last_packet"]), stamp-float64(businessTimeout)+num(e["expires"]))
			if len(arr(set["elem"])) >= 4096 {
				r["partial"] = true
			}
		}
	}
	rows := []M{}
	for _, r := range flows {
		// Missing opposite-direction elements mean no packets observed in that
		// direction, not missing kernel accounting.
		if r["upload"] == nil {
			r["upload"] = 0.
		}
		if r["download"] == nil {
			r["download"] = 0.
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func (m *ConnectionMonitor) businessCollect(ctx context.Context, targets []businessTarget) ([]M, M) {
	rows, summaries := []M{}, M{}
	groups := map[string][]businessTarget{}
	for _, t := range targets {
		if iface(t.device) != nil {
			continue
		}
		raw, err := m.command(ctx, t.args("ip", "-j", "link", "show", "dev", t.device)...)
		var links []M
		if err != nil || jsonDecode(raw, &links) != nil || len(links) != 1 {
			summaries[t.module] = M{"available": false, "error": "未发现可观测的隧道接口：" + t.device}
			continue
		}
		groups[t.namespace] = append(groups[t.namespace], t)
	}
	for _, group := range groups {
		t := group[0]
		raw, err := m.command(ctx, t.args("nft", "-j", "list", "table", "inet", businessTable)...)
		create, existing := err != nil, map[string]bool{}
		var meterItems []M
		if err == nil {
			var doc struct {
				NFTables []M `json:"nftables"`
			}
			if jsonDecode(raw, &doc) != nil {
				err = fmt.Errorf("业务连接计数表无法解析")
			} else {
				meterItems = doc.NFTables
				owned := false
				for _, v := range doc.NFTables {
					if str(obj(v["table"])["comment"]) == businessOwner {
						owned = true
					}
					if v["set"] != nil {
						existing[str(obj(v["set"])["name"])] = true
					}
				}
				if !owned {
					err = fmt.Errorf("同名计数表不属于FoXray，已保留原表")
				}
			}
		}
		if create || err == nil {
			script := businessRules(group, existing, create)
			if script != "" {
				if m.app.readonly {
					err = fmt.Errorf("只读模式不安装业务连接计数器")
				} else {
					_, err = m.app.runner.Run(ctx, t.args("nft", "-f", "-"), script)
				}
			}
			if err == nil && script != "" {
				raw, err = m.command(ctx, t.args("nft", "-j", "list", "table", "inet", businessTable)...)
				var fresh struct {
					NFTables []M `json:"nftables"`
				}
				if err == nil {
					err = jsonDecode(raw, &fresh)
					meterItems = fresh.NFTables
				}
			}
		}
		part := []M{}
		if err == nil {
			part, err = parseBusinessMeterItems(meterItems, group, now())
		}
		for _, target := range group {
			sum := obj(summaries[target.module])
			if summaries[target.module] == nil {
				sum = M{"available": true, "note": "实际经过隧道的业务IP连接；只统计安装计数器后的字节。近期流量保留60秒，已关闭TCP不计入；Xray不猜测出站节点。"}
				summaries[target.module] = sum
			}
			if err != nil {
				sum["available"] = false
				sum["error"] = "业务连接采集不可用：" + err.Error()
			}
		}
		if err == nil {
			rows = append(rows, part...)
		}
	}
	return rows, summaries
}

func businessStates(raw, module string) map[string]string {
	out := map[string]string{}
	for _, line := range textLines(raw) {
		fields, proto, state := map[string][]string{}, "", "近期有流量"
		for _, word := range strings.Fields(line) {
			if word == "tcp" || word == "udp" {
				proto = word
			}
			if strings.Contains("|ESTABLISHED|SYN_SENT|SYN_RECV|FIN_WAIT|CLOSE_WAIT|LAST_ACK|TIME_WAIT|CLOSE|", "|"+word+"|") {
				state = word
			}
			pair := strings.SplitN(word, "=", 2)
			if len(pair) == 2 {
				fields[pair[0]] = append(fields[pair[0]], pair[1])
			}
		}
		if len(fields["src"]) < 1 || len(fields["dst"]) < 1 || len(fields["sport"]) < 1 || len(fields["dport"]) < 1 {
			continue
		}
		src, dst := net.JoinHostPort(fields["src"][0], fields["sport"][0]), net.JoinHostPort(fields["dst"][0], fields["dport"][0])
		out[module+"|"+proto+"|"+src+"|"+dst] = state
	}
	return out
}

// Keep each direction cumulative while its opposite-direction set is idle or
// expires. Raw nft counters may restart independently; never emit negative rates.
func updateBusinessRates(row, old M, stamp float64) {
	first := min(stamp, num(row["last_packet"]))
	if old != nil {
		first = num(old["first_seen"])
	}
	elapsed := stamp - num(old["time"])
	for _, direction := range []string{"upload", "download"} {
		raw := num(row[direction])
		previousRaw := num(old["_raw_"+direction])
		if old != nil && elapsed > 0 {
			delta := raw - previousRaw
			if delta < 0 {
				delta = raw
			}
			if !yes(row["_observed_"+direction]) {
				delta = 0
				raw = previousRaw
			}
			row[direction] = num(old[direction]) + delta
			row[direction+"_bps"] = delta / elapsed
		}
		row["_raw_"+direction] = raw
	}
	row["first_seen"], row["time"] = first, stamp
}
func publicBusinessRow(row M) M {
	result := clone(row)
	for key := range result {
		if strings.HasPrefix(key, "_") {
			delete(result, key)
		}
	}
	return result
}

func serverConnectionSummary(raw string, err error) M {
	protocols := M{"tcp": 0, "udp": 0, "icmp": 0, "icmpv6": 0, "other": 0}
	count := 0
	if err != nil {
		return M{"available": false, "error": "无法读取主机连接跟踪，请检查conntrack及权限"}
	}
	for _, line := range textLines(raw) {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.Contains(line, "src=") || !strings.Contains(line, "dst=") {
			continue
		}
		proto := "other"
		for _, word := range fields {
			if _, ok := protocols[word]; ok {
				proto = word
				break
			}
		}
		protocols[proto] = num(protocols[proto]) + 1
		count++
	}
	return M{"available": true, "count": count, "protocols": protocols, "note": "主机内核连接跟踪项；包含尚未过期的空闲流，不等于HTTP请求数或正在传输的套接字数。"}
}
