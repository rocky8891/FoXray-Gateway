package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ddnsTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,256}$`)

const ddnsCheckInterval = time.Minute

type ddnsEntry struct {
	Provider  string `json:"provider"`
	Domain    string `json:"domain"`
	IPv4      string `json:"ipv4,omitempty"`
	Source    string `json:"source,omitempty"`
	Interface string `json:"interface,omitempty"`
	Interval  int    `json:"interval_minutes,omitempty"`
	Enabled   bool   `json:"enabled"`
	Token     string `json:"token"`
}

type ddnsResult struct {
	IP      string    `json:"ip,omitempty"`
	Checked time.Time `json:"checked"`
	Error   string    `json:"error,omitempty"`
}

type managementAudit func(action, message string, ok bool, details M, uiAction ...string) error

type managementDDNS struct {
	audit   managementAudit
	base    string
	client  *http.Client
	mu      sync.Mutex
	entries map[string]ddnsEntry
	results map[string]ddnsResult
	running map[string]bool
	cancel  context.CancelFunc
	ctx     context.Context
}

func newManagementDDNS(base string, readonly bool, callbacks ...managementAudit) (*managementDDNS, error) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &managementDDNS{base: base, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, entries: map[string]ddnsEntry{}, results: map[string]ddnsResult{}, running: map[string]bool{}, cancel: cancel, ctx: ctx}
	if len(callbacks) > 0 {
		d.audit = callbacks[0]
	}
	if err := readJSON(d.configPath(), &d.entries); err != nil && !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, err
	}
	if d.entries == nil {
		d.entries = map[string]ddnsEntry{}
	}
	if !readonly {
		go func() {
			ticker := time.NewTicker(ddnsCheckInterval)
			defer ticker.Stop()
			d.updateEnabled(ctx)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					d.updateEnabled(ctx)
				}
			}
		}()
	}
	return d, nil
}

func (d *managementDDNS) configPath() string {
	return filepath.Join(d.base, "private", "management-ddns.json")
}
func validDDNSDomain(domain string) bool {
	if !certificateName.MatchString(domain) || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

func (d *managementDDNS) cloudflareToken(domain string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry := d.entries[strings.ToLower(domain)]
	if entry.Provider == "cloudflare" && ddnsTokenPattern.MatchString(entry.Token) {
		return entry.Token
	}
	return ""
}

func (e ddnsEntry) intervalMinutes() int {
	if e.Interval <= 0 {
		return 5
	}
	return e.Interval
}

func (e ddnsEntry) addressSource() string {
	if e.Source != "" {
		return e.Source
	}
	if e.IPv4 != "" {
		return "manual"
	}
	return "auto"
}

func ddnsInterfaces() []M {
	result := []M{}
	interfaces, _ := net.Interfaces()
	for _, nic := range interfaces {
		if nic.Flags&net.FlagUp == 0 || nic.Flags&net.FlagLoopback != 0 {
			continue
		}
		if ip, err := ddnsInterfaceIPv4(nic.Name); err == nil {
			result = append(result, M{"name": nic.Name, "ipv4": ip})
		}
	}
	return result
}

func ddnsInterfaceIPv4(name string) (string, error) {
	nic, err := net.InterfaceByName(name)
	if err == nil && nic.Flags&net.FlagUp != 0 {
		addresses, _ := nic.Addrs()
		for _, addr := range addresses {
			ip, _, _ := net.ParseCIDR(addr.String())
			if ip.To4() != nil && ip.IsGlobalUnicast() {
				return ip.To4().String(), nil
			}
		}
	}
	return "", errors.New("接口没有可用的 IPv4 地址")
}

func (d *managementDDNS) address(ctx context.Context, entry ddnsEntry) (string, error) {
	switch entry.addressSource() {
	case "disabled":
		return "", errors.New("IPv4 更新已禁用")
	case "manual":
		return entry.IPv4, nil
	case "interface":
		return ddnsInterfaceIPv4(entry.Interface)
	}
	text, err := d.request(ctx, http.MethodGet, "https://api.ipify.org", "", nil)
	if err != nil {
		return "", errors.New("无法检测公网 IPv4")
	}
	ip := net.ParseIP(strings.TrimSpace(string(text)))
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return "", errors.New("公网 IPv4 检测结果无效")
	}
	return ip.To4().String(), nil
}

func (d *managementDDNS) serve(w http.ResponseWriter, r *http.Request, readonly bool) {
	if r.Method == http.MethodGet {
		d.mu.Lock()
		defer d.mu.Unlock()
		names := make([]string, 0, len(d.entries))
		for domain := range d.entries {
			names = append(names, domain)
		}
		sort.Strings(names)
		rows := make([]M, 0, len(names))
		for _, domain := range names {
			entry := d.entries[domain]
			rows = append(rows, M{"domain": domain, "provider": entry.Provider, "ipv4": entry.IPv4, "source": entry.addressSource(), "interface": entry.Interface, "interval_minutes": entry.intervalMinutes(), "enabled": entry.Enabled, "result": d.results[domain], "running": d.running[domain]})
		}
		writeResponse(w, 200, M{"items": rows, "interfaces": ddnsInterfaces()})
		return
	}
	if r.Method != http.MethodPost || readonly {
		writeResponse(w, 405, M{"error": "当前不允许修改 DDNS"})
		return
	}
	input, err := managementInput(w, r, 8192)
	domain := strings.ToLower(strings.TrimSpace(str(input["domain"])))
	if err != nil || !validDDNSDomain(domain) {
		writeResponse(w, 400, M{"error": "请输入有效的域名"})
		return
	}
	operation := str(input["operation"])
	if operation == "test" {
		entry, err := d.parseEntry(domain, input)
		if err != nil {
			writeResponse(w, 400, M{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		ip := ""
		if entry.addressSource() != "disabled" {
			ip, err = d.address(ctx, entry)
		}
		message := "Cloudflare 令牌和 DNS 读取测试通过"
		if err == nil {
			if entry.Provider == "cloudflare" {
				var route string
				route, err = d.cloudflareRecordRoute(ctx, entry)
				if err == nil {
					_, err = d.cloudflareFind(ctx, route+"?"+url.Values{"type": {"A"}, "name": {entry.Domain}}.Encode(), entry.Token)
				}
			} else {
				_, err = d.request(ctx, http.MethodGet, "https://www.duckdns.org/", "", nil)
				message = "DuckDNS 可达，令牌在实际更新时校验"
			}
		}
		if err != nil {
			writeResponse(w, 400, M{"error": err.Error()})
			return
		}
		writeResponse(w, 200, M{"message": message, "ipv4": ip})
		return
	}
	if operation == "update" {
		err = d.update(r.Context(), domain, true, r.Header.Get("X-Foxray-UI-Action"))
		if err != nil {
			writeResponse(w, 400, M{"error": err.Error()})
			return
		}
		writeResponse(w, 200, M{"ok": true})
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running[domain] {
		writeResponse(w, 409, M{"error": "该域名正在更新，请稍后重试"})
		return
	}
	next := make(map[string]ddnsEntry, len(d.entries)+1)
	for name, entry := range d.entries {
		next[name] = entry
	}
	switch operation {
	case "delete":
		delete(next, domain)
	case "save":
		entry, err := d.parseEntryLocked(domain, input)
		if err != nil {
			writeResponse(w, 400, M{"error": err.Error()})
			return
		}
		old := d.entries[domain]
		if old.Domain == "" && len(next) >= 32 {
			writeResponse(w, 400, M{"error": "已达到 32 个 DDNS 域名上限"})
			return
		}
		next[domain] = entry
	default:
		writeResponse(w, 400, M{"error": "未知 DDNS 操作"})
		return
	}
	raw, err := json.Marshal(next)
	if err == nil {
		err = atomicFile(d.configPath(), raw, 0600)
	}
	if err != nil {
		writeResponse(w, 500, M{"error": "无法保存 DDNS 配置"})
		return
	}
	d.entries = next
	delete(d.results, domain)
	if d.audit != nil {
		message := "已保存 DDNS 配置"
		if operation == "delete" {
			message = "已删除 DDNS 配置"
		}
		_ = d.audit("ddns."+operation, message, true, M{"domain": domain}, r.Header.Get("X-Foxray-UI-Action"))
	}
	writeResponse(w, 200, M{"ok": true})
	if entry := next[domain]; entry.Enabled && entry.addressSource() != "disabled" && operation == "save" {
		go func() { _ = d.update(d.ctx, domain, false) }()
	}
}

func (d *managementDDNS) parseEntry(domain string, input M) (ddnsEntry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.parseEntryLocked(domain, input)
}

func (d *managementDDNS) parseEntryLocked(domain string, input M) (ddnsEntry, error) {
	entry := ddnsEntry{Domain: domain, Provider: str(input["provider"]), IPv4: strings.TrimSpace(str(input["ipv4"])), Source: str(input["source"]), Interface: str(input["interface"]), Enabled: yes(input["enabled"]), Token: strings.TrimSpace(str(input["token"]))}
	if raw := str(input["interval_minutes"]); raw != "" {
		minutes, err := strconv.Atoi(raw)
		if err != nil || minutes < 1 || minutes > 1440 {
			return entry, errors.New("更新间隔须为 1～1440 分钟")
		}
		entry.Interval = minutes
	}
	old := d.entries[domain]
	if entry.Token == "" && entry.Provider == old.Provider {
		entry.Token = old.Token
	}
	if !ddnsTokenPattern.MatchString(entry.Token) || entry.Provider != "cloudflare" && entry.Provider != "duckdns" {
		return entry, errors.New("请选择服务商并填写有效令牌")
	}
	if entry.Provider == "duckdns" {
		label, ok := strings.CutSuffix(domain, ".duckdns.org")
		if !ok || strings.Contains(label, ".") {
			return entry, errors.New("DuckDNS 需要已注册的单级 duckdns.org 域名")
		}
	}
	entry.Source = entry.addressSource()
	switch entry.Source {
	case "manual":
		ip := net.ParseIP(entry.IPv4)
		if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return entry, errors.New("请输入公网 IPv4 地址")
		}
		entry.IPv4 = ip.To4().String()
		entry.Interface = ""
	case "interface":
		if _, err := ddnsInterfaceIPv4(entry.Interface); err != nil {
			return entry, err
		}
		entry.IPv4 = ""
	case "auto", "disabled":
		entry.IPv4, entry.Interface = "", ""
	default:
		return entry, errors.New("请选择有效的 IPv4 地址来源")
	}
	return entry, nil
}

func (d *managementDDNS) updateEnabled(ctx context.Context) {
	d.mu.Lock()
	names := make([]string, 0, len(d.entries))
	for name, entry := range d.entries {
		if entry.Enabled && entry.addressSource() != "disabled" && time.Since(d.results[name].Checked) >= time.Duration(entry.intervalMinutes())*time.Minute {
			names = append(names, name)
		}
	}
	d.mu.Unlock()
	for _, name := range names {
		if ctx.Err() != nil {
			return
		}
		_ = d.update(ctx, name, false)
	}
}

func (d *managementDDNS) update(ctx context.Context, domain string, manual bool, uiAction ...string) (err error) {
	d.mu.Lock()
	entry, ok := d.entries[domain]
	if !ok || !manual && !entry.Enabled {
		d.mu.Unlock()
		return errors.New("此 DDNS 未配置或未启用")
	}
	if d.running[domain] {
		d.mu.Unlock()
		return errors.New("该域名正在更新")
	}
	previous := d.results[domain]
	d.running[domain] = true
	d.mu.Unlock()
	result := ddnsResult{}
	defer func() {
		result.Checked = time.Now().UTC()
		if err != nil {
			result.Error = err.Error()
		}
		d.mu.Lock()
		delete(d.running, domain)
		d.results[domain] = result
		d.mu.Unlock()
		if d.audit != nil && (manual || result.IP != previous.IP || result.Error != previous.Error || previous.Checked.IsZero()) {
			message := "DDNS 更新成功"
			source := "auto"
			if manual {
				source = "manual"
			}
			if err != nil {
				message = "DDNS 更新失败"
			}
			_ = d.audit("ddns.update", message, err == nil, M{"domain": domain, "provider": entry.Provider, "ipv4": result.IP, "source": source, "error": result.Error}, uiAction...)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result.IP, err = d.address(ctx, entry)
	if err != nil {
		return err
	}
	if !manual && previous.Error == "" && previous.IP == result.IP && entry.Provider == "duckdns" {
		return nil
	}
	switch entry.Provider {
	case "cloudflare":
		return d.updateCloudflare(ctx, entry, result.IP)
	case "duckdns":
		args := url.Values{"domains": {strings.TrimSuffix(entry.Domain, ".duckdns.org")}, "token": {entry.Token}, "ip": {result.IP}}
		text, e := d.request(ctx, http.MethodGet, "https://www.duckdns.org/update?"+args.Encode(), "", nil)
		if e != nil || strings.TrimSpace(string(text)) != "OK" {
			return errors.New("DuckDNS 更新失败，请检查域名和令牌")
		}
		return nil
	default:
		return errors.New("DDNS 服务商无效")
	}
}

func (d *managementDDNS) request(ctx context.Context, method, address, token string, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("无法构建 DDNS 请求")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, errors.New("无法连接 DDNS 服务商")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("DDNS 服务商拒绝请求")
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<10))
}

type cloudflareObject struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (d *managementDDNS) cloudflareFind(ctx context.Context, route, token string) ([]cloudflareObject, error) {
	raw, err := d.request(ctx, http.MethodGet, "https://api.cloudflare.com/client/v4"+route, token, nil)
	var response struct {
		Success bool               `json:"success"`
		Result  []cloudflareObject `json:"result"`
	}
	if err != nil || json.Unmarshal(raw, &response) != nil || !response.Success {
		return nil, errors.New("Cloudflare 查询失败，请检查 DNS 和区域权限")
	}
	return response.Result, nil
}

func (d *managementDDNS) cloudflareRecordRoute(ctx context.Context, entry ddnsEntry) (string, error) {
	parts := strings.Split(entry.Domain, ".")
	zone := ""
	for i := 0; i < len(parts)-1; i++ {
		name := strings.Join(parts[i:], ".")
		found, err := d.cloudflareFind(ctx, "/zones?"+url.Values{"name": {name}, "status": {"active"}, "per_page": {"5"}}.Encode(), entry.Token)
		if err != nil {
			return "", err
		}
		for _, item := range found {
			if item.Name == name && item.ID != "" {
				zone = item.ID
				break
			}
		}
		if zone != "" {
			break
		}
	}
	if zone == "" {
		return "", errors.New("找不到可管理的 Cloudflare DNS 区域")
	}
	return "/zones/" + url.PathEscape(zone) + "/dns_records", nil
}

func (d *managementDDNS) updateCloudflare(ctx context.Context, entry ddnsEntry, ip string) error {
	route, err := d.cloudflareRecordRoute(ctx, entry)
	if err != nil {
		return err
	}
	found, err := d.cloudflareFind(ctx, route+"?"+url.Values{"type": {"A"}, "name": {entry.Domain}, "per_page": {"100"}}.Encode(), entry.Token)
	if err != nil {
		return err
	}
	if len(found) > 1 {
		return errors.New("存在多条同名 A 记录，请保留唯一一条供 DDNS 管理")
	}
	method := http.MethodPost
	data := M{"type": "A", "name": entry.Domain, "content": ip, "ttl": 1, "proxied": false}
	if len(found) == 1 {
		if found[0].Name != entry.Domain || found[0].ID == "" {
			return errors.New("Cloudflare 返回的 A 记录无效")
		}
		if found[0].Content == ip {
			return nil
		}
		method = http.MethodPatch
		route += "/" + url.PathEscape(found[0].ID)
		data = M{"content": ip}
	}
	payload, _ := json.Marshal(data)
	raw, err := d.request(ctx, method, "https://api.cloudflare.com/client/v4"+route, entry.Token, payload)
	var response struct {
		Success bool `json:"success"`
	}
	if err != nil || json.Unmarshal(raw, &response) != nil || !response.Success {
		return errors.New("Cloudflare A 记录更新失败，请检查 DNS 编辑权限")
	}
	return nil
}
