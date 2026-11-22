package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func activeRules(c M) []any {
	for _, v := range arr(c["rule_sets"]) {
		r := obj(v)
		if str(r["id"]) == str(c["active_rule_set"]) {
			return arr(r["rules"])
		}
	}
	return arr(c["custom_rules"])
}
func validateRuleSets(c M) error {
	sets := arr(c["rule_sets"])
	if len(sets) > 30 {
		return errors.New("规则集最多30个")
	}
	ids := map[string]bool{}
	for _, v := range sets {
		r := obj(v)
		format := str(r["source_format"])
		if format != "" && format != "json" && format != "domain-list" {
			return errors.New("规则源格式无效")
		}
		id := str(r["id"])
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(id) || ids[id] || len(str(r["name"])) == 0 || len(str(r["name"])) > 180 {
			return errors.New("规则集名称或标识无效")
		}
		ids[id] = true
		if p := str(r["profile"]); p != "bypass_cn" && p != "global" && p != "gfwlist" {
			return errors.New("规则集基础分流模式无效")
		}
		if e := validateRuleSetRules(r); e != nil {
			return e
		}
		if _, e := integer(first(r["interval_hours"], 24), 1, 168); e != nil {
			return e
		}
		if source := str(r["source"]); source != "" {
			u, e := url.Parse(source)
			if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
				return errors.New("规则更新地址必须为无认证的 HTTP(S) URL")
			}
		}
		if yes(r["auto_update"]) && str(r["source"]) == "" {
			return errors.New("自动更新需要配置规则源")
		}
	}
	if str(c["active_rule_set"]) != "" && !ids[str(c["active_rule_set"])] {
		return errors.New("活动规则集不存在")
	}
	return nil
}
func routingComparable(c M) M {
	out := clone(c)
	out["custom_rules"] = activeRules(c)
	delete(out, "rule_sets")
	delete(out, "active_rule_set")
	return out
}
func (a *App) updateRuleSet(ctx context.Context, j *Job, id string) error {
	a.mu.RLock()
	var previous M
	for _, v := range arr(a.cfg["rule_sets"]) {
		if str(obj(v)["id"]) == id {
			previous = clone(obj(v))
		}
	}
	a.mu.RUnlock()
	if previous == nil || str(previous["source"]) == "" {
		return errors.New("规则集没有更新地址")
	}
	a.mu.Lock()
	attempts := M{}
	_ = readJSON(filepath.Join(a.base, "ruleset-update-attempts.json"), &attempts)
	attempts[id] = now()
	_ = writeJSON(filepath.Join(a.base, "ruleset-update-attempts.json"), attempts)
	a.mu.Unlock()
	a.stage(j, "正在下载并校验规则集")
	child, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(child, "GET", str(previous["source"]), nil)
	if e != nil {
		return errors.New("规则源地址无效")
	}
	res, e := a.httpClient().Do(req)
	if e != nil {
		return errors.New("规则源下载失败，保留原规则")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("规则源响应异常，保留原规则")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024+1))
	if e != nil || len(raw) > 8*1024*1024 {
		return errors.New("规则源超过8MiB或未完整下载")
	}
	rules, e := decodeRuleSource(raw, previous)
	if e != nil {
		return e
	}

	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.RLock()
	sets := clone(arr(a.cfg["rule_sets"]))
	a.mu.RUnlock()
	found := false
	for i, v := range sets {
		r := obj(v)
		if str(r["id"]) == id {
			if fingerprint(r) != fingerprint(previous) {
				return errors.New("规则集已被编辑，取消过期更新")
			}
			r["rules"], r["updated"] = rules, now()
			sets[i] = r
			found = true
		}
	}
	if !found {
		return errors.New("规则集已删除，取消更新")
	}
	if e = a.saveXraySettingsLocked(M{"rule_sets": sets}, uiActionFromContext(ctx)); e != nil {
		return e
	}
	a.audit("rules.update", "规则集已更新", true, M{"id": id, "name": previous["name"], "count": len(rules)}, uiActionFromContext(ctx))
	return nil
}
func (a *App) ruleSetLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if !a.moduleEnabled("xray") {
				continue
			}
			a.mu.RLock()
			sets := []any{}
			for _, value := range arr(a.cfg["rule_sets"]) {
				r := obj(value)
				sets = append(sets, M{"id": r["id"], "auto_update": r["auto_update"], "updated": r["updated"], "interval_hours": r["interval_hours"]})
			}
			a.mu.RUnlock()
			attempts := M{}
			_ = readJSON(filepath.Join(a.base, "ruleset-update-attempts.json"), &attempts)
			for _, v := range sets {
				r := obj(v)
				if yes(r["auto_update"]) && now()-max(num(r["updated"]), num(attempts[str(r["id"])])) >= num(first(r["interval_hours"], 24))*3600 {
					_, _ = a.submitAutomatic("更新规则集", "rules.update", func(ctx context.Context, j *Job) error { return a.updateRuleSet(ctx, j, str(r["id"])) })
				}
			}
		}
	}
}

// builtinRuleView exposes only public routing selectors, never node connection data.
func (a *App) builtinRuleView(q url.Values) M {
	profile := q.Get("profile")
	rows := []M{}
	if profile != "bypass_cn" && profile != "gfwlist" && profile != "global" {
		return M{"error": "基础模式无效", "items": []M{}}
	}
	if profile == "gfwlist" {
		data := M{}
		if err := readJSON(filepath.Join(a.base, "rules", "gfwlist_domains.json"), &data); err != nil {
			return M{"error": "本机 GFWList 数据暂不可读取", "items": []M{}}
		}
		for _, kind := range []string{"direct", "proxy"} {
			for _, target := range arr(data[kind]) {
				rows = append(rows, M{"action": kind, "type": "域名及子域名", "target": str(target)})
			}
		}
		rows = append(rows, M{"action": "direct", "type": "默认规则", "target": "未匹配流量"})
	} else {
		if profile == "bypass_cn" {
			rows = append(rows, M{"action": "direct", "type": "系统直连标记", "target": "meta mark / ct mark 0x100cf"})
		}
		rows = append(rows, M{"action": "proxy", "type": "默认规则", "target": "其余已接管公网 TCP/UDP"})
	}
	filtered := []M{}
	query := strings.ToLower(strings.TrimSpace(q.Get("query")))
	for _, row := range rows {
		if query == "" || strings.Contains(strings.ToLower(str(row["target"])), query) {
			filtered = append(filtered, row)
		}
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	offset = max(0, min(offset, len(filtered)))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return M{"items": filtered[offset:min(len(filtered), offset+limit)], "total": len(filtered), "offset": offset, "limit": limit, "profile": profile}
}
