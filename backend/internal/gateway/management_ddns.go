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
	"strings"
	"sync"
	"time"
)

var ddnsTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,256}$`)

const ddnsCheckInterval = 5 * time.Minute

type ddnsEntry struct {
	Provider string `json:"provider"`
	Domain   string `json:"domain"`
	IPv4     string `json:"ipv4,omitempty"`
	Enabled  bool   `json:"enabled"`
	Token    string `json:"token"`
}

type ddnsResult struct {
	IP      string    `json:"ip,omitempty"`
	Checked time.Time `json:"checked"`
	Error   string    `json:"error,omitempty"`
}

type managementDDNS struct {
	base    string
	client  *http.Client
	mu      sync.Mutex
	entries map[string]ddnsEntry
	results map[string]ddnsResult
	running map[string]bool
	cancel  context.CancelFunc
	ctx     context.Context
}

func newManagementDDNS(base string, readonly bool) (*managementDDNS, error) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &managementDDNS{base: base, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, entries: map[string]ddnsEntry{}, results: map[string]ddnsResult{}, running: map[string]bool{}, cancel: cancel, ctx: ctx}
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
			rows = append(rows, M{"domain": domain, "provider": entry.Provider, "ipv4": entry.IPv4, "enabled": entry.Enabled, "result": d.results[domain], "running": d.running[domain]})
		}
		writeResponse(w, 200, M{"items": rows})
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
	if operation == "update" {
		err = d.update(r.Context(), domain, true)
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
		entry := ddnsEntry{Provider: str(input["provider"]), Domain: domain, IPv4: strings.TrimSpace(str(input["ipv4"])), Enabled: yes(input["enabled"]), Token: strings.TrimSpace(str(input["token"]))}
		old := d.entries[domain]
		if entry.Token == "" && entry.Provider == old.Provider {
			entry.Token = old.Token
		}
		if !ddnsTokenPattern.MatchString(entry.Token) || entry.Provider != "cloudflare" && entry.Provider != "duckdns" {
			writeResponse(w, 400, M{"error": "请选择服务商并填写有效令牌"})
			return
		}
		if entry.Provider == "duckdns" {
			label, ok := strings.CutSuffix(domain, ".duckdns.org")
			if !ok || strings.Contains(label, ".") {
				writeResponse(w, 400, M{"error": "DuckDNS 需要已注册的单级 duckdns.org 域名"})
				return
			}
		}
		if entry.IPv4 != "" {
			ip := net.ParseIP(entry.IPv4)
			if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
				writeResponse(w, 400, M{"error": "请输入公网 IPv4 地址或留空自动检测"})
				return
			}
			entry.IPv4 = ip.To4().String()
		}
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
	writeResponse(w, 200, M{"ok": true})
	if entry := next[domain]; entry.Enabled && operation == "save" {
		go func() { _ = d.update(d.ctx, domain, false) }()
	}
}

func (d *managementDDNS) updateEnabled(ctx context.Context) {
	d.mu.Lock()
	names := make([]string, 0, len(d.entries))
	for name, entry := range d.entries {
		if entry.Enabled {
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

func (d *managementDDNS) update(ctx context.Context, domain string, manual bool) (err error) {
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
	}()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result.IP = entry.IPv4
	if result.IP == "" {
		text, e := d.request(ctx, http.MethodGet, "https://api.ipify.org", "", nil)
		if e != nil {
			return errors.New("无法检测公网 IPv4")
		}
		ip := net.ParseIP(strings.TrimSpace(string(text)))
		if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return errors.New("公网 IPv4 检测结果无效")
		}
		result.IP = ip.To4().String()
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

func (d *managementDDNS) updateCloudflare(ctx context.Context, entry ddnsEntry, ip string) error {
	parts := strings.Split(entry.Domain, ".")
	zone := ""
	for i := 0; i < len(parts)-1; i++ {
		name := strings.Join(parts[i:], ".")
		found, err := d.cloudflareFind(ctx, "/zones?"+url.Values{"name": {name}, "status": {"active"}, "per_page": {"5"}}.Encode(), entry.Token)
		if err != nil {
			return err
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
		return errors.New("找不到可管理的 Cloudflare DNS 区域")
	}
	route := "/zones/" + url.PathEscape(zone) + "/dns_records"
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
