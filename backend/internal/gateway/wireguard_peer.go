package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const wgDisabledPeerPrefix = "# FoXray-Disabled-Peer: "

func mergeWGPeerRows(configured any, live []M) []M {
	result := []M{}
	byKey := map[string]M{}
	for _, row := range live {
		byKey[str(row["public_key"])] = row
	}
	// Converting through JSON honors the profile's secret-excluding tags.
	data, _ := json.Marshal(configured)
	var saved []M
	_ = json.Unmarshal(data, &saved)
	for _, row := range saved {
		key := str(row["public_key"])
		runtime := byKey[key]
		for field, value := range runtime {
			row[field] = value
		}
		row["runtime_present"] = runtime != nil
		row["configured"] = true
		result = append(result, row)
		delete(byKey, key)
	}
	keys := []string{}
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		byKey[key]["configured"] = false
		result = append(result, byKey[key])
	}
	return result
}

func commentWGPeer(block string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(block, "\n") {
		if line != "" {
			out.WriteString(wgDisabledPeerPrefix + line)
		}
	}
	return out.String()
}

// Keep every original directive and comment, including unsupported peer options.
func rewriteWGPeerState(raw, key string, disabled bool) (string, error) {
	lines := strings.SplitAfter(raw, "\n")
	start, end, matches := -1, len(lines), 0
	inPeer, blockStart := false, -1
	for i, original := range lines {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(original), wgDisabledPeerPrefix))
		if strings.HasPrefix(line, "[") {
			if start >= 0 && end == len(lines) {
				end = i
			}
			inPeer = strings.EqualFold(line, "[Peer]")
			blockStart = i
		}
		parts := strings.SplitN(line, "=", 2)
		if inPeer && len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "PublicKey") && strings.TrimSpace(parts[1]) == key {
			matches++
			start = blockStart
		}
	}
	if matches != 1 {
		return "", errors.New("Peer 不存在或公钥重复")
	}
	for i := start; i < end; i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), wgDisabledPeerPrefix) {
			prefix := strings.Index(line, wgDisabledPeerPrefix)
			line = line[:prefix] + strings.TrimPrefix(line[prefix:], wgDisabledPeerPrefix)
		}
		if disabled && line != "" {
			line = wgDisabledPeerPrefix + line
		}
		lines[i] = line
	}
	// wg-quick SaveConfig would discard the saved commented peer on shutdown.
	// Keep the durable user configuration authoritative for later restarts.
	return string(wgStopConfiguration([]byte(strings.Join(lines, "")))), nil
}

func WireGuardPeerState(name, key string) error {
	profile, err := readWG(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return syncWGPeer(ctx, SystemRunner{}, name, key, profile)
}

func syncWGPeer(ctx context.Context, runner Runner, name, key string, profile *WGProfile) error {
	if iface(name) != nil || !wgKey(key) {
		return errors.New("Peer 参数无效")
	}
	var peer *WGPeer
	for i := range profile.Peers {
		if profile.Peers[i].PublicKey == key {
			if peer != nil {
				return errors.New("Peer 公钥重复")
			}
			peer = &profile.Peers[i]
		}
	}
	if peer == nil {
		return errors.New("Peer 不存在")
	}
	if peer.Disabled {
		_, err := runner.Run(ctx, []string{"wg", "set", name, "peer", key, "remove"}, "")
		return err
	}
	allowed, err := wgAllowedInput(peer.AllowedIPs)
	if err != nil || len(allowed) == 0 {
		return errors.New("Peer 缺少有效 AllowedIPs")
	}
	// Enabling an identical prefix owned by another live peer would steal it.
	raw, err := runner.Run(ctx, []string{"wg", "show", name, "allowed-ips"}, "")
	if err != nil {
		return errors.New("无法核对运行中 Peer 路由")
	}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.ReplaceAll(line, ",", " "))
		if len(fields) < 2 || fields[0] == key {
			continue
		}
		for _, existing := range fields[1:] {
			for _, wanted := range allowed {
				if existing == wanted {
					return errors.New("此 AllowedIPs 已由其他运行中 Peer 使用")
				}
			}
		}
	}
	// addconf changes only this peer. Never send the interface private key.
	single := &WGProfile{Peers: []WGPeer{*peer}}
	config := single.render()
	index := strings.Index(config, "[Peer]")
	if index < 0 {
		return errors.New("Peer 配置无效")
	}
	_, err = runner.Run(ctx, []string{"wg", "addconf", name, "/dev/stdin"}, config[index:])
	return err
}

func (n *Network) planWGPeerState(plan *NetworkPlan, name, key string, disabled bool) error {
	return n.planWGPeerStateFile(plan, name, key, disabled, "/etc/wireguard/"+name+".conf")
}

func (n *Network) planWGPeerStateFile(plan *NetworkPlan, name, key string, disabled bool, path string) error {
	if iface(name) != nil || !wgKey(key) {
		return errors.New("Peer 参数无效")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("Peer 启停只支持已有普通配置文件")
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return errors.New("无法读取接口配置")
	}
	payload, err := rewriteWGPeerState(string(old), key, disabled)
	if err != nil {
		return err
	}
	state := n.app.service("wg-quick@" + name)
	active := str(state["state"]) == "active"
	live, err := n.app.command(4, "", "wg", "show", "interfaces")
	if err != nil {
		return errors.New("无法核对接口运行状态")
	}
	running := false
	for _, entry := range strings.Fields(live) {
		running = running || entry == name
	}
	if running != active {
		return errors.New("接口运行状态不一致或由其他管理器运行")
	}
	backup := filepath.Join(n.app.base, "private", "network-backups", plan.ID+".conf")
	plan.restoreFile = &FileRestore{Target: path, Backup: backup, Existed: true, OldHash: fingerprint(string(old))}
	plan.precondition = func() error {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("接口配置状态已变化")
		}
		current, err := os.ReadFile(path)
		if err != nil || fingerprint(string(current)) != fingerprint(string(old)) {
			return errors.New("接口配置已变化，请重新预览")
		}
		if (str(n.app.service("wg-quick@" + name)["state"]) == "active") != active {
			return errors.New("接口运行状态已变化，请重新预览")
		}
		return nil
	}
	if active {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		step := Step{Args: []string{exe, "--wireguard-peer", key, "--interface", name}}
		plan.steps = []Step{step}
		// Restore the file first, then synchronize only the selected peer. This is
		// armed before any mutation, including command failure or process death.
		plan.afterRestore = []Step{step}
		plan.postcondition = func() error {
			peers, err := n.app.command(4, "", "wg", "show", name, "peers")
			if err != nil {
				return errors.New("无法核对 Peer 操作结果")
			}
			present := false
			for _, value := range strings.Fields(peers) {
				present = present || value == key
			}
			if present == disabled {
				return errors.New("Peer 运行状态未达到预期")
			}
			return nil
		}
	}
	plan.applyFiles = func() error {
		if err := atomicFile(backup, old, 0600); err != nil {
			return err
		}
		plan.restoreFile.ExpectedHash = fingerprint(payload)
		if err := n.writeSpec(plan, 0); err != nil {
			return err
		}
		return atomicFile(path, []byte(payload), 0600)
	}
	action := "启用"
	if disabled {
		action = "停用"
	}
	plan.Summary = action + " WireGuard Peer · " + name
	plan.Preview = []string{action + "选定 Peer，保留其全部配置；接口及其他 Peer 保持运行"}
	plan.Warnings = []string{"只改变此 Peer 的通信状态；相关路由保留，停用期间不会继续转发到此 Peer。120 秒内确认，否则恢复。", "SaveConfig 自动关闭，避免接口停止时覆盖已停用 Peer 的配置。"}
	return nil
}
