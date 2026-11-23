package gateway

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func wgAllowedInput(value any) ([]string, error) {
	result := []string{}
	var values []any
	switch v := value.(type) {
	case string:
		values = []any{v}
	case []any:
		values = v
	case []string:
		for _, item := range v {
			values = append(values, item)
		}
	default:
		return nil, errors.New("AllowedIPs 须为多个 IP 或 CIDR")
	}
	seen := map[string]bool{}
	for _, part := range values {
		s, ok := part.(string)
		if !ok {
			return nil, errors.New("AllowedIPs 须为 IP 或 CIDR")
		}
		for _, prefix := range strings.FieldsFunc(s, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			if prefix == "all" || prefix == "default" {
				return nil, errors.New("AllowedIPs 须为 IP 或 CIDR")
			}
			_, network, err := canonicalSource(prefix)
			if err != nil {
				return nil, errors.New("AllowedIPs 须为有效 IP 或 CIDR")
			}
			prefix = network.String()
			if !seen[prefix] {
				result = append(result, prefix)
				seen[prefix] = true
			}
		}
	}
	return result, nil
}

// Stop using the original configuration before replacing it. On rollback stop
// the new service before restoring files, then restart the original interface.
func (n *Network) planWGEditFiles(plan *NetworkPlan, profile *WGProfile, path, newName string, oldBytes []byte, active, autostart bool) error {
	name := profile.Name
	if newName == "" {
		newName = name
	}
	if err := iface(newName); err != nil {
		return err
	}
	rename := newName != name
	target := filepath.Join(filepath.Dir(path), newName+".conf")
	if rename {
		next := n.app.service("wg-quick@" + newName)
		if str(next["state"]) == "active" || yes(next["autostart"]) {
			return errors.New("新接口名称已有运行或自启服务")
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return errors.New("新接口名称已存在或无法确认")
		}
		for _, line := range profile.extras {
			if strings.Contains(line, name) {
				return errors.New("自定义接口钩子包含原接口名，请先调整钩子再重命名")
			}
		}
		n.mu.Lock()
		managed := clone(arr(n.managed["routes"]))
		n.mu.Unlock()
		for _, value := range managed {
			if str(obj(value)["dev"]) == name {
				return errors.New("此接口被手工路由引用，请先调整对应路由再重命名")
			}
		}
	}
	backup := filepath.Join(n.app.base, "private", "network-backups", plan.ID+".conf")
	original := &FileRestore{Target: path, Backup: backup, Existed: true, OldHash: fingerprint(string(oldBytes))}
	plan.restoreFile = original
	var destination *FileRestore
	if rename {
		destination = &FileRestore{Target: target, Existed: false, OldHash: fingerprint("")}
		plan.restoreFile = nil
		plan.restoreFiles = []*FileRestore{destination, original}
	}
	originalPrecondition := plan.precondition
	plan.precondition = func() error {
		current, err := os.ReadFile(path)
		if err != nil || fingerprint(string(current)) != fingerprint(string(oldBytes)) {
			return errors.New("接口配置已变化，请重新预览")
		}
		if originalPrecondition != nil {
			if err := originalPrecondition(); err != nil {
				return err
			}
		}
		state := n.app.service("wg-quick@" + name)
		if (str(state["state"]) == "active") != active || yes(state["autostart"]) != autostart {
			return errors.New("接口运行或自启状态已变化，请重新预览")
		}
		if rename {
			next := n.app.service("wg-quick@" + newName)
			if str(next["state"]) == "active" || yes(next["autostart"]) {
				return errors.New("新接口名称已有运行或自启服务")
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				return errors.New("新接口名称已被占用")
			}
			live, err := n.app.command(4, "", "wg", "show", "interfaces")
			if err != nil {
				return errors.New("无法核对接口名称占用")
			}
			for _, value := range strings.Fields(live) {
				if value == newName {
					return errors.New("新接口名称已被占用")
				}
			}
		}
		return nil
	}
	appendStep := func(args, undo []string) {
		plan.steps = append(plan.steps, Step{Args: args})
		plan.undo = append(plan.undo, Step{Args: undo})
	}
	if rename && autostart {
		appendStep([]string{"systemctl", "disable", "wg-quick@" + name}, []string{"systemctl", "enable", "wg-quick@" + name})
		appendStep([]string{"systemctl", "enable", "wg-quick@" + newName}, []string{"systemctl", "disable", "wg-quick@" + newName})
	}
	if rename && str(n.app.service("firewalld")["state"]) == "active" {
		bindings, err := n.wgRenameBindings(name, newName)
		if err != nil {
			return err
		}
		previous := plan.precondition
		plan.precondition = func() error {
			if err := previous(); err != nil {
				return err
			}
			current, err := n.wgRenameBindings(name, newName)
			if err != nil {
				return err
			}
			if fingerprint(current) != fingerprint(bindings) {
				return errors.New("接口区域绑定已变化，请重新预览")
			}
			return nil
		}
		for _, binding := range bindings {
			prefix := zoneArgs(binding.Zone, binding.Permanent)
			appendStep(append(append([]string(nil), prefix...), "--remove-interface="+name), append(append([]string(nil), prefix...), "--add-interface="+name))
			appendStep(append(append([]string(nil), prefix...), "--add-interface="+newName), append(append([]string(nil), prefix...), "--remove-interface="+newName))
		}
	}
	if active {
		plan.beforeRestore = []Step{{Args: []string{"systemctl", "stop", "wg-quick@" + newName}}}
		if rename {
			plan.beforeRestore = append(plan.beforeRestore, Step{Args: []string{"systemctl", "stop", "wg-quick@" + name}})
		}
		plan.afterRestore = []Step{{Args: []string{"systemctl", "start", "wg-quick@" + name}}}
		appendStep([]string{"systemctl", "start", "wg-quick@" + newName}, []string{"systemctl", "stop", "wg-quick@" + newName})
	}
	profile.Name = newName
	plan.applyFiles = func() error {
		if err := atomicFile(backup, oldBytes, 0600); err != nil {
			return err
		}
		payload := []byte(profile.render())
		if active {
			stoppedConfig := wgStopConfiguration(oldBytes)
			original.ExpectedHash = fingerprint(string(stoppedConfig))
			if destination != nil {
				destination.ExpectedHash = fingerprint(string(payload))
			}
			if err := n.writeSpec(plan, 0); err != nil {
				return err
			}
			if string(stoppedConfig) != string(oldBytes) {
				if err := atomicFile(path, stoppedConfig, 0600); err != nil {
					return err
				}
			}
			if _, err := n.app.command(20, "", "systemctl", "stop", "wg-quick@"+name); err != nil {
				return err
			}
			if string(stoppedConfig) != string(oldBytes) {
				if err := atomicFile(path, oldBytes, 0600); err != nil {
					return err
				}
			}
		}
		if destination != nil {
			destination.ExpectedHash = fingerprint(string(payload))
			original.ExpectedHash = "delete"
		} else {
			original.ExpectedHash = fingerprint(string(payload))
		}
		if err := n.writeSpec(plan, 0); err != nil {
			return err
		}

		var err error
		if rename {
			err = atomicNewFile(target, payload, 0600)
			if err == nil {
				err = os.Remove(path)
			}
		} else {
			err = atomicFile(path, payload, 0600)
		}
		profile.PrivateKey = ""
		return err
	}
	plan.postcondition = func() error {
		state := n.app.service("wg-quick@" + newName)
		if (str(state["state"]) == "active") != active {
			return errors.New("接口应用后的运行状态不符合预期，已请求回退")
		}
		if rename && yes(state["autostart"]) != autostart {
			return errors.New("接口自启状态核对失败，已请求回退")
		}
		return nil
	}
	plan.Summary = "WireGuard 配置 replace · " + name
	if rename {
		plan.Summary += " → " + newName
	}
	plan.Preview = append(plan.Preview, fmt.Sprintf("备份并更新 %s（密钥保持本机受限保存）", target))
	if active {
		plan.Preview = append([]string{"先使用原配置停止 wg-quick@" + name}, plan.Preview...)
		plan.Warnings = append(plan.Warnings, "应用时仅重启此接口，短暂中断其 VPN 连接；失败或120秒未确认将恢复原配置、原名称和运行状态。")
	} else {
		plan.Warnings = append(plan.Warnings, "此接口当前已停止；应用后继续保持停止状态。")
	}
	return nil
}

type wgRenameBinding struct {
	Zone      string
	Permanent bool
}

func (n *Network) wgRenameBindings(name, newName string) ([]wgRenameBinding, error) {
	result := []wgRenameBinding{}
	for _, permanent := range []bool{false, true} {
		args := []string{"firewall-cmd"}
		if permanent {
			args = append(args, "--permanent")
		}
		raw, err := n.app.command(12, "", append(args, "--list-all-zones")...)
		if err != nil {
			return nil, errors.New("无法核对接口区域绑定")
		}
		zones := reloadObjects(raw)
		if len(zones) == 0 {
			return nil, errors.New("区域快照不完整")
		}
		for zone, lines := range zones {
			for _, line := range lines {
				if strings.HasPrefix(line, "interfaces:") {
					for _, iface := range strings.Fields(strings.TrimPrefix(line, "interfaces:")) {
						if iface == newName {
							return nil, errors.New("新接口名称已有区域绑定")
						}
						if iface == name {
							result = append(result, wgRenameBinding{Zone: zone, Permanent: permanent})
						}
					}
				}
			}
		}
	}
	return result, nil
}

var wgSaveConfigEnabled = regexp.MustCompile(`(?im)^(\s*SaveConfig\s*=\s*)true(\s*)$`)

func wgStopConfiguration(raw []byte) []byte {
	return wgSaveConfigEnabled.ReplaceAll(raw, []byte(`${1}false${2}`))
}
func prepareWGRollbackFiles(path string, spec *rollbackSpec, files []*FileRestore) error {
	if spec.Kind != "wireguard" || len(spec.BeforeFile) == 0 {
		return nil
	}
	for _, file := range files {
		if file.ExpectedHash == "" {
			continue
		}
		raw, err := os.ReadFile(file.Target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		patched := wgStopConfiguration(raw)
		if string(raw) == string(patched) {
			continue
		}
		file.ExpectedHash = fingerprint(string(patched))
		if err = writeJSON(path, spec); err != nil {
			return err
		}
		if err = atomicFile(file.Target, patched, 0600); err != nil {
			return err
		}
	}
	return nil
}
