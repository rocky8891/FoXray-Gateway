package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var releaseVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

func newerVersion(latest, current string) bool {
	l, c := releaseVersion.FindStringSubmatch(strings.TrimSpace(latest)), releaseVersion.FindStringSubmatch(strings.TrimSpace(current))
	if len(l) != 4 || len(c) != 4 {
		return false
	}
	for i := 1; i < 4; i++ {
		a, _ := strconv.Atoi(l[i])
		b, _ := strconv.Atoi(c[i])
		if a != b {
			return a > b
		}
	}
	return false
}
func (a *App) updateFile(service string) string {
	if service == "foxray" {
		return filepath.Join(a.base, "manager-update.json")
	}
	return filepath.Join(a.base, "private", service+"-update.json")
}
func validUpdateService(service string) bool {
	return service == "foxray" || service == "xray" || service == "warp" || service == "wireguard"
}
func (a *App) updatesSnapshot() M {
	services := M{}
	for _, service := range []string{"foxray", "xray", "warp", "wireguard"} {
		if service != "foxray" && !a.moduleEnabled(service) {
			continue
		}
		row := M{"status": "unchecked", "update_available": false}
		_ = readJSON(a.updateFile(service), &row)
		row["service"] = service
		row["check_policy"] = a.versionCheckPolicy(service)
		if service == "foxray" {
			row["current"] = Version
			row["update_available"] = newerVersion(str(row["latest"]), Version)
			if str(row["latest"]) != "" && str(row["status"]) != "error" && str(row["status"]) != "checking" {
				if yes(row["update_available"]) {
					row["status"] = "available"
					row["message"] = "发现新版本：" + str(row["latest"]) + "；当前：" + Version
				} else {
					row["status"] = "current"
					row["message"] = "当前版本：" + Version + "；最新已知稳定版：" + str(row["latest"])
				}
			}
		}
		services[service] = row
	}
	return M{"services": services, "automatic_check_hours": 24, "read_only": a.readonly}
}
func (a *App) installedVersion(service string) (string, error) {
	if service == "foxray" {
		return Version, nil
	}
	if service == "xray" {
		raw, err := a.command(5, "", filepath.Join(a.base, "xray"), "version")
		v := strings.Fields(raw)
		if err != nil || len(v) < 2 {
			return "", errors.New("当前核心版本不可读取")
		}
		return v[1], nil
	}
	pkg := "cloudflare-warp"
	if service == "wireguard" {
		pkg = "wireguard-tools"
	}
	raw, err := a.command(8, "", "rpm", "-q", pkg, "--qf", "%{VERSION}-%{RELEASE}")
	if err != nil {
		return "", errors.New("当前软件包未安装或版本不可读取")
	}
	return strings.TrimSpace(raw), nil
}
func (a *App) githubRelease(ctx context.Context, service string) (M, error) {
	repo := "rocky8891/FoXray-Gateway"
	if service == "xray" {
		repo = "XTLS/Xray-core"
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := a.httpClient().Do(req)
	if err != nil {
		return nil, errors.New("版本源暂不可访问，请稍后重试")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("版本源返回 HTTP %d，未判断为最新", res.StatusCode)
	}
	raw, err := limitedRead(res.Body, 2*1024*1024)
	if err != nil {
		return nil, errors.New("版本信息过大或未完整读取")
	}
	row := M{}
	if json.Unmarshal(raw, &row) != nil || !releaseVersion.MatchString(str(row["tag_name"])) || yes(row["draft"]) || yes(row["prerelease"]) {
		return nil, errors.New("稳定版本信息无效")
	}
	return row, nil
}
func (a *App) checkServiceVersion(ctx context.Context, j *Job, service string) (err error) {
	if !validUpdateService(service) {
		return errors.New("更新模块无效")
	}
	if service != "foxray" && !a.moduleEnabled(service) {
		return errors.New("该模块未启用")
	}
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	previous := M{}
	_ = readJSON(a.updateFile(service), &previous)
	defer func() {
		if err != nil {
			previous["status"] = "error"
			previous["message"] = err.Error()
			previous["checked_at"] = now()
			_ = writeJSON(a.updateFile(service), previous)
		}
	}()
	previous["status"] = "checking"
	previous["check_started_at"] = now()
	_ = writeJSON(a.updateFile(service), previous)
	current, err := a.installedVersion(service)
	if err != nil {
		return err
	}
	info := M{"current": current, "checked_at": now(), "status": "current", "update_available": false, "source": "GitHub 稳定版"}
	if service == "foxray" || service == "xray" {
		row, e := a.githubRelease(ctx, service)
		if e != nil {
			return e
		}
		latest := str(row["tag_name"])
		info["latest"] = latest
		info["update_available"] = newerVersion(latest, current)
		if service == "foxray" {
			info["update_available"] = newerVersion(latest, current)
		}
		notes := str(row["body"])
		if len(notes) > 64000 {
			notes = string([]rune(notes)[:min(16000, len([]rune(notes)))])
		}
		info["release_notes"] = notes
		info["release_url"] = row["html_url"]
		info["published_at"] = row["published_at"]
	} else {
		pkg := "cloudflare-warp"
		if service == "wireguard" {
			pkg = "wireguard-tools"
		}
		info["source"] = "系统软件源（" + pkg + "）"
		raw, e := a.command(75, "", "dnf", "-q", "--refresh", "repoquery", "--upgrades", "--latest-limit=1", "--qf", "%{version}-%{release}", pkg)
		if e != nil {
			return errors.New("软件源检查失败，未判断为最新")
		}
		latest := ""
		for _, line := range textLines(raw) {
			line = strings.TrimSpace(line)
			if regexp.MustCompile(`^[0-9][a-zA-Z0-9._:+~-]{0,160}$`).MatchString(line) {
				latest = line
			}
		}
		if latest != "" && latest != current {
			info["latest"] = latest
			info["update_available"] = true
		} else {
			info["latest"] = current
		}
		notes, e := a.command(30, "", "dnf", "-q", "repoquery", "--latest-limit=1", "--changelogs", pkg)
		if e == nil && len(notes) > 0 {
			if len(notes) > 64000 {
				notes = notes[:64000]
			}
			info["release_notes"] = notes
		} else {
			info["release_notes"] = "软件源未提供更新日志，请参考软件源的发行说明。"
		}
	}
	message := "当前已是最新版本：" + current
	if yes(info["update_available"]) {
		info["status"] = "available"
		message = "发现新版本：" + str(info["latest"]) + "；当前：" + current
	}
	info["message"] = message
	if err = writeJSON(a.updateFile(service), info); err != nil {
		return err
	}
	a.stage(j, message)
	a.jobDetail(j, message)
	a.jobDetail(j, "更新日志可在服务管理的版本卡片中查看")
	return nil
}
func (a *App) versionCheckLoop() {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		for _, service := range []string{"foxray", "xray", "warp", "wireguard"} {
			if service != "foxray" && !a.moduleEnabled(service) {
				continue
			}
			running := false
			a.jobs.mu.RLock()
			for _, job := range a.jobs.items {
				if job.Group == "version.check."+service && job.Status == "running" {
					running = true
					break
				}
			}
			a.jobs.mu.RUnlock()
			if running {
				continue
			}
			previous := M{}
			_ = readJSON(a.updateFile(service), &previous)
			if versionCheckDue(a.versionCheckPolicy(service), previous, now()) {
				target := service
				_, _ = a.submitAutomatic("自动版本检查 · "+target, "version.check."+target, func(ctx context.Context, j *Job) error { return a.checkServiceVersion(ctx, j, target) })
			}
		}
		timer.Reset(time.Minute)
	}
}

func (a *App) upgradeServiceVersion(ctx context.Context, j *Job, service, expected string) error {
	if a.readonly {
		return errors.New("只读实例不能更新服务")
	}
	if service != "foxray" && !a.moduleEnabled(service) {
		return errors.New("该模块未启用")
	}
	if service == "foxray" {
		return a.prepareManagerUpgrade(ctx, j, expected)
	}
	if err := a.checkServiceVersion(ctx, j, service); err != nil {
		return err
	}
	info := M{}
	_ = readJSON(a.updateFile(service), &info)
	if str(info["latest"]) != expected {
		return errors.New("目标版本已变化，请重新检查并确认")
	}
	if !yes(info["update_available"]) {
		return errors.New("没有已确认的新版本")
	}
	if service == "wireguard" {
		a.mutate.Lock()
		_, err := a.command(180, "", "dnf", "-y", "upgrade", "wireguard-tools-"+expected)
		a.mutate.Unlock()
		if err != nil {
			return errors.New("WireGuard工具包更新失败；未执行接口重启")
		}
	} else if err := a.serviceAction(ctx, j, service, "upgrade", M{"allow_outage": true, "target_version": expected}); err != nil {
		return err
	}
	return a.checkServiceVersion(ctx, j, service)
}
