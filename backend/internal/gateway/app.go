package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type App struct {
	measurementMu                           sync.Mutex
	publicRulesMu                           sync.Mutex
	publicRules                             M
	logPolicyMu                             sync.RWMutex
	logPruneMu                              sync.Mutex
	logPolicies                             M
	lastLogPrune                            time.Time
	dataMu                                  sync.RWMutex
	monitorData                             M
	monitorMu                               sync.RWMutex
	monitoringItems                         M
	connections                             *ConnectionMonitor
	updateMu                                sync.Mutex
	listener                                string
	interfaceCache                          interfaceCounterCache
	host                                    *HostMonitor
	traffic                                 *Traffic
	wgKeys                                  map[string]wgKeyMaterial
	warpDisabled                            bool
	base, ui                                string
	readonly                                bool
	runner                                  Runner
	ctx                                     context.Context
	cancel                                  context.CancelFunc
	mu                                      sync.RWMutex
	mutate                                  sync.Mutex
	cfg, native, settings, icmp, components M
	results                                 map[string]M
	history                                 map[string][]M
	active                                  string
	applied                                 bool
	lastSwitch                              float64
	activeSince                             float64
	recoveryIssue                           string
	moduleMu                                sync.Mutex
	geoMu                                   sync.RWMutex
	geo                                     M
	jobs                                    jobSet
	logdb, histdb                           *sql.DB
	network                                 *Network
	warpMu                                  sync.Mutex
	warpOperate                             sync.Mutex
	scoutOperate                            sync.Mutex
	warpCache                               M
	warpAt                                  time.Time
	warpConnectedSince                      float64
	warpConnectionKey                       string
	probeMu                                 sync.Mutex
	warpProbeMu                             sync.Mutex
	pendingImports                          map[string]M
	lastCandidate                           string
	candidateStreak                         int
	started                                 sync.Once
	stateJSON                               atomic.Value
}

func New(base, ui string, readonly bool) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{base: base, ui: ui, readonly: readonly, runner: SystemRunner{}, ctx: ctx, cancel: cancel, results: map[string]M{}, history: map[string][]M{}, pendingImports: map[string]M{}, jobs: jobSet{gate: make(chan struct{}, 8)}}
	a.connections = newConnectionMonitor(a)
	a.monitoringItems = defaultMonitorItems()
	_ = readJSON(filepath.Join(base, "monitoring-items.json"), &a.monitoringItems)
	a.monitoringItems["host"] = canonicalHostMonitorItems(obj(a.monitoringItems["host"]))
	a.cfg = M{"enabled": false, "mode": "manual", "fallback_active": false, "selected": "direct", "interval": 30, "latency_enabled": true, "latency_concurrency": 4, "speed_url": "https://cachefly.cachefly.net/100mb.test", "speed_timeout": 15, "speed_enabled": true, "speed_size_mb": 50, "speed_concurrency": 1, "timeout": 10, "failures": 3, "hold": 300, "improvement": 5, "confirmations": 3, "route_profile": "bypass_cn", "capture_host": true, "capture_lan": true, "score_latency_window_minutes": 30, "score_latency_min_samples": 1, "score_metrics": []any{"latency"}, "score_weights": M{"latency": 80, "loss": 10, "speed": 10}, "latency_url": "https://www.google.com/generate_204", "custom_rules": []any{}, "rule_sets": []any{}, "active_rule_set": "", "bypass_ips": []any{}, "vpn_exclusions": defaultVPNExclusions(), "endpoints": []any{}}
	if e := readJSON(filepath.Join(base, "config.json"), &a.cfg); e != nil && !os.IsNotExist(e) {
		cancel()
		return nil, errors.New("代理配置无法读取")
	}
	migratePresetSources(a.cfg)
	a.native = M{"nodes": []any{}, "subscriptions": []any{}, "excluded": M{}}
	if e := readJSON(filepath.Join(base, "private", "nodes.json"), &a.native); e != nil && !os.IsNotExist(e) {
		cancel()
		return nil, errors.New("节点数据无法读取")
	}
	a.settings = M{"log_retention_days": 30, "measurement_retention_days": 30, "log_max_rows": 10000, "chart_refresh_seconds": 10}
	_ = readJSON(filepath.Join(base, "manager-settings.json"), &a.settings)
	a.loadMonitorData()
	a.icmp = M{"enabled": false, "interval": 300, "count": 20, "size": 56, "timeout": 2}
	_ = readJSON(filepath.Join(base, "icmp-policy.json"), &a.icmp)
	_, warpInstalled := exec.LookPath("warp-cli")
	a.components = M{"xray": true, "wireguard": true, "warp": warpInstalled == nil, "komari": false, "komari_url": ""}
	_ = readJSON(filepath.Join(base, "components.json"), &a.components)
	if yes(a.components["komari"]) && str(a.components["komari_url"]) == "" {
		a.components["komari_url"] = "http://192.168.1.8:25774/"
	}
	a.geo = M{}
	_ = readJSON(filepath.Join(base, "geo-cache.json"), &a.geo)
	if a.geo == nil {
		a.geo = M{}
	}
	metrics := M{}
	if readJSON(filepath.Join(base, "measurements.json"), &metrics) == nil {
		for k, v := range obj(metrics["results"]) {
			a.results[k] = obj(v)
		}
		for k, v := range obj(metrics["history"]) {
			for _, row := range arr(v) {
				a.history[k] = append(a.history[k], obj(row))
			}
		}
	}
	adoption := M{}
	durable := M{}
	if readJSON(filepath.Join(base, "private", "control-state.json"), &durable) == nil {
		cfg, _ := os.ReadFile(filepath.Join(base, "config.json"))
		engine, _ := os.ReadFile(filepath.Join(base, "engine.json"))
		if str(durable["config_hash"]) == fingerprint(string(cfg)) && str(durable["engine_hash"]) == fingerprint(string(engine)) {
			adoption = durable
			adoption["time"] = now()
		}
	}
	short := M{}
	if len(adoption) == 0 && readJSON(filepath.Join(base, "private", "go-adoption.json"), &short) == nil && now()-num(short["time"]) < 300 {
		adoption = short
	}
	if len(adoption) > 0 && time.Since(time.Unix(int64(num(adoption["time"])), 0)) < 5*time.Minute {
		if _, err := a.adoptControl(adoption); err != nil {
			a.recoveryIssue = err.Error()
		}
	}
	if a.active == "" {
		if yes(a.cfg["enabled"]) && a.recoveryIssue == "" {
			a.recoveryIssue = "未找到可验证的 Xray 接管快照，未恢复接管"
		}
		a.active = "direct"
		if yes(a.components["warp"]) {
			a.active = "warp"
		}
		a.cfg["enabled"] = false
	}
	var e error
	a.loadExitRuntime()
	a.logdb, e = openDB(filepath.Join(base, "operation-history.sqlite3"))
	if e != nil {
		cancel()
		return nil, e
	}
	_, e = a.logdb.Exec(`CREATE TABLE IF NOT EXISTS operations(id TEXT PRIMARY KEY,t REAL,level TEXT,action TEXT,status TEXT,message TEXT,details TEXT)`)
	if e != nil {
		return nil, e
	}
	a.loadLogRetention()
	if a.recoveryIssue != "" && !readonly {
		a.audit("proxy.startup", a.recoveryIssue+"；当前出口已回退，请检查 Xray", false, nil)
	}
	a.histdb, e = openDB(filepath.Join(base, "latency-history.sqlite3"))
	if e != nil {
		return nil, e
	}
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS samples(node TEXT,t REAL,latency REAL,PRIMARY KEY(node,t))`, `CREATE TABLE IF NOT EXISTS icmp_samples(node TEXT,t REAL,latency REAL,success REAL,sent INTEGER,received INTEGER,status TEXT,PRIMARY KEY(node,t))`} {
		if _, e = a.histdb.Exec(q); e != nil {
			return nil, e
		}
	}
	prefs := M{}
	_ = readJSON(filepath.Join(base, "service-preferences.json"), &prefs)
	a.warpDisabled = yes(prefs["warp_manual_stop"])
	a.network = NewNetwork(a)
	a.host, e = NewHostMonitor(a)
	if e != nil {
		return nil, e
	}
	a.traffic, e = NewTraffic(a)
	if e != nil {
		return nil, e
	}

	return a, nil
}
func openDB(path string) (*sql.DB, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	_ = f.Close()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, e = db.Exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;")
	return db, e
}
func (a *App) Close() {
	a.cancel()
	if a.host != nil {
		a.host.close()
	}
	if a.traffic != nil {
		a.traffic.close()
	}
	if a.network != nil && !a.readonly {
		a.network.rollbackPending()
	}
	if a.logdb != nil {
		a.logdb.Close()
	}
	if a.histdb != nil {
		a.histdb.Close()
	}
}
func (a *App) persist() error {
	a.publicRulesMu.Lock()
	a.publicRules = nil
	a.publicRulesMu.Unlock()
	return writeJSON(filepath.Join(a.base, "config.json"), a.cfg)
}
func (a *App) persistNative() error {
	return writeJSON(filepath.Join(a.base, "private", "nodes.json"), a.native)
}
func (a *App) saveMetrics() {
	a.mu.RLock()
	data := M{"results": clone(a.results), "history": clone(a.history)}
	a.mu.RUnlock()
	_ = writeJSON(filepath.Join(a.base, "measurements.json"), data)
}
func (a *App) audit(action, message string, ok bool, details M) error {
	status, level := "success", "info"
	if !ok {
		status, level = "error", "error"
	}
	if details == nil {
		details = M{}
	}
	if str(details["result_status"]) == "partial" {
		status, level = "partial", "warning"
	}
	b, _ := json.Marshal(details)
	_, err := a.logdb.Exec("INSERT INTO operations(id,t,level,action,status,message,details) VALUES(?,?,?,?,?,?,?)", id(), now(), level, action, status, label(message), string(b))
	if err != nil {
		return err
	}
	_ = a.pruneLogs(false)
	return nil
}
func (a *App) operations(before, kind string) M {
	q := "SELECT id,t,level,action,status,message,details FROM operations WHERE 1=1"
	args := []any{}
	if kind == "" {
		q += " AND (" + logScope("operations") + ")"
	}
	if before != "" {
		q += " AND t<(SELECT t FROM operations WHERE id=?)"
		args = append(args, before)
	}
	if kind != "" {
		q += " AND action LIKE ?"
		args = append(args, kind+"%")
	}
	q += " ORDER BY t DESC LIMIT 50"
	rows, e := a.logdb.Query(q, args...)
	items := []M{}
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var identity, level, action, status, message, details string
			var t float64
			if rows.Scan(&identity, &t, &level, &action, &status, &message, &details) == nil {
				d := M{}
				_ = json.Unmarshal([]byte(details), &d)
				items = append(items, M{"id": identity, "time": t, "level": level, "action": action, "status": status, "message": message, "details": d})
			}
		}
	}
	next := ""
	if len(items) == 50 {
		next = str(items[49]["id"])
	}
	return M{"items": items, "next": next}
}
func (a *App) operation(identity string) M {
	var t float64
	var level, action, status, message, details string
	e := a.logdb.QueryRow("SELECT t,level,action,status,message,details FROM operations WHERE id=?", identity).Scan(&t, &level, &action, &status, &message, &details)
	if e != nil {
		return nil
	}
	d := M{}
	_ = json.Unmarshal([]byte(details), &d)
	return M{"id": identity, "time": t, "level": level, "action": action, "status": status, "message": message, "details": d}
}
func (a *App) maintenance() {
	timer := time.NewTicker(time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
			a.cleanupLatencySize()
			_ = a.pruneLogs(true)

		}
	}
}
func (a *App) publicNative() M {
	nodes, subs := []M{}, []M{}
	for _, v := range arr(a.native["nodes"]) {
		n := obj(v)
		row := M{}
		for _, k := range []string{"id", "name", "enabled", "priority", "subscription", "created", "updated"} {
			row[k] = n[k]
		}
		row["protocol"] = obj(n["outbound"])["type"]
		nodes = append(nodes, row)
	}
	for _, v := range arr(a.native["subscriptions"]) {
		s := obj(v)
		row := M{}
		for _, k := range []string{"id", "name", "auto_update", "interval", "created", "updated", "error"} {
			row[k] = s[k]
		}
		row["enabled"] = s["enabled"] != false
		row["created"] = first(s["created"], s["created_at"])
		count := 0
		for _, n := range nodes {
			if n["subscription"] == s["id"] {
				count++
			}
		}
		row["count"] = count
		subs = append(subs, row)
	}
	return M{"nodes": nodes, "subscriptions": subs}
}
func (a *App) state() M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	native := a.publicNative()
	nodes := []M{}
	scores := a.scores()
	connections := a.connections.counts()
	lookup := map[string]M{}
	for _, s := range scores {
		lookup[str(s["id"])] = s
	}
	for _, v := range arr(a.cfg["endpoints"]) {
		e := obj(v)
		identity := str(e["id"])
		if identity == "warp" && !yes(a.components["warp"]) && a.active != identity || identity != "warp" && !yes(a.components["xray"]) && a.active != identity {
			continue
		}
		enabled := yes(e["enabled"]) && (identity != "warp" || !a.warpDisabled)
		m := clone(a.results[identity])
		if m == nil {
			m = M{}
		}
		status := "待检测"
		if !enabled {
			status = "停用"
		} else if yes(m["ok"]) {
			status = "可用"
		} else if m["ok"] != nil {
			status = "不可用"
		}
		name, protocol, subscription := str(e["name"]), str(e["kind"]), any(nil)
		geo := M{}
		for _, n := range arr(a.native["nodes"]) {
			n := obj(n)
			if n["id"] == identity {
				name = str(n["name"])
				protocol = str(obj(n["outbound"])["type"])
				geo = a.entryGeo(str(obj(n["outbound"])["server"]))
				subscription = n["subscription"]
			}
		}
		metrics := M{"latency_ms": m["latency"], "checked_at": m["time"], "bandwidth_kib_s": first(m["last_speed"], m["speed"]), "bandwidth_at": first(m["last_speed_time"], m["speed_time"]), "request_success_pct": lookup[identity]["success_pct"], "latency_average_ms": lookup[identity]["latency_average_ms"], "latency_average_samples": lookup[identity]["latency_average_samples"], "latency_average_window_minutes": lookup[identity]["latency_average_window_minutes"], "latency_average_min_samples": lookup[identity]["latency_average_min_samples"]}
		if a.connections != nil {
			module := "xray"
			if identity == "warp" {
				module = "warp"
			}
			summary := connections["module:"+module]
			if yes(summary["available"]) && !yes(summary["partial"]) && !yes(obj(summary["node_unavailable"])[identity]) {
				metrics["active_connections"] = num(connections[identity]["count"])
				metrics["active_connections_tcp"] = num(obj(connections[identity]["protocols"])["tcp"])
				metrics["active_connections_udp"] = num(obj(connections[identity]["protocols"])["udp"])
				metrics["connections_at"] = summary["time"]
			}
			metrics["connections_note"] = summary["note"]
		}
		for _, k := range []string{"icmp_success_pct", "icmp_average_ms", "packet_loss_at", "packet_loss_status", "packet_loss_pct", "packet_loss_ttl", "latency_probe_count", "latency_probe_successes", "latency_probe_samples"} {
			metrics[k] = m[k]
		}
		nodes = append(nodes, M{"id": identity, "name": name, "protocol": protocol, "source": map[bool]string{true: "Xray-core", false: "WARP"}[identity != "warp"], "native": identity != "warp", "enabled": enabled, "priority": e["priority"], "subscription": subscription, "active": a.active == identity, "status": status, "metrics": metrics, "geo": geo, "score": lookup[identity]})
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return num(obj(nodes[i]["score"])["score"]) > num(obj(nodes[j]["score"])["score"])
	})
	fallback := "direct"
	if yes(a.components["warp"]) && !yes(a.settings["proxy_paused"]) {
		fallback = "warp"
	}
	return M{"monitoring_items": a.monitorItemsSnapshot(), "connection_monitor": M{"xray": connections["module:xray"], "warp": connections["module:warp"], "wireguard": connections["module:wireguard"]}, "version": Version, "backend": "Go", "frontend": "TypeScript/React", "read_only": a.readonly, "config": a.publicConfigLocked(), "nodes": nodes, "native": native, "scores": scores, "state": M{"active": a.active, "applied": a.applied, "last_switch": a.lastSwitch, "active_since": a.activeSince, "recovery_issue": a.recoveryIssue, "history": clone(a.history)}, "components": clone(a.components), "manager_settings": clone(a.settings), "icmp_policy": clone(a.icmp), "proxy": M{"enabled": a.applied, "host_exit": map[bool]string{true: a.active, false: fallback}[a.applied && yes(a.cfg["capture_host"])], "lan_exit": map[bool]string{true: a.active, false: fallback}[a.applied && yes(a.cfg["capture_lan"])], "route_profile": a.cfg["route_profile"]}}
}
func first(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

func (a *App) Start() {
	a.started.Do(func() {
		a.refreshSnapshot()
		a.traffic.start()
		a.host.start()
		a.connections.start()
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-a.ctx.Done():
					return
				case <-t.C:
					a.refreshSnapshot()
				}
			}
		}()
		if !a.readonly {
			go a.periodic()
			go a.icmpAutomatic()
			go a.wgMonitorLoop()
			go a.warpAutomatic()
			go a.warpMonitorLoop()
			go a.ruleSetLoop()
			go a.versionCheckLoop()
			go a.geoAutomatic()
			go a.maintenance()
			go func() {
				if e := a.network.reconcile(); e != nil {
					a.audit("network.reconcile", "持久网络规则未全部恢复", false, nil)
				}
			}()
		}
	})
}
func (a *App) refreshSnapshot() {
	b, e := json.Marshal(a.state())
	if e == nil {
		a.stateJSON.Store(b)
	}
}
