package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"net/http"
	"time"
)

func (g *ManagementSecurity) saveAccountSettings(w http.ResponseWriter, r *http.Request, actor, previousMode string) {
	input, err := managementInput(w, r, 8192)
	mode, account := str(input["mode"]), str(input["password_account"])
	hours, hoursOK := input["session_hours"].(float64)
	home, homeOK := input["home_monitor_enabled"].(bool)
	editUser, editOK := input["edit_user"].(bool)
	if err != nil || !validLoginMode(mode) || !hoursOK || hours < 1 || hours > 168 || hours != float64(int(hours)) || !homeOK || !editOK {
		writeResponse(w, 400, M{"error": "请选择有效的登录方式并填写 1～168 小时的会话时长"})
		return
	}
	oldName, name, password := str(input["old_name"]), str(input["username"]), str(input["password"])
	var passwordUser managementUser
	if editUser {
		if !usernamePattern.MatchString(name) || len(password) > 1024 || (oldName == "" && password == "") {
			writeResponse(w, 400, M{"error": "请填写有效的用户名和密码"})
			return
		}
		if password != "" {
			select {
			case g.logins <- struct{}{}:
				defer func() { <-g.logins }()
			default:
				writeResponse(w, 429, M{"error": "请求过多，请稍后重试"})
				return
			}
			passwordUser.Salt = make([]byte, 16)
			if _, err := rand.Read(passwordUser.Salt); err != nil {
				writeResponse(w, 500, M{"error": "无法保存账号"})
				return
			}
			passwordUser.Digest = passwordBackupKey(password, passwordUser.Salt)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if previousMode != g.mode {
		writeResponse(w, 409, M{"error": "登录方式已变化，请刷新"})
		return
	}
	if g.mode != "off" {
		cookie, err := r.Cookie(managementCookieName(r))
		if err != nil {
			writeResponse(w, 401, M{"error": "请重新登录"})
			return
		}
		session := g.sessions[sha256.Sum256([]byte(cookie.Value))]
		if session.Username != actor || !time.Now().Before(session.Expires) {
			writeResponse(w, 401, M{"error": "请重新登录"})
			return
		}
	}
	users := g.users
	if editUser {
		user, exists := users[oldName]
		_, taken := users[name]
		if (oldName != "" && !exists) || (name != oldName && taken) || (oldName == "" && len(users) >= 64) {
			writeResponse(w, 409, M{"error": "账号不存在、已占用或已达到账号上限"})
			return
		}
		if password != "" {
			user = passwordUser
		}
		users = make(map[string]managementUser, len(g.users)+1)
		for key, value := range g.users {
			if key != oldName {
				users[key] = value
			}
		}
		users[name] = user
		if oldName != "" && account == oldName {
			account = name
		}
	}
	if mode == "password" {
		if user, ok := users[account]; !ok || len(user.Digest) == 0 {
			writeResponse(w, 400, M{"error": "请选择已设置密码的账号"})
			return
		}
	} else {
		account = ""
	}
	prior := g.loginPolicy
	g.loginPolicy = loginPolicy{SessionHours: int(hours), HomeMonitor: home}
	if err := g.saveAccounts(users, mode, account); err != nil {
		g.loginPolicy = prior
		writeResponse(w, 500, M{"error": "无法保存账号与登录设置"})
		return
	}
	modeChanged := g.mode != mode || g.passwordAccount != account
	g.users, g.mode, g.passwordAccount = users, mode, account
	if modeChanged {
		g.generation++
		clear(g.sessions)
		clear(g.homeSessions)
	} else if editUser && oldName != "" {
		for id, session := range g.sessions {
			if session.Username == oldName {
				delete(g.sessions, id)
			}
		}
		for id, session := range g.homeSessions {
			if session.Username == oldName {
				delete(g.homeSessions, id)
			}
		}
	}
	if !home {
		clear(g.homeSessions)
	}
	writeResponse(w, 200, M{"ok": true, "relogin": modeChanged || (editUser && actor != "" && actor == oldName)})
}
