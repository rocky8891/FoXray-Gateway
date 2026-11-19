package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ConnectionMonitor struct {
	metadataAt         float64
	domainHintStatus   string
	domainHints        map[string]string
	applicationAt      float64
	applicationSockets string
	wake               chan struct{}
	hosts              map[string]struct {
		at  float64
		ips []string
	}
	routeCache map[string]struct {
		at  float64
		dev string
	}
	sampleAt            map[string]float64
	historyError        string
	historyReady        bool
	lastPersist         float64
	persisted           map[string]string
	pendingGroups       map[string]M
	lastBusinessCleanup int64
	lastHistory         int64
	staleAfter          float64
	app                 *App
	mu                  sync.RWMutex
	transport           []M
	rows                []M
	summaries           M
	at                  float64
	previous            map[string]M
}

func newConnectionMonitor(a *App) *ConnectionMonitor {
	return &ConnectionMonitor{app: a, wake: make(chan struct{}, 1), staleAfter: 15, rows: []M{}, summaries: M{}, previous: map[string]M{}, hosts: map[string]struct {
		at  float64
		ips []string
	}{}, routeCache: map[string]struct {
		at  float64
		dev string
	}{}}
}
func (m *ConnectionMonitor) command(ctx context.Context, args ...string) (string, error) {
	raw, err := m.app.runner.Run(ctx, args, "")
	if len(raw) > 8*1024*1024 {
		return "", fmt.Errorf("连接数据超过8MiB，未展示不完整结果")
	}
	return raw, err
}

var socketBytes = regexp.MustCompile(`\b(bytes_sent|bytes_acked|bytes_received):([0-9]+)`)

func parseSocketConnections(raw string, endpoints map[string][]string) []M {
	rows := []M{}
	for _, line := range textLines(raw) {
		f := strings.Fields(line)
		if len(f) < 6 || !strings.Contains(line, `("xray",`) {
			continue
		}
		if f[1] != "ESTAB" && f[1] != "ESTABLISHED" && f[1] != "UNCONN" {
			continue
		}
		if f[5] == "*:*" || strings.HasSuffix(f[5], ":0") {
			continue
		}
		ids := endpoints[f[5]]
		node := ""
		if len(ids) == 1 {
			node = ids[0]
		}
		role := "核心套接字（未归属节点）"
		if node != "" {
			role = "节点传输连接"
		}
		row := M{"id": fingerprint("xray|" + f[0] + "|" + f[4] + "|" + f[5]), "module": "xray", "node_id": node, "protocol": f[0], "state": f[1], "source": f[4], "destination": f[5], "process": "Xray", "kind": role, "attribution": map[bool]string{true: "节点入口匹配", false: "无法可靠归属；不计入节点数"}[node != ""], "counter_scope": "内核套接字字节（含传输开销）"}
		for _, pair := range socketBytes.FindAllStringSubmatch(line, -1) {
			v, _ := strconv.ParseUint(pair[2], 10, 64)
			if pair[1] == "bytes_received" {
				row["download"] = v
			} else if pair[1] == "bytes_sent" || row["upload"] == nil {
				row["upload"] = v
			}
		}
		rows = append(rows, row)
	}
	return rows
}
func parseTrackedConnections(raw, module string) []M {
	rows := []M{}
	for _, line := range textLines(raw) {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		proto := ""
		state := "TRACKED"
		for _, v := range f {
			if v == "tcp" || v == "udp" {
				proto = v
			}
			if v == "ESTABLISHED" {
				state = v
			}
		}
		if proto == "" || proto == "tcp" && state != "ESTABLISHED" || strings.Contains(line, "[UNREPLIED]") {
			continue
		}
		fields := map[string][]string{}
		for _, v := range f {
			p := strings.SplitN(v, "=", 2)
			if len(p) == 2 {
				fields[p[0]] = append(fields[p[0]], p[1])
			}
		}
		if len(fields["src"]) < 2 || len(fields["dst"]) < 2 {
			continue
		}
		port := func(k string) string {
			if len(fields[k]) > 0 {
				return fields[k][0]
			}
			return "0"
		}
		src, dst := net.JoinHostPort(fields["src"][0], port("sport")), net.JoinHostPort(fields["dst"][0], port("dport"))
		r := M{"id": fingerprint(module + "|" + proto + "|" + src + "|" + dst), "module": module, "protocol": proto, "state": state, "source": src, "destination": dst, "destination_ip": fields["dst"][0], "source_ip": fields["src"][0], "kind": "内核连接跟踪", "counter_scope": "conntrack 原方向/回复方向字节；未启用内核计数时为空"}
		if len(fields["mark"]) > 0 {
			r["mark"] = fields["mark"][0]
		}
		if len(fields["bytes"]) >= 2 {
			r["upload"], _ = strconv.ParseUint(fields["bytes"][0], 10, 64)
			r["download"], _ = strconv.ParseUint(fields["bytes"][1], 10, 64)
		}
		if module == "warp" {
			r["node_id"] = "warp"
			r["attribution"] = "WARP 独立网络空间"
		}
		rows = append(rows, r)
	}
	return rows
}
func (m *ConnectionMonitor) collect() {
	ctx, cancel := context.WithTimeout(m.app.ctx, 8*time.Second)
	defer cancel()
	a := m.app
	staleAfter := 15.
	sampleStamp := now()
	if m.sampleAt == nil {
		m.sampleAt = map[string]float64{}
	}
	deferred := map[string]bool{}
	intervals := map[string]float64{}
	a.mu.RLock()
	nodes := clone(arr(a.native["nodes"]))
	components := clone(a.components)
	a.mu.RUnlock()
	active := clone(components)
	active["host"] = true
	for _, module := range []string{"host", "xray", "warp", "wireguard"} {
		interval := num(a.monitorDataPolicy(module, "connections")["interval"])
		intervals[module] = interval
		staleAfter = max(staleAfter, interval*3+8)
		if !yes(active[module]) || !a.monitorEnabled(module, "connections") {
			components[module] = false
			delete(m.sampleAt, module)
			continue
		}
		if sampleStamp-m.sampleAt[module] < interval {
			deferred[module] = true
			components[module] = false
		} else {
			m.sampleAt[module] = sampleStamp
		}
	}
	endpoints := map[string][]string{}
	names := map[string]string{}
	for _, v := range nodes {
		n := obj(v)
		names[str(n["id"])] = str(n["name"])
		if !yes(components["xray"]) {
			continue
		}
		o := obj(n["outbound"])
		host := str(o["server"])
		port := intstr(o["server_port"])
		if host == "" {
			continue
		}
		ips := []string{host}
		if net.ParseIP(host) == nil {
			cached, ok := m.hosts[host]
			if !ok || now()-cached.at > 60 {
				lookup, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
				values, e := net.DefaultResolver.LookupIPAddr(lookup, host)
				cancel()
				cached.ips = nil
				if e == nil {
					for _, v := range values {
						cached.ips = append(cached.ips, v.IP.String())
					}
				}
				cached.at = now()
				m.hosts[host] = cached
			}
			ips = cached.ips
		}
		for _, ip := range ips {
			k := net.JoinHostPort(ip, port)
			endpoints[k] = append(endpoints[k], str(n["id"]))
		}
	}
	unavailable := M{}
	for _, ids := range endpoints {
		if len(uniq(ids)) > 1 {
			for _, id := range ids {
				unavailable[id] = true
			}
		}
	}
	rows := []M{}
	summaries := M{}
	knownWG := []string{}
	xraySockets, hostCT, warpCT := "", "", ""
	var hostCTErr error
	if yes(components["xray"]) {
		raw, err := m.command(ctx, "ss", "-HatunpieO")
		xraySockets = raw
		part := parseSocketConnections(raw, endpoints)
		rows = append(rows, part...)
		summaries["xray"] = M{"node_unavailable": unavailable, "available": err == nil, "note": "Xray 内核套接字；节点卡只计可唯一匹配节点入口的传输连接，复用连接不等于逻辑请求数。"}
		if err != nil {
			obj(summaries["xray"])["error"] = "无法读取 Xray 套接字，请确认 ss 可用及读取权限"
		}
	}
	if yes(components["warp"]) {
		raw, err := m.command(ctx, "ip", "netns", "exec", "foxray-warp", "conntrack", "-L", "-o", "extended")
		warpCT = raw
		rows = append(rows, parseTrackedConnections(raw, "warp")...)
		summaries["warp"] = M{"available": err == nil, "note": "WARP 网络空间内仍被内核跟踪的已建立/双向活动连接，过期前可能保留空闲流。"}
		if err != nil {
			obj(summaries["warp"])["error"] = "WARP 未运行或 conntrack 无法读取；未修改内核计数设置"
		}
	}
	if yes(components["wireguard"]) {
		interfaces, err := m.command(ctx, "wg", "show", "interfaces")
		wg := map[string]bool{}
		for _, name := range strings.Fields(interfaces) {
			wg[name] = true
			knownWG = append(knownWG, "wg:"+name)
		}
		ct, e := m.command(ctx, "conntrack", "-L", "-o", "extended")
		hostCT, hostCTErr = ct, e
		summaries["wireguard"] = M{"available": err == nil && e == nil}
	}

	if yes(components["xray"]) {
		if !yes(components["wireguard"]) {
			hostCT, hostCTErr = m.command(ctx, "conntrack", "-L", "-o", "extended")
		}
		if hostCTErr == nil {
			rows = append(rows, xrayUDPConnections(xraySockets, hostCT, endpoints)...)
		} else {
			for _, v := range nodes {
				n := obj(v)
				protocol := str(obj(n["outbound"])["type"])
				if protocol == "hysteria2" || protocol == "tuic" || protocol == "wireguard" {
					unavailable[str(n["id"])] = true
				}
			}
		}
		obj(summaries["xray"])["note"] = "TCP读取Xray套接字；未连接UDP套接字与conntrack交叉匹配。节点卡统计唯一匹配入口的传输连接，包含检测；复用不等于请求数，UDP空闲流在内核超时前可能保留。"
	}
	if a.monitorEnabled("host", "connections") && !deferred["host"] && hostCT == "" {
		hostCT, hostCTErr = m.command(ctx, "conntrack", "-L", "-o", "extended")
	}
	transport := rows
	targets := []businessTarget{}
	if yes(components["xray"]) {
		targets = append(targets, businessTarget{"xray", "", "vpm-tun"})
	}
	if yes(components["warp"]) {
		targets = append(targets, businessTarget{"warp", "foxray-warp", "CloudflareWARP"})
	}
	for _, name := range knownWG {
		targets = append(targets, businessTarget{"wireguard", "", strings.TrimPrefix(name, "wg:")})
	}
	rows, summaries = m.businessCollect(ctx, targets)
	if a.monitorEnabled("host", "connections") && !deferred["host"] {
		summaries["host"] = serverConnectionSummary(hostCT, hostCTErr)
	}
	m.mu.RLock()
	for module := range deferred {
		summary := clone(obj(m.summaries[module]))
		summary["_deferred"] = true
		summaries[module] = summary
	}
	for _, old := range m.previous {
		if deferred[str(old["module"])] {
			r := clone(old)
			r["_deferred"] = true
			rows = append(rows, r)
		}
	}
	for _, old := range m.transport {
		if deferred[str(old["module"])] {
			transport = append(transport, clone(old))
		}
	}
	m.mu.RUnlock()
	states := businessStates(hostCT, "xray")
	for key, state := range businessStates(hostCT, "wireguard") {
		states[key] = state
	}
	if yes(components["warp"]) {
		for key, state := range businessStates(warpCT, "warp") {
			states[key] = state
		}
	}
	observedRows := []M{}
	for _, r := range rows {
		if yes(r["_deferred"]) {
			observedRows = append(observedRows, r)
			continue
		}
		state := states[str(r["module"])+"|"+str(r["protocol"])+"|"+str(r["source"])+"|"+str(r["destination"])]
		active := state != "CLOSE" && state != "TIME_WAIT" && state != "LAST_ACK" && now()-num(r["last_packet"]) <= 60
		r["active"] = active

		if state != "" {
			r["state"] = state
		}
		observedRows = append(observedRows, r)
		if yes(r["partial"]) {
			obj(summaries[str(r["module"])])["partial"] = true
		}
	}
	rows = observedRows
	if xraySockets != "" {
		m.applicationSockets = xraySockets
		m.applicationAt = now()
	}
	if xraySockets == "" && (yes(components["warp"]) || yes(components["wireguard"])) && now()-m.applicationAt >= 5 {
		m.applicationSockets, _ = m.command(ctx, "ss", "-Hatunp")
		m.applicationAt = now()
	}
	socketsForMetadata := m.applicationSockets
	if now()-m.applicationAt > 5 {
		socketsForMetadata = ""
	}
	enrichConnectionMetadata(rows, socketsForMetadata, m.existingDomainHints(now()))
	for _, row := range rows {
		if row["module"] == "xray" && str(row["domain"]) == "" && !yes(row["_deferred"]) {
			row["domain_note"] = m.domainHintStatus
		}
	}
	if yes(components["xray"]) {
		unavailable = M{}
		for _, v := range nodes {
			unavailable[str(obj(v)["id"])] = true
		}
		obj(summaries["xray"])["node_unavailable"] = unavailable
	}
	for _, module := range []string{"xray", "warp", "wireguard"} {
		if yes(components[module]) && summaries[module] == nil {
			summaries[module] = M{"available": false, "error": "未发现可观测的隧道接口"}
		}
	}
	seen := map[string]bool{}
	unique := []M{}
	for _, r := range rows {
		if !seen[str(r["id"])] {
			seen[str(r["id"])] = true
			unique = append(unique, r)
		}
	}
	rows = unique
	stamp := now()
	m.mu.Lock()
	next := map[string]M{}
	counts := map[string]int{}
	for _, r := range rows {
		if !yes(r["_deferred"]) {
			updateBusinessRates(r, m.previous[str(r["id"])], stamp)
		}

		node := str(r["node_id"])
		r["node_name"] = firstNonempty(names[node], strings.TrimPrefix(node, "wg:"))
		if node == "warp" {
			r["node_name"] = "WARP"
		}
		r["observed_seconds"] = stamp - num(r["first_seen"])
		next[str(r["id"])] = r
		if yes(r["active"]) {
			counts[str(r["node_id"])]++
		}
		delete(r, "destination_ip")
		delete(r, "source_ip")
		delete(r, "mark")
	}
	for module, v := range summaries {
		if !deferred[module] {
			obj(v)["time"] = stamp
		}
		obj(v)["stale_after"] = max(15., intervals[module]*3+8)
		if module == "host" {
			continue
		}
		sum := obj(v)
		sum["count"] = 0
		sum["protocols"] = M{"tcp": 0, "udp": 0}
		for _, r := range rows {
			if str(r["module"]) == module && yes(r["active"]) {
				sum["count"] = num(sum["count"]) + 1
				protocols := obj(sum["protocols"])
				if proto := str(r["protocol"]); proto == "tcp" || proto == "udp" {
					protocols[proto] = num(protocols[proto]) + 1
				}
			}
		}
	}
	m.rows, m.at, m.previous, m.summaries, m.transport = nil, stamp, next, summaries, transport
	m.staleAfter = staleAfter
	for _, r := range rows {
		if yes(r["active"]) {
			m.rows = append(m.rows, r)
		}
	}
	m.mu.Unlock()
	m.saveBusinessHistory(rows, stamp)
	minute := int64(stamp/60) * 60
	if minute != m.lastHistory && m.app.histdb != nil {
		if _, err := m.app.histdb.Exec("CREATE TABLE IF NOT EXISTS business_node_samples(node TEXT,t INTEGER,count INTEGER,PRIMARY KEY(node,t))"); err == nil {
			known := append([]string{}, knownWG...)
			for _, v := range nodes {
				known = append(known, str(obj(v)["id"]))
			}
			if yes(components["warp"]) {
				known = append(known, "warp")
			}
			for _, r := range rows {
				if str(r["module"]) == "wireguard" {
					known = append(known, str(r["node_id"]))
				}
			}
			for _, id := range uniq(known) {
				module := "xray"
				if id == "warp" {
					module = "warp"
				} else if strings.HasPrefix(id, "wg:") {
					module = "wireguard"
				}
				if !deferred[module] && yes(obj(summaries[module])["available"]) && !yes(obj(summaries[module])["partial"]) && !yes(obj(obj(summaries[module])["node_unavailable"])[id]) {
					_, _ = m.app.histdb.Exec("INSERT OR REPLACE INTO business_node_samples(node,t,count) VALUES(?,?,?)", id, minute, counts[id])
				}
			}
			for _, module := range []string{"xray", "warp", "wireguard"} {
				_, _ = m.app.histdb.Exec("DELETE FROM business_node_samples WHERE ("+monitorScope(module)+") AND t<?", float64(minute)-num(a.monitorDataPolicy(module, "connections")["retention_days"])*86400)
			}
			m.lastHistory = minute
		}
	}
}
func jsonDecode(raw string, value any) error { return json.Unmarshal([]byte(raw), value) }
func (m *ConnectionMonitor) snapshot(module string) M {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rows := []M{}
	for _, r := range m.rows {
		if module == "" || str(r["module"]) == module {
			rows = append(rows, publicBusinessRow(r))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return str(rows[i]["id"]) < str(rows[j]["id"]) })
	stamp := m.at
	if sample := num(obj(m.summaries[module])["time"]); sample > 0 {
		stamp = sample
	}
	return M{"items": rows, "summaries": m.currentSummaries(), "time": stamp, "error": m.historyError, "note": "连接元数据按本模块设置保留，历史查询每次最多10000条；不反向解析域名、不采集请求内容。"}
}
func (m *ConnectionMonitor) counts() map[string]M {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]M{}
	for module, value := range m.summaries {
		summary := clone(obj(value))
		if summary["time"] == nil {
			summary["time"] = m.at
		}
		if now()-num(summary["time"]) > max(15., num(summary["stale_after"])) {
			summary["available"] = false
		}
		out["module:"+module] = summary
	}
	for _, r := range m.rows {
		id := str(r["node_id"])
		if id == "" {
			continue
		}
		if out[id] == nil {
			out[id] = M{"count": 0, "protocols": M{"tcp": 0, "udp": 0}}
		}
		out[id]["count"] = num(out[id]["count"]) + 1
		if proto := str(r["protocol"]); proto == "tcp" || proto == "udp" {
			obj(out[id]["protocols"])[proto] = num(obj(out[id]["protocols"])[proto]) + 1
		}
	}
	for _, r := range out {
		if r["time"] == nil {
			r["time"] = m.at
		}
	}
	for identity, value := range out {
		if strings.HasPrefix(identity, "module:") {
			continue
		}
		module := trafficModule(identity)
		summary := out["module:"+module]
		if summary != nil {
			if summary["nodes"] == nil {
				summary["nodes"] = M{}
			}
			obj(summary["nodes"])[identity] = clone(value)
		}
	}
	return out
}
func (m *ConnectionMonitor) start() {
	go func() {
		for {
			m.collect()
			timer := time.NewTimer(time.Duration(m.connectionInterval()) * time.Second)
			select {
			case <-m.app.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			case <-m.wake:
				timer.Stop()
			}
		}
	}()
}

func (m *ConnectionMonitor) history(q url.Values) M {
	end := now()
	hours := 24.
	if h, e := strconv.ParseFloat(q.Get("hours"), 64); e == nil && h >= 1./60 && h <= 8760 {
		hours = h
	}
	start := end - hours*3600
	if v, e := strconv.ParseFloat(q.Get("start"), 64); e == nil && v > 0 {
		start = v
	}
	if v, e := strconv.ParseFloat(q.Get("end"), 64); e == nil && v > start {
		end = v
	}
	if end <= start || end-start > 365*86400 {
		return M{"items": []M{}, "error": "时间范围无效"}
	}
	width := max(60, int((end-start)/600))
	rows, e := m.app.histdb.Query("SELECT CAST(t/? AS INTEGER)*?,MAX(count) FROM business_node_samples WHERE node=? AND t>=? AND t<=? GROUP BY 1 ORDER BY 1", width, width, q.Get("node"), start, end)
	items := []M{}
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var stamp, count float64
			if rows.Scan(&stamp, &count) == nil {
				items = append(items, M{"time": stamp, "latency": count})
			}
		}
	}
	return M{"items": items, "start": start, "end": end, "note": "业务连接数按分钟保存，长范围取每桶峰值，最多保留30天；无法可靠归属的Xray节点不生成节点曲线。"}
}

func (m *ConnectionMonitor) currentSummaries() M {
	summaries := clone(m.summaries)
	for _, v := range summaries {
		r := obj(v)
		delete(r, "_deferred")
		stamp := num(r["time"])
		if stamp == 0 {
			stamp = m.at
		}
		if now()-stamp > max(15., num(r["stale_after"])) {
			r["available"] = false
			r["error"] = "连接采样已过期，等待下一次成功采集"
		}
	}
	return summaries
}
func (m *ConnectionMonitor) connectionInterval() float64 {
	interval := 300.
	for _, module := range []string{"host", "xray", "warp", "wireguard"} {
		if (module == "host" || m.app.moduleEnabled(module)) && m.app.monitorEnabled(module, "connections") {
			interval = min(interval, num(m.app.monitorDataPolicy(module, "connections")["interval"]))
		}
	}
	return max(1., interval)
}
