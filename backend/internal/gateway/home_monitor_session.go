package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
)

type loginPolicy struct {
	SessionHours int  `json:"session_hours"`
	HomeMonitor  bool `json:"home_monitor_enabled"`
}

const homeMonitorIdle = 30 * time.Minute

func homeMonitorCookie(r *http.Request) string { return managementCookieName(r) + "_monitor" }

func (g *ManagementSecurity) saveLoginPolicy(w http.ResponseWriter, r *http.Request) {
	input, err := managementInput(w, r, 8192)
	hours, hoursOK := input["session_hours"].(float64)
	enabled, enabledOK := input["home_monitor_enabled"].(bool)
	if err != nil || !hoursOK || !enabledOK || hours < 1 || hours > 168 || hours != float64(int(hours)) {
		writeResponse(w, 400, M{"error": "登录会话保持时间须为 1～168 小时的整数"})
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	old := g.loginPolicy
	g.loginPolicy = loginPolicy{SessionHours: int(hours), HomeMonitor: enabled}
	if err := g.saveAccounts(g.users, g.mode, g.passwordAccount); err != nil {
		g.loginPolicy = old
		writeResponse(w, 500, M{"error": "无法保存登录设置"})
		return
	}
	if !enabled {
		clear(g.homeSessions)
	}
	writeResponse(w, 200, M{"ok": true})
}

func (g *ManagementSecurity) homeSession(r *http.Request, renew bool) bool {
	cookie, err := r.Cookie(homeMonitorCookie(r))
	if err != nil || len(cookie.Value) != 64 {
		return false
	}
	id := sha256.Sum256([]byte(cookie.Value))
	g.mu.Lock()
	defer g.mu.Unlock()
	session, ok := g.homeSessions[id]
	user, exists := g.users[session.Username]
	stamp := time.Now()
	if !g.loginPolicy.HomeMonitor || !ok || !exists || len(user.Digest) == 0 || !stamp.Before(session.Expires) {
		delete(g.homeSessions, id)
		return false
	}
	if renew {
		session.Expires = stamp.Add(homeMonitorIdle)
		g.homeSessions[id] = session
	}
	return true
}

func (g *ManagementSecurity) openHomeMonitor(w http.ResponseWriter, r *http.Request, username string) {
	w.Header().Set("Cache-Control", "no-store")
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.loginPolicy.HomeMonitor || username == "" || len(g.users[username].Digest) == 0 {
		writeResponse(w, 200, M{"enabled": false})
		return
	}
	// Recheck the management session under the same lock as permission issuance.
	cookie, err := r.Cookie(managementCookieName(r))
	if err != nil {
		writeResponse(w, 401, M{"error": "请先登录"})
		return
	}
	management := g.sessions[sha256.Sum256([]byte(cookie.Value))]
	stamp := time.Now()
	if management.Username != username || !stamp.Before(management.Expires) {
		writeResponse(w, 401, M{"error": "请先登录"})
		return
	}
	if cookie, err := r.Cookie(homeMonitorCookie(r)); err == nil {
		id := sha256.Sum256([]byte(cookie.Value))
		if session, ok := g.homeSessions[id]; ok && session.Username == username && stamp.Before(session.Expires) {
			session.Expires = stamp.Add(homeMonitorIdle)
			g.homeSessions[id] = session
			writeResponse(w, 200, M{"enabled": true})
			return
		}
		delete(g.homeSessions, id)
	}
	for id, session := range g.homeSessions {
		if !stamp.Before(session.Expires) {
			delete(g.homeSessions, id)
		}
	}
	if len(g.homeSessions) >= 256 {
		writeResponse(w, 503, M{"error": "常驻监控会话已达到上限"})
		return
	}
	var entropy [32]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		writeResponse(w, 500, M{"error": "无法启动常驻监控"})
		return
	}
	value := hex.EncodeToString(entropy[:])
	g.homeSessions[sha256.Sum256([]byte(value))] = managementSession{Username: username, Expires: stamp.Add(homeMonitorIdle)}
	http.SetCookie(w, &http.Cookie{Name: homeMonitorCookie(r), Value: value, Path: "/api/management/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	writeResponse(w, 200, M{"enabled": true})
}

func (g *ManagementSecurity) serveHomeMonitor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !g.homeSession(r, true) {
		writeResponse(w, 401, M{"error": "请先登录"})
		return
	}
	a := g.app
	host := M{}
	if a.host != nil && a.monitorModuleEnabled("host") {
		snapshot := a.host.snapshot()
		policy, current := obj(snapshot["policy"]), obj(snapshot["current"])
		if policy["enabled"] != false && current["supported"] != false {
			host = M{"load": current[str(policy["default_load"])], "cpu": current["cpu_percent"], "memory": current["memory_percent"]}
			for _, mount := range arr(current["mounts"]) {
				row := obj(mount)
				if row["path"] == policy["default_partition"] {
					host["disk"] = row["percent"]
					break
				}
			}
		}
	}
	modules := map[string]M{}
	for _, module := range []string{"xray", "warp", "wireguard"} {
		if a.moduleEnabled(module) && a.monitorModuleEnabled(module) {
			modules[module] = M{}
		}
	}
	if a.traffic != nil {
		for _, row := range a.traffic.snapshot()["items"].([]trafficReading) {
			if item := modules[trafficModule(row.ID)]; item != nil && row.Available {
				item["upload"] = num(item["upload"]) + row.UploadRate
				item["download"] = num(item["download"]) + row.DownloadRate
			}
		}
	}
	if a.connections != nil {
		counts := a.connections.counts()
		for module, item := range modules {
			if s := counts["module:"+module]; yes(s["available"]) {
				item["connections"] = s["count"]
			}
		}
	}
	writeResponse(w, 200, M{"host": host, "modules": modules, "time": now()})
}
