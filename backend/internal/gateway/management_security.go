package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type managementAuthorizedKey struct{}

func managementCookieName(r *http.Request) string {
	if r.TLS != nil {
		return "foxray_session"
	}
	return "foxray_http_session"
}

const defaultPasswordDigest = "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8"

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,31}$`)

type managementUser struct {
	Salt   []byte `json:"salt"`
	Digest []byte `json:"digest"`
}
type managementAccounts struct {
	Users           map[string]managementUser `json:"users"`
	Mode            string                    `json:"mode"`
	PasswordAccount string                    `json:"password_account,omitempty"`
}

type managementSession struct {
	Username string
	Expires  time.Time
}
type loginAttempt struct {
	Count int
	Until time.Time
}

type ManagementSecurity struct {
	app             *App
	mu              sync.Mutex
	users           map[string]managementUser
	mode            string
	passwordAccount string
	generation      uint64
	sessions        map[[32]byte]managementSession
	attempts        map[string]loginAttempt
	logins          chan struct{}
	tls             *managementTLS
	ddns            *managementDDNS
}

func NewManagementSecurity(app *App, address string) (*ManagementSecurity, error) {
	guard := &ManagementSecurity{app: app, sessions: make(map[[32]byte]managementSession), attempts: make(map[string]loginAttempt), mode: "account", logins: make(chan struct{}, 2)}
	raw, err := os.ReadFile(guard.userPath())
	if errors.Is(err, os.ErrNotExist) {
		guard.users = map[string]managementUser{"admin": {}}
	} else if err != nil {
		return nil, err
	} else {
		var saved managementAccounts
		if json.Unmarshal(raw, &saved) == nil && saved.Users != nil {
			guard.users, guard.mode, guard.passwordAccount = saved.Users, saved.Mode, saved.PasswordAccount
		} else if err = json.Unmarshal(raw, &guard.users); err != nil {
			return nil, errors.New("管理账号文件无效")
		}
	}
	if len(guard.users) == 0 || !validLoginMode(guard.mode) {
		return nil, errors.New("管理账号文件无效")
	}
	if guard.mode == "password" {
		if user, ok := guard.users[guard.passwordAccount]; !ok || len(user.Digest) == 0 {
			return nil, errors.New("仅密码登录账号无效")
		}
	}
	for name, user := range guard.users {
		if !usernamePattern.MatchString(name) || (len(user.Digest) != 32 || len(user.Salt) != 16) && !(name == "admin" && len(user.Digest) == 0 && len(user.Salt) == 0) {
			return nil, errors.New("管理账号文件含无效记录")
		}
	}
	guard.ddns, err = newManagementDDNS(app.base, app.readonly, app.audit)
	if err != nil {
		return nil, errors.New("无法加载 DDNS 配置")
	}
	guard.tls = newManagementTLS(app.base, address, guard, app.audit)
	guard.tls.ddns = guard.ddns
	return guard, nil
}

func (g *ManagementSecurity) userPath() string {
	return filepath.Join(g.app.base, "private", "management-users.json")
}

func validLoginMode(mode string) bool {
	return mode == "account" || mode == "password" || mode == "off"
}

func (g *ManagementSecurity) saveAccounts(users map[string]managementUser, mode, account string) error {
	payload, err := json.Marshal(managementAccounts{Users: users, Mode: mode, PasswordAccount: account})
	if err != nil {
		return err
	}
	return atomicFile(g.userPath(), payload, 0600)
}

func matchesManagementPassword(user managementUser, password string) bool {
	if len(password) == 0 || len(password) > 1024 {
		return false
	}
	if len(user.Digest) == 0 {
		sum := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(defaultPasswordDigest)) == 1
	}
	digest := passwordBackupKey(password, user.Salt)
	defer clear(digest)
	return subtle.ConstantTimeCompare(digest, user.Digest) == 1
}

func (g *ManagementSecurity) readSession(r *http.Request) (string, bool, bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.mode == "off" {
		return "", false, true, g.mode
	}
	cookie, err := r.Cookie(managementCookieName(r))
	if err != nil || len(cookie.Value) != 64 {
		return "", false, false, g.mode
	}
	id := sha256.Sum256([]byte(cookie.Value))
	session, ok := g.sessions[id]
	user, exists := g.users[session.Username]
	if !ok || !exists || time.Now().After(session.Expires) {
		delete(g.sessions, id)
		return "", false, false, g.mode
	}
	return session.Username, len(user.Digest) == 0, true, g.mode
}

func (g *ManagementSecurity) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	if g.tls != nil && g.tls.serveChallenge(w, r) {
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || !(ip.IsLoopback() || ip.IsPrivate()) {
		writeResponse(w, 403, M{"error": "仅允许受信任内网访问"})
		return
	}
	route := r.URL.Path
	if route == "/api/health" && r.Method == http.MethodGet {
		if r.TLS != nil || g.localHealthProbe(r, ip) {
			g.app.ServeHTTP(w, r)
			return
		}
	}
	if g.tls != nil && g.tls.handlePlainHTTP(w, r) {
		return
	}
	if strings.HasPrefix(filepath.Base(route), "foxray-config-") && (strings.HasSuffix(route, ".zip") || strings.HasSuffix(route, ".foxbackup")) {
		writeResponse(w, 404, M{"error": "备份请通过管理界面或 SFTP 获取"})
		return
	}
	if !strings.HasPrefix(route, "/api/") || route == "/api/health" && r.Method == http.MethodGet {
		g.app.ServeHTTP(w, r)
		return
	}
	if r.Method == http.MethodPost {
		origin, e := url.Parse(r.Header.Get("Origin"))
		kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if kind != "application/json" || e != nil || origin.Host != r.Host || origin.Scheme != scheme || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeResponse(w, 403, M{"error": "请求来源无效"})
			return
		}
	}
	username, initial, authenticated, mode := g.readSession(r)
	if route == "/api/management/session" && r.Method == http.MethodGet {
		writeResponse(w, 200, M{"authenticated": authenticated, "username": username, "initial": initial, "mode": mode})
		return
	}
	if route == "/api/management/login" && r.Method == http.MethodPost {
		g.login(w, r, host)
		return
	}
	// v0.9.48 authorizes updates before switching binaries and probes /api/state.
	if !authenticated && route == "/api/state" && r.Method == http.MethodGet && g.legacyUpgradeProbe(r, ip) {
		writeResponse(w, 200, M{"version": Version})
		return
	}
	if !authenticated {
		writeResponse(w, 401, M{"error": "请先登录"})
		return
	}
	if route == "/api/management/logout" && r.Method == http.MethodPost {
		cookie, _ := r.Cookie(managementCookieName(r))
		g.mu.Lock()
		if cookie != nil {
			delete(g.sessions, sha256.Sum256([]byte(cookie.Value)))
		}
		g.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: managementCookieName(r), Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
		writeResponse(w, 200, M{"ok": true})
		return
	}
	if route == "/api/management/users" && r.Method == http.MethodPost && !g.app.readonly {
		g.saveUser(w, r, username, initial)
		return
	}
	if initial {
		writeResponse(w, 403, M{"error": "请先设置新的管理密码"})
		return
	}
	if route == "/api/management/login-mode" && r.Method == http.MethodPost && !g.app.readonly {
		g.changeLoginMode(w, r, username, mode)
		return
	}
	if route == "/api/management/users" && r.Method == http.MethodGet {
		g.mu.Lock()
		users := make([]string, 0, len(g.users))
		for name := range g.users {
			users = append(users, name)
		}
		passwordAccount := g.passwordAccount
		g.mu.Unlock()
		sort.Strings(users)
		writeResponse(w, 200, M{"users": users, "current": username, "mode": mode, "password_account": passwordAccount})
		return
	}
	if route == "/api/management/ddns" {
		g.ddns.serve(w, r, g.app.readonly)
		return
	}
	if route == "/api/management/https" {
		g.tls.serve(w, r, g.app.readonly)
		return
	}
	if route == "/api/config-backup" && r.Method == http.MethodGet {
		writeResponse(w, 200, M{"ready": !g.app.readonly})
		return
	}
	if route == "/api/config-backup/export" && r.Method == http.MethodPost && !g.app.readonly {
		input, err := managementInput(w, r, 8192)
		if err != nil {
			writeResponse(w, 400, M{"error": "备份参数无效"})
			return
		}
		g.app.exportConfigBackup(w, str(input["password"]), str(input["directory"]))
		return
	}
	g.app.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), managementAuthorizedKey{}, true)))
}

func (g *ManagementSecurity) localHealthProbe(r *http.Request, peer net.IP) bool {
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return false
	}
	host, _, err := net.SplitHostPort(local.String())
	return err == nil && peer.Equal(net.ParseIP(host))
}

func (g *ManagementSecurity) legacyUpgradeProbe(r *http.Request, peer net.IP) bool {
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return false
	}
	host, _, err := net.SplitHostPort(local.String())
	if err != nil || !peer.Equal(net.ParseIP(host)) {
		return false
	}
	plan := M{}
	if readJSON(filepath.Join(g.app.base, "private", "manager-upgrade-plan.json"), &plan) != nil {
		return false
	}
	return str(plan["version"]) == Version && now()-num(plan["created_at"]) < 600 && str(plan["health"]) == "http://"+r.Host+r.URL.Path
}

func managementInput(w http.ResponseWriter, r *http.Request, limit int64) (M, error) {
	var input M
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(&input)
	return input, err
}

func (g *ManagementSecurity) changeLoginMode(w http.ResponseWriter, r *http.Request, actor, previousMode string) {
	input, err := managementInput(w, r, 8192)
	mode, account := str(input["mode"]), str(input["password_account"])
	if err != nil || !validLoginMode(mode) {
		writeResponse(w, 400, M{"error": "请选择有效的登录方式"})
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, actorExists := g.users[actor]
	if previousMode != g.mode || (g.mode != "off" && !actorExists) {
		writeResponse(w, 409, M{"error": "登录方式已变化，请刷新"})
		return
	}
	if mode == "password" {
		if user, ok := g.users[account]; !ok || len(user.Digest) == 0 {
			writeResponse(w, 400, M{"error": "请选择已设置密码的账号"})
			return
		}
	} else {
		account = ""
	}
	if mode != g.mode || account != g.passwordAccount {
		if err = g.saveAccounts(g.users, mode, account); err != nil {
			writeResponse(w, 500, M{"error": "无法保存登录方式"})
			return
		}
		g.mode, g.passwordAccount = mode, account
		g.generation++
		clear(g.sessions)
	}
	writeResponse(w, 200, M{"ok": true})
}

func (g *ManagementSecurity) login(w http.ResponseWriter, r *http.Request, host string) {
	select {
	case g.logins <- struct{}{}:
		defer func() { <-g.logins }()
	default:
		writeResponse(w, 429, M{"error": "登录请求过多，请稍后重试"})
		return
	}
	input, err := managementInput(w, r, 8192)
	if err != nil {
		writeResponse(w, 400, M{"error": "登录参数无效"})
		return
	}
	name, password := str(input["username"]), str(input["password"])
	now := time.Now()
	g.mu.Lock()
	mode, generation := g.mode, g.generation
	if mode == "off" {
		g.mu.Unlock()
		writeResponse(w, 200, M{"authenticated": true, "username": "", "initial": false, "mode": mode})
		return
	}
	if mode == "password" {
		name = g.passwordAccount
	}
	for ip, entry := range g.attempts {
		if now.After(entry.Until) {
			delete(g.attempts, ip)
		}
	}
	attempt := g.attempts[host]
	if attempt.Count >= 10 || len(g.attempts) >= 1024 {
		g.mu.Unlock()
		writeResponse(w, 429, M{"error": "登录尝试过多，请稍后重试"})
		return
	}
	if attempt.Count == 0 {
		attempt.Until = now.Add(time.Minute)
	}
	attempt.Count++
	g.attempts[host] = attempt
	user, exists := g.users[name]
	g.mu.Unlock()
	if !exists || !matchesManagementPassword(user, password) {
		writeResponse(w, 401, M{"error": "用户名或密码错误"})
		return
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		writeResponse(w, 500, M{"error": "无法建立会话"})
		return
	}
	value := hex.EncodeToString(random)
	g.mu.Lock()
	defer g.mu.Unlock()
	current, ok := g.users[name]
	if !ok || generation != g.generation || subtle.ConstantTimeCompare(user.Digest, current.Digest) != 1 {
		writeResponse(w, 409, M{"error": "账号已更新，请重新登录"})
		return
	}
	delete(g.attempts, host)
	for id, session := range g.sessions {
		if now.After(session.Expires) {
			delete(g.sessions, id)
		}
	}
	if len(g.sessions) >= 256 {
		writeResponse(w, 503, M{"error": "登录会话已达到上限"})
		return
	}
	g.sessions[sha256.Sum256([]byte(value))] = managementSession{name, now.Add(8 * time.Hour)}
	http.SetCookie(w, &http.Cookie{Name: managementCookieName(r), Value: value, Path: "/", MaxAge: 8 * 3600, SameSite: http.SameSiteStrictMode, HttpOnly: true, Secure: r.TLS != nil})
	writeResponse(w, 200, M{"authenticated": true, "username": name, "initial": len(user.Digest) == 0, "mode": mode})
}

func (g *ManagementSecurity) saveUser(w http.ResponseWriter, r *http.Request, actor string, initial bool) {
	input, err := managementInput(w, r, 8192)
	oldName, name, password := str(input["old_name"]), str(input["username"]), str(input["password"])
	invalid := err != nil || !usernamePattern.MatchString(name) || (password != "" && len(password) > 1024)
	if invalid || initial && (oldName != actor || password == "") || oldName == "" && password == "" {
		writeResponse(w, 400, M{"error": "请填写有效的用户名和非空密码"})
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
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.users[actor]; !ok && !(actor == "" && g.mode == "off") {
		writeResponse(w, 401, M{"error": "请重新登录"})
		return
	}
	user, exists := g.users[oldName]
	_, taken := g.users[name]
	if (oldName != "" && !exists) || (name != oldName && taken) || (oldName == "" && len(g.users) >= 64) {
		writeResponse(w, 409, M{"error": "账号不存在、已占用或已达到账号上限"})
		return
	}
	if oldName == name && password == "" {
		writeResponse(w, 200, M{"ok": true})
		return
	}
	if password != "" {
		user.Salt = make([]byte, 16)
		if _, err = rand.Read(user.Salt); err != nil {
			writeResponse(w, 500, M{"error": "无法保存账号"})
			return
		}
		user.Digest = passwordBackupKey(password, user.Salt)
	}
	next := make(map[string]managementUser, len(g.users)+1)
	for key, value := range g.users {
		if key != oldName {
			next[key] = value
		}
	}
	next[name] = user
	account := g.passwordAccount
	if account != "" && account == oldName {
		account = name
	}
	err = g.saveAccounts(next, g.mode, account)
	if err != nil {
		writeResponse(w, 500, M{"error": "无法保存账号文件"})
		return
	}
	g.users, g.passwordAccount = next, account
	for id, session := range g.sessions {
		if session.Username == oldName {
			delete(g.sessions, id)
		}
	}
	writeResponse(w, 200, M{"ok": true, "relogin": actor != "" && oldName == actor})
}

func (g *ManagementSecurity) Close() { g.ddns.cancel(); g.tls.close() }

func managementPort(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ":18443"
	}
	return net.JoinHostPort(host, "18443")
}
