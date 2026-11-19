package gateway

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

func (a *App) historyIntervalsView(module string) M {
	a.dataMu.RLock()
	defer a.dataMu.RUnlock()
	values := M{}
	for _, kind := range []string{"traffic", "latency", "connections"} {
		values[kind] = a.historyIntervalLocked(module, kind)
	}
	return M{"intervals": values}
}

func (a *App) saveHistoryIntervals(module string, values M) error {
	if module != "xray" && module != "warp" && module != "wireguard" {
		return errors.New("历史记录服务无效")
	}
	if len(values) != 3 {
		return errors.New("请提供流量、延迟和活动连接的历史记录间隔")
	}
	for _, kind := range []string{"traffic", "latency", "connections"} {
		if _, err := integer(values[kind], 1, 3600); err != nil {
			return errors.New("历史记录间隔（秒）允许范围：1～3600，且必须为整数")
		}
	}
	a.dataMu.Lock()
	defer a.dataMu.Unlock()
	next := clone(a.monitorData)
	history := obj(next["history_intervals"])
	for kind, value := range values {
		history[module+":"+kind] = value
	}
	next["history_intervals"] = history
	if err := writeJSON(filepath.Join(a.base, "monitor-data-policy.json"), next); err != nil {
		return err
	}
	a.monitorData = next
	return nil
}

func defaultHistoryInterval(kind string) float64 {
	if kind == "latency" {
		return 1
	}
	return 60
}
func (a *App) historyIntervalLocked(module, kind string) float64 {
	n := num(obj(a.monitorData["history_intervals"])[module+":"+kind])
	if n >= 1 && n <= 3600 {
		return n
	}
	return defaultHistoryInterval(kind)
}
func (a *App) historyInterval(module, kind string) float64 {
	a.dataMu.RLock()
	defer a.dataMu.RUnlock()
	return a.historyIntervalLocked(module, kind)
}
func (a *App) saveHistoryInterval(module, kind string, value any) error {
	if !validMonitorData(module, kind) || kind == "metrics" {
		return errors.New("历史记录类型无效")
	}
	return a.saveMonitorData(module, kind, M{"independent": a.monitorDataView(module, kind)["independent"], "storage_interval": value, "history_only": true})
}

// History throttling never schedules an active probe. Each protocol/node keeps
// its own timestamp and live measurements continue to update at their old rate.
func (a *App) storeMeasurement(node string, icmp bool, row M) error {
	table := "samples"
	stamp := num(row["time"])
	if icmp {
		table = "icmp_samples"
		stamp = num(row["packet_loss_at"])
	}
	if stamp <= 0 {
		return nil
	}
	module := "xray"
	if node == "warp" {
		module = "warp"
	} else if strings.HasPrefix(node, "wg:") {
		module = "wireguard"
	}
	if !a.monitorModuleEnabled(module) {
		return nil
	}
	interval := a.historyInterval(module, "latency")
	a.measurementMu.Lock()
	defer a.measurementMu.Unlock()
	var last sql.NullFloat64
	if err := a.histdb.QueryRow("SELECT MAX(t) FROM "+table+" WHERE node=?", node).Scan(&last); err != nil {
		return err
	}
	if last.Valid && stamp-last.Float64 < interval {
		return nil
	}
	if icmp {
		_, err := a.histdb.Exec("INSERT OR REPLACE INTO icmp_samples(node,t,latency,success,sent,received,status) VALUES(?,?,?,?,?,?,?)", node, stamp, first(row["icmp_average_ms"], -1), row["icmp_success_pct"], row["icmp_sent"], row["icmp_received"], row["packet_loss_status"])
		return err
	}
	_, err := a.histdb.Exec("INSERT OR REPLACE INTO samples(node,t,latency) VALUES(?,?,?)", node, stamp, row["latency"])
	return err
}
