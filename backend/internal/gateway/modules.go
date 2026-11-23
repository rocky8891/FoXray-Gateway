package gateway

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func (a *App) moduleEnabled(name string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return yes(a.components[name])
}
func (a *App) moduleRequestAllowed(path string, data M) error {
	name := ""
	if strings.HasPrefix(path, "/api/warp/") {
		name = "warp"
	}
	for _, prefix := range []string{"/api/native/", "/api/subscriptions/", "/api/nodes/import/", "/api/backups/"} {
		if strings.HasPrefix(path, prefix) {
			name = "xray"
		}
	}
	if path == "/api/native/activate" && str(data["id"]) == "warp" {
		name = "warp"
	}
	if strings.HasPrefix(path, "/api/wireguard/") {
		name = "wireguard"
	}
	if path == "/api/proxy" || path == "/api/config" || path == "/api/traffic/enable" {
		name = "xray"
	}
	if strings.HasPrefix(path, "/api/services/") {
		name = str(data["service"])
	}
	if strings.HasPrefix(path, "/api/network/") && str(data["kind"]) == "wireguard" {
		name = "wireguard"
	}
	if name != "" && !a.moduleEnabled(name) {
		return errors.New("相关功能模块已停用，请在全局设置的功能管理中启用")
	}
	return nil
}
func presentFile(path string) bool {
	stat, e := os.Stat(path)
	return e == nil && stat.Mode().IsRegular()
}
func installedPath(paths ...string) string {
	for _, p := range paths {
		if strings.Contains(p, "/") {
			if presentFile(p) {
				return p
			}
		} else if v, e := exec.LookPath(p); e == nil {
			return v
		}
	}
	return ""
}
func (a *App) moduleInventory() M {
	a.mu.RLock()
	config := clone(a.components)
	a.mu.RUnlock()
	result := M{}
	for _, name := range []string{"xray", "warp", "wireguard", "komari"} {
		binary, unit := "", ""
		switch name {
		case "xray":
			binary = installedPath(filepath.Join(a.base, "xray"), "xray", "/usr/local/bin/xray")
			unit = "foxray-engine"
		case "warp":
			binary = installedPath("warp-cli")
			unit = "warp-svc"
		case "wireguard":
			binary = installedPath("wg")
			unit = "wg-quick@"
		}
		row := M{"enabled": yes(config[name]), "installed": binary != "", "path": binary, "unit": unit}
		if name == "komari" {
			row["url"] = str(config["komari_url"])
			row["manual_only"] = true
		}
		result[name] = row
	}
	return M{"modules": result, "note": "停用模块仅停止相关管理与自动任务，不卸载、停止服务或删除 VPN 配置"}
}
func normalizeKomariURL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Komari URL 须为不含认证信息、查询参数和片段的 HTTP/HTTPS 地址")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	return u.String(), nil
}
func probeKomari(ctx context.Context, raw string) (M, error) {
	address, e := normalizeKomariURL(raw)
	if e != nil {
		return nil, e
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.User != nil || req.URL.RawQuery != "" {
			return errors.New("Komari 跳转不可验证")
		}
		return nil
	}}
	req, _ := http.NewRequestWithContext(ctx, "GET", address+"api/version", nil)
	response, e := client.Do(req)
	if e == nil {
		defer response.Body.Close()
		data, _ := limitedRead(response.Body, 65536)
		m := M{}
		_ = json.Unmarshal(data, &m)
		meta := m
		if obj(m["data"])["version"] != nil {
			meta = obj(m["data"])
		}
		if response.StatusCode == 200 && meta["version"] != nil && meta["hash"] != nil {
			return M{"url": address, "reachable": true, "recognized": true}, nil
		}
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", address, nil)
	response, e = client.Do(req)
	if e != nil {
		return nil, errors.New("Komari 地址连接失败，请确认 URL 和服务监听地址")
	}
	defer response.Body.Close()
	data, _ := limitedRead(response.Body, 262144)
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return nil, errors.New("Komari 页面未返回可用响应")
	}
	return M{"url": address, "reachable": true, "recognized": strings.Contains(strings.ToLower(string(data)), "komari")}, nil
}
func (a *App) changeModule(ctx context.Context, j *Job, data M) error {
	a.moduleMu.Lock()
	defer a.moduleMu.Unlock()
	name := str(data["module"])
	if name != "xray" && name != "warp" && name != "wireguard" && name != "komari" {
		return errors.New("模块名称无效")
	}
	enabled := yes(data["enabled"])
	a.mu.RLock()
	next := clone(a.components)
	a.mu.RUnlock()
	if name == "komari" && str(data["url"]) != "" {
		address, e := normalizeKomariURL(str(data["url"]))
		if e != nil {
			return e
		}
		next["komari_url"] = address
	}
	if enabled {
		if name == "komari" {
			raw := str(data["url"])
			if raw == "" {
				raw = str(next["komari_url"])
			}
			if raw == "" {
				raw = "http://127.0.0.1:25774/"
			}
			a.stage(j, "检测 Komari URL，不安装或修改 Komari 服务")
			check, e := probeKomari(ctx, raw)
			if e != nil {
				return e
			}
			if !yes(check["recognized"]) {
				return errors.New("URL 可访问，但未识别为 Komari；请检查所填地址")
			}
			next["komari_url"] = check["url"]
		} else if e := a.ensureModule(ctx, j, name, yes(data["accept_terms"]) || name == "warp" && yes(next["warp_terms_accepted"])); e != nil {
			return e
		}
	}
	next[name] = enabled
	next["version"] = 2
	if name == "warp" && yes(data["accept_terms"]) {
		next["warp_terms_accepted"] = true
	}
	if e := writeJSON(filepath.Join(a.base, "components.json"), next); e != nil {
		return e
	}
	a.mu.Lock()
	a.components = next
	if name == "warp" && enabled {
		found := false
		for _, v := range arr(a.cfg["endpoints"]) {
			if str(obj(v)["id"]) == "warp" {
				found = true
			}
		}
		if !found {
			a.cfg["endpoints"] = append(arr(a.cfg["endpoints"]), M{"id": "warp", "name": "Cloudflare WARP", "kind": "warp", "enabled": true, "priority": 30})
		}
		if e := a.persist(); e != nil {
			a.mu.Unlock()
			return e
		}
	}
	a.mu.Unlock()
	a.refreshSnapshot()
	a.audit("module.updated", "功能模块设置已更新", true, M{"module": name, "enabled": enabled}, uiActionFromContext(ctx))
	a.stage(j, map[bool]string{true: "模块已启用，已有服务和配置保留", false: "管理功能已停用；现有服务与连接保持"}[enabled])
	return nil
}
func (a *App) moduleRun(ctx context.Context, args ...string) error {
	child, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_, e := a.runner.Run(child, args, "")
	if e != nil {
		return errors.New("模块部署命令执行失败：" + args[0])
	}
	return nil
}
func (a *App) ensureModule(ctx context.Context, j *Job, name string, terms bool) error {
	if runtime.GOOS != "linux" {
		return errors.New("自动部署模块仅支持 Linux")
	}
	a.stage(j, "检查已有安装与服务，避免重复部署")
	switch name {
	case "xray":
		path := installedPath(filepath.Join(a.base, "xray"), "xray", "/usr/local/bin/xray")
		unit := "/etc/systemd/system/foxray-engine.service"
		if !presentFile(unit) {
			for _, other := range []string{"xray.service", "v2ray.service"} {
				state := a.service(other)
				loaded, _ := a.command(5, "", "systemctl", "show", other, "-p", "LoadState", "--value")
				if strings.TrimSpace(loaded) == "loaded" || str(state["state"]) == "active" {
					return errors.New("检测到其他 Xray/V2Ray 服务正在运行；保留现有配置，未重复部署。请先完成该服务的迁移")
				}
			}
		}
		if path == "" {
			a.stage(j, "正在下载并校验 Xray 官方稳定版")
			if e := a.installXrayModule(ctx); e != nil {
				return e
			}
		} else if path != filepath.Join(a.base, "xray") {
			if e := os.Symlink(path, filepath.Join(a.base, "xray")); e != nil {
				return errors.New("已有 Xray 二进制未能纳管，未覆盖原文件")
			}
		}
		if _, e := exec.LookPath("useradd"); e == nil {
			if _, e := a.command(5, "", "id", "vpm-engine"); e != nil {
				if e = a.moduleRun(ctx, "useradd", "--system", "--user-group", "--no-create-home", "--shell", "/sbin/nologin", "vpm-engine"); e != nil {
					return e
				}
			}
		}
		if !presentFile(unit) {
			body := "[Unit]\nDescription=FoXray Xray-core\nAfter=network-online.target\nConditionPathExists=" + a.base + "/engine.json\n[Service]\nUser=vpm-engine\nGroup=vpm-engine\nAmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE\nExecStart=" + a.base + "/xray run -c " + a.base + "/engine.json\nExecStopPost=-/usr/sbin/nft delete table inet vpm\nRestart=on-failure\n[Install]\nWantedBy=multi-user.target\n"
			if e := atomicFile(unit, []byte(body), 0644); e != nil {
				return e
			}
			return a.moduleRun(ctx, "systemctl", "daemon-reload")
		}
	case "wireguard":
		if installedPath("wg") == "" || installedPath("wg-quick") == "" {
			a.stage(j, "正在安装 WireGuard 工具；不创建或启动 VPN 接口")
			if e := a.installPackages(ctx, "wireguard-tools"); e != nil {
				return e
			}
		}
		quick := installedPath("wg-quick")
		if quick == "" {
			return errors.New("WireGuard 工具未安装完整")
		}
		for _, path := range []string{"/etc/systemd/system/wg-quick@.service", "/usr/lib/systemd/system/wg-quick@.service", "/lib/systemd/system/wg-quick@.service"} {
			if presentFile(path) {
				return nil
			}
		}
		unit := "[Unit]\nDescription=WireGuard via wg-quick (%i)\nAfter=network-online.target\nWants=network-online.target\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + quick + " up %i\nExecStop=" + quick + " down %i\n[Install]\nWantedBy=multi-user.target\n"
		if e := atomicFile("/etc/systemd/system/wg-quick@.service", []byte(unit), 0644); e != nil {
			return e
		}
		return a.moduleRun(ctx, "systemctl", "daemon-reload")
	case "warp":
		if installedPath("warp-cli") == "" {
			if !terms {
				return errors.New("首次安装 WARP 需要勾选接受 Cloudflare 服务条款")
			}
			if _, e := exec.LookPath("dnf"); e == nil {
				req, _ := http.NewRequestWithContext(ctx, "GET", "https://pkg.cloudflareclient.com/cloudflare-warp-ascii.repo", nil)
				response, e := a.httpClient().Do(req)
				if e != nil {
					return errors.New("Cloudflare 软件源下载失败")
				}
				defer response.Body.Close()
				raw, e := limitedRead(response.Body, 65536)
				if e != nil || response.StatusCode != 200 {
					return errors.New("Cloudflare 软件源不可用")
				}
				if !presentFile("/etc/yum.repos.d/cloudflare-warp.repo") {
					if e = atomicFile("/etc/yum.repos.d/cloudflare-warp.repo", raw, 0644); e != nil {
						return e
					}
				}
			} else if _, e := exec.LookPath("apt-get"); e == nil {
				raw, _ := os.ReadFile("/etc/os-release")
				code := ""
				for _, line := range textLines(string(raw)) {
					if strings.HasPrefix(line, "VERSION_CODENAME=") {
						code = strings.Trim(strings.TrimPrefix(line, "VERSION_CODENAME="), "\"")
					}
				}
				if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(code) {
					return errors.New("无法识别系统发行代号，请先手动配置 Cloudflare 官方仓库")
				}
				req, _ := http.NewRequestWithContext(ctx, "GET", "https://pkg.cloudflareclient.com/pubkey.gpg", nil)
				response, err := a.httpClient().Do(req)
				if err != nil {
					return errors.New("Cloudflare 签名公钥下载失败")
				}
				defer response.Body.Close()
				key, err := limitedRead(response.Body, 65536)
				if err != nil || response.StatusCode != 200 {
					return errors.New("Cloudflare 签名公钥不可用")
				}
				path := "/usr/share/keyrings/foxray-cloudflare-warp.asc"
				if !strings.Contains(string(key), "BEGIN PGP PUBLIC KEY BLOCK") {
					path = "/usr/share/keyrings/foxray-cloudflare-warp.gpg"
				}
				if !presentFile(path) {
					if err = atomicFile(path, key, 0644); err != nil {
						return err
					}
				}
				source := "/etc/apt/sources.list.d/foxray-cloudflare-warp.list"
				if !presentFile(source) {
					if err = atomicFile(source, []byte("deb [signed-by="+path+"] https://pkg.cloudflareclient.com/ "+code+" main\n"), 0644); err != nil {
						return err
					}
				}
			} else {
				return errors.New("未找到支持的包管理器，请先安装 Cloudflare WARP 客户端")
			}
			if str(a.service("warp-svc")["state"]) == "active" {
				return errors.New("已有 WARP 服务运行中，未覆盖现有部署")
			}
			if e := a.moduleRun(ctx, "systemctl", "mask", "--runtime", "warp-svc.service"); e != nil {
				return e
			}
			defer func() { _ = a.moduleRun(context.Background(), "systemctl", "unmask", "--runtime", "warp-svc.service") }()
			a.stage(j, "正在安装 Cloudflare WARP；安装阶段禁止自动启动主机隧道")
			if e := a.installPackages(ctx, "cloudflare-warp"); e != nil {
				return e
			}
		}
		if e := a.ensureScout(ctx, j); e != nil {
			return e
		}
		if presentFile("/run/netns/foxray-warp") {
			return a.verifyWarp()
		}
		if _, e := os.Stat("/run/netns/foxray-warp"); e == nil {
			return a.verifyWarp()
		}
		// A running host tunnel must never be silently moved into a namespace.
		if str(a.service("warp-svc")["state"]) == "active" {
			return errors.New("检测到未隔离的主机 WARP 服务；为保留 VPN 与现有连接，未自动迁移，请先完成隧道隔离")
		}
		prep := "/etc/systemd/system/foxray-warp-netns.service"
		if !presentFile(prep) {
			if e := atomicFile(prep, []byte("[Unit]\nDescription=FoXray isolated WARP namespace\nAfter=network-online.target\n[Service]\nType=oneshot\nExecStart="+a.base+"/foxray --warp-network\nRemainAfterExit=yes\n"), 0644); e != nil {
				return e
			}
		}
		dnsFile := "/etc/netns/foxray-warp/resolv.conf"
		if !presentFile(dnsFile) {
			if e := os.MkdirAll(filepath.Dir(dnsFile), 0755); e != nil {
				return e
			}
			if e := atomicFile(dnsFile, []byte("nameserver 223.5.5.5\nnameserver 1.1.1.1\n"), 0644); e != nil {
				return e
			}
		}
		dropin := "/etc/systemd/system/warp-svc.service.d/foxray-netns.conf"
		if !presentFile(dropin) {
			if e := os.MkdirAll(filepath.Dir(dropin), 0755); e != nil {
				return e
			}
			if e := atomicFile(dropin, []byte("[Unit]\nRequires=foxray-warp-netns.service\nAfter=foxray-warp-netns.service\n[Service]\nNetworkNamespacePath=/run/netns/foxray-warp\nBindPaths=/etc/netns/foxray-warp/resolv.conf:/etc/resolv.conf\nTemporaryFileSystem=/run/dbus:ro /run/NetworkManager:ro\nPrivateMounts=yes\n"), 0644); e != nil {
				return e
			}
		}
		return a.moduleRun(ctx, "systemctl", "daemon-reload")
	default:
		return errors.New("此模块只能手动部署")
	}
	return nil
}
func (a *App) installPackages(ctx context.Context, packages ...string) error {
	if _, e := exec.LookPath("dnf"); e == nil {
		return a.moduleRun(ctx, append([]string{"dnf", "install", "-y"}, packages...)...)
	}
	if _, e := exec.LookPath("apt-get"); e == nil {
		if e = a.moduleRun(ctx, "apt-get", "update"); e != nil {
			return e
		}
		return a.moduleRun(ctx, append([]string{"apt-get", "install", "-y"}, packages...)...)
	}
	return errors.New("未找到支持的包管理器，请先安装服务后重新检测")
}
func (a *App) installXrayModule(ctx context.Context) error {
	asset := map[string]string{"amd64": "Xray-linux-64.zip", "arm64": "Xray-linux-arm64-v8a.zip"}[runtime.GOARCH]
	if asset == "" {
		return errors.New("Xray 自动安装仅支持 amd64/arm64")
	}
	fetch := func(address string, limit int64) ([]byte, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
		client := a.httpClient()
		client.Timeout = 5 * time.Minute
		r, e := client.Do(req)
		if e != nil {
			return nil, errors.New("官方安装资源连接失败")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return nil, errors.New("官方安装资源不可用")
		}
		return limitedRead(r.Body, limit)
	}
	raw, e := fetch("https://api.github.com/repos/XTLS/Xray-core/releases/latest", 1048576)
	if e != nil {
		return e
	}
	release := M{}
	if json.Unmarshal(raw, &release) != nil || yes(release["prerelease"]) {
		return errors.New("稳定版元数据无效")
	}
	address, digest := "", ""
	for _, v := range arr(release["assets"]) {
		m := obj(v)
		if str(m["name"]) == asset {
			address, digest = str(m["browser_download_url"]), str(m["digest"])
		}
	}
	if !strings.HasPrefix(address, "https://github.com/XTLS/Xray-core/releases/download/") || !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		return errors.New("官方资源缺少可靠 SHA256 校验")
	}
	raw, e = fetch(address, 100*1024*1024)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != strings.TrimPrefix(digest, "sha256:") {
		return errors.New("Xray SHA256 校验失败")
	}
	z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		return e
	}
	for _, f := range z.File {
		if f.Name == "xray" {
			r, e := f.Open()
			if e != nil {
				return e
			}
			defer r.Close()
			binary, e := limitedRead(r, 150*1024*1024)
			if e != nil {
				return e
			}
			return atomicFile(filepath.Join(a.base, "xray"), binary, 0755)
		}
	}
	return errors.New("官方安装包缺少 xray")
}

// EnsureModule is shared by the release installer and the Web module manager.
func EnsureModule(base, name string, terms bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	a := &App{base: base, ctx: ctx, runner: SystemRunner{}, cfg: M{}, native: M{}}
	return a.ensureModule(ctx, &Job{}, name, terms)
}

func CheckKomari(address string) (M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return probeKomari(ctx, address)
}
func NormalizeKomariURL(address string) (string, error) { return normalizeKomariURL(address) }

func (a *App) ensureScout(ctx context.Context, j *Job) error {
	path := filepath.Join(a.base, "probe-tools", "warpscout")
	if !presentFile(path) {
		a.stage(j, "正在安装隔离端点检测工具 WARPSCOUT，不切换当前隧道")
		arch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
		if arch == "" {
			return errors.New("端点工具不支持当前架构")
		}
		fetch := func(address string, max int64) ([]byte, error) {
			req, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
			client := a.httpClient()
			client.Timeout = 5 * time.Minute
			r, e := client.Do(req)
			if e != nil {
				return nil, errors.New("WARPSCOUT 官方资源连接失败")
			}
			defer r.Body.Close()
			if r.StatusCode != 200 {
				return nil, errors.New("WARPSCOUT 官方资源不可用")
			}
			return limitedRead(r.Body, max)
		}
		raw, e := fetch("https://api.github.com/repos/vernette/warpscout/releases/tags/v0.16.1", 1048576)
		if e != nil {
			return e
		}
		release := M{}
		if json.Unmarshal(raw, &release) != nil {
			return errors.New("WARPSCOUT 元数据无效")
		}
		asset := "warpscout_0.16.1_linux_" + arch + ".tar.gz"
		address, digest := "", ""
		for _, v := range arr(release["assets"]) {
			m := obj(v)
			if str(m["name"]) == asset {
				address, digest = str(m["browser_download_url"]), str(m["digest"])
			}
		}
		if !strings.HasPrefix(address, "https://github.com/vernette/warpscout/releases/download/") || !strings.HasPrefix(digest, "sha256:") {
			return errors.New("WARPSCOUT 资源缺少可靠校验")
		}
		raw, e = fetch(address, 100*1024*1024)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != strings.TrimPrefix(digest, "sha256:") {
			return errors.New("WARPSCOUT 校验失败")
		}
		gz, e := gzip.NewReader(bytes.NewReader(raw))
		if e != nil {
			return e
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		found := false
		for {
			header, e := reader.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if header.Name == "warpscout" && header.Typeflag == tar.TypeReg {
				binary, e := limitedRead(reader, 150*1024*1024)
				if e != nil {
					return e
				}
				if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
					return e
				}
				if e = atomicFile(path, binary, 0755); e != nil {
					return e
				}
				found = true
				break
			}
		}
		if !found {
			return errors.New("WARPSCOUT 安装包缺少主程序")
		}
	}
	if _, e := a.command(5, "", "id", "foxray-scout"); e != nil {
		if e = a.moduleRun(ctx, "useradd", "--system", "--user-group", "--no-create-home", "--shell", "/sbin/nologin", "foxray-scout"); e != nil {
			return e
		}
	}
	return a.moduleRun(ctx, "install", "-d", "-m", "0700", "-o", "foxray-scout", "-g", "foxray-scout", "/var/lib/foxray-scout")
}
