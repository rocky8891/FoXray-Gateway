package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	urlpath "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func writeResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recover() != nil {
			writeResponse(w, 500, M{"error": "服务请求未完成，状态保持，请查看操作记录"})
		}
	}()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() && !ip.IsPrivate() {
		writeResponse(w, 403, M{"error": "仅允许受信任内网访问"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method == http.MethodGet {
			a.getAPI(w, r)
			return
		}
		if r.Method != http.MethodPost {
			writeResponse(w, 405, M{"error": "方法不支持"})
			return
		}
		previewOnly := r.URL.Path == "/api/network/plan" || r.URL.Path == "/api/network/wireguard/keys" || r.URL.Path == "/api/nodes/import/preview"
		if a.readonly && !previewOnly {
			writeResponse(w, 409, M{"error": "当前为只读验证实例，未执行变更"})
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host {
				writeResponse(w, 403, M{"error": "来源校验失败"})
				return
			}
		}
		requestHost := r.Host
		if h, _, e := net.SplitHostPort(r.Host); e == nil {
			requestHost = h
		}
		allowed := requestHost == "127.0.0.1" || requestHost == "localhost" || requestHost == "::1"
		if local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			localHost, _, err := net.SplitHostPort(local.String())
			requestIP, localIP := net.ParseIP(requestHost), net.ParseIP(localHost)
			allowed = allowed || err == nil && requestIP != nil && localIP != nil && !localIP.IsUnspecified() && requestIP.Equal(localIP)
		}
		for _, h := range strings.Split(os.Getenv("FOXRAY_ALLOWED_HOSTS"), ",") {
			allowed = allowed || h != "" && h == requestHost
		}
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !allowed || media != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeResponse(w, 403, M{"error": "来源或内容类型无效"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024*1024)
		var d M
		decoder := json.NewDecoder(r.Body)
		if decoder.Decode(&d) != nil || d == nil {
			writeResponse(w, 400, M{"error": "JSON 请求无效"})
			return
		}
		result, err := a.postAPI(r, d)
		if err != nil {
			writeResponse(w, 400, M{"error": err.Error()})
			return
		}
		if r.URL.Path != "/api/manager/service" {
			a.maintainControlState()
		}
		writeResponse(w, 200, result)
		return
	}
	path := urlpath.Clean(r.URL.Path)
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/api") {
		writeResponse(w, 404, M{"error": "不存在"})
		return
	}
	if path == "/。" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	target := filepath.Join(a.ui, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	if !regular(target) {
		if filepath.Ext(path) != "" || strings.HasPrefix(path, "/assets/") {
			writeResponse(w, http.StatusNotFound, M{"error": "静态资源不存在，请刷新页面"})
			return
		}
		target = filepath.Join(a.ui, "index.html")
	}
	if !regular(target) {
		writeResponse(w, 503, M{"error": "前端资源未安装"})
		return
	}
	if strings.HasPrefix(path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// ZIP timestamps may predate the browser's cached release. Always send
		// the current entry document rather than allowing a false 304 response.
		w.Header().Set("Cache-Control", "no-store")
		r = r.Clone(r.Context())
		r.Header.Del("If-Modified-Since")
		r.Header.Del("If-None-Match")
	}
	http.ServeFile(w, r, target)
}
func (a *App) getAPI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch path := r.URL.Path; {
	case path == "/api/health":
		writeResponse(w, 200, M{"ok": true, "version": Version, "backend": "Go"})
	case path == "/api/modules":
		writeResponse(w, 200, a.moduleInventory())
	case path == "/api/migration/check" && a.readonly:
		writeResponse(w, 200, a.migrationCheck())
	case path == "/api/state":
		if b, ok := a.stateJSON.Load().([]byte); ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(b)
		} else {
			writeResponse(w, 200, a.state())
		}
	case path == "/api/tasks":
		a.jobs.mu.RLock()
		items := []M{}
		for _, j := range a.jobs.items {
			items = append(items, M{"id": j.ID, "title": j.Title, "group": j.Group, "source": j.Source, "status": j.Status, "message": j.Message, "started": j.Started, "finished": j.Finished, "details": append([]string{}, j.Details...)})
		}
		a.jobs.mu.RUnlock()
		writeResponse(w, 200, M{"tasks": items})
	case path == "/api/tasks/history":
		writeResponse(w, 200, a.taskCenterHistory(q.Get("before")))
	case path == "/api/operations" || path == "/api/detection-logs":
		kind := ""
		if path == "/api/detection-logs" {
			kind = "probe."
		}
		writeResponse(w, 200, a.operations(q.Get("before"), kind))
	case strings.HasPrefix(path, "/api/operations/"):
		row := a.operation(strings.TrimPrefix(path, "/api/operations/"))
		if row == nil {
			writeResponse(w, 404, M{"error": "记录不存在或已清理"})
		} else {
			writeResponse(w, 200, row)
		}
	case path == "/api/latency-history":
		writeResponse(w, 200, a.latencyHistory(q))
	case path == "/api/switches":
		writeResponse(w, 200, a.switchLogs())
	case path == "/api/updates":
		writeResponse(w, 200, a.updatesSnapshot())
	case path == "/api/settings":
		a.mu.RLock()
		settings := clone(a.settings)
		a.mu.RUnlock()
		writeResponse(w, 200, M{"settings": settings, "version": Version, "update": a.updateStatus()})
	case path == "/api/icmp-policy":
		a.mu.RLock()
		policy := clone(a.icmp)
		a.mu.RUnlock()
		writeResponse(w, 200, policy)
	case path == "/api/manager/service":
		writeResponse(w, 200, a.managerServiceStatus())
	case path == "/api/services":
		writeResponse(w, 200, a.services())
	case path == "/api/warp":
		writeResponse(w, 200, a.warp())
	case path == "/api/warp/scout":
		writeResponse(w, 200, a.scoutStatus())
	case path == "/api/backups":
		writeResponse(w, 200, M{"items": a.backups(), "limit": 10})
	case path == "/api/bypass":
		writeResponse(w, 200, a.bypassStatus())
	case path == "/api/diagnostics/result":
		writeResponse(w, 200, a.diagnosticResult(q.Get("job")))
	case path == "/api/diagnostics/history":
		writeResponse(w, 200, a.diagnosticHistory(q.Get("before")))
	case path == "/api/network/plans":
		writeResponse(w, 200, M{"plans": a.network.Plans(), "server_time": now()})
	case path == "/api/time":
		endpoint := q.Get("server")
		if endpoint == "" {
			endpoint = str(a.presentationPolicy()["clock_server"])
		}
		writeResponse(w, 200, displayTime(q.Get("source"), endpoint))
	case path == "/api/system":
		writeResponse(w, 200, a.host.snapshot())
	case path == "/api/system/history":
		writeResponse(w, 200, a.host.history(q))
	case path == "/api/rulesets/targets":
		writeResponse(w, 200, a.ruleTargets(q.Get("ref")))
	case path == "/api/rulesets/builtin":
		writeResponse(w, 200, a.builtinRuleView(q))
	case path == "/api/network/dns":
		writeResponse(w, 200, a.network.DNSView(q.Get("scope"), q.Get("interface")))
	case path == "/api/connections/business-history":
		writeResponse(w, 200, a.connections.businessHistory(q))
	case path == "/api/connections/history":
		writeResponse(w, 200, a.connections.history(q))
	case path == "/api/connections":
		writeResponse(w, 200, a.connections.query(q))
	case path == "/api/traffic":
		writeResponse(w, 200, a.traffic.snapshot())
	case path == "/api/traffic/policy":
		if q.Get("module") != "" {
			writeResponse(w, 200, a.traffic.moduleSnapshot(q.Get("module")))
		} else {
			writeResponse(w, 200, a.traffic.policySnapshot())
		}
	case path == "/api/wireguard/monitor-policy":
		writeResponse(w, 200, a.wgMonitorPolicy())
	case path == "/api/warp/monitor-policy":
		writeResponse(w, 200, a.warpMonitorPolicy())
	case path == "/api/monitoring/retention":
		writeResponse(w, 200, a.latencyRetention(q.Get("module")))
	case path == "/api/logs/retention":
		writeResponse(w, 200, a.logRetentionView())
	case path == "/api/monitoring/data-policy":
		writeResponse(w, 200, a.monitorDataView(q.Get("module"), q.Get("kind")))
	case path == "/api/monitoring/history-policy":
		writeResponse(w, 200, a.historyIntervalsView(q.Get("module")))
	case path == "/api/monitoring/probe-intervals":
		writeResponse(w, 200, a.probeIntervals(q.Get("module")))
	case path == "/api/monitoring/items":
		writeResponse(w, 200, a.monitorItemsSnapshot())
	case path == "/api/monitoring/modules":
		writeResponse(w, 200, a.monitorModules())
	case path == "/api/connections/host":
		a.connections.mu.RLock()
		value := clone(obj(a.connections.currentSummaries()["host"]))
		if value["time"] == nil {
			value["time"] = a.connections.at
		}
		a.connections.mu.RUnlock()
		writeResponse(w, 200, value)
	case path == "/api/monitoring/presentation":
		writeResponse(w, 200, a.presentationPolicy())
	case path == "/api/monitoring/display":
		writeResponse(w, 200, a.displayPolicy())
	case path == "/api/probe/policy":
		writeResponse(w, 200, a.probePolicySnapshot())
	case path == "/api/traffic/history-policy":
		writeResponse(w, 200, a.traffic.historyPolicySnapshot(q.Get("module")))
	case path == "/api/traffic/history":
		writeHistoryResponse(w, r, a.traffic.historyContext(r.Context(), q))
	case path == "/api/network/firewall":
		writeResponse(w, 200, a.network.FirewallView(q.Get("driver"), q.Get("zone")))
	case path == "/api/network/interfaces":
		writeResponse(w, 200, a.network.InterfaceSnapshot())
	case path == "/api/network/wireguard/client-template":
		profile, e := readWG(q.Get("name"))
		if e != nil {
			writeResponse(w, 400, M{"error": e.Error()})
			return
		}
		peer, _ := strconv.Atoi(q.Get("peer"))
		template, e := profile.clientTemplate(peer)
		if e != nil {
			writeResponse(w, 400, M{"error": e.Error()})
		} else {
			writeResponse(w, 200, M{"template": template})
		}
	case path == "/api/network/wireguard/profile":
		profile, e := readWG(q.Get("name"))
		if e != nil {
			writeResponse(w, 400, M{"error": e.Error()})
		} else {
			writeResponse(w, 200, profile)
		}
	case strings.HasPrefix(path, "/api/network/"):
		kind := strings.TrimPrefix(path, "/api/network/")
		if kind != "wireguard" && kind != "routes" && kind != "rules" && kind != "firewall" {
			writeResponse(w, 404, M{"error": "模块不存在"})
			return
		}
		writeResponse(w, 200, a.network.Snapshot(kind))
	default:
		writeResponse(w, 404, M{"error": "接口不存在"})
	}
}
func (a *App) postAPI(r *http.Request, d M) (any, error) {
	if e := a.moduleRequestAllowed(r.URL.Path, d); e != nil {
		return nil, e
	}
	switch r.URL.Path {
	case "/api/modules":
		name := str(d["module"])
		if name != "xray" && name != "warp" && name != "wireguard" && name != "komari" {
			return nil, errors.New("模块名称无效")
		}
		if _, ok := d["enabled"].(bool); !ok {
			return nil, errors.New("模块启用状态必须为布尔值")
		}
		if str(d["module"]) == "komari" && str(d["url"]) == "" {
			a.mu.RLock()
			d["url"] = a.components["komari_url"]
			a.mu.RUnlock()
			if str(d["url"]) == "" {
				host, _, _ := net.SplitHostPort(r.Host)
				if host == "" {
					host = r.Host
				}
				d["url"] = "http://" + net.JoinHostPort(host, "25774") + "/"
			}
		}
		identity, e := a.submit(str(d["module"])+" 模块", "module.settings", func(ctx context.Context, j *Job) error { return a.changeModule(ctx, j, d) })
		return M{"ok": e == nil, "id": identity}, e
	case "/api/modules/komari-check":
		address := str(d["url"])
		if address == "" {
			host, _, _ := net.SplitHostPort(r.Host)
			if host == "" {
				host = r.Host
			}
			address = "http://" + net.JoinHostPort(host, "25774") + "/"
		}
		return probeKomari(r.Context(), address)
	case "/api/system/policy":
		e := a.host.configure(d)
		if e == nil {
			a.audit("monitor.settings", "主机监控设置已更新", true, nil)
		}
		return M{"ok": e == nil}, e
	case "/api/traffic/enable":
		identity, e := a.submit("出口流量统计设置", "traffic", func(ctx context.Context, j *Job) error { return a.enableTraffic(ctx, j, yes(d["enabled"])) })
		return M{"ok": e == nil, "id": identity}, e
	case "/api/traffic/policy":
		var err error
		if r.URL.Query().Get("module") != "" {
			err = a.traffic.configureModule(r.URL.Query().Get("module"), d)
		} else {
			err = a.traffic.configure(d)
		}
		a.audit("monitor.traffic.policy", "流量监控设置已更新", err == nil, nil)
		return M{"ok": err == nil}, err
	case "/api/probe/policy":
		err := a.configureProbePolicy(d)
		a.audit("monitor.probe.policy", "节点检测设置已更新", err == nil, nil)
		return M{"ok": err == nil}, err
	case "/api/tasks/cancel":
		a.jobs.mu.Lock()
		defer a.jobs.mu.Unlock()
		for _, j := range a.jobs.items {
			if j.ID == str(d["id"]) && j.Status == "running" {
				j.cancel()
				return M{"ok": true}, nil
			}
		}
		return nil, errors.New("运行任务不存在")
	case "/api/wireguard/monitor-policy":
		err := a.saveWGMonitor(d)
		return M{"ok": err == nil}, err
	case "/api/proxy/pause":
		identity, e := a.submit("断开代理", "proxy.pause", func(ctx context.Context, j *Job) error {
			a.mutate.Lock()
			defer a.mutate.Unlock()
			a.mu.RLock()
			currentWarp := a.active == "warp" && !a.applied
			a.mu.RUnlock()
			if currentWarp && a.moduleEnabled("warp") && yes(a.warp()["connected"]) {
				if err := a.serviceAction(ctx, j, "warp", "disconnect", M{}); err != nil {
					return err
				}
			}
			if err := a.proxyControlLocked(M{"enabled": false}); err != nil {
				return err
			}
			if !currentWarp && a.moduleEnabled("xray") {
				if _, err := a.command(12, "", "systemctl", "stop", "foxray-engine"); err != nil {
					return err
				}
			}
			a.mu.Lock()
			if currentWarp || !yes(a.components["warp"]) {
				a.active = "direct"
			}
			a.settings["proxy_paused"] = currentWarp
			err := a.persist()
			if err == nil {
				err = writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings)
			}
			a.mu.Unlock()
			a.audit("proxy.pause", "已临时断开代理", err == nil, M{})
			return err
		})
		return M{"ok": e == nil, "id": identity}, e
	case "/api/proxy":
		identity, e := a.submit("Xray 代理模式", "proxy", func(ctx context.Context, j *Job) error {
			a.stage(j, "校验配置与 VPN 排除范围")
			return a.proxyControl(d)
		})
		return M{"ok": e == nil, "id": identity}, e
	case "/api/rulesets/update":
		identity, e := a.submit("更新规则集", "rules.update", func(ctx context.Context, j *Job) error { return a.updateRuleSet(ctx, j, str(d["id"])) })
		return M{"ok": e == nil, "id": identity}, e
	case "/api/config":
		err := a.saveXraySettings(d)
		return M{"ok": err == nil}, err
	case "/api/native/activate":
		identity := str(d["id"])
		a.mu.RLock()
		c := clone(a.cfg)
		a.mu.RUnlock()
		if identity == "warp" {
			if a.applied {
				return nil, errors.New("Xray 优先级高于 WARP；管理隧道不会切换当前 Xray 出口")
			}
			j, e := a.submit("激活 WARP", "node.switch", func(ctx context.Context, job *Job) error {
				if err := a.serviceAction(ctx, job, "warp", "connect", M{}); err != nil {
					return err
				}
				return a.proxyControl(M{"enabled": false})
			})
			return M{"ok": e == nil, "id": j}, e
		}
		c["selected"], c["enabled"], c["mode"] = identity, true, "manual"
		j, e := a.submit("节点切换", "node.switch", func(ctx context.Context, job *Job) error {
			a.stage(job, "切换出口并验证路由")
			return a.proxyControl(c)
		})
		return M{"ok": e == nil, "id": j}, e
	case "/api/probe", "/api/speed":
		speed := r.URL.Path == "/api/speed"
		ids := []string{}
		for _, v := range arr(d["ids"]) {
			ids = append(ids, str(v))
		}
		title := map[bool]string{true: "节点带宽测速", false: "节点真连接检测"}[speed]
		if len(ids) == 1 && ids[0] == "warp" {
			title = map[bool]string{true: "WARP 带宽测速", false: "WARP 真连接检测"}[speed]
		}
		j, e := a.submit(title, map[bool]string{true: "probe.speed", false: "probe.latency"}[speed], func(ctx context.Context, j *Job) error { return a.detect(ctx, j, speed, ids) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/native/node", "/api/native/batch", "/api/native/view", "/api/native/config":
		return a.nodeAction(r.URL.Path, d)
	case "/api/nodes/import/preview":
		return a.importPreview(d)
	case "/api/nodes/import/commit":
		return a.importCommit(d)
	case "/api/native/subscription":
		return a.subscriptionAction(d)
	case "/api/subscriptions/batch":
		if str(d["operation"]) != "update" {
			return a.subscriptionBatch(d)
		}
		j, e := a.submit("批量更新订阅", "subscription.update", func(ctx context.Context, j *Job) error { return a.updateSubscriptions(ctx, j, d) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/subscriptions/update":
		identity := str(d["id"])
		j, e := a.submit("更新订阅", "subscription.update", func(ctx context.Context, j *Job) error {
			return a.updateSubscription(ctx, j, identity, str(d["conflict_policy"]))
		})
		return M{"ok": e == nil, "id": j}, e
	case "/api/manager/service":
		return a.managerServiceAction(d)
	case "/api/services/action":
		service, action := str(d["service"]), str(d["action"])
		j, e := a.submit("服务操作 · "+service, "service."+action, func(ctx context.Context, j *Job) error { return a.serviceAction(ctx, j, service, action, d) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/monitoring/retention":
		err := a.saveLatencyRetention(d, r.URL.Query().Get("module"))
		return M{"ok": err == nil}, err
	case "/api/warp/monitor-policy":
		err := a.saveWarpMonitor(d)
		return M{"ok": err == nil}, err
	case "/api/logs/retention":
		err := a.saveLogRetention(r.URL.Query().Get("kind"), d)
		return M{"ok": err == nil}, err
	case "/api/monitoring/data-policy":
		err := a.saveMonitorData(r.URL.Query().Get("module"), r.URL.Query().Get("kind"), d)
		if err == nil && (r.URL.Query().Get("module") == "all" || r.URL.Query().Get("kind") == "latency") {
			a.cleanupLatencySize()
		}
		return M{"ok": err == nil}, err
	case "/api/monitoring/history-policy":
		err := a.saveHistoryIntervals(r.URL.Query().Get("module"), d)
		return M{"ok": err == nil}, err
	case "/api/monitoring/probe-intervals":
		err := a.saveProbeIntervals(r.URL.Query().Get("module"), d)
		return M{"ok": err == nil}, err
	case "/api/monitoring/items":
		err := a.saveMonitorItems(r.URL.Query().Get("module"), d)
		return M{"ok": err == nil}, err
	case "/api/monitoring/modules":
		err := a.saveMonitorModules(d)
		return M{"ok": err == nil}, err
	case "/api/monitoring/presentation":
		err := a.savePresentation(d)
		return M{"ok": err == nil}, err
	case "/api/traffic/history-policy":
		err := a.traffic.saveHistoryPolicy(r.URL.Query().Get("module"), d)
		return M{"ok": err == nil}, err
	case "/api/monitoring/display":
		err := a.saveDisplayPolicy(d)
		return M{"ok": err == nil}, err
	case "/api/settings":
		for key := range d {
			if key != "log_retention_days" && key != "measurement_retention_days" && key != "log_max_rows" {
				return nil, errors.New("管理设置包含不支持字段")
			}
		}
		for _, field := range []string{"log_retention_days", "measurement_retention_days"} {
			if _, e := integer(d[field], 1, 365); e != nil {
				return nil, e
			}
		}
		if _, e := integer(d["log_max_rows"], 100, 100000); e != nil {
			return nil, e
		}
		a.mu.Lock()
		for k, v := range d {
			a.settings[k] = v
		}
		e := writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings)
		a.mu.Unlock()
		a.audit("settings.updated", "管理设置已保存", e == nil, nil)
		return M{"ok": e == nil}, e
	case "/api/updates/upgrade":
		service := str(d["service"])
		if !validUpdateService(service) || !yes(d["confirmed"]) {
			return nil, errors.New("请确认所选模块的版本更新")
		}
		id, err := a.submit("版本更新 · "+service, "version.upgrade."+service, func(ctx context.Context, j *Job) error {
			return a.upgradeServiceVersion(ctx, j, service, str(d["version"]))
		})
		return M{"ok": err == nil, "id": id}, err
	case "/api/manager/recover-control":
		err := a.recoverControl(d)
		return M{"ok": err == nil}, err
	case "/api/updates/policy":
		err := a.saveVersionCheckPolicy(d)
		return M{"ok": err == nil}, err
	case "/api/updates/check":
		service := str(d["service"])
		if !validUpdateService(service) {
			return nil, errors.New("更新模块无效")
		}
		j, e := a.submit("版本检查 · "+service, "version.check."+service, func(ctx context.Context, j *Job) error { return a.checkServiceVersion(ctx, j, service) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/settings/check-update":
		j, e := a.submit("版本更新检查", "version.check", func(ctx context.Context, j *Job) error { return a.checkUpdate(ctx, j) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/icmp-policy":
		err := a.saveXraySettings(M{"icmp_policy": d})
		return M{"ok": err == nil}, err

	case "/api/backups/create":
		return a.createBackup(str(d["name"]), "manual")
	case "/api/backups/restore":
		j, e := a.submit("恢复节点与订阅", "backup.restore", func(ctx context.Context, j *Job) error { return a.restoreBackup(str(d["id"])) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/warp/action", "/api/warp/scout", "/api/warp/policy":
		return a.warpAction(r.URL.Path, d)
	case "/api/diagnostics":
		j, e := a.submit("目标路径诊断", "diagnostic.run", func(ctx context.Context, j *Job) error { return a.diagnose(ctx, j, d) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/diagnostics/replay":
		params := a.diagnosticParams(str(d["id"]))
		if len(params) == 0 {
			return nil, errors.New("历史诊断参数不存在")
		}
		j, e := a.submit("重新执行历史诊断", "diagnostic.run", func(ctx context.Context, j *Job) error { return a.diagnose(ctx, j, params) })
		return M{"ok": e == nil, "id": j}, e
	case "/api/network/wireguard/keys":
		return a.newWGKey(str(d["name"]))
	case "/api/network/plan":
		return a.network.Preview(d)
	case "/api/network/apply":
		j, e := a.submit("应用网络变更", "network.apply", func(ctx context.Context, j *Job) error {
			a.stage(j, "校验变更并创建独立回退任务")
			e := a.network.ApplyProgress(ctx, str(d["id"]), func(message string) { a.stage(j, message) })
			if e == nil {
				a.stage(j, "变更已应用，请在期限内确认网络正常")
			}
			return e
		})
		return M{"ok": e == nil, "id": j}, e
	case "/api/network/confirm":
		e := a.network.Confirm(str(d["id"]))
		return M{"ok": e == nil}, e
	case "/api/network/rollback":
		a.network.mu.Lock()
		defer a.network.mu.Unlock()
		p := a.network.plans[str(d["id"])]
		if p == nil || p.Status != "awaiting_confirmation" {
			return nil, errors.New("无待回退变更")
		}
		a.mutate.Lock()
		defer a.mutate.Unlock()
		a.network.rollback(p)
		return M{"ok": true}, nil
	default:
		return nil, errors.New("接口不存在")
	}
}
func (a *App) latencyHistory(q url.Values) M {
	end := now()
	if v, e := strconv.ParseFloat(q.Get("end"), 64); e == nil {
		end = v
	}
	hours := 24.
	if v, e := strconv.ParseFloat(q.Get("hours"), 64); e == nil && v > 0 && v <= 8760 {
		hours = v
	}
	start := end - hours*3600
	if v, e := strconv.ParseFloat(q.Get("start"), 64); e == nil {
		start = v
	}
	if start >= end || end-start > 365*86400 {
		return M{"error": "时间范围无效，最多365天"}
	}
	kind := q.Get("kind")
	table, selectFields := "samples", "node,t,latency"
	if kind == "icmp" {
		table = "icmp_samples"
		selectFields += ""
	}
	_ = selectFields
	bucket := int((end - start) / 600)
	if bucket < 1 {
		bucket = 1
	}
	query := "SELECT node, CAST(t/? AS INTEGER)*?, AVG(CASE WHEN latency>=0 THEN latency END),COUNT(*),SUM(CASE WHEN latency<0 THEN 1 ELSE 0 END)"
	if table == "icmp_samples" {
		query += ",AVG(success)"
	}
	query += " FROM " + table + " WHERE t>=? AND t<=?"
	args := []any{bucket, bucket, start, end}
	if module := q.Get("module"); module != "" {
		query += " AND (" + monitorScope(module) + ")"
	}
	if node := q.Get("node"); node != "" {
		query += " AND node=?"
		args = append(args, node)
	}
	query += " GROUP BY node,CAST(t/? AS INTEGER) ORDER BY t"
	args = append(args, bucket)
	rows, e := a.histdb.Query(query, args...)
	series := M{}
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var node string
			var t float64
			var latency, success sql.NullFloat64
			var count, failed int
			if table == "icmp_samples" {
				e = rows.Scan(&node, &t, &latency, &count, &failed, &success)
			} else {
				e = rows.Scan(&node, &t, &latency, &count, &failed)
			}
			if e != nil {
				continue
			}
			value := -1.
			if latency.Valid {
				value = latency.Float64
			}
			row := M{"time": t, "latency": value, "count": count, "failed": failed}
			if success.Valid {
				row["success_pct"] = success.Float64
			}
			series[node] = append(arr(series[node]), row)
		}
	}
	return M{"start": start, "end": end, "bucket_seconds": bucket, "series": series}
}
func (a *App) updateStatus() M {
	m := M{"status": "idle", "current": Version}
	_ = readJSON(filepath.Join(a.base, "manager-update.json"), &m)
	return m
}
func (a *App) checkUpdate(ctx context.Context, j *Job) error {
	return a.checkServiceVersion(ctx, j, "foxray")
}

func (a *App) httpClient() *http.Client {
	a.mu.RLock()
	port := a.probePortLocked(a.active)
	a.mu.RUnlock()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if port > 0 {
		u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: transport, Timeout: 25 * time.Second}
}
