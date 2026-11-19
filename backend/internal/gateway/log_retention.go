package gateway

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
)

var logTypes = []string{"operations", "probes", "switches", "diagnostics"}

func defaultLogRetention() M {
	return M{"days_enabled": true, "retention_days": 30, "rows_enabled": false, "max_rows": 10000, "size_enabled": false, "max_bytes": 1000000000, "size_unit": "GB"}
}
func logScope(kind string) string {
	switch kind {
	case "probes":
		return "action LIKE 'probe.%'"
	case "switches":
		return "action LIKE 'switch.%'"
	case "diagnostics":
		return "action LIKE 'diagnostic.%'"
	default:
		return "action NOT LIKE 'probe.%' AND action NOT LIKE 'switch.%' AND action NOT LIKE 'diagnostic.%'"
	}
}
func (a *App) loadLogRetention() {
	a.logPolicies = M{}
	for _, kind := range logTypes {
		a.logPolicies[kind] = defaultLogRetention()
	}
	_ = readJSON(filepath.Join(a.base, "log-retention-policy.json"), &a.logPolicies)
	_, _ = a.logdb.Exec("CREATE INDEX IF NOT EXISTS operation_time ON operations(t);CREATE TABLE IF NOT EXISTS log_migrations(name TEXT PRIMARY KEY)")
	rows := []M{}
	_ = readJSON(filepath.Join(a.base, "switch-history.json"), &rows)
	marker := "switch-json-v2:" + fingerprint(rows)
	var migrated int
	_ = a.logdb.QueryRow("SELECT COUNT(*) FROM log_migrations WHERE name=?", marker).Scan(&migrated)
	if migrated != 0 {
		return
	}
	tx, err := a.logdb.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		if _, err = tx.Exec("INSERT OR IGNORE INTO operations VALUES(?,?,?,?,?,?,?)", "switch-"+fingerprint(row), row["time"], "info", "switch.result", row["status"], str(row["error"]), string(raw)); err != nil {
			return
		}
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO log_migrations VALUES(?)", marker); err == nil {
		_ = tx.Commit()
	}
}
func (a *App) logRetentionView() M {
	a.logPolicyMu.RLock()
	defer a.logPolicyMu.RUnlock()
	return clone(a.logPolicies)
}
func (a *App) saveLogRetention(kind string, p M) error {
	valid := false
	for _, v := range logTypes {
		valid = valid || v == kind
	}
	if !valid {
		return errors.New("日志类型无效")
	}
	if len(p) != 7 {
		return errors.New("日志策略字段不完整")
	}
	for _, key := range []string{"days_enabled", "rows_enabled", "size_enabled"} {
		if _, ok := p[key].(bool); !ok {
			return errors.New("保留条件必须为开关值")
		}
	}
	for key, bounds := range map[string][2]int{"retention_days": {1, 3650}, "max_rows": {1, 100000000}, "max_bytes": {1, 1000000000000}} {
		if _, err := integer(p[key], bounds[0], bounds[1]); err != nil {
			return errors.New("日志保留限制超出范围：" + key)
		}
	}
	if p["size_unit"] != "MB" && p["size_unit"] != "GB" {
		return errors.New("日志大小单位必须为MB或GB")
	}
	a.logPolicyMu.Lock()
	next := clone(a.logPolicies)
	next[kind] = clone(p)
	err := writeJSON(filepath.Join(a.base, "log-retention-policy.json"), next)
	if err == nil {
		a.logPolicies = next
	}
	a.logPolicyMu.Unlock()
	if err != nil {
		return err
	}
	return a.pruneLogs(true)
}
func (a *App) pruneLogs(force bool) error {
	if force {
		a.logPruneMu.Lock()
	} else if !a.logPruneMu.TryLock() {
		return nil
	}
	defer a.logPruneMu.Unlock()
	if !force && time.Since(a.lastLogPrune) < time.Minute {
		return nil
	}
	policies := a.logRetentionView()
	for _, kind := range logTypes {
		p := obj(policies[kind])
		scope := logScope(kind)
		if yes(p["days_enabled"]) {
			if _, err := a.logdb.Exec("DELETE FROM operations WHERE ("+scope+") AND t<?", now()-num(p["retention_days"])*86400); err != nil {
				return err
			}
		}
		if yes(p["rows_enabled"]) {
			if _, err := a.logdb.Exec("DELETE FROM operations WHERE ("+scope+") AND rowid NOT IN (SELECT rowid FROM operations WHERE "+scope+" ORDER BY t DESC,rowid DESC LIMIT ?)", p["max_rows"]); err != nil {
				return err
			}
		}
		if yes(p["size_enabled"]) {
			if _, err := a.logdb.Exec("DELETE FROM operations WHERE rowid IN (SELECT rid FROM (SELECT rowid rid,SUM(length(CAST(level||action||status||message||details AS BLOB))+64) OVER(ORDER BY t DESC,rowid DESC) used FROM operations WHERE "+scope+") WHERE used>?)", p["max_bytes"]); err != nil {
				return err
			}
		}
	}
	a.lastLogPrune = time.Now()
	return nil
}
func (a *App) switchLogs() M {
	result := []M{}
	rows, err := a.logdb.Query("SELECT t,status,message,details FROM operations WHERE action='switch.result' ORDER BY t DESC LIMIT 1000")
	if err != nil {
		return M{"items": result, "error": "切换日志读取失败"}
	}
	defer rows.Close()
	for rows.Next() {
		var stamp float64
		var status, message, raw string
		if rows.Scan(&stamp, &status, &message, &raw) == nil {
			row := M{}
			_ = json.Unmarshal([]byte(raw), &row)
			row["time"], row["status"], row["error"] = stamp, status, message
			result = append(result, row)
		}
	}
	return M{"items": result}
}
