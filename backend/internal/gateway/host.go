package gateway

import (
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HostMonitor struct {
	app                       *App
	mu                        sync.Mutex
	current, previous, policy M
	window                    []M
	lastFlush                 time.Time
	done                      chan struct{}
	wake                      chan struct{}
	started                   bool
}

func NewHostMonitor(a *App) (*HostMonitor, error) {
	if _, e := a.histdb.Exec("CREATE TABLE IF NOT EXISTS host_samples(t REAL PRIMARY KEY,payload TEXT)"); e != nil {
		return nil, e
	}
	policy := M{"enabled": true, "interval": 2, "storage_interval": 60, "retention_days": 30, "default_interface": "physical", "default_disk": "physical", "default_load": "load1", "default_cpu": "cpu_percent", "default_memory": "memory_percent", "default_partition": "/", "max_storage_mb": 256}
	_ = readJSON(filepath.Join(a.base, "host-monitor-policy.json"), &policy)
	return &HostMonitor{app: a, current: M{"supported": false, "status": "等待采样"}, policy: policy, lastFlush: time.Now(), done: make(chan struct{}), wake: make(chan struct{}, 1)}, nil
}
func hostRates(previous, current M, elapsed float64) {
	if previous == nil || elapsed <= 0 || elapsed > 600 {
		return
	}
	total := num(current["_cpu_total"]) - num(previous["_cpu_total"])
	idle := num(current["_cpu_idle"]) - num(previous["_cpu_idle"])
	if total > 0 && idle >= 0 {
		current["cpu_percent"] = max(0., min(100., 100*(total-idle)/total))
		for _, key := range []string{"hi", "si", "st"} {
			if current["_cpu_"+key] != nil && previous["_cpu_"+key] != nil {
				delta := num(current["_cpu_"+key]) - num(previous["_cpu_"+key])
				if delta >= 0 {
					current["cpu_"+key] = min(100., 100*delta/total)
				}
			}
		}
	}
	for _, kind := range []string{"interfaces", "disks"} {
		old := map[string]M{}
		for _, v := range arr(previous[kind]) {
			r := obj(v)
			old[str(r["name"])] = r
		}
		for _, v := range arr(current[kind]) {
			r := obj(v)
			before := old[str(r["name"])]
			if before == nil {
				continue
			}
			fields := map[string]string{"receive_total": "download_bps", "send_total": "upload_bps"}
			if kind == "disks" {
				fields = map[string]string{"read_total": "read_bps", "write_total": "write_bps", "read_ops": "read_iops", "write_ops": "write_iops", "busy_ms": "busy_percent"}
			}
			if kind == "disks" && r["io_time_ms"] != nil && before["io_time_ms"] != nil {
				reads := num(r["read_ops"]) - num(before["read_ops"])
				writes := num(r["write_ops"]) - num(before["write_ops"])
				millis := num(r["io_time_ms"]) - num(before["io_time_ms"])
				if reads >= 0 && writes >= 0 && millis >= 0 {
					r["total_iops"] = (reads + writes) / elapsed
					if reads+writes > 0 {
						r["await_ms"] = millis / (reads + writes)
					}
				}
			}
			for input, output := range fields {
				delta := num(r[input]) - num(before[input])
				if delta < 0 {
					continue
				}
				value := delta / elapsed
				if output == "busy_percent" {
					value = min(100., value/10)
				}
				r[output] = value
			}
		}
	}
}
func hostAggregate(window []M) M {
	latest := clone(window[len(window)-1])
	for _, key := range []string{"cpu_percent", "cpu_hi", "cpu_si", "cpu_st", "memory_percent", "memory_used", "memory_free", "memory_shared", "memory_buff_cache", "memory_available", "load1", "load5", "load15"} {
		sum, count := 0., 0.
		for _, s := range window {
			if s[key] != nil {
				sum += num(s[key])
				count++
			}
		}
		if count > 0 {
			latest[key] = sum / count
		}
	}
	for _, kind := range []string{"interfaces", "disks"} {
		for _, v := range arr(latest[kind]) {
			row := obj(v)
			if kind == "disks" {
				weighted, ops := 0., 0.
				for _, sample := range window {
					for _, x := range arr(sample[kind]) {
						r := obj(x)
						if r["name"] == row["name"] && r["await_ms"] != nil && num(r["total_iops"]) > 0 {
							weighted += num(r["await_ms"]) * num(r["total_iops"])
							ops += num(r["total_iops"])
						}
					}
				}
				row["await_ms"] = nil
				if ops > 0 {
					row["await_ms"] = weighted / ops
				}
			}
			for _, key := range []string{"upload_bps", "download_bps", "read_bps", "write_bps", "read_iops", "write_iops", "total_iops", "busy_percent"} {
				sum, count := 0., 0.
				for _, sample := range window {
					for _, x := range arr(sample[kind]) {
						r := obj(x)
						if r["name"] == row["name"] && r[key] != nil {
							sum += num(r[key])
							count++
						}
					}
				}
				if count > 0 {
					row[key] = sum / count
				}
			}
		}
	}
	delete(latest, "_cpu_total")
	delete(latest, "_cpu_idle")
	for _, key := range []string{"hi", "si", "st"} {
		delete(latest, "_cpu_"+key)
	}
	return latest
}
func (h *HostMonitor) collect() {
	if !h.app.monitorModuleEnabled("host") {
		return
	}
	interfaces, _ := h.app.interfaceCounters()
	raw, e := hostRaw(interfaces)
	h.mu.Lock()
	defer h.mu.Unlock()
	if e != nil {
		h.current = M{"supported": false, "status": "主机指标暂不可读取", "time": now()}
		return
	}
	elapsed := num(raw["time"]) - num(h.previous["time"])
	hostRates(h.previous, raw, elapsed)
	h.previous = clone(raw)
	delete(raw, "_cpu_total")
	delete(raw, "_cpu_idle")
	for _, key := range []string{"hi", "si", "st"} {
		delete(raw, "_cpu_"+key)
	}
	h.current = raw
	h.window = append(h.window, clone(raw))
	if len(h.window) > 1800 {
		h.window = h.window[len(h.window)-1800:]
	}
	if time.Since(h.lastFlush) >= time.Duration(num(h.policy["storage_interval"]))*time.Second {
		_ = h.flushLocked()
		h.lastFlush = time.Now()
	}
}
func (h *HostMonitor) flushLocked() error {
	if len(h.window) == 0 {
		return nil
	}
	sample := hostAggregate(h.window)
	b, e := json.Marshal(sample)
	if e != nil {
		return e
	}
	if _, e = h.app.histdb.Exec("INSERT OR REPLACE INTO host_samples(t,payload) VALUES(?,?)", sample["time"], string(b)); e != nil {
		return e
	}
	var size, count int64
	_ = h.app.histdb.QueryRow("SELECT COALESCE(SUM(length(payload)+64),0),COUNT(*) FROM host_samples").Scan(&size, &count)
	budget := num(h.app.monitorDataPolicy("host", "metrics")["max_storage_mb"]) * 1024 * 1024
	if float64(size) > budget && count > 0 {
		drop := min(count, int64(float64(count)*(float64(size)-budget)/float64(size))+1)
		_, _ = h.app.histdb.Exec("DELETE FROM host_samples WHERE rowid IN (SELECT rowid FROM host_samples ORDER BY t LIMIT ?)", drop)
	}
	h.window = nil
	_, _ = h.app.histdb.Exec("DELETE FROM host_samples WHERE t<?", now()-num(h.app.monitorDataPolicy("host", "metrics")["retention_days"])*86400)
	return nil
}
func (h *HostMonitor) start() {
	h.mu.Lock()
	h.started = true
	h.mu.Unlock()
	go func() {
		defer close(h.done)
		for {
			h.mu.Lock()
			enabled, interval := yes(h.policy["enabled"]), num(h.app.monitorDataPolicy("host", "metrics")["interval"])
			h.mu.Unlock()
			if enabled {
				h.collect()
			}
			timer := time.NewTimer(time.Duration(max(1., interval)) * time.Second)
			select {
			case <-h.app.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			case <-h.wake:
				timer.Stop()
			}
		}
	}()
}
func (h *HostMonitor) close() {
	h.mu.Lock()
	started := h.started
	h.mu.Unlock()
	if started {
		<-h.done
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = h.flushLocked()
}
func (h *HostMonitor) snapshot() M {
	h.mu.Lock()
	defer h.mu.Unlock()
	return M{"current": clone(h.current), "policy": clone(h.policy), "effective_interval": num(h.app.monitorDataPolicy("host", "metrics")["interval"])}
}
func (h *HostMonitor) history(q url.Values) M {
	end := now()
	hours := 24.
	if v, e := strconv.ParseFloat(q.Get("hours"), 64); e == nil && v >= 1./60 && v <= 8760 {
		hours = v
	}
	start := end - hours*3600
	if v, e := strconv.ParseFloat(q.Get("start"), 64); e == nil && v > 0 && v < end {
		start = v
	}
	if v, e := strconv.ParseFloat(q.Get("end"), 64); e == nil && v > start && v <= end+60 {
		end = v
	}
	if end-start > 365*86400 {
		start = end - 365*86400
	}
	bucket := max(60., (end-start)/600)
	rows, e := h.app.histdb.Query("SELECT MIN(t),payload FROM host_samples WHERE t>=? AND t<=? GROUP BY CAST((t-?)/? AS INTEGER) ORDER BY t", start, end, start, bucket)
	items := []M{}
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var stamp float64
			var raw string
			if rows.Scan(&stamp, &raw) == nil {
				r := M{}
				if json.Unmarshal([]byte(raw), &r) == nil {
					r["time"] = stamp
					items = append(items, r)
				}
			}
		}
	}
	h.mu.Lock()
	current := clone(h.current)
	enabled := yes(h.policy["enabled"])
	h.mu.Unlock()
	stamp := num(current["time"])
	last := 0.
	if len(items) > 0 {
		last = num(items[len(items)-1]["time"])
	}
	if enabled && yes(current["supported"]) && stamp >= start && stamp <= end && stamp > last {
		current["live_tail"] = true
		items = append(items, current)
	}
	return M{"items": items, "start": start, "end": end, "bucket_seconds": bucket, "complete": e == nil}
}
func (h *HostMonitor) configure(input M) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	next := clone(h.policy)
	for k, v := range input {
		if _, known := next[k]; !known {
			return errors.New("未知主机监控设置")
		}
		next[k] = v
	}
	if _, e := integer(next["interval"], 1, 300); e != nil {
		return e
	}
	if _, e := integer(next["storage_interval"], 10, 3600); e != nil {
		return e
	}
	if num(next["storage_interval"]) < num(next["interval"]) {
		return errors.New("历史汇总间隔不能小于实时采样间隔")
	}
	if _, e := integer(next["retention_days"], 1, 365); e != nil {
		return e
	}
	for _, key := range []string{"default_interface", "default_disk"} {
		if v := str(next[key]); v != "physical" && v != "all" {
			if e := iface(v); e != nil {
				return e
			}
		}
	}
	for key, allowed := range map[string][]string{
		"default_load":   {"load1", "load5", "load15"},
		"default_cpu":    {"cpu_percent", "cpu_hi", "cpu_si", "cpu_st"},
		"default_memory": {"memory_percent", "memory_used", "memory_free", "memory_shared", "memory_buff_cache", "memory_available"},
	} {
		valid := false
		for _, option := range allowed {
			if str(next[key]) == option {
				valid = true
			}
		}
		if !valid {
			return errors.New("默认监控指标无效：" + key)
		}
	}
	if value := str(next["default_partition"]); !strings.HasPrefix(value, "/") || len(value) > 256 || strings.ContainsAny(value, "\n\r\x00") {
		return errors.New("默认分区必须为有效绝对挂载路径")
	}
	if _, ok := next["enabled"].(bool); !ok {
		return errors.New("监控开关无效")
	}
	if e := writeJSON(filepath.Join(h.app.base, "host-monitor-policy.json"), next); e != nil {
		return e
	}
	if input["interval"] != nil || input["retention_days"] != nil || input["max_storage_mb"] != nil {
		p := h.app.monitorDataPolicy("host", "metrics")
		for _, key := range []string{"interval", "retention_days", "max_storage_mb"} {
			if value, ok := input[key]; ok {
				p[key] = value
			}
		}
		p["independent"] = true
		if err := h.app.saveMonitorData("host", "metrics", p); err != nil {
			return err
		}
	}
	h.policy = next
	select {
	case h.wake <- struct{}{}:
	default:
	}
	return nil
}
