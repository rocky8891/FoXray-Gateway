package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func parseDocument(content string) ([]M, error) {
	if len(content) > 2*1024*1024 {
		return nil, errors.New("导入内容超过2MB")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("导入内容为空")
	}
	if content[0] == '{' || content[0] == '[' {
		var raw any
		if json.Unmarshal([]byte(content), &raw) != nil {
			return nil, errors.New("节点 JSON 无效")
		}
		values := arr(raw)
		if m, ok := raw.(map[string]any); ok {
			values = arr(m["outbounds"])
		}
		out := []M{}
		for _, v := range values {
			o := obj(v)
			kind := str(o["type"])
			if kind == "direct" || kind == "block" || kind == "selector" || kind == "urltest" {
				continue
			}
			o = clone(o)
			for _, key := range []string{"tag", "detour", "routing_mark", "bind_interface"} {
				delete(o, key)
			}
			if _, e := canonicalOutbound(o, "check"); e != nil {
				return nil, e
			}
			out = append(out, M{"name": label(firstNonempty(str(obj(v)["tag"]), str(o["server"]))), "outbound": o})
		}
		if len(out) == 0 || len(out) > 128 {
			return nil, errors.New("节点数量应为1至128")
		}
		return out, nil
	}
	if !strings.Contains(content, "://") {
		var e error
		content, e = decode64(content)
		if e != nil {
			return nil, e
		}
	}
	rows := []M{}
	seen := map[string]bool{}
	for i, line := range textLines(content) {
		if strings.HasPrefix(line, "#") {
			continue
		}
		n, e := parseNodeURI(line)
		if e != nil {
			return nil, fmt.Errorf("第%d条：%s", i+1, e.Error())
		}
		h := fingerprint(n["outbound"])
		if !seen[h] {
			rows = append(rows, n)
			seen[h] = true
		}
	}
	if len(rows) == 0 || len(rows) > 128 {
		return nil, errors.New("节点数量应为1至128")
	}
	return rows, nil
}
func redacted(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := M{}
		for k, value := range x {
			if secretField(k) {
				m[k] = "<redacted>"
			} else {
				m[k] = redacted(value)
			}
		}
		return m
	case []any:
		out := []any{}
		for _, v := range x {
			out = append(out, redacted(v))
		}
		return out
	}
	return v
}
func mergeMasked(old, patch M) M {
	out := clone(old)
	for k, v := range patch {
		if str(v) == "<redacted>" {
			continue
		}
		if m, ok := v.(map[string]any); ok {
			out[k] = mergeMasked(obj(out[k]), m)
		} else {
			out[k] = v
		}
	}
	return out
}
func (a *App) rebuildEndpoints(c, native M) {
	eps := []any{}
	for _, v := range arr(c["endpoints"]) {
		e := obj(v)
		if str(e["kind"]) != "native" {
			eps = append(eps, e)
		}
	}
	for _, v := range arr(native["nodes"]) {
		n := obj(v)
		eps = append(eps, M{"id": n["id"], "name": n["name"], "kind": "native", "enabled": n["enabled"], "priority": n["priority"]})
	}
	c["endpoints"] = eps
}
func (a *App) nodeAction(path string, d M) (M, error) {
	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.RLock()
	native, c, active, applied := clone(a.native), clone(a.cfg), a.active, a.applied
	a.mu.RUnlock()
	identity := str(d["id"])
	if path == "/api/native/view" {
		for _, v := range arr(native["nodes"]) {
			n := obj(v)
			if str(n["id"]) == identity {
				return M{"node": redacted(n)}, nil
			}
		}
		return nil, errors.New("节点不存在")
	}
	ids := map[string]bool{identity: true}
	if path == "/api/native/batch" {
		ids = map[string]bool{}
		for _, v := range arr(d["ids"]) {
			ids[str(v)] = true
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("请选择节点")
	}
	op := str(d["operation"])
	if op == "" {
		if yes(d["delete"]) {
			op = "delete"
		} else if d["enabled"] != nil {
			if yes(d["enabled"]) {
				op = "enable"
			} else {
				op = "disable"
			}
		} else {
			op = "edit"
		}
	}
	if op != "delete" && op != "disable" && op != "enable" && op != "edit" {
		return nil, errors.New("节点操作无效")
	}
	if op == "delete" || op == "disable" {
		var e error
		c, active, applied, e = a.subscriptionFallbackLocked(ids, c, active, applied)
		if e != nil {
			return nil, e
		}
	}
	next := []any{}
	changes := []M{}
	for _, v := range arr(native["nodes"]) {
		n := obj(v)
		if !ids[str(n["id"])] {
			next = append(next, n)
			continue
		}
		switch op {
		case "delete":
			if subscription := str(n["subscription"]); subscription != "" {
				excluded := obj(native["excluded"])
				excluded[subscription] = append(arr(excluded[subscription]), fingerprint(n["outbound"]))
				native["excluded"] = excluded
			}
		case "enable", "disable":
			n["enabled"] = op == "enable"
			next = append(next, n)
		case "edit":
			if value, ok := d["name"]; ok {
				n["name"] = label(str(value))
			}
			if p, ok := d["priority"]; ok {
				n["priority"] = p
			}
			if patch := obj(d["outbound"]); len(patch) > 0 {
				n["outbound"] = mergeMasked(obj(n["outbound"]), patch)
				if _, e := canonicalOutbound(obj(n["outbound"]), "check"); e != nil {
					return nil, e
				}
			}
			if _, e := integer(n["priority"], 1, 999); e != nil {
				return nil, e
			}
			next = append(next, n)
		}
		n["updated"] = now()
		changes = append(changes, M{"id": n["id"], "name": n["name"], "operation": op})
	}
	if len(changes) == 0 {
		return nil, errors.New("所选节点不存在")
	}
	native["nodes"] = next
	a.rebuildEndpoints(c, native)
	if str(a.service("foxray-engine")["state"]) == "active" {
		if e := a.startEngine(c, native, active); e != nil {
			return nil, e
		}
	}
	if applied {
		if e := a.attachWithNative(c, native); e != nil {
			return nil, e
		}
	}
	a.mu.Lock()
	a.cfg, a.native = c, native
	e := a.persistNative()
	if e == nil {
		e = a.persist()
	}
	a.mu.Unlock()
	a.audit("node."+op, "节点操作完成："+op, e == nil, M{"nodes": changes})
	return M{"ok": e == nil, "count": len(changes)}, e
}
func (a *App) attachWithNative(c, native M) error { return a.attachConfig(c, native) }
func (a *App) importPreview(d M) (M, error) {
	parsed, e := parseDocument(str(d["content"]))
	if e != nil {
		return nil, e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	conflicts := []M{}
	newCount := 0
	for _, p := range parsed {
		found := false
		for _, v := range arr(a.native["nodes"]) {
			n := obj(v)
			duplicate := fingerprint(n["outbound"]) == fingerprint(p["outbound"])
			nameConflict := str(n["name"]) == str(p["name"])
			if duplicate || nameConflict {
				conflicts = append(conflicts, M{"name": p["name"], "existing_id": n["id"], "existing_name": n["name"], "duplicate": duplicate, "name_conflict": nameConflict})
				found = true
				break
			}
		}
		if !found {
			newCount++
		}
	}
	token := id()
	a.pendingImports[token] = M{"nodes": parsed, "created": now(), "revision": fingerprint(a.native)}
	for k, v := range a.pendingImports {
		if now()-num(v["created"]) > 600 {
			delete(a.pendingImports, k)
		}
	}
	return M{"id": token, "count": len(parsed), "new_count": newCount, "conflicts": conflicts}, nil
}
func (a *App) importCommit(d M) (M, error) {
	a.mutate.Lock()
	defer a.mutate.Unlock()
	policy := str(d["policy"])
	if policy != "skip" && policy != "replace" {
		return nil, errors.New("请选择跳过或替换重复项")
	}
	a.mu.RLock()
	pending := clone(a.pendingImports[str(d["id"])])
	native, c, active, applied := clone(a.native), clone(a.cfg), a.active, a.applied
	a.mu.RUnlock()
	if pending == nil || now()-num(pending["created"]) > 600 {
		return nil, errors.New("导入预览已过期")
	}
	if fingerprint(native) != str(pending["revision"]) {
		return nil, errors.New("节点已发生变化，请重新预览")
	}
	parsed := []M{}
	for _, v := range arr(pending["nodes"]) {
		parsed = append(parsed, obj(v))
	}
	count, skip, e := mergeNodes(native, parsed, "", policy)
	if e != nil {
		return nil, e
	}
	a.rebuildEndpoints(c, native)
	if str(a.service("foxray-engine")["state"]) == "active" {
		if e = a.startEngine(c, native, active); e != nil {
			return nil, e
		}
	}
	if applied {
		if e = a.attachWithNative(c, native); e != nil {
			return nil, e
		}
	}
	a.mu.Lock()
	a.native, a.cfg = native, c
	e = a.persistNative()
	if e == nil {
		e = a.persist()
	}
	delete(a.pendingImports, str(d["id"]))
	a.mu.Unlock()
	a.audit("node.import", fmt.Sprintf("导入节点 %d 项，跳过 %d 项", count, skip), e == nil, nil)
	return M{"ok": e == nil, "count": count, "skipped": skip}, e
}
func mergeNodes(native M, parsed []M, subscription, policy string) (int, int, error) {
	nodes := arr(native["nodes"])
	count, skipped := 0, 0
	for _, p := range parsed {
		excluded := false
		for _, h := range arr(obj(native["excluded"])[subscription]) {
			if str(h) == fingerprint(p["outbound"]) {
				excluded = true
			}
		}
		if excluded {
			skipped++
			continue
		}
		matched := -1
		for i, v := range nodes {
			n := obj(v)
			if fingerprint(n["outbound"]) == fingerprint(p["outbound"]) || str(n["name"]) == str(p["name"]) {
				matched = i
				break
			}
		}
		sameOwner := matched >= 0 && subscription != "" && str(obj(nodes[matched])["subscription"]) == subscription
		if matched >= 0 && policy == "skip" && !sameOwner {
			skipped++
			continue
		}
		node := M{"id": "n" + id()[:16], "enabled": true, "priority": 20, "created": now()}
		if matched >= 0 {
			node = clone(obj(nodes[matched]))
		}
		node["name"], node["outbound"], node["updated"] = p["name"], p["outbound"], now()
		if subscription != "" {
			node["subscription"] = subscription
		}
		if matched >= 0 {
			nodes[matched] = node
		} else {
			nodes = append(nodes, node)
		}
		count++
	}
	if len(nodes) > 128 {
		return 0, 0, errors.New("节点数量超过128")
	}
	native["nodes"] = nodes
	return count, skipped, nil
}
func validateSubscriptionURL(s string) error {
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("订阅地址须为 HTTPS，且不能包含用户名密码")
	}
	return nil
}
func (a *App) subscriptionAction(d M) (M, error) {
	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.RLock()
	native := clone(a.native)
	active := a.active
	a.mu.RUnlock()
	identity := str(d["id"])
	deleting := yes(d["delete"])
	subs := []any{}
	found := false
	for _, v := range arr(native["subscriptions"]) {
		s := obj(v)
		if s["id"] != identity {
			subs = append(subs, s)
			continue
		}
		found = true
		if deleting {
			continue
		}
		if value := str(d["name"]); value != "" {
			s["name"] = label(value)
		}
		if value := str(d["url"]); value != "" {
			if e := validateSubscriptionURL(value); e != nil {
				return nil, e
			}
			s["url"] = value
		}
		for _, k := range []string{"auto_update", "interval"} {
			if d[k] != nil {
				s[k] = d[k]
			}
		}
		if _, e := integer(s["interval"], 300, 86400); e != nil {
			return nil, e
		}
		subs = append(subs, s)
	}
	if identity != "" && !found {
		return nil, errors.New("订阅不存在")
	}
	if deleting {
		nodes := []any{}
		for _, v := range arr(native["nodes"]) {
			n := obj(v)
			if str(n["subscription"]) != identity {
				nodes = append(nodes, n)
				continue
			}
			if yes(d["delete_nodes"]) {
				if n["id"] == active {
					return nil, errors.New("订阅包含当前出口，请先切换后再删除所属节点")
				}
				continue
			}
			n["subscription"] = nil
			nodes = append(nodes, n)
		}
		native["nodes"] = nodes
	} else if !found {
		address := str(d["url"])
		if e := validateSubscriptionURL(address); e != nil {
			return nil, e
		}
		identity = "s" + id()[:16]
		interval := first(d["interval"], 3600)
		if _, e := integer(interval, 300, 86400); e != nil {
			return nil, e
		}
		subs = append(subs, M{"id": identity, "name": label(str(d["name"])), "url": address, "auto_update": yes(d["auto_update"]), "interval": interval, "created": now(), "updated": 0})
	}
	native["subscriptions"] = subs
	a.mu.Lock()
	c := clone(a.cfg)
	a.rebuildEndpoints(c, native)
	a.native, a.cfg = native, c
	e := a.persistNative()
	if e == nil {
		e = a.persist()
	}
	a.mu.Unlock()
	a.audit("subscription.updated", "订阅配置已更新", e == nil, M{"id": identity, "deleted": deleting, "delete_nodes": yes(d["delete_nodes"])})
	return M{"ok": e == nil, "id": identity}, e
}
func (a *App) updateSubscription(ctx context.Context, j *Job, identity, policy string) error {
	a.mu.RLock()
	native := clone(a.native)
	a.mu.RUnlock()
	var sub M
	for _, v := range arr(native["subscriptions"]) {
		if obj(v)["id"] == identity {
			sub = obj(v)
		}
	}
	if sub == nil {
		return errors.New("订阅不存在")
	}
	if sub["enabled"] == false {
		return errors.New("订阅已停用，请先启用")
	}
	a.stage(j, "正在下载订阅，地址不写入日志")
	req, e := http.NewRequestWithContext(ctx, "GET", str(sub["url"]), nil)
	if e != nil {
		return errors.New("订阅地址无效")
	}
	res, e := a.httpClient().Do(req)
	if e != nil {
		return errors.New("订阅下载未完成")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("订阅响应 HTTP %d", res.StatusCode)
	}
	b, e := limitedRead(res.Body, 2*1024*1024)
	if e != nil {
		return e
	}
	parsed, e := parseDocument(string(b))
	if e != nil {
		return e
	}
	if policy != "skip" && policy != "replace" {
		policy = "skip"
	}
	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.RLock()
	native = clone(a.native)
	c, active, applied := clone(a.cfg), a.active, a.applied
	a.mu.RUnlock()
	currentSub := M(nil)
	for _, v := range arr(native["subscriptions"]) {
		if str(obj(v)["id"]) == identity {
			currentSub = obj(v)
		}
	}
	if currentSub == nil {
		return errors.New("订阅已删除，取消写入")
	}
	if currentSub["enabled"] == false {
		return errors.New("订阅已停用，取消写入")
	}
	if str(currentSub["url"]) != str(sub["url"]) {
		return errors.New("订阅地址已修改，请重新更新")
	}
	count, skipped, e := mergeNodes(native, parsed, identity, policy)
	if e != nil {
		return e
	}
	for _, v := range arr(native["subscriptions"]) {
		s := obj(v)
		if s["id"] == identity {
			s["updated"] = now()
			s["error"] = ""
		}
	}
	remote := map[string]bool{}
	for _, p := range parsed {
		remote[fingerprint(p["outbound"])] = true
	}
	kept := []any{}
	for _, v := range arr(native["nodes"]) {
		n := obj(v)
		if str(n["subscription"]) == identity && !remote[fingerprint(n["outbound"])] {
			if str(n["id"]) == active {
				n["subscription"] = nil
			} else {
				continue
			}
		}
		kept = append(kept, n)
	}
	native["nodes"] = kept
	a.rebuildEndpoints(c, native)
	if str(a.service("foxray-engine")["state"]) == "active" || applied {
		if e = a.reloadNodeEngine(c, native, active, applied); e != nil {
			return e
		}
	}
	a.mu.Lock()
	a.native, a.cfg = native, c
	e = a.persistNative()
	if e == nil {
		e = a.persist()
	}
	a.mu.Unlock()
	a.stage(j, fmt.Sprintf("更新完成，处理%d项，跳过%d项", count, skipped))
	return e
}
func (a *App) backups() []M {
	paths, _ := filepath.Glob(filepath.Join(a.base, "private", "node-backups", "*.json"))
	out := []M{}
	for _, p := range paths {
		m := M{}
		if readJSON(p, &m) == nil && str(m["format"]) == "foxray-node-backup" {
			row := M{}
			for _, k := range []string{"id", "created", "name", "reason", "node_count", "subscription_count", "app_version"} {
				row[k] = m[k]
			}
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return num(out[i]["created"]) > num(out[j]["created"]) })
	return out
}
func (a *App) createBackup(name, reason string) (M, error) {
	a.mu.RLock()
	data := clone(a.native)
	a.mu.RUnlock()
	identity := id()
	record := M{"format": "foxray-node-backup", "format_version": 1, "id": identity, "created": now(), "name": label(firstNonempty(name, "节点与订阅备份")), "reason": reason, "app_version": Version, "node_count": len(arr(data["nodes"])), "subscription_count": len(arr(data["subscriptions"])), "data": data, "checksum": fingerprint(data)}
	path := filepath.Join(a.base, "private", "node-backups", identity+".json")
	if e := writeJSON(path, record); e != nil {
		return nil, e
	}
	rows := a.backups()
	for i := 10; i < len(rows); i++ {
		_ = os.Remove(filepath.Join(a.base, "private", "node-backups", str(rows[i]["id"])+".json"))
	}
	delete(record, "data")
	delete(record, "checksum")
	a.audit("backup.create", "已创建节点订阅备份", true, M{"id": identity})
	return record, nil
}
func (a *App) restoreBackup(identity string) error {
	if !regexpID(identity) {
		return errors.New("备份标识无效")
	}
	m := M{}
	if readJSON(filepath.Join(a.base, "private", "node-backups", identity+".json"), &m) != nil {
		return errors.New("备份不存在")
	}
	native := obj(m["data"])
	if str(m["checksum"]) != fingerprint(native) {
		return errors.New("备份校验失败")
	}
	if _, e := a.createBackup("恢复前自动备份", "before_restore"); e != nil {
		return e
	}
	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.RLock()
	c, active, applied := clone(a.cfg), a.active, a.applied
	a.mu.RUnlock()
	a.rebuildEndpoints(c, native)
	found := active == "warp" || active == "direct"
	for _, v := range arr(native["nodes"]) {
		if obj(v)["id"] == active && yes(obj(v)["enabled"]) {
			found = true
		}
	}
	if !found {
		return errors.New("备份不含当前启用出口，请先切换再恢复")
	}
	if e := a.reloadNodeEngine(c, native, active, applied); e != nil {
		return e
	}
	a.mu.Lock()
	a.native, a.cfg = native, c
	e := a.persistNative()
	if e == nil {
		e = a.persist()
	}
	a.mu.Unlock()
	return e
}
func regexpID(s string) bool {
	if len(s) < 16 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'f' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

var _ io.Reader
var _ http.Client
var _ time.Time

func secretField(k string) bool {
	key := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
	for _, part := range []string{"password", "secret", "token", "authorization", "cookie", "private", "preshared"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return key == "uuid" || key == "auth" || key == "pass"
}

// Caller holds mutate; reuse the verified node fallback for subscription operations.
func (a *App) subscriptionFallbackLocked(ids map[string]bool, c M, active string, applied bool) (M, string, bool, error) {
	if ids[active] && applied {
		a.mu.RLock()
		scores := a.scores()
		a.mu.RUnlock()
		sort.Slice(scores, func(i, j int) bool { return num(scores[i]["score"]) > num(scores[j]["score"]) })
		fallback := ""
		for _, s := range scores {
			if str(s["id"]) != "warp" && !ids[str(s["id"])] && yes(s["eligible"]) {
				fallback = str(s["id"])
				break
			}
		}
		if fallback == "" {
			if yes(a.warp()["connected"]) {
				if e := a.proxyControlLocked(M{"enabled": false}); e != nil {
					return c, active, applied, e
				}
				applied = false
				active = "warp"
			} else {
				return c, active, applied, errors.New("当前出口在所选节点中，且没有已验证回退；请先切换可用出口")
			}
		} else {
			if e := a.proxyControlLocked(M{"enabled": true, "selected": fallback, "mode": "manual"}); e != nil {
				return c, active, applied, e
			}
			active = fallback
		}
		a.mu.RLock()
		c = clone(a.cfg)
		a.mu.RUnlock()
	}
	return c, active, applied, nil
}

func subscriptionIDs(d M, native M) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, v := range arr(d["ids"]) {
		id := str(v)
		if id == "" || ids[id] {
			return nil, errors.New("订阅选择无效或重复")
		}
		ids[id] = true
	}
	if len(ids) == 0 {
		return nil, errors.New("请选择订阅")
	}
	found := 0
	for _, v := range arr(native["subscriptions"]) {
		if ids[str(obj(v)["id"])] {
			found++
		}
	}
	if found != len(ids) {
		return nil, errors.New("部分订阅已不存在，请刷新后重试")
	}
	return ids, nil
}
func (a *App) subscriptionBatch(d M) (M, error) {
	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.RLock()
	native, c, active, applied := clone(a.native), clone(a.cfg), a.active, a.applied
	a.mu.RUnlock()
	ids, e := subscriptionIDs(d, native)
	if e != nil {
		return nil, e
	}
	op := str(d["operation"])
	if op != "enable" && op != "disable" && op != "stop_updates" && op != "delete" {
		return nil, errors.New("订阅操作无效")
	}
	nodeIDs := map[string]bool{}
	for _, v := range arr(native["nodes"]) {
		n := obj(v)
		if ids[str(n["subscription"])] {
			nodeIDs[str(n["id"])] = true
		}
	}
	if v, ok := d["delete_nodes"]; ok {
		if _, valid := v.(bool); !valid {
			return nil, errors.New("删除节点选项须为布尔值")
		}
	}
	deletingNodes := op == "delete" && yes(d["delete_nodes"])
	if op == "disable" || deletingNodes {
		c, active, applied, e = a.subscriptionFallbackLocked(nodeIDs, c, active, applied)
		if e != nil {
			return nil, e
		}
	}
	subs := []any{}
	for _, v := range arr(native["subscriptions"]) {
		sub := obj(v)
		if ids[str(sub["id"])] {
			if op == "delete" {
				continue
			}
			if op == "stop_updates" {
				sub["auto_update"] = false
			} else {
				sub["enabled"] = op == "enable"
			}
		}
		subs = append(subs, sub)
	}
	nodes := []any{}
	for _, v := range arr(native["nodes"]) {
		n := obj(v)
		if ids[str(n["subscription"])] {
			if deletingNodes {
				continue
			}
			if op == "delete" {
				n["subscription"] = nil
			} else if op != "stop_updates" {
				n["enabled"] = op == "enable"
				n["updated"] = now()
			}
		}
		nodes = append(nodes, n)
	}
	native["subscriptions"], native["nodes"] = subs, nodes
	a.rebuildEndpoints(c, native)
	if op == "enable" || op == "disable" || deletingNodes {
		if str(a.service("foxray-engine")["state"]) == "active" || applied {
			if e = a.reloadNodeEngine(c, native, active, applied); e != nil {
				return nil, e
			}
		}
	}
	a.mu.Lock()
	a.native, a.cfg = native, c
	e = a.persistNative()
	if e == nil {
		e = a.persist()
	}
	a.mu.Unlock()
	a.audit("subscription."+op, "订阅批量操作完成", e == nil, M{"count": len(ids)})
	return M{"ok": e == nil, "count": len(ids)}, e
}
func (a *App) updateSubscriptions(ctx context.Context, j *Job, d M) error {
	a.mu.RLock()
	_, e := subscriptionIDs(d, a.native)
	a.mu.RUnlock()
	if e != nil {
		return e
	}
	failed := 0
	for i, v := range arr(d["ids"]) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.stage(j, fmt.Sprintf("更新订阅 %d / %d", i+1, len(arr(d["ids"]))))
		if err := a.updateSubscription(ctx, j, str(v), str(d["conflict_policy"])); err != nil {
			failed++
			a.jobDetail(j, fmt.Sprintf("第%d项失败：%s", i+1, err.Error()))
		} else {
			a.jobDetail(j, fmt.Sprintf("第%d项更新成功", i+1))
		}
	}
	if failed > 0 {
		return taskCompletionError(len(arr(d["ids"]))-failed, len(arr(d["ids"])), fmt.Sprintf("批量更新完成，成功%d项，失败%d项", len(arr(d["ids"]))-failed, failed))
	}
	a.stage(j, fmt.Sprintf("批量更新完成，成功%d项", len(arr(d["ids"]))))
	return nil
}
