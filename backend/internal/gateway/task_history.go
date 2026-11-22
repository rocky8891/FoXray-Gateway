package gateway

import (
	"encoding/json"
	"strings"
)

func isPeriodicNodeTask(source, group string) bool {
	return source == "automatic" && group == "probe.latency"
}

func (a *App) taskHistory(before string) M       { return a.queryTaskHistory(before, false) }
func (a *App) taskCenterHistory(before string) M { return a.queryTaskHistory(before, true) }
func (a *App) queryTaskHistory(before string, hidePeriodic bool) M {
	query := "SELECT id,t,action,status,message,details FROM operations WHERE json_valid(details) AND json_extract(details,'$.job_id') IS NOT NULL AND json_extract(details,'$.job_id')<>''"
	if hidePeriodic {
		query += " AND NOT (action='probe.latency' AND COALESCE(json_extract(details,'$.source'),'')='automatic')"
	}
	args := []any{}
	if before != "" {
		query += " AND (t<(SELECT t FROM operations WHERE id=?) OR (t=(SELECT t FROM operations WHERE id=?) AND id<?))"
		args = append(args, before, before, before)
	}
	query += " ORDER BY t DESC,id DESC LIMIT 50"
	rows, err := a.logdb.Query(query, args...)
	items := []M{}
	if err != nil {
		return M{"items": items, "error": "任务历史暂不可读取"}
	}
	defer rows.Close()
	for rows.Next() {
		var logID, group, status, message, raw string
		var stamp float64
		if rows.Scan(&logID, &stamp, &group, &status, &message, &raw) != nil {
			continue
		}
		d := M{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		title := str(d["title"])
		if title == "" {
			title = strings.SplitN(message, "：", 2)[0]
		}
		source := str(d["source"])
		if str(d["result_status"]) == "partial" {
			status = "partial"
		}
		if source != "manual" && source != "automatic" {
			source = "unknown"
		}
		items = append(items, M{"id": d["job_id"], "log_id": logID, "ui_action": d["ui_action"], "title": title, "group": group, "source": source, "status": status, "message": message, "started": first(d["started"], stamp), "finished": first(d["finished"], stamp), "details": arr(d["results"])})
	}
	if rows.Err() != nil {
		return M{"items": []M{}, "error": "任务历史读取失败"}
	}
	next := ""
	if len(items) == 50 {
		next = str(items[49]["log_id"])
	}
	return M{"items": items, "next": next}
}
