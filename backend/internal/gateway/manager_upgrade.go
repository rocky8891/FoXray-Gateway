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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (a *App) ConfigureListener(address string) { a.listener = address }
func trustedManagerAsset(address, tag string) bool {
	return strings.HasPrefix(address, "https://github.com/rocky8891/FoXray-Gateway/releases/download/"+tag+"/")
}
func verifyManagerArchive(raw []byte, version string) (map[string][]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, errors.New("发行包不是有效ZIP")
	}
	if len(z.File) > 3000 {
		return nil, errors.New("发行包文件数量超过限制")
	}
	entries := map[string][]byte{}
	var total int64
	for _, f := range z.File {
		name := f.Name
		if strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || filepath.ToSlash(filepath.Clean(name)) != name || name == ".." || strings.HasPrefix(name, "../") || f.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("发行包包含不安全路径")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if _, exists := entries[name]; exists {
			return nil, errors.New("发行包路径重复")
		}
		total += int64(f.UncompressedSize64)
		if total > 256*1024*1024 || f.UncompressedSize64 > 100*1024*1024 {
			return nil, errors.New("发行包解压大小超过限制")
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		data, e := limitedRead(r, 100*1024*1024)
		_ = r.Close()
		if e != nil {
			return nil, e
		}
		entries[name] = data
	}
	manifest := map[string]string{}
	if json.Unmarshal(entries["MANIFEST_SHA256.json"], &manifest) != nil {
		return nil, errors.New("发行包缺少有效校验清单")
	}
	for name, data := range entries {
		if name == "MANIFEST_SHA256.json" {
			continue
		}
		hash := sha256.Sum256(data)
		if manifest[name] != hex.EncodeToString(hash[:]) {
			return nil, errors.New("发行包文件校验失败")
		}
	}
	for name := range manifest {
		if _, exists := entries[name]; !exists {
			return nil, errors.New("发行包校验清单引用缺失文件")
		}
	}
	binary := entries["foxray_v"+version]
	if len(binary) < 4 || !bytes.Equal(binary[:4], []byte{0x7f, 'E', 'L', 'F'}) || len(entries["web/index.html"]) == 0 {
		return nil, errors.New("发行包缺少Linux程序或前端入口")
	}
	return entries, nil
}
func insideBase(base, path string) bool {
	root, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func atomicLink(target, path string) error {
	temp := path + ".update-link"
	_ = os.Remove(temp)
	if err := os.Symlink(target, temp); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func (a *App) prepareManagerUpgrade(ctx context.Context, j *Job, expected string) error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("一键更新当前仅支持Linux amd64")
	}
	a.mutate.Lock()
	defer a.mutate.Unlock()
	pending := M{}
	if readJSON(filepath.Join(a.base, "private", "manager-upgrade-plan.json"), &pending) == nil && now()-num(pending["created_at"]) < 600 {
		return errors.New("已有管理服务更新计划，请等待完成")
	}
	if a.listener == "" {
		return errors.New("未配置管理服务健康检查地址")
	}
	release, err := a.githubRelease(ctx, "foxray")
	if err != nil {
		return err
	}
	tag := str(release["tag_name"])
	version := strings.TrimPrefix(tag, "v")
	if tag != expected {
		return errors.New("目标版本已变化，请重新检查并确认")
	}
	if !newerVersion(version, Version) {
		return errors.New("当前没有较新的稳定版本")
	}
	var asset M
	for _, value := range arr(release["assets"]) {
		row := obj(value)
		if str(row["name"]) == "FoXray-Gateway-"+tag+"-linux-amd64.zip" {
			asset = row
		}
	}
	digest := strings.TrimPrefix(str(asset["digest"]), "sha256:")
	expectedHash, err := hex.DecodeString(digest)
	address := str(asset["browser_download_url"])
	if err != nil || len(expectedHash) != 32 || !strings.HasPrefix(str(asset["digest"]), "sha256:") || !trustedManagerAsset(address, tag) {
		return errors.New("发行资产缺少可信SHA256或下载地址，未更新")
	}
	a.stage(j, "下载并校验 FoXray "+tag)
	req, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
	response, err := a.httpClient().Do(req)
	if err != nil {
		return errors.New("发行包下载未完成")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("发行包下载响应异常")
	}
	raw, err := limitedRead(response.Body, 64*1024*1024)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	if !bytes.Equal(hash[:], expectedHash) {
		return errors.New("发行包SHA256与版本源不一致")
	}
	files, err := verifyManagerArchive(raw, version)
	if err != nil {
		return err
	}
	oldBin, err := filepath.EvalSymlinks(filepath.Join(a.base, "foxray"))
	if err != nil {
		return errors.New("当前程序入口不可读取")
	}
	oldWeb, err := filepath.EvalSymlinks(filepath.Join(a.base, "web"))
	if err != nil {
		return errors.New("当前前端入口不可读取")
	}
	if _, err = os.Readlink(filepath.Join(a.base, "foxray")); err != nil {
		return errors.New("当前安装未使用版本入口，请使用部署包升级")
	}
	if _, err = os.Readlink(filepath.Join(a.base, "web")); err != nil {
		return errors.New("当前前端未使用版本入口，请使用部署包升级")
	}
	root := filepath.Join(a.base, "releases")
	if err = os.MkdirAll(root, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, "v"+version+"-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	for name, data := range files {
		if name != "foxray_v"+version && !strings.HasPrefix(name, "web/") {
			continue
		}
		target := filepath.Join(stage, filepath.FromSlash(name))
		mode := os.FileMode(0644)
		if name == "foxray_v"+version {
			mode = 0755
		}
		if err = atomicFile(target, data, mode); err != nil {
			return err
		}
	}
	bin := filepath.Join(stage, "foxray_v"+version)
	web := filepath.Join(stage, "web")
	host, port, err := net.SplitHostPort(a.listener)
	if err != nil {
		return errors.New("健康检查监听地址无效")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	checks := M{}
	for name, data := range files {
		if name == "foxray_v"+version || strings.HasPrefix(name, "web/") {
			checks[name] = hex.EncodeToString(sha256sum(data))
		}
	}
	plan := M{"file_sha256": checks, "version": version, "binary": bin, "web": web, "previous_binary": oldBin, "previous_web": oldWeb, "health": "http://" + net.JoinHostPort(host, port) + "/api/state", "created_at": now(), "binary_sha256": hex.EncodeToString(sha256sum(files["foxray_v"+version]))}
	planPath := filepath.Join(a.base, "private", "manager-upgrade-plan.json")
	if err = writeJSON(planPath, plan); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	if _, err = a.command(10, "", "systemd-run", "--quiet", "--collect", "--unit=foxray-version-upgrade", "--on-active=3s", "--timer-property=AccuracySec=1s", "--property=Type=oneshot", executable, "--apply-manager-update", "--base", a.base); err != nil {
		_ = os.Remove(planPath)
		return errors.New("未能安排独立更新进程，旧版本保持运行")
	}
	keep = true
	a.stage(j, "发行包校验通过；3秒后更新管理服务，页面将短暂重连")
	return nil
}
func sha256sum(data []byte) []byte { v := sha256.Sum256(data); return v[:] }
func managerHealth(address, version string) bool {
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("健康检查不接受重定向") }}
	defer client.CloseIdleConnections()
	response, err := client.Get(address)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return false
	}
	var state M
	return json.Unmarshal(raw, &state) == nil && str(state["version"]) == version
}
func applyManagerPlan(base string, plan M, runner Runner, healthy func(string, string) bool) error {
	for _, key := range []string{"binary", "web", "previous_binary", "previous_web"} {
		if !insideBase(base, str(plan[key])) {
			return errors.New("更新目标不在应用目录内")
		}
	}
	version := str(plan["version"])
	if !releaseVersion.MatchString(version) {
		return errors.New("目标版本无效")
	}
	raw, err := os.ReadFile(str(plan["binary"]))
	if err != nil || hex.EncodeToString(sha256sum(raw)) != str(plan["binary_sha256"]) {
		return errors.New("暂存程序校验失败")
	}
	checks := obj(plan["file_sha256"])
	if len(checks) == 0 {
		return errors.New("更新计划缺少文件校验")
	}
	stage := filepath.Dir(str(plan["binary"]))
	for name, expected := range checks {
		if name != "foxray_v"+version && !strings.HasPrefix(name, "web/") {
			return errors.New("更新计划包含无效文件")
		}
		target := filepath.Join(stage, filepath.FromSlash(name))
		if !insideBase(stage, target) {
			return errors.New("更新文件越界")
		}
		data, err := os.ReadFile(target)
		if err != nil || hex.EncodeToString(sha256sum(data)) != str(expected) {
			return errors.New("暂存文件校验失败")
		}
	}
	if checks["web/index.html"] == nil {
		return errors.New("更新计划缺少前端入口校验")
	}
	if filepath.Clean(str(plan["web"])) != filepath.Join(stage, "web") || filepath.Base(str(plan["binary"])) != "foxray_v"+version {
		return errors.New("暂存版本结构无效")
	}
	currentBin, err := filepath.EvalSymlinks(filepath.Join(base, "foxray"))
	if err != nil || currentBin != str(plan["previous_binary"]) {
		return errors.New("当前程序入口已变化，取消过期更新")
	}
	currentWeb, err := filepath.EvalSymlinks(filepath.Join(base, "web"))
	if err != nil || currentWeb != str(plan["previous_web"]) {
		return errors.New("当前前端入口已变化，取消过期更新")
	}
	switchTo := func(bin, web string) error {
		if err := atomicLink(bin, filepath.Join(base, "foxray")); err != nil {
			return err
		}
		return atomicLink(web, filepath.Join(base, "web"))
	}
	restart := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := runner.Run(ctx, []string{"systemctl", "restart", "foxray.service"}, "")
		return err
	}
	rollback := func() error {
		if err := switchTo(str(plan["previous_binary"]), str(plan["previous_web"])); err != nil {
			return err
		}
		return restart()
	}
	fail := func(message string) error {
		if err := rollback(); err != nil {
			return fmt.Errorf("%s；旧版本恢复失败：%w", message, err)
		}
		return errors.New(message + "；已恢复旧入口")
	}
	if err = switchTo(str(plan["binary"]), str(plan["web"])); err != nil {
		return fail("版本入口切换失败")
	}
	if err = restart(); err != nil {
		return fail("新版本启动失败")
	}
	if !healthy(str(plan["health"]), version) {
		return fail("新版本健康检查未通过")
	}
	return nil
}
func ApplyManagerUpdate(base string) error {
	plan := M{}
	if err := readJSON(filepath.Join(base, "private", "manager-upgrade-plan.json"), &plan); err != nil {
		return err
	}
	if now()-num(plan["created_at"]) > 600 {
		return errors.New("更新计划已过期")
	}
	healthy := func(address, version string) bool {
		for i := 0; i < 20; i++ {
			if managerHealth(address, version) {
				return true
			}
			time.Sleep(time.Second)
		}
		return false
	}
	err := applyManagerPlan(base, plan, SystemRunner{}, healthy)
	result := M{}
	_ = readJSON(filepath.Join(base, "manager-update.json"), &result)
	previousCurrent := result["current"]
	result["current"] = str(plan["version"])
	result["status"] = "current"
	result["checked_at"] = now()
	result["update_available"] = false
	result["message"] = "已更新至 v" + str(plan["version"])
	if err != nil {
		result["current"] = previousCurrent
		result["status"] = "error"
		result["message"] = err.Error()
	}
	_ = writeJSON(filepath.Join(base, "manager-update.json"), result)
	_ = os.Remove(filepath.Join(base, "private", "manager-upgrade-plan.json"))
	return err
}
