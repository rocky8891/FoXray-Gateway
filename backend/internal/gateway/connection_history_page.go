package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

type connectionHistoryCursor struct {
	Last float64 `json:"last"`
	ID   string  `json:"id"`
}

func (m *ConnectionMonitor) historySummaries() M {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentSummaries()
}

func (m *ConnectionMonitor) historicalConnectionPage(ctx context.Context, q url.Values, start, end float64) M {
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		return M{"items": []M{}, "error": "分页大小必须为 1～100"}
	}
	where := "module=? AND last>=? AND first<=?"
	args := []any{q.Get("module"), start, end}
	for key, field := range map[string]string{"protocol": "protocol", "interface": "interface", "node": "node_id"} {
		if value := q.Get(key); value != "" && value != "all" {
			where += " AND json_extract(payload,'$." + field + "')=?"
			args = append(args, value)
		}
	}
	if query := strings.ToLower(strings.TrimSpace(q.Get("query"))); query != "" {
		where += " AND instr(lower(COALESCE(json_extract(payload,'$.source'),'') || ' ' || COALESCE(json_extract(payload,'$.destination'),'') || ' ' || COALESCE(json_extract(payload,'$.node_name'),'') || ' ' || COALESCE(json_extract(payload,'$.node_id'),'') || ' ' || COALESCE(json_extract(payload,'$.interface'),'') || ' ' || COALESCE(json_extract(payload,'$.domain'),'') || ' ' || COALESCE(json_extract(payload,'$.application'),'')), ?) > 0"
		args = append(args, query)
	}
	if encoded := q.Get("cursor"); encoded != "" {
		raw, err := base64.RawURLEncoding.DecodeString(encoded)
		var cursor connectionHistoryCursor
		if err != nil || len(raw) > 1024 || json.Unmarshal(raw, &cursor) != nil || cursor.ID == "" || cursor.Last <= 0 {
			return M{"items": []M{}, "error": "分页游标无效"}
		}
		where += " AND (last,id)<(?,?)"
		args = append(args, cursor.Last, cursor.ID)
	}
	ctx, cancel := context.WithTimeout(ctx, historyQueryTimeout)
	defer cancel()
	args = append(args, limit+1)
	rows, err := m.app.historyReader().QueryContext(ctx, "SELECT id,last,payload FROM business_flows WHERE "+where+" ORDER BY last DESC,id DESC LIMIT ?", args...)
	items := []M{}
	next := ""
	if err == nil {
		defer rows.Close()
		var last connectionHistoryCursor
		for rows.Next() {
			var cursor connectionHistoryCursor
			var raw string
			if err = rows.Scan(&cursor.ID, &cursor.Last, &raw); err != nil {
				break
			}
			if len(items) == limit {
				encoded, _ := json.Marshal(last)
				next = base64.RawURLEncoding.EncodeToString(encoded)
				break
			}
			var row M
			if json.Unmarshal([]byte(raw), &row) != nil {
				continue
			}
			summary := M{"id": cursor.ID, "state": "历史观测"}
			for _, key := range []string{"source", "destination", "protocol", "domain", "domain_note", "interface", "kind", "upload", "download", "upload_bps", "download_bps", "observed_seconds", "last_packet", "first_seen", "node_id", "node_name", "routing_classification", "routing_note"} {
				summary[key] = row[key]
			}
			items = append(items, summary)
			last = cursor
		}
		if err == nil {
			err = rows.Err()
		}
	}
	result := M{"items": items, "next_cursor": next, "start": start, "end": end, "summaries": m.historySummaries()}
	if err != nil {
		result["error"] = "连接历史查询超时或不可用，请缩小查询时间范围"
	}
	return result
}

func (m *ConnectionMonitor) historicalConnectionDetail(ctx context.Context, module, id string) M {
	ctx, cancel := context.WithTimeout(ctx, historyQueryTimeout)
	defer cancel()
	var raw string
	if id == "" || len(id) > 512 || m.app.historyReader().QueryRowContext(ctx, "SELECT payload FROM business_flows WHERE id=? AND module=?", id, module).Scan(&raw) != nil {
		return M{"error": "连接详情不可用或已清理"}
	}
	var row M
	if json.Unmarshal([]byte(raw), &row) != nil {
		return M{"error": "连接详情不可用"}
	}
	row["state"] = "历史观测"
	return M{"item": row}
}
