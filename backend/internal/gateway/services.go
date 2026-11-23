package gateway

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func (a *App) services() M {
	out := M{}
	if a.moduleEnabled("xray") {
		out["xray"] = a.service("foxray-engine")
		update := M{}
		_ = readJSON(filepath.Join(a.base, "private", "xray-update.json"), &update)
		obj(out["xray"])["latest"] = update["latest"]
		obj(out["xray"])["rollback_available"] = regular(filepath.Join(a.base, "xray.previous"))
		a.mu.RLock()
		obj(out["xray"])["proxy_enabled"] = a.applied
		a.mu.RUnlock()
		raw, _ := a.command(4, "", filepath.Join(a.base, "xray"), "version")
		if lines := textLines(raw); len(lines) > 0 {
			obj(out["xray"])["version"] = label(lines[0])
			fields := strings.Fields(lines[0])
			if len(fields) > 1 {
				obj(out["xray"])["update_available"] = str(update["latest"]) != "" && strings.TrimPrefix(str(update["latest"]), "v") != fields[1]
			}
		}
	}
	if yes(a.components["warp"]) {
		w := a.service("warp-svc")
		v, _ := a.command(4, "", "rpm", "-q", "cloudflare-warp", "--qf", "%{VERSION}-%{RELEASE}")
		w["version"] = strings.TrimSpace(v)
		update := M{}
		_ = readJSON(filepath.Join(a.base, "private", "warp-update.json"), &update)
		w["latest"] = update["latest"]
		w["update_available"] = str(update["latest"]) != "" && str(update["latest"]) != strings.TrimSpace(v)
		w["rollback_available"] = false
		w["tunnel"] = a.warp()
		out["warp"] = w
	}
	for _, service := range []string{"xray", "warp"} {
		if row := obj(out[service]); len(row) > 0 {
			update := M{}
			_ = readJSON(a.updateFile(service), &update)
			if value, exists := update["update_available"]; exists {
				row["update_available"] = value
			}
			row["update"] = update
		}
	}
	return M{"services": out}
}

var endpointPattern = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]+`)

func (a *App) warp() M {
	a.warpMu.Lock()
	defer a.warpMu.Unlock()
	if time.Since(a.warpAt) < 15*time.Second && a.warpCache != nil {
		return clone(a.warpCache)
	}
	m := M{"state": "inactive", "connected": false, "checked_at": now()}
	if !yes(a.components["warp"]) {
		m["state"] = "not_installed"
		a.noteWarpConnection(m)
		return m
	}
	if str(a.service("warp-svc")["state"]) != "active" {
		a.noteWarpConnection(m)
		a.warpCache = m
		a.warpAt = time.Now()
		return clone(m)
	}
	status, _ := a.command(4, "", "warp-cli", "--accept-tos", "status")
	m["state"] = "disconnected"
	if strings.Contains(status, "Status update: Connected") {
		m["connected"], m["state"] = true, "connected"
	} else if strings.Contains(strings.ToLower(status), "connecting") {
		m["state"] = "connecting"
	}
	settings, _ := a.command(4, "", "warp-cli", "--accept-tos", "settings")
	for _, line := range textLines(settings) {
		if strings.Contains(line, "Override WARP endpoint") {
			m["override"] = endpointPattern.FindString(line)
		}
		if strings.Contains(line, "WARP tunnel protocol") {
			m["protocol"] = "MASQUE"
			if strings.Contains(strings.ToLower(line), "wireguard") {
				m["protocol"] = "WireGuard"
			}
		}
		if strings.Contains(line, "HTTP Version:") {
			m["masque_options"] = "h3-with-h2-fallback"
			if strings.Contains(line, "HTTP/2") && !strings.Contains(strings.ToLower(line), "fallback") {
				m["masque_options"] = "h2-only"
			} else if strings.Contains(line, "HTTP/3") && !strings.Contains(strings.ToLower(line), "fallback") {
				m["masque_options"] = "h3-only"
			}
		}
	}
	if yes(m["connected"]) {
		stats, _ := a.command(4, "", "warp-cli", "--accept-tos", "tunnel", "stats")
		for _, line := range textLines(stats) {
			if strings.Contains(line, "Endpoints:") {
				address := endpointPattern.FindString(line)
				if address == "" {
					ipRE := regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)
					m["endpoint"] = ipRE.FindString(line)
				} else {
					m["endpoint_address"] = address
					m["endpoint"] = strings.Split(address, ":")[0]
				}
			}
		}
		m["transport"] = "MASQUE"
		if strings.Contains(stats, "HTTPS via TCP") {
			m["transport"] = "MASQUE TCP"
		} else if strings.Contains(stats, "WireGuard") {
			m["transport"] = "WireGuard"
		}
	}
	a.noteWarpConnection(m)
	a.warpCache = m
	a.warpAt = time.Now()
	return clone(m)
}
func (a *App) invalidateWarp() { a.warpMu.Lock(); a.warpAt = time.Time{}; a.warpMu.Unlock() }
func (a *App) verifyWarp() error {
	ns, err := os.Stat("/run/netns/foxray-warp")
	if err != nil {
		return errors.New("WARP 隔离网络空间不存在，未执行")
	}
	raw, e := a.command(5, "", "systemctl", "show", "warp-svc", "-p", "NetworkNamespacePath", "-p", "MainPID")
	if e != nil {
		return e
	}
	props := parseKV(raw)
	if str(props["NetworkNamespacePath"]) != "/run/netns/foxray-warp" {
		return errors.New("WARP 未配置网络空间隔离")
	}
	pid := str(props["MainPID"])
	if pid != "0" && pid != "" {
		process, e := os.Stat("/proc/" + pid + "/ns/net")
		if e != nil || !os.SameFile(ns, process) {
			return errors.New("WARP 进程网络空间不匹配")
		}
		hostDNS, e1 := os.Stat("/etc/resolv.conf")
		privateDNS, e2 := os.Stat("/proc/" + pid + "/root/etc/resolv.conf")
		if e1 != nil || e2 != nil || os.SameFile(hostDNS, privateDNS) {
			return errors.New("WARP DNS 挂载隔离未通过")
		}
		if _, e := os.Stat("/proc/" + pid + "/root/run/dbus/system_bus_socket"); e == nil {
			return errors.New("WARP 仍可访问主机系统总线")
		}
	}
	if _, e := a.command(4, "", "nft", "list", "table", "inet", "cloudflare-warp"); e == nil {
		return errors.New("主机存在 WARP 全局防火墙，未执行")
	}
	return nil
}
func (a *App) warpCommand(ctx context.Context, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, e := a.runner.Run(c, append([]string{"warp-cli", "--accept-tos"}, args...), "")
	a.invalidateWarp()
	return out, e
}
func (a *App) waitWarp(ctx context.Context, j *Job, seconds int) error {
	return a.waitWarpState(ctx, j, seconds, true)
}
func (a *App) waitWarpState(ctx context.Context, j *Job, seconds int, failFast bool) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	failedChecks := 0
	for time.Now().Before(deadline) {
		child, cancel := context.WithDeadline(ctx, deadline)
		c, stop := context.WithTimeout(child, 4*time.Second)
		status, e := a.runner.Run(c, []string{"warp-cli", "--accept-tos", "status"}, "")
		stop()
		cancel()
		if e == nil && strings.Contains(status, "Status update: Connected") {
			a.invalidateWarp()
			return nil
		}
		if failFast && e == nil && strings.Contains(status, "Status update: Unable") && strings.Contains(strings.ToLower(status), "failed to perform happy eyeballs") {
			failedChecks++
			if failedChecks >= 2 {
				a.invalidateWarp()
				return errors.New("WARP 官方客户端连接握手失败（Happy Eyeballs），目标端点未连接")
			}
		} else {
			failedChecks = 0
		}
		a.stage(j, fmt.Sprintf("等待 WARP 隧道确认 · 剩余 %d 秒；Xray 保持独立", int(time.Until(deadline).Seconds())))
		select {
		case <-ctx.Done():
			return errors.New("WARP 连接等待已取消")
		case <-time.After(2 * time.Second):
		}
	}
	a.invalidateWarp()
	return errors.New("WARP 隧道连接超时；Xray 配置与路由未回退")
}

func (a *App) serviceAction(ctx context.Context, j *Job, service, action string, data M) error {
	if service != "xray" && service != "warp" {
		return errors.New("服务无效")
	}
	allowed := map[string]bool{"start": true, "stop": true, "restart": true, "enable": true, "disable": true, "connect": true, "disconnect": true, "check": true, "upgrade": true, "rollback": true}
	if !allowed[action] {
		return errors.New("操作无效")
	}
	if action == "check" {
		return a.checkComponent(ctx, j, service)
	}
	unit := "foxray-engine"
	if service == "warp" {
		if !yes(a.components["warp"]) {
			return errors.New("WARP 未安装")
		}
		unit = "warp-svc"
		a.warpOperate.Lock()
		defer a.warpOperate.Unlock()
		if action == "start" || action == "restart" || action == "connect" || action == "enable" || action == "disable" {
			if _, e := a.command(4, "", "nft", "list", "table", "inet", "cloudflare-warp"); e == nil {
				return errors.New("主机存在 WARP 全局防火墙，未执行")
			}
			raw, e := a.command(5, "", "systemctl", "show", "warp-svc", "-p", "NetworkNamespacePath", "-p", "MainPID")
			props := parseKV(raw)
			if e != nil || str(props["NetworkNamespacePath"]) != "/run/netns/foxray-warp" {
				return errors.New("WARP 服务尚未配置独立网络空间，未启动或修改")
			}
			if pid := str(props["MainPID"]); pid != "0" && pid != "" {
				if e = a.verifyWarp(); e != nil {
					return e
				}
			}
		} else if e := a.verifyWarp(); e != nil {
			return e
		}
	} else {
		a.mutate.Lock()
		defer a.mutate.Unlock()
	}
	if action == "check" {
		return a.checkComponent(ctx, j, service)
	}
	if action == "upgrade" || action == "rollback" {
		if service == "xray" {
			return a.upgradeXray(ctx, j, action == "rollback", str(data["target_version"]))
		}
		if action == "rollback" {
			return errors.New("未选择可验证的历史 RPM 安装包，未执行包回退")
		}
		a.stage(j, "通过系统软件源升级 WARP，保留隔离单元")
		_, e := a.command(180, "", "dnf", "-y", "upgrade", func() string {
			if str(data["target_version"]) != "" {
				return "cloudflare-warp-" + str(data["target_version"])
			}
			return "cloudflare-warp"
		}())
		if e != nil {
			return e
		}
		a.invalidateWarp()
		return a.verifyWarp()
	}
	if service == "warp" {
		if action == "connect" {
			if _, e := a.command(20, "", "systemctl", "start", unit); e != nil {
				return e
			}
			time.Sleep(200 * time.Millisecond)
			if e := a.verifyWarp(); e != nil {
				return e
			}
			if _, e := a.warpCommand(ctx, "registration", "show"); e != nil {
				a.mu.RLock()
				accepted := yes(a.components["warp_terms_accepted"])
				a.mu.RUnlock()
				if !accepted {
					return errors.New("WARP 尚未注册；请在功能管理中接受服务条款并保存后再连接")
				}
				a.stage(j, "首次注册隔离 WARP 客户端，认证内容不写入日志")
				if _, e = a.warpCommand(ctx, "registration", "new"); e != nil {
					return errors.New("WARP 首次注册未完成")
				}
			}
			if _, e := a.warpCommand(ctx, "connect"); e != nil {
				return e
			}
			e := a.waitWarp(ctx, j, 45)
			if e == nil {
				a.setWarpDisabled(false)
			}
			return e
		}
		if action == "disconnect" {
			_, e := a.warpCommand(ctx, "disconnect")
			if e == nil {
				a.setWarpDisabled(true)
			}
			return e
		}
		if action == "stop" {
			_, _ = a.warpCommand(ctx, "disconnect")
		}
		a.stage(j, "正在执行 WARP 服务操作，原生代理保持运行")
		if _, e := a.command(45, "", "systemctl", action, unit); e != nil {
			return e
		}
		a.invalidateWarp()
		if action == "stop" {
			a.setWarpDisabled(true)
		} else if action == "start" || action == "restart" {
			a.setWarpDisabled(false)
		}
		if action == "start" || action == "restart" {
			time.Sleep(200 * time.Millisecond)
			if e := a.verifyWarp(); e != nil {
				return e
			}
			a.stage(j, "WARP 服务已启动；隧道状态单独显示")
		}
		return nil
	}
	if action == "connect" || action == "disconnect" {
		return errors.New("Xray 代理启停请使用代理模式控制")
	}
	if action == "start" || action == "restart" {
		_, err := os.Stat(filepath.Join(a.base, "engine.json"))
		if errors.Is(err, os.ErrNotExist) {
			a.jobDetail(j, "缺少 Xray 配置文件 engine.json")
			return errors.New("Xray-core 无法启动：请先在 Xray 设置中保存配置并启用代理")
		}
		if err != nil {
			return errors.New("无法检查 Xray 配置文件 engine.json，请检查文件权限")
		}
	}
	if action == "stop" || action == "restart" {
		a.mu.RLock()
		applied := a.applied
		a.mu.RUnlock()
		if applied && !yes(data["allow_outage"]) {
			return errors.New("核心正在承载流量，请确认本次服务维护会中断代理")
		}
		if applied {
			_, _ = a.command(5, "", "nft", "delete", "table", "inet", "vpm")
			a.mu.Lock()
			previousActive := a.active
			a.applied = false
			a.cfg["enabled"] = false
			a.active = "warp"
			_ = a.persist()
			a.mu.Unlock()
			a.noteExitSwitch(previousActive, "warp")
		}
	}
	if action == "enable" && str(a.service(unit)["unit_file_state"]) == "static" {
		// Older installations had no [Install] section. A drop-in adds boot
		// integration without replacing the user's unit or restarting its process.
		dir := "/etc/systemd/system/foxray-engine.service.d"
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := atomicFile(filepath.Join(dir, "foxray-autostart.conf"), []byte("[Install]\nWantedBy=multi-user.target\n"), 0644); err != nil {
			return err
		}
		if _, err := a.command(10, "", "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	a.stage(j, "正在操作 Xray-core 服务")
	_, commandErr := a.command(30, "", "systemctl", action, unit)
	if action == "start" || action == "restart" || action == "stop" {
		raw, statusErr := a.command(5, "", "systemctl", "show", unit, "-p", "ActiveState", "-p", "SubState", "-p", "ConditionResult", "-p", "Result", "-p", "ExecMainStatus")
		if statusErr != nil {
			return errors.Join(commandErr, errors.New("无法核实 Xray-core 操作后的运行状态"))
		}
		props := parseKV(raw)
		// Report structured systemd state without copying potentially sensitive Xray stderr.
		for _, field := range []string{"ActiveState", "SubState", "ConditionResult", "Result", "ExecMainStatus"} {
			if value := str(props[field]); value != "" {
				a.jobDetail(j, field+"="+value)
			}
		}
		active := str(props["ActiveState"])
		if action == "stop" {
			if commandErr != nil || active != "inactive" {
				return errors.Join(commandErr, errors.New("Xray-core 停止失败，请查看服务详情"))
			}
			a.stage(j, "Xray-core 已停止")
			return nil
		}
		if str(props["ConditionResult"]) == "no" && active != "active" {
			return errors.New("Xray-core 未启动：systemd 跳过了服务，因为启动条件未满足")
		}
		if commandErr != nil || active != "active" || str(props["SubState"]) != "running" {
			return errors.Join(commandErr, errors.New("Xray-core 未进入运行状态，请查看服务详情"))
		}
		a.stage(j, "Xray-core 已运行")
		return nil
	}
	if commandErr == nil && (action == "enable" || action == "disable") {
		if yes(a.service(unit)["autostart"]) != (action == "enable") {
			return errors.New("开机自启状态未达到目标，请查看服务单元配置")
		}
		if action == "enable" {
			a.stage(j, "Xray-core 已开启自启")
		} else {
			a.stage(j, "Xray-core 已关闭自启")
		}
	}
	return commandErr
}

func (a *App) checkComponent(ctx context.Context, j *Job, service string) error {
	return a.checkServiceVersion(ctx, j, service)
}

func (a *App) upgradeXray(ctx context.Context, j *Job, rollback bool, expected ...string) error {
	path := filepath.Join(a.base, "xray")
	if rollback {
		previous := path + ".previous"
		if !regular(previous) {
			return errors.New("没有可用的核心回退文件")
		}
		current, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		old, e := os.ReadFile(previous)
		if e != nil {
			return e
		}
		if e = atomicFile(path, old, 0755); e != nil {
			return e
		}
		_ = atomicFile(previous, current, 0755)
		_, e = a.command(20, "", "systemctl", "restart", "foxray-engine")
		return e
	}
	if runtime.GOARCH != "amd64" {
		return errors.New("当前升级器只选择 amd64 官方资产，其他架构请使用部署流程")
	}
	a.stage(j, "下载并校验官方核心资产")
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/XTLS/Xray-core/releases/latest", nil)
	res, e := a.httpClient().Do(req)
	if e != nil {
		return errors.New("无法查询核心版本")
	}
	defer res.Body.Close()
	release := M{}
	if json.NewDecoder(res.Body).Decode(&release) != nil {
		return errors.New("官方版本信息异常")
	}
	if len(expected) > 0 && expected[0] != "" && str(release["tag_name"]) != expected[0] {
		return errors.New("核心目标版本已变化，请重新确认")
	}
	assetURL, digestURL := "", ""
	for _, v := range arr(release["assets"]) {
		asset := obj(v)
		if str(asset["name"]) == "Xray-linux-64.zip" {
			assetURL = str(asset["browser_download_url"])
		}
		if str(asset["name"]) == "Xray-linux-64.zip.dgst" {
			digestURL = str(asset["browser_download_url"])
		}
	}
	if assetURL == "" || digestURL == "" {
		return errors.New("官方资产或校验文件不存在")
	}
	fetch := func(address string, limit int64) ([]byte, error) {
		r, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
		p, e := a.httpClient().Do(r)
		if e != nil {
			return nil, e
		}
		defer p.Body.Close()
		if p.StatusCode != 200 {
			return nil, errors.New("官方资产下载失败")
		}
		return limitedRead(p.Body, limit)
	}
	archive, e := fetch(assetURL, 64*1024*1024)
	if e != nil {
		return errors.New("核心下载未完成")
	}
	digest, e := fetch(digestURL, 16384)
	if e != nil {
		return errors.New("校验文件下载未完成")
	}
	hash := sha256.Sum256(archive)
	if !strings.Contains(strings.ToLower(string(digest)), hex.EncodeToString(hash[:])) {
		return errors.New("核心资产 SHA256 校验失败")
	}
	z, e := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if e != nil {
		return e
	}
	var binary []byte
	for _, f := range z.File {
		if f.Name == "xray" {
			r, er := f.Open()
			if er != nil {
				return er
			}
			binary, er = limitedRead(r, 100*1024*1024)
			_ = r.Close()
			if er != nil {
				return er
			}
			break
		}
	}
	if len(binary) == 0 {
		return errors.New("安装包缺少核心文件")
	}
	candidate := filepath.Join(a.base, "private", "xray.candidate")
	if e = atomicFile(candidate, binary, 0755); e != nil {
		return e
	}
	if _, e = a.command(12, "", candidate, "run", "-test", "-c", filepath.Join(a.base, "engine.json")); e != nil {
		return errors.New("新核心与现有配置不兼容")
	}
	old, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if e = atomicFile(path+".previous", old, 0755); e != nil {
		return e
	}
	if e = atomicFile(path, binary, 0755); e != nil {
		return e
	}
	if _, e = a.command(20, "", "systemctl", "restart", "foxray-engine"); e != nil {
		_ = atomicFile(path, old, 0755)
		_, _ = a.command(20, "", "systemctl", "restart", "foxray-engine")
		return errors.New("新核心启动失败，已恢复旧文件")
	}
	return nil
}

var _ io.Reader
