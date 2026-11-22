package gateway

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

// Only reclaims management metadata after independently verifying the already
// running engine. It never restarts a core or attaches/replaces capture rules.
func (a *App) recoverControl(data M, uiAction ...string) error {
	if a.readonly || !yes(data["confirmed"]) {
		return errors.New("请确认恢复管理状态认领")
	}
	for key := range data {
		if key != "confirmed" && key != "engine_pid" {
			return errors.New("恢复请求包含未知字段")
		}
	}
	expected := str(data["engine_pid"])
	pid, err := strconv.Atoi(expected)
	if err != nil || pid <= 0 {
		return errors.New("请提供升级前已核验的核心进程")
	}
	a.mutate.Lock()
	defer a.mutate.Unlock()
	a.mu.RLock()
	candidate, native, issue := clone(a.cfg), clone(a.native), a.recoveryIssue
	a.mu.RUnlock()
	if issue == "" {
		return errors.New("当前没有需要恢复的接管状态")
	}
	if err = readJSON(filepath.Join(a.base, "config.json"), &candidate); err != nil || !yes(candidate["enabled"]) || str(candidate["selected"]) == "" {
		return errors.New("持久配置未启用代理，未恢复")
	}
	running, err := a.command(4, "", "systemctl", "show", "foxray-engine", "-p", "MainPID", "--value")
	if err != nil || strings.TrimSpace(running) != expected {
		return errors.New("核心进程已变化，未恢复")
	}
	saved := M{}
	if readJSON(filepath.Join(a.base, "engine.json"), &saved) != nil {
		return errors.New("运行核心配置无法核验")
	}
	generated, err := a.buildEngine(candidate, native, str(candidate["selected"]))
	if err != nil || fingerprint(generated) != fingerprint(saved) {
		return errors.New("核心配置与持久出口不一致，未恢复")
	}
	if _, err = a.command(4, "", "nft", "list", "table", "inet", "vpm"); err != nil {
		return errors.New("接管规则缺失，未修改网络")
	}
	running, err = a.command(4, "", "systemctl", "show", "foxray-engine", "-p", "MainPID", "--value")
	if err != nil || strings.TrimSpace(running) != expected {
		return errors.New("核心进程在核验期间变化，未恢复")
	}
	a.mu.Lock()
	a.cfg = candidate
	a.active = str(candidate["selected"])
	a.applied = true
	a.recoveryIssue = ""
	a.loadExitRuntime()
	a.lastSwitch = a.activeSince
	a.mu.Unlock()
	a.maintainControlState()
	a.audit("manager.control.recover", "已核验并恢复管理认领；未重载核心或修改网络", true, nil, uiAction...)
	return nil
}
