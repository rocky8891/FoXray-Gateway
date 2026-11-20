package gateway

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func connectionRange(q url.Values) (float64, float64, bool) {
	end, hours := now(), 24.
	if n, e := strconv.ParseFloat(q.Get("hours"), 64); e == nil && n >= 1./60 && n <= 8760 {
		hours = n
	}
	start := end - hours*3600
	if n, e := strconv.ParseFloat(q.Get("start"), 64); e == nil && n > 0 {
		start = n
	}
	if n, e := strconv.ParseFloat(q.Get("end"), 64); e == nil && n > 0 {
		end = n
	}
	return start, end, end > start && end-start <= 365*86400
}

func (m *ConnectionMonitor) saveBusinessHistory(rows []M, stamp float64) {
	db := m.app.histdb
	if db == nil {
		return
	}
	var err error
	if !m.historyReady {
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS business_flows(id TEXT PRIMARY KEY,module TEXT,first REAL,last REAL,payload TEXT);
CREATE INDEX IF NOT EXISTS business_flow_time ON business_flows(module,last);
CREATE INDEX IF NOT EXISTS business_flow_last ON business_flows(last);
CREATE TABLE IF NOT EXISTS business_samples(t INTEGER,module TEXT,device TEXT,protocol TEXT,node TEXT,count INTEGER,upload REAL,download REAL,PRIMARY KEY(t,module,device,protocol,node));
CREATE INDEX IF NOT EXISTS business_sample_scope ON business_samples(module,t);
CREATE TABLE IF NOT EXISTS business_node_samples(node TEXT,t INTEGER,count INTEGER,PRIMARY KEY(node,t));`)
		if err != nil {
			m.setHistoryError("业务连接历史存储不可用")
			return
		}
		m.historyReady = true
	}
	if m.persisted == nil {
		m.persisted = map[string]string{}
	}
	if m.pendingGroups == nil {
		m.pendingGroups = map[string]M{}
	}
	groups := map[string]M{}

	for _, r := range rows {
		if yes(r["_deferred"]) || !proxyConnectionCounted(r) {
			continue
		}
		key := str(r["module"]) + "|" + str(r["interface"]) + "|" + str(r["protocol"]) + "|" + str(r["node_id"])
		g := groups[key]
		if g == nil {
			g = M{"module": r["module"], "device": r["interface"], "protocol": r["protocol"], "node": r["node_id"], "count": 0}
			groups[key] = g
		}
		if yes(r["active"]) {
			g["count"] = num(g["count"]) + 1
		}
		if r["upload_bps"] != nil {
			g["upload"] = num(g["upload"]) + num(r["upload_bps"])
		}
		if r["download_bps"] != nil {
			g["download"] = num(g["download"]) + num(r["download_bps"])
		}
	}
	for protocol, value := range obj(obj(m.summaries["host"])["protocols"]) {
		if yes(obj(m.summaries["host"])["_deferred"]) {
			continue
		}
		groups["host|"+protocol] = M{"module": "host", "device": "", "node": "", "protocol": protocol, "count": value, "upload": nil, "download": nil}
	}
	minute := int64(stamp/60) * 60
	// Each healthy module gets a zero bucket even when there are no flows.
	for module, summary := range m.summaries {
		if yes(obj(summary)["_deferred"]) {
			continue
		}
		hasFlows := false
		for _, g := range groups {
			if str(g["module"]) == module {
				hasFlows = true
				break
			}
		}
		if yes(obj(summary)["available"]) && !hasFlows {
			groups[module+"|empty"] = M{"module": module, "device": "", "protocol": "", "node": "", "count": 0, "upload": 0, "download": 0}
		}
	}
	for key, g := range groups {
		interval := int64(m.app.historyInterval(str(g["module"]), "connections"))
		bucket := int64(stamp) / interval * interval
		key = strconv.FormatInt(bucket, 10) + "|" + key
		g["_minute"] = bucket
		old := m.pendingGroups[key]
		if old != nil {
			for _, metric := range []string{"count", "upload", "download"} {
				if old[metric] != nil && (g[metric] == nil || num(old[metric]) > num(g[metric])) {
					g[metric] = old[metric]
				}
			}
		}
		m.pendingGroups[key] = g
	}
	if stamp-m.lastPersist < 5 {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		m.setHistoryError("业务连接历史写入失败")
		return
	}
	defer tx.Rollback()
	signatures := map[string]string{}
	for _, r := range rows {
		key := str(r["id"])
		signature := fmt.Sprint(r["last_packet"], "|", r["upload"], "|", r["download"], "|", r["state"], "|", r["active"], "|", r["domain"], "|", r["application"], "|", r["routing_classification"], "|", r["outbound_tag"], "|", r["proxy_counted"])
		signatures[key] = signature
		if m.persisted[key] == signature {
			continue
		}
		payload, _ := json.Marshal(publicBusinessRow(r))
		if _, err = tx.Exec(`INSERT INTO business_flows VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET first=MIN(first,excluded.first),last=excluded.last,payload=excluded.payload`, r["id"], r["module"], r["first_seen"], r["last_packet"], string(payload)); err != nil {
			break
		}
	}
	groups = m.pendingGroups
	for _, g := range groups {
		if err != nil {
			break
		}
		_, err = tx.Exec(`INSERT INTO business_samples VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(t,module,device,protocol,node) DO UPDATE SET count=MAX(count,excluded.count),upload=CASE WHEN upload IS NULL THEN excluded.upload WHEN excluded.upload IS NULL THEN upload ELSE MAX(upload,excluded.upload) END,download=CASE WHEN download IS NULL THEN excluded.download WHEN excluded.download IS NULL THEN download ELSE MAX(download,excluded.download) END`, g["_minute"], businessSampleModule(str(g["module"])), g["device"], g["protocol"], g["node"], g["count"], g["upload"], g["download"])
	}
	if err == nil && minute != m.lastBusinessCleanup {
		for _, module := range []string{"host", "xray", "warp", "wireguard"} {
			policy := m.app.monitorDataPolicy(module, "connections")
			cutoff := stamp - num(policy["retention_days"])*86400
			if _, err = tx.Exec("DELETE FROM business_flows WHERE module=? AND last<?", module, cutoff); err != nil {
				break
			}
			if _, err = tx.Exec("DELETE FROM business_samples WHERE module IN (?,?) AND t<?", module, businessSampleModule(module), cutoff); err != nil {
				break
			}
			var bytes, count int64
			err = tx.QueryRow("SELECT COALESCE(SUM(length(payload)+128),0),COUNT(*) FROM business_flows WHERE module=?", module).Scan(&bytes, &count)
			if err != nil {
				break
			}
			var samples int64
			err = tx.QueryRow("SELECT COUNT(*) FROM business_samples WHERE module IN (?,?)", module, businessSampleModule(module)).Scan(&samples)
			if err != nil {
				break
			}
			nodeScope := "0=1"
			if module != "host" {
				nodeScope = monitorScope(module)
			}
			if _, err = tx.Exec("DELETE FROM business_node_samples WHERE ("+nodeScope+") AND t<?", cutoff); err != nil {
				break
			}
			var nodeCount int64
			err = tx.QueryRow("SELECT COUNT(*) FROM business_node_samples WHERE " + nodeScope).Scan(&nodeCount)
			if err != nil {
				break
			}
			budget := int64(num(policy["max_storage_mb"]) * 1024 * 1024)
			excess := bytes + samples*160 + nodeCount*96 - budget
			if excess > 0 && count > 0 {
				drop := min(count, (excess*count/max(int64(1), bytes))+1)
				_, err = tx.Exec("DELETE FROM business_flows WHERE rowid IN (SELECT rowid FROM business_flows WHERE module=? ORDER BY last LIMIT ?)", module, drop)
				if err != nil {
					break
				}
				excess -= drop * bytes / count
			}
			if excess > 0 {
				drop := min(samples, excess/160+1)
				_, err = tx.Exec("DELETE FROM business_samples WHERE rowid IN (SELECT rowid FROM business_samples WHERE module IN (?,?) ORDER BY t LIMIT ?)", module, businessSampleModule(module), drop)
				if err != nil {
					break
				}
				excess -= drop * 160
			}
			if excess > 0 {
				_, err = tx.Exec("DELETE FROM business_node_samples WHERE rowid IN (SELECT rowid FROM business_node_samples WHERE "+nodeScope+" ORDER BY t LIMIT ?)", excess/96+1)
				if err != nil {
					break
				}
			}

		}
	}

	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		m.setHistoryError("业务连接历史写入失败")
	} else {
		m.setHistoryError("")
		m.lastPersist = stamp
		m.persisted = signatures
		m.pendingGroups = map[string]M{}
		m.lastBusinessCleanup = minute
	}
}

func connectionMatches(r M, q url.Values) bool {
	for key, field := range map[string]string{"module": "module", "protocol": "protocol", "interface": "interface", "node": "node_id"} {
		if v := q.Get(key); v != "" && v != "all" && str(r[field]) != v {
			return false
		}
	}
	search := strings.ToLower(q.Get("query"))
	return search == "" || strings.Contains(strings.ToLower(str(r["source"])+" "+str(r["destination"])+" "+str(r["node_name"])+" "+str(r["interface"])+" "+str(r["domain"])+" "+str(r["application"])), search)
}

func (m *ConnectionMonitor) query(q url.Values) M {
	start, end, valid := connectionRange(q)
	if !valid {
		return M{"items": []M{}, "error": "时间范围无效"}
	}
	if q.Get("scope") == "transport" {
		m.mu.RLock()
		defer m.mu.RUnlock()
		rows := []M{}
		for _, r := range m.transport {
			if connectionMatches(r, q) {
				rows = append(rows, clone(r))
			}
		}
		return M{"items": rows, "summaries": m.currentSummaries(), "time": m.at, "note": "仅用于隧道传输诊断；不代表业务连接，不提供传输历史。"}
	}
	if q.Get("scope") != "history" {
		snapshot := m.snapshot(q.Get("module"))
		rows := []M{}
		for _, r := range snapshot["items"].([]M) {
			if num(r["last_packet"]) >= start && num(r["first_seen"]) <= end && connectionMatches(r, q) {
				rows = append(rows, r)
			}
		}
		snapshot["items"] = rows
		return snapshot
	}
	rows := []M{}
	result, err := m.queryHistoricalFlows(q, start, end)
	truncated := false
	if err == nil {
		defer result.Close()
		for result.Next() {
			var raw string
			if result.Scan(&raw) == nil {
				var r M
				if jsonDecode(raw, &r) == nil && connectionMatches(r, q) {
					if len(rows) == 10000 {
						truncated = true
						break
					}
					r["state"] = "历史观测"
					rows = append(rows, r)
				}
			}
		}
		err = result.Err()
	}
	out := M{"items": rows, "start": start, "end": end, "truncated": truncated, "summaries": m.snapshot(q.Get("module"))["summaries"], "time": now(), "note": "按本模块保留策略保存的连接元数据，单次查询最多10000条；字节为该连接的观测累计值，包含所选区间外已累计部分。图表仅统计所选时间范围。"}
	if err != nil {
		out["error"] = "业务连接历史不可读取，等待首次成功采样"
	}
	return out
}

func (m *ConnectionMonitor) businessHistory(q url.Values) M {
	start, end, valid := connectionRange(q)
	if !valid {
		return M{"items": []M{}, "error": "时间范围无效"}
	}
	width := max(int(m.app.historyInterval(q.Get("module"), "connections")), int((end-start)/600))
	where, args := "module=? AND t>=? AND t<=?", []any{businessSampleModule(q.Get("module")), start, end}
	for key, column := range map[string]string{"protocol": "protocol", "interface": "device", "node": "node"} {
		if v := q.Get(key); v != "" && v != "all" {
			where += " AND " + column + "=?"
			args = append(args, v)
		}
	}
	args = append([]any{width, width}, args...)
	result, err := m.app.histdb.Query(`SELECT CAST(t/? AS INTEGER)*?,MAX(n),MAX(u),MAX(d) FROM (SELECT t,SUM(count) n,SUM(upload) u,SUM(download) d FROM business_samples WHERE `+where+` GROUP BY t) GROUP BY 1 ORDER BY 1`, args...)
	items := []M{}
	if err == nil {
		defer result.Close()
		for result.Next() {
			var t, n float64
			var u, d sql.NullFloat64
			if result.Scan(&t, &n, &u, &d) == nil {
				row := M{"time": t, "count": n, "upload": nil, "download": nil}
				if u.Valid {
					row["upload"] = u.Float64
				}
				if d.Valid {
					row["download"] = d.Float64
				}
				items = append(items, row)
			}
		}
		err = result.Err()
	}
	out := M{"items": items, "start": start, "end": end, "note": "业务连接曲线按分钟记录观测峰值，按本模块保留周期和大小清理；缺失采样不补零。"}
	if err != nil {
		out["error"] = "业务连接历史尚不可用，等待首次成功采样"
	}
	return out
}

func (m *ConnectionMonitor) setHistoryError(value string) {
	m.mu.Lock()
	m.historyError = value
	m.mu.Unlock()
}

func (m *ConnectionMonitor) queryHistoricalFlows(q url.Values, start, end float64) (*sql.Rows, error) {
	where := "module=? AND last>=? AND first<=?"
	args := []any{q.Get("module"), start, end}
	for key, field := range map[string]string{"protocol": "protocol", "interface": "interface", "node": "node_id"} {
		if value := q.Get(key); value != "" && value != "all" {
			where += " AND json_extract(payload,'$." + field + "')=?"
			args = append(args, value)
		}
	}
	return m.app.histdb.Query("SELECT payload FROM business_flows WHERE "+where+" ORDER BY last DESC LIMIT 10001", args...)
}
