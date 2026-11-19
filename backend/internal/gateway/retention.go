package gateway

import (
	"errors"
	"path/filepath"
)

func monitorScope(module string) string {
	switch module {
	case "warp":
		return "node='warp'"
	case "wireguard":
		return "node LIKE 'wg:%'"
	default:
		return "node<>'warp' AND node NOT LIKE 'wg:%'"
	}
}
func monitorModule(s string) string {
	if s == "warp" || s == "wireguard" {
		return s
	}
	return "xray"
}
func (a *App) latencyRetention(module ...string) M {
	s := "xray"
	if len(module) > 0 {
		s = monitorModule(module[0])
	}
	a.mu.RLock()
	days := first(a.settings["measurement_"+s+"_days"], first(a.settings["measurement_retention_days"], 30))
	size := first(a.settings["measurement_"+s+"_mb"], 256)
	a.mu.RUnlock()
	resolved := a.monitorDataPolicy(s, "latency")
	days, size = resolved["retention_days"], resolved["max_storage_mb"]
	var count int
	_ = a.histdb.QueryRow("SELECT (SELECT COUNT(*) FROM samples WHERE " + monitorScope(s) + ")+(SELECT COUNT(*) FROM icmp_samples WHERE " + monitorScope(s) + ")").Scan(&count)
	return M{"retention_days": days, "max_storage_mb": size, "estimated_bytes": count * 256}
}
func (a *App) saveLatencyRetention(d M, module ...string) error {
	if len(d) != 2 {
		return errors.New("保留策略字段无效")
	}
	s := "xray"
	if len(module) > 0 {
		s = monitorModule(module[0])
	}
	days, e := integer(d["retention_days"], 1, 365)
	if e != nil {
		return e
	}
	size, e := integer(d["max_storage_mb"], 1, 10240)
	if e != nil {
		return e
	}
	a.mu.Lock()
	old := clone(a.settings)
	a.settings["measurement_"+s+"_days"], a.settings["measurement_"+s+"_mb"] = days, size
	e = writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings)
	if e != nil {
		a.settings = old
	}
	a.mu.Unlock()
	if e == nil {
		p := a.monitorDataPolicy(s, "latency")
		p["retention_days"], p["max_storage_mb"], p["independent"] = days, size, true
		if e = a.saveMonitorData(s, "latency", p); e != nil {
			return e
		}
		a.cleanupLatencySize()
	}
	return e
}
func (a *App) cleanupLatencySize() {
	for _, s := range []string{"xray", "warp", "wireguard"} {
		p := a.latencyRetention(s)
		scope := monitorScope(s)
		for _, table := range []string{"samples", "icmp_samples"} {
			_, _ = a.histdb.Exec("DELETE FROM "+table+" WHERE ("+scope+") AND t<?", now()-num(p["retention_days"])*86400)
		}
		var count int
		_ = a.histdb.QueryRow("SELECT (SELECT COUNT(*) FROM samples WHERE " + scope + ")+(SELECT COUNT(*) FROM icmp_samples WHERE " + scope + ")").Scan(&count)
		excess := count - int(num(p["max_storage_mb"])*1024*1024/256)
		if excess <= 0 {
			continue
		}
		limit := excess
		if limit > 10000 {
			limit = 10000
		}
		rows, e := a.histdb.Query("SELECT kind,id FROM (SELECT 0 kind,rowid id,t FROM samples WHERE "+scope+" UNION ALL SELECT 1 kind,rowid id,t FROM icmp_samples WHERE "+scope+") ORDER BY t LIMIT ?", limit)
		if e != nil {
			continue
		}
		type item struct{ kind, id int }
		items := []item{}
		for rows.Next() {
			var r item
			if rows.Scan(&r.kind, &r.id) == nil {
				items = append(items, r)
			}
		}
		_ = rows.Close()
		tx, e := a.histdb.Begin()
		if e != nil {
			continue
		}
		ok := true
		for _, r := range items {
			table := "samples"
			if r.kind == 1 {
				table = "icmp_samples"
			}
			if _, e = tx.Exec("DELETE FROM "+table+" WHERE rowid=?", r.id); e != nil {
				ok = false
				break
			}
		}
		if ok {
			_ = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
}
