package gateway

import (
	"context"
	"encoding/json"
	"errors"

	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var errTrafficUnavailable = errors.New("流量计数暂不可用")

const trafficMetricsAddress = "127.0.0.1:10086"

type trafficCounter struct {
	Up, Down  uint64
	Available bool
	Status    string
}
type trafficReading struct {
	InterfaceUp            uint64  `json:"interface_upload,omitempty"`
	InterfaceDown          uint64  `json:"interface_download,omitempty"`
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	Source                 string  `json:"source"`
	At                     float64 `json:"time"`
	Available              bool    `json:"available"`
	Status                 string  `json:"status"`
	UploadRate             float64 `json:"upload_bps"`
	DownloadRate           float64 `json:"download_bps"`
	TotalUp                uint64  `json:"total_upload"`
	TotalDown              uint64  `json:"total_download"`
	previous               trafficCounter
	previousAt             time.Time
	lastStoredAt           time.Time
	previousInterval       float64
	pendingUp, pendingDown uint64
	pendingSeconds         float64
}
type Traffic struct {
	lastCleanup    time.Time
	historyPolicy  M
	modulePolicies M
	wgNames        []string
	wgNamesAt      time.Time
	app            *App
	mu             sync.Mutex
	rows           map[string]*trafficReading
	client         *http.Client
	started        bool
	policy         M
	wake           chan struct{}
	storageBytes   int64
	storageMethod  string
	storageChecked float64
	cleanupError   string
	done           chan struct{}
}

func NewTraffic(a *App) (*Traffic, error) {
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS traffic_samples(node TEXT,t REAL,up INTEGER,down INTEGER,seconds REAL,PRIMARY KEY(node,t))`, `CREATE INDEX IF NOT EXISTS traffic_history_cover ON traffic_samples(t,node,up,down,seconds)`, `CREATE TABLE IF NOT EXISTS traffic_totals(node TEXT PRIMARY KEY,name TEXT,source TEXT,up INTEGER,down INTEGER)`} {
		if _, e := a.histdb.Exec(q); e != nil {
			return nil, e
		}
	}
	t := &Traffic{app: a, rows: map[string]*trafficReading{}, done: make(chan struct{}), client: &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConns: 2, MaxIdleConnsPerHost: 2}}}
	t.policy, t.wake = defaultTrafficPolicy(), make(chan struct{}, 1)
	t.historyPolicy = M{}
	_ = readJSON(filepath.Join(a.base, "traffic-history-policy.json"), &t.historyPolicy)
	t.modulePolicies = M{}
	_ = readJSON(filepath.Join(a.base, "traffic-monitor-modules.json"), &t.modulePolicies)
	if days := num(a.settings["measurement_retention_days"]); days >= 1 && days <= 365 {
		t.policy["retention_days"] = days
	}
	_ = readJSON(filepath.Join(a.base, "traffic-monitor-policy.json"), &t.policy)
	if validateTrafficPolicy(t.policy) != nil {
		t.policy = defaultTrafficPolicy()
	}
	if t.modulePolicies == nil {
		t.modulePolicies = M{}
	}
	for _, module := range []string{"xray", "warp", "wireguard"} {
		if p := obj(t.modulePolicies[module]); len(p) > 0 && validateTrafficPolicy(p) != nil {
			delete(t.modulePolicies, module)
		}
	}
	t.recomputeIntervalLocked()
	t.storageBytes, t.storageMethod, _ = t.storageLocked()
	t.storageChecked = now()
	rows, e := a.histdb.Query("SELECT node,name,source,up,down FROM traffic_totals")
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			r := &trafficReading{}
			if rows.Scan(&r.ID, &r.Name, &r.Source, &r.TotalUp, &r.TotalDown) == nil {
				r.Status = "等待采样"
				t.rows[r.ID] = r
			}
		}
	}
	return t, nil
}
func trafficDelta(previous, current uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}
func (t *Traffic) readXray(ctx context.Context) (map[string]trafficCounter, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+trafficMetricsAddress+"/debug/vars", nil)
	res, e := t.client.Do(req)
	if e != nil {
		return nil, errTrafficUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errTrafficUnavailable
	}
	raw, e := limitedRead(res.Body, 4*1024*1024)
	if e != nil {
		return nil, errTrafficUnavailable
	}
	var payload struct {
		Stats struct {
			Outbound map[string]struct {
				Up   json.Number `json:"uplink"`
				Down json.Number `json:"downlink"`
			} `json:"outbound"`
		} `json:"stats"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Stats.Outbound == nil {
		return nil, errTrafficUnavailable
	}
	counters := map[string]trafficCounter{}
	for tag, v := range payload.Stats.Outbound {
		if !strings.HasPrefix(tag, "out-") {
			continue
		}
		up, _ := strconv.ParseUint(v.Up.String(), 10, 64)
		down, _ := strconv.ParseUint(v.Down.String(), 10, 64)
		counters[strings.TrimPrefix(tag, "out-")] = trafficCounter{Up: up, Down: down, Available: true, Status: "Xray 出口统计"}
	}
	return counters, nil
}
func (t *Traffic) collect() {
	a := t.app
	a.mu.RLock()
	nodes := clone(arr(a.native["nodes"]))
	warp := yes(a.components["warp"])
	a.mu.RUnlock()
	policy := M{}
	_ = readJSON(filepath.Join(a.base, "private", "traffic-policy.json"), &policy)
	counters := map[string]trafficCounter{}
	e := errTrafficUnavailable
	if yes(policy["enabled"]) && a.monitorModuleEnabled("xray") {
		counters, e = t.readXray(a.ctx)
		if counters == nil {
			counters = map[string]trafficCounter{}
		}
	}
	stamp := time.Now()
	meta := map[string]M{}
	for _, v := range nodes {
		n := obj(v)
		identity := str(n["id"])
		meta[identity] = M{"name": n["name"], "source": "Xray"}
		if _, ok := counters[identity]; !ok {
			if counters == nil {
				counters = map[string]trafficCounter{}
			}
			counters[identity] = trafficCounter{Available: e == nil, Status: map[bool]string{true: "暂无传输", false: "核心未启用出口统计或服务不可达"}[e == nil]}
		}
	}
	if warp && a.monitorModuleEnabled("warp") {
		meta["warp"] = M{"name": "Cloudflare WARP", "source": "WARP"}
		c, err := warpTrafficCounter()
		if err != nil {
			c.Status = "WARP 隧道接口不可用"
		}
		counters["warp"] = c
	}
	t.wireguardCounters(counters, meta)
	t.mu.Lock()
	defer t.mu.Unlock()
	for identity, m := range meta {
		current := counters[identity]
		row := t.rows[identity]
		if row == nil {
			row = &trafficReading{ID: identity, lastStoredAt: stamp}
			t.rows[identity] = row
		}
		module := "xray"
		if identity == "warp" {
			module = "warp"
		} else if strings.HasPrefix(identity, "wg:") {
			module = "wireguard"
		}
		moduleInterval := num(t.modulePolicyLocked(module)["interval"])
		if !row.previousAt.IsZero() && stamp.Sub(row.previousAt).Seconds() < moduleInterval && current.Available == row.previous.Available {
			continue
		}
		if row.lastStoredAt.IsZero() {
			row.lastStoredAt = stamp
		}
		row.Name, row.Source = str(m["name"]), str(m["source"])
		row.At = float64(stamp.UnixMilli()) / 1000
		row.Status, row.Available = current.Status, current.Available
		if module == "wireguard" {
			row.InterfaceUp = current.Up
			row.InterfaceDown = current.Down
		}
		row.UploadRate, row.DownloadRate = 0, 0
		if current.Available && !row.previousAt.IsZero() && row.previous.Available {
			elapsed := stamp.Sub(row.previousAt).Seconds()
			if elapsed > 0 && elapsed < max(30., max(moduleInterval, row.previousInterval)*3) {
				up, down := trafficDelta(row.previous.Up, current.Up), trafficDelta(row.previous.Down, current.Down)
				row.UploadRate, row.DownloadRate = float64(up)/elapsed, float64(down)/elapsed
				row.TotalUp += up
				row.TotalDown += down
				row.pendingUp += up
				row.pendingDown += down
				row.pendingSeconds += elapsed
			}
		}
		row.previous, row.previousAt = current, stamp
		row.previousInterval = moduleInterval
	}
	_ = t.flushDueLocked(stamp, false)
}
func (t *Traffic) flushLocked() error { return t.flushDueLocked(time.Now(), true) }
func (t *Traffic) flushDueLocked(at time.Time, force bool) error {
	due := []*trafficReading{}
	for _, r := range t.rows {
		if force || r.pendingSeconds > 0 && at.Sub(r.lastStoredAt).Seconds() >= t.historyIntervalLocked(trafficModule(r.ID)) {
			due = append(due, r)
		}
	}
	if len(due) == 0 {
		return nil
	}
	tx, e := t.app.histdb.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	stamp := now()
	for _, r := range due {
		if r.pendingSeconds > 0 {
			if _, e = tx.Exec("INSERT OR REPLACE INTO traffic_samples(node,t,up,down,seconds) VALUES(?,?,?,?,?)", r.ID, stamp, r.pendingUp, r.pendingDown, r.pendingSeconds); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("INSERT OR REPLACE INTO traffic_totals(node,name,source,up,down) VALUES(?,?,?,?,?)", r.ID, r.Name, r.Source, r.TotalUp, r.TotalDown); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	for _, r := range due {
		r.pendingUp, r.pendingDown, r.pendingSeconds = 0, 0, 0
		r.lastStoredAt = at
	}
	t.cleanupError = ""
	if force || at.Sub(t.lastCleanup) >= time.Minute {
		if err := t.pruneLocked(); err != nil {
			t.cleanupError = err.Error()
		} else {
			t.lastCleanup = at
		}
	}
	return nil
}
func (t *Traffic) run() {
	for {
		t.collect()
		t.mu.Lock()
		interval := t.intervalLocked()
		t.mu.Unlock()
		timer := time.NewTimer(interval)
		select {
		case <-t.app.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-t.wake:
			timer.Stop()
		}
	}
}
func (t *Traffic) snapshot() M {
	t.mu.Lock()
	defer t.mu.Unlock()
	rows := []trafficReading{}
	for _, r := range t.rows {
		copy := *r
		if now()-copy.At > max(8., num(t.policy["interval"])*3) {
			copy.Available = false
			copy.UploadRate, copy.DownloadRate = 0, 0
			copy.Status = "未继续采样或出口已移除"
		}
		rows = append(rows, copy)
	}
	enabled := M{}
	_ = readJSON(filepath.Join(t.app.base, "private", "traffic-policy.json"), &enabled)
	return M{"items": rows, "interval": num(t.policy["interval"]), "storage_interval": t.historyIntervalLocked(""), "storage_intervals": clone(t.historyPolicy), "enabled": yes(enabled["enabled"]), "note": "Xray 为代理出口字节，WARP 为命名空间内隧道 IP 字节；包含该出口内的探测流量，不作为运营商计费依据。历史按各模块的记录间隔汇总，查询时按范围合并；可在监控设置调整记录间隔。"}
}
func (t *Traffic) history(q url.Values) M { return t.historyContext(context.Background(), q) }
func (t *Traffic) historyContext(ctx context.Context, q url.Values) M {
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
	if end-start > 365*86400 {
		start = end - 365*86400
	}
	t.mu.Lock()
	storageInterval := t.historyIntervalLocked(q.Get("module"))
	samplingInterval := t.samplingIntervalLocked(q.Get("module"))
	t.mu.Unlock()
	width := max(max(storageInterval, samplingInterval), (end-start)/600)
	// This controls display aggregation only. Saved samples and byte totals stay intact.
	if window, err := strconv.Atoi(q.Get("window")); err == nil && window >= 1 && window <= 365*86400 {
		width = max(width, float64(window))
	}
	args := []any{width, start, end}
	sql := "SELECT node,CAST(t/? AS INTEGER),MIN(t),SUM(up),SUM(down),SUM(seconds) FROM traffic_samples WHERE t>=? AND t<=?"
	if module := q.Get("module"); module != "" {
		sql += " AND (" + monitorScope(module) + ")"
	}
	if node := q.Get("node"); node != "" && node != "all" {
		sql += " AND node=?"
		args = append(args, node)
	}
	sql += " GROUP BY node,2 ORDER BY node,2"
	rows, e := t.app.histdb.QueryContext(ctx, sql, args...)
	items := []M{}
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var node string
			var bucket int
			var stamp, seconds float64
			var up, down uint64
			if e = rows.Scan(&node, &bucket, &stamp, &up, &down, &seconds); e != nil {
				break
			}
			uploadRate, downloadRate := 0., 0.
			if seconds > 0 {
				uploadRate, downloadRate = float64(up)/seconds, float64(down)/seconds
			}
			items = append(items, M{"id": node, "time": stamp, "bucket_time": float64(bucket) * width, "upload_bps": uploadRate, "download_bps": downloadRate, "upload": up, "download": down, "seconds": seconds})
		}
		if e == nil {
			e = rows.Err()
		}
		_ = rows.Close()
	}
	t.mu.Lock()
	for _, r := range t.rows {
		if !trafficModuleMatches(q.Get("module"), r.ID) || !r.Available || r.At < start || r.At > end || q.Get("node") != "" && q.Get("node") != "all" && q.Get("node") != r.ID {
			continue
		}
		items = append(items, M{"id": r.ID, "time": r.At, "upload_bps": r.UploadRate, "download_bps": r.DownloadRate, "upload": 0, "download": 0, "seconds": 0, "live_tail": true})
	}
	t.mu.Unlock()
	return M{"items": items, "start": start, "end": end, "bucket_seconds": width, "sampling_interval": samplingInterval, "storage_interval": storageInterval, "complete": e == nil}
}
func (a *App) enableTraffic(ctx context.Context, j *Job, enabled bool) error {
	a.mutate.Lock()
	defer a.mutate.Unlock()
	path := filepath.Join(a.base, "private", "traffic-policy.json")
	previous := M{}
	_ = readJSON(path, &previous)
	if enabled {
		engine := M{}
		_ = readJSON(filepath.Join(a.base, "engine.json"), &engine)
		if str(obj(engine["metrics"])["listen"]) != trafficMetricsAddress {
			if conn, e := net.DialTimeout("tcp", trafficMetricsAddress, 200*time.Millisecond); e == nil {
				conn.Close()
				return errors.New("本机统计端口已被其他程序占用，未修改核心")
			}
		}
	}
	if e := writeJSON(path, M{"enabled": enabled}); e != nil {
		return e
	}
	if !enabled {
		a.audit("traffic.disable", "已关闭 Xray 统计配置；现有核心将在下次正常更新时移除统计监听", true, nil, uiActionFromContext(ctx))
		return nil
	}
	if str(a.service("foxray-engine")["state"]) != "active" {
		a.audit("traffic.enable", "流量统计将在核心下次启动时生效", true, nil, uiActionFromContext(ctx))
		return nil
	}
	a.mu.RLock()
	cfg, native, active, applied := clone(a.cfg), clone(a.native), a.active, a.applied
	a.mu.RUnlock()
	a.stage(j, "启用本机出口统计；首次启用需要重载 Xray 核心")
	if e := a.startEngine(cfg, native, active); e != nil {
		_ = writeJSON(path, previous)
		_ = a.startEngine(cfg, native, active)
		if applied {
			_ = a.attach(cfg)
		}
		return e
	}
	if applied {
		if e := a.attach(cfg); e != nil {
			_ = writeJSON(path, previous)
			_ = a.startEngine(cfg, native, active)
			_ = a.attach(cfg)
			return e
		}
	}
	a.audit("traffic.enable", "Xray 出口统计已启用，监听仅限本机回环", true, M{"address": trafficMetricsAddress}, uiActionFromContext(ctx))
	return nil
}
func (t *Traffic) start() {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return
	}
	t.started = true
	t.mu.Unlock()
	go func() { defer close(t.done); t.run() }()
}
func (t *Traffic) close() {
	t.mu.Lock()
	started := t.started
	t.mu.Unlock()
	if started {
		<-t.done
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.flushLocked()
	t.client.CloseIdleConnections()
}
