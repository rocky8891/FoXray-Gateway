package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const wgPeerProbePrefix = "# FoXray-Peer-Probe: "
const wgPeerGuardProtocol = "242"
const wgPeerProbeProtocol = "241"
const wgMissLimit = 10
const wgRecoveryLimit = 3

type WGPeerProbe struct {
	Enabled      bool     `json:"enabled"`
	Target       string   `json:"target"`
	AutoWithdraw bool     `json:"auto_withdraw"`
	Routes       []string `json:"routes"`
}

type wgPeerHealth struct {
	state        string
	reason       string
	failure      int
	recovery     int
	lastProbe    time.Time
	lastSuccess  time.Time
	latency      float64
	routes       []M
	config       string
	demonstrated bool
}

func wgProfileAutoWithdraw(p *WGProfile) bool {
	for _, peer := range p.Peers {
		if peer.Probe.Enabled && peer.Probe.AutoWithdraw {
			return true
		}
	}
	return false
}

func wgProbeTarget(profile *WGProfile, peer WGPeer) (netip.Addr, error) {
	target, err := netip.ParseAddr(strings.TrimSpace(peer.Probe.Target))
	if err != nil || target.Zone() != "" || target.Is4In6() || !target.IsGlobalUnicast() || target.IsLoopback() || !wgCovers(netip.PrefixFrom(target, target.BitLen()), peer.AllowedIPs) {
		return netip.Addr{}, errors.New("探测目标必须是此 Peer 的 AllowedIPs 覆盖的单播 IP")
	}
	for _, value := range profile.Addresses {
		own, err := netip.ParsePrefix(value)
		if err == nil && own.Addr() == target {
			return netip.Addr{}, errors.New("探测目标不能是本接口地址")
		}
	}
	return target, nil
}

func wgPeerRouteMode(p *WGProfile, peer WGPeer) (prefixes []string, table, protocol string, err error) {
	if p.Network != nil && p.Network.RouteMode == "manual" {
		network, err := validateWGNetwork(*p.Network)
		if err != nil {
			return nil, "", "", err
		}
		for _, value := range network.Routes {
			_, prefix, err := canonicalSource(value)
			if err == nil && wgCovers(prefix, peer.AllowedIPs) {
				prefixes = append(prefixes, prefix.String())
			}
		}
		return prefixes, network.RouteTable, "187", nil
	}
	if p.Table == "off" || (p.Network != nil && p.Network.RouteMode == "off") {
		return nil, "", "", nil
	}
	table = "254"
	if p.Table != "" && p.Table != "auto" {
		number, err := routeTable(p.Table)
		if err != nil {
			return nil, "", "", err
		}
		table = strconv.Itoa(number)
	}
	prefixes, err = wgAllowedInput(peer.AllowedIPs)
	return prefixes, table, "boot", err
}

func wgProbePeerOwner(p *WGProfile, target netip.Addr) string {
	owner := ""
	maxBits := -1
	for _, peer := range p.Peers {
		if peer.Disabled {
			continue
		}
		for _, value := range peer.AllowedIPs {
			_, prefix, err := canonicalSource(value)
			if err != nil || !prefix.Contains(target) {
				continue
			}
			if prefix.Bits() > maxBits {
				owner, maxBits = peer.PublicKey, prefix.Bits()
			} else if prefix.Bits() == maxBits && owner != peer.PublicKey {
				owner = ""
			}
		}
	}
	return owner
}

func validateWGPeerProbes(p *WGProfile) error {
	for _, peer := range p.Peers {
		if !peer.Probe.Enabled && !peer.Probe.AutoWithdraw {
			continue
		}
		target, err := wgProbeTarget(p, peer)
		if err != nil {
			return err
		}
		if !peer.Disabled && wgProbePeerOwner(p, target) != peer.PublicKey {
			return errors.New("探测目标与其他 Peer 的 AllowedIPs 冲突")
		}
		if !peer.Probe.AutoWithdraw {
			continue
		}
		if !peer.Probe.Enabled || len(peer.Probe.Routes) == 0 || len(peer.Probe.Routes) > 50 {
			return errors.New("自动撤路由需要开启主动探测并选择 1–50 条业务路由")
		}
		configured, table, _, err := wgPeerRouteMode(p, peer)
		if err != nil {
			return err
		}
		if table != "254" {
			return errors.New("自动撤路由仅支持主路由表中的接口托管路由")
		}
		chosen := map[netip.Prefix]bool{}
		for _, route := range peer.Probe.Routes {
			_, prefix, err := canonicalSource(route)
			if err != nil || prefix.Bits() == 0 || chosen[prefix] || !slicesContain(configured, prefix.String()) {
				return errors.New("只能选择此 Peer 唯一拥有的非默认托管路由")
			}
			chosen[prefix] = true
			for _, other := range p.Peers {
				if other.PublicKey == peer.PublicKey {
					continue
				}
				for _, value := range other.AllowedIPs {
					_, overlap, err := canonicalSource(value)
					if err == nil && overlap.Overlaps(prefix) {
						return errors.New("待撤销的路由与其他 Peer 的 AllowedIPs 重叠")
					}
				}
			}
		}
		for prefix := range chosen {
			if prefix.Contains(target) && prefix.Bits() == target.BitLen() {
				return errors.New("探测目标的精确主机路由不能作为撤销项")
			}
		}
	}
	return nil
}

func slicesContain(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func wgFamily(address netip.Addr) string {
	if address.Is4() {
		return "-4"
	}
	return "-6"
}

func wgLiveProbe(ctx context.Context, runner Runner, p *WGProfile, peer WGPeer, target netip.Addr) (float64, bool, error) {
	raw, err := runner.Run(ctx, []string{"wg", "show", p.Name, "allowed-ips"}, "")
	if err != nil {
		return 0, false, errors.New("接口未运行或无法读取 Peer")
	}
	live := &WGProfile{}
	for _, line := range textLines(raw) {
		fields := strings.Fields(strings.ReplaceAll(line, ",", " "))
		if len(fields) > 1 {
			live.Peers = append(live.Peers, WGPeer{PublicKey: fields[0], AllowedIPs: fields[1:]})
		}
	}
	if wgProbePeerOwner(live, target) != peer.PublicKey {
		return 0, false, errors.New("运行时 AllowedIPs 未将探测目标交给此 Peer")
	}
	route, err := runner.Run(ctx, []string{"ip", "-j", wgFamily(target), "route", "get", target.String()}, "")
	var routes []M
	if err != nil || json.Unmarshal([]byte(route), &routes) != nil || len(routes) != 1 || str(routes[0]["dev"]) != p.Name {
		return 0, false, errors.New("探测目标当前未通过此接口路由")
	}
	reply, err := runner.Run(ctx, []string{"ping", wgFamily(target), "-n", "-I", p.Name, "-c", "1", "-W", "2", "--", target.String()}, "")
	summary := pingCount.FindStringSubmatch(reply)
	if len(summary) != 3 || summary[1] != "1" || ctx.Err() != nil {
		return 0, false, errors.New("探测程序未返回有效结果")
	}
	if summary[2] == "0" {
		if strings.Contains(reply, "errors") || strings.Contains(reply, "Unreachable") {
			return 0, false, errors.New("探测程序报告网络错误")
		}
		return 0, false, nil
	}
	if summary[2] != "1" || err != nil {
		return 0, false, errors.New("探测程序报告异常")
	}
	latency := -1.
	if times := pingAverage.FindStringSubmatch(reply); len(times) == 2 {
		latency, _ = strconv.ParseFloat(times[1], 64)
	}
	return latency, true, nil
}

func (h *wgPeerHealth) observe(reply bool, err error, latency float64) {
	h.lastProbe = time.Now()
	if err != nil {
		h.state, h.reason, h.failure, h.recovery = "无法探测", err.Error(), 0, 0
		return
	}
	h.reason = ""
	if reply {
		h.failure = 0
		h.lastSuccess, h.latency, h.demonstrated = h.lastProbe, latency, true
		h.recovery = min(h.recovery+1, wgRecoveryLimit)
		if h.recovery == wgRecoveryLimit {
			h.state = "正常"
		} else if h.state != "失联" {
			h.state = "检测中"
		}
		return
	}
	h.recovery = 0
	h.failure = min(h.failure+1, wgMissLimit)
	if h.failure == wgMissLimit || h.state == "失联" {
		h.state = "失联"
	} else {
		h.state = "探测异常"
	}
}

func wgGuardRouteState(ctx context.Context, runner Runner, p *WGProfile, peer WGPeer, destination string, desired string) (string, error) {
	_, prefix, err := canonicalSource(destination)
	if err != nil {
		return "未确认", err
	}
	_, table, protocol, err := wgPeerRouteMode(p, peer)
	if err != nil {
		return "未确认", err
	}
	family := wgFamily(prefix.Addr())
	raw, err := runner.Run(ctx, []string{"ip", "-j", family, "route", "show", "table", table, "exact", prefix.String()}, "")
	var rows []M
	if err != nil || json.Unmarshal([]byte(raw), &rows) != nil {
		return "未确认", errors.New("无法读取实际路由")
	}
	if len(rows) != 1 {
		return "未确认", errors.New("目标前缀没有唯一的托管路由")
	}
	row := rows[0]
	kind := firstNonempty(str(row["type"]), "unicast")
	proto := "boot"
	if row["protocol"] != nil {
		proto = strOrNumber(row["protocol"])
	}
	if proto == "isis" {
		proto = "187"
	}
	if proto == "3" {
		proto = "boot"
	}
	current := "未确认"
	metric, metricOK := row["metric"].(float64)
	expectedMetric := !metricOK || metric == 0 || (family == "-6" && metric == 1024)
	if kind == "unicast" && proto == protocol && str(row["dev"]) == p.Name && expectedMetric && row["gateway"] == nil && row["prefsrc"] == nil && row["via"] == nil && row["multipath"] == nil && row["nhid"] == nil && row["encap"] == nil && row["metrics"] == nil {
		current = "已安装"
	}
	if kind == "unreachable" && proto == wgPeerGuardProtocol && expectedMetric {
		current = "已撤销"
	}
	if desired == "" {
		return current, nil
	}
	if current == "未确认" {
		return "未确认", errors.New("实际路由不符合托管条件")
	}
	if desired == "withdraw" && current == "已安装" {
		_, err = runner.Run(ctx, wgGuardRouteCommand(family, prefix.String(), table, wgPeerGuardProtocol, "unreachable", "", row), "")
		if err != nil {
			return "撤销失败", err
		}
		state, err := wgGuardRouteState(ctx, runner, p, peer, destination, "")
		if err != nil || state != "已撤销" {
			return "撤销失败", errors.New("无法确认业务路由已撤销")
		}
		return state, nil
	}
	if desired == "restore" && current == "已撤销" {
		_, err = runner.Run(ctx, wgGuardRouteCommand(family, prefix.String(), table, protocol, "unicast", p.Name, row), "")
		if err != nil {
			return "恢复失败", err
		}
		state, err := wgGuardRouteState(ctx, runner, p, peer, destination, "")
		if err != nil || state != "已安装" {
			return "恢复失败", errors.New("无法确认业务路由已恢复")
		}
		return "已恢复", nil
	}
	return current, nil
}

func wgGuardRouteCommand(family, prefix, table, protocol, kind, device string, row M) []string {
	args := []string{"ip", family, "route", "replace", kind, prefix, "table", table, "proto", protocol}
	if device != "" {
		args = append(args, "dev", device)
	}
	if row["metric"] != nil {
		args = append(args, "metric", strOrNumber(row["metric"]))
	}
	return args
}

func (n *Network) wgPeerMonitorLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.app.ctx.Done():
			return
		case <-ticker.C:
			if n.app.moduleEnabled("wireguard") {
				n.monitorWGPeers()
			}
		}
	}
}

func (n *Network) monitorWGPeers() {
	paths, _ := filepath.Glob("/etc/wireguard/*.conf")
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".conf")
		p, err := readWG(name)
		if err != nil {
			continue
		}
		for _, peer := range p.Peers {
			if peer.Disabled || !peer.Probe.Enabled {
				continue
			}
			wg.Go(func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				ctx, cancel := context.WithTimeout(n.app.ctx, 4*time.Second)
				defer cancel()
				target, err := wgProbeTarget(p, peer)
				latency, reply := -1., false
				if err == nil {
					latency, reply, err = wgLiveProbe(ctx, n.app.runner, p, peer, target)
				}
				n.recordWGPeerProbe(p, peer, latency, reply, err)
			})
		}
	}
	wg.Wait()
}

func wgPeerHealthKey(name, peer string) string { return name + "|" + peer }

func (n *Network) recordWGPeerProbe(p *WGProfile, peer WGPeer, latency float64, reply bool, err error) {
	key := wgPeerHealthKey(p.Name, peer.PublicKey)
	revision := fingerprintWGPeerProbe(peer.Probe)
	n.peerHealthMu.RLock()
	health := n.peerHealth[key]
	n.peerHealthMu.RUnlock()
	if health.config != revision {
		health = wgPeerHealth{config: revision}
	}
	health.observe(reply, err, latency)
	if peer.Probe.AutoWithdraw && validateWGPeerProbes(p) == nil && n.mu.TryLock() {
		canChange := n.app.mutate.TryLock()
		if canChange {
			for _, plan := range n.plans {
				if plan.Status == "awaiting_confirmation" || plan.Status == "armed" {
					canChange = false
					break
				}
			}
			if !canChange {
				n.app.mutate.Unlock()
			}
		}
		if canChange {
			if unlock, lockErr := lockWGPeerInterface(p.Name); lockErr == nil {
				latest, latestErr := readWG(p.Name)
				if latestErr == nil && fingerprint(latest) == fingerprint(p) && str(n.app.service("wg-quick@" + p.Name)["state"]) == "active" {
					n.updateWGPeerRoutes(p, peer, &health, err)
				}
				unlock()
			}
			n.app.mutate.Unlock()
		}
		n.mu.Unlock()
	}
	n.peerHealthMu.Lock()
	if n.peerHealth == nil {
		n.peerHealth = map[string]wgPeerHealth{}
	}
	n.peerHealth[key] = health
	n.peerHealthMu.Unlock()
}

func (n *Network) updateWGPeerRoutes(p *WGProfile, peer WGPeer, health *wgPeerHealth, probeErr error) {
	desired := ""
	if probeErr == nil && health.demonstrated && health.failure >= wgMissLimit {
		desired = "withdraw"
	}
	if probeErr == nil && health.recovery >= wgRecoveryLimit {
		desired = "restore"
	}
	if desired == "withdraw" {
		ctx, cancel := context.WithTimeout(n.app.ctx, 5*time.Second)
		err := ensureWGProbePath(ctx, n.app.runner, p, peer)
		cancel()
		if err != nil {
			health.routes = nil
			for _, route := range peer.Probe.Routes {
				health.routes = append(health.routes, M{"prefix": route, "status": "撤销失败", "error": err.Error()})
			}
			return
		}
	}
	previous := map[string]M{}
	for _, row := range health.routes {
		previous[str(row["prefix"])] = row
	}
	health.routes = nil
	allInstalled := true
	for _, destination := range peer.Probe.Routes {
		ctx, cancel := context.WithTimeout(n.app.ctx, 3*time.Second)
		state, err := wgGuardRouteState(ctx, n.app.runner, p, peer, destination, desired)
		cancel()
		old := previous[destination]
		if err != nil && desired == "withdraw" {
			state = "撤销失败"
		}
		if err != nil && desired == "restore" {
			state = "恢复失败"
		}
		if state == "已安装" && old["status"] == "已恢复" {
			state = "已恢复"
		}
		row := M{"prefix": destination, "status": state}
		if err != nil {
			row["error"] = err.Error()
		}
		if old["status"] != state {
			row["changed_at"] = now()
			if desired != "" && state != "已安装" {
				_ = n.app.audit("network.wireguard.peer.route", "WireGuard Peer 路由 · "+state, err == nil && state != "未确认", M{"interface": p.Name, "prefix": destination, "status": state})
			}
		} else {
			row["changed_at"] = old["changed_at"]
		}
		health.routes = append(health.routes, row)
		allInstalled = allInstalled && (state == "已安装" || state == "已恢复")
	}
	if desired == "restore" && allInstalled && wgProbeNeedsHostRoute(peer) {
		ctx, cancel := context.WithTimeout(n.app.ctx, 3*time.Second)
		defer cancel()
		if err := removeWGProbeHostRoute(ctx, n.app.runner, p.Name, peer); err != nil {
			_ = n.app.audit("network.wireguard.peer.route", "WireGuard Peer · 清理探测路由失败", false, M{"interface": p.Name})
		}
	}
}

func (n *Network) decorateWGPeers(name string, up bool, profile *WGProfile, configured any, inventory map[string][]M) []M {
	var rows []M
	raw, _ := json.Marshal(configured)
	_ = json.Unmarshal(raw, &rows)
	for _, row := range rows {
		probe := obj(row["probe"])
		status, reason := "未握手", ""
		health := wgPeerHealth{}
		n.peerHealthMu.RLock()
		health = n.peerHealth[wgPeerHealthKey(name, str(row["public_key"]))]
		n.peerHealthMu.RUnlock()
		if !up || yes(row["disabled"]) || !yes(probe["enabled"]) || health.config != fingerprintWGPeerProbe(probe) {
			health = wgPeerHealth{}
		}
		if yes(row["disabled"]) {
			status = "已停用"
		} else if !up {
			status, reason = "无法探测", "接口未运行"
		} else if !yes(probe["enabled"]) {
			if seconds, _ := strconv.ParseFloat(strOrNumber(row["handshake"]), 64); seconds > 0 {
				status = "握手成功"
			}
		} else if str(probe["target"]) == "" {
			status, reason = "无法探测", "未配置探测目标"
		} else {
			status, reason = firstNonempty(health.state, "检测中"), health.reason
		}
		var tunnel string
		for _, v := range strings.Fields(strings.ReplaceAll(str(row["allowed_ips"]), ",", " ")) {
			if _, p, err := canonicalSource(v); err == nil && p.Bits() == p.Addr().BitLen() {
				tunnel = p.Addr().String()
				break
			}
		}
		if tunnel == "" {
			for _, v := range arr(row["allowed_ips"]) {
				if _, p, err := canonicalSource(str(v)); err == nil && p.Bits() == p.Addr().BitLen() {
					tunnel = p.Addr().String()
					break
				}
			}
		}
		target := str(probe["target"])
		if yes(probe["enabled"]) && target != "" {
			if status == "正常" {
				if target == tunnel {
					status = "隧道正常"
				} else {
					status = "目标可达"
				}
			}
			if status == "失联" {
				if target == tunnel {
					status = "隧道不可达"
				} else {
					status = "目标不可达"
				}
			}
		}
		if profile != nil && !yes(probe["auto_withdraw"]) {
			for _, peer := range profile.Peers {
				if peer.PublicKey == str(row["public_key"]) {
					health.routes = n.wgPeerRouteInventory(profile, peer, inventory)
					break
				}
			}
		}
		report := M{"status": status, "reason": reason, "target": target, "tunnel": tunnel, "failures": health.failure, "recoveries": health.recovery, "last_probe": wgHealthTime(health.lastProbe), "last_success": wgHealthTime(health.lastSuccess), "routes": health.routes, "auto_withdraw": yes(probe["auto_withdraw"])}
		if !health.lastSuccess.IsZero() && health.latency >= 0 {
			report["latency"] = health.latency
		}
		row["health"] = report
	}
	return rows
}

func (n *Network) wgPeerRouteInventory(profile *WGProfile, peer WGPeer, inventory map[string][]M) []M {
	prefixes, table, _, err := wgPeerRouteMode(profile, peer)
	if err != nil || table == "" {
		return nil
	}
	var report []M
	for _, destination := range prefixes {
		_, prefix, err := canonicalSource(destination)
		if err != nil {
			continue
		}
		family := wgFamily(prefix.Addr())
		lookupTable := table
		if prefix.Bits() == 0 && (profile.Table == "" || profile.Table == "auto") && (profile.Network == nil || profile.Network.RouteMode != "manual") {
			lookupTable = "all"
		}
		key := family + "|" + lookupTable
		routes, read := inventory[key]
		if !read {
			ctx, cancel := context.WithTimeout(n.app.ctx, 2*time.Second)
			raw, err := n.app.runner.Run(ctx, []string{"ip", "-j", family, "route", "show", "table", lookupTable}, "")
			cancel()
			if err != nil || json.Unmarshal([]byte(raw), &routes) != nil {
				routes = nil
			} else if routes == nil {
				routes = []M{}
			}
			inventory[key] = routes
		}
		row := M{"prefix": destination, "status": "未安装精确路由"}
		if routes == nil {
			row["status"] = "未确认"
		}
		var covering netip.Prefix
		for _, route := range routes {
			if str(route["dev"]) != profile.Name {
				continue
			}
			routeDestination := str(route["dst"])
			if routeDestination == "default" && prefix.Bits() == 0 {
				row["status"] = "已安装"
				break
			}
			if addr, err := netip.ParseAddr(routeDestination); err == nil {
				routeDestination = netip.PrefixFrom(addr, addr.BitLen()).String()
			}
			_, installed, err := canonicalSource(routeDestination)
			if err != nil {
				continue
			}
			if installed == prefix {
				row["status"] = "已安装"
				break
			}
			// A directly connected kernel subnet can cover a host AllowedIPs entry.
			if str(route["protocol"]) == "kernel" && str(route["scope"]) == "link" &&
				installed.Bits() < prefix.Bits() && installed.Contains(prefix.Addr()) &&
				(!covering.IsValid() || installed.Bits() > covering.Bits()) {
				covering = installed
			}
		}
		if row["status"] == "未安装精确路由" && covering.IsValid() {
			row["status"] = "由直连网段覆盖"
			row["covering_prefix"] = covering.String()
		}
		report = append(report, row)
	}
	return report
}

func wgHealthTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return float64(value.UnixNano()) / 1e9
}

func fingerprintWGPeerProbe(value any) string {
	var probe WGPeerProbe
	raw, _ := json.Marshal(value)
	_ = json.Unmarshal(raw, &probe)
	return fingerprint(probe)
}

func ensureWGProbePath(ctx context.Context, runner Runner, profile *WGProfile, peer WGPeer) error {
	target, err := wgProbeTarget(profile, peer)
	if err != nil {
		return err
	}
	for _, value := range peer.Probe.Routes {
		_, prefix, err := canonicalSource(value)
		if err != nil {
			return err
		}
		if !prefix.Contains(target) {
			continue
		}
		route, readErr := runner.Run(ctx, []string{"ip", "-j", wgFamily(target), "route", "get", target.String()}, "")
		var rows []M
		if readErr != nil || json.Unmarshal([]byte(route), &rows) != nil || len(rows) != 1 || str(rows[0]["dev"]) != profile.Name {
			return errors.New("无法保留经此 Peer 的探测路径")
		}
		host := netip.PrefixFrom(target, target.BitLen()).String()
		raw, err := runner.Run(ctx, []string{"ip", "-j", wgFamily(target), "route", "show", "table", "254", "exact", host}, "")
		if err != nil || json.Unmarshal([]byte(raw), &rows) != nil {
			return errors.New("无法核对探测主机路由")
		}
		if len(rows) == 1 && str(rows[0]["dev"]) == profile.Name {
			return nil
		}
		if len(rows) > 0 {
			return errors.New("探测主机路由已由其他对象占用")
		}
		_, err = runner.Run(ctx, []string{"ip", wgFamily(target), "route", "add", host, "dev", profile.Name, "table", "254", "proto", wgPeerProbeProtocol}, "")
		if err != nil {
			return errors.New("无法安装专用探测路径")
		}
		return nil
	}
	return nil
}

func wgProbeNeedsHostRoute(peer WGPeer) bool {
	target, err := netip.ParseAddr(peer.Probe.Target)
	if err != nil {
		return false
	}
	for _, value := range peer.Probe.Routes {
		if _, prefix, err := canonicalSource(value); err == nil && prefix.Contains(target) {
			return true
		}
	}
	return false
}

func removeWGProbeHostRoute(ctx context.Context, runner Runner, name string, peer WGPeer) error {
	if !wgProbeNeedsHostRoute(peer) {
		return nil
	}
	target, _ := netip.ParseAddr(peer.Probe.Target)
	host := netip.PrefixFrom(target, target.BitLen()).String()
	family := wgFamily(target)
	raw, err := runner.Run(ctx, []string{"ip", "-j", family, "route", "show", "table", "254", "exact", host, "dev", name, "proto", wgPeerProbeProtocol}, "")
	var rows []M
	if err != nil || json.Unmarshal([]byte(raw), &rows) != nil {
		return errors.New("无法核对专用探测路由")
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) != 1 || str(rows[0]["dev"]) != name {
		return errors.New("无法确认专用探测路由的归属")
	}
	args := []string{"ip", family, "route", "del", host, "dev", name, "table", "254", "proto", wgPeerProbeProtocol}
	if metric := rows[0]["metric"]; metric != nil {
		args = append(args, "metric", strOrNumber(metric))
	}
	_, err = runner.Run(ctx, args, "")
	return err
}

func purgeWGPeerGuards(ctx context.Context, runner Runner, name string, peer WGPeer) error {
	for _, value := range peer.Probe.Routes {
		_, prefix, err := canonicalSource(value)
		if err != nil {
			return err
		}
		raw, err := runner.Run(ctx, []string{"ip", "-j", wgFamily(prefix.Addr()), "route", "show", "table", "254", "exact", prefix.String(), "proto", wgPeerGuardProtocol}, "")
		var rows []M
		if err != nil || json.Unmarshal([]byte(raw), &rows) != nil {
			return errors.New("无法核对 Peer 阻断路由")
		}
		for _, row := range rows {
			if str(row["type"]) != "unreachable" {
				continue
			}
			args := []string{"ip", wgFamily(prefix.Addr()), "route", "del", "unreachable", prefix.String(), "table", "254", "proto", wgPeerGuardProtocol}
			if row["metric"] != nil {
				args = append(args, "metric", strOrNumber(row["metric"]))
			}
			if _, err := runner.Run(ctx, args, ""); err != nil {
				return errors.New("无法清理 Peer 阻断路由")
			}
		}
	}
	return removeWGProbeHostRoute(ctx, runner, name, peer)
}

func lockWGPeerInterface(name string) (func(), error) {
	dir := "/run/foxray-wireguard"
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return lockPlan(filepath.Join(dir, name+".json.lock"))
}

func (n *Network) testWGInterfaceProbe(ctx context.Context, name, targetIP string) (M, error) {
	target, err := netip.ParseAddr(strings.TrimSpace(targetIP))
	if err != nil {
		return nil, errors.New("请指定有效的探测目标 IP")
	}
	profile, err := readWG(name)
	if err != nil {
		return nil, err
	}
	key := wgProbePeerOwner(profile, target)
	if key == "" {
		return nil, errors.New("探测目标必须由此接口下唯一的已启用 Peer 负责")
	}
	return n.testWGPeerProbe(ctx, name, key, target.String())
}

func (n *Network) testWGPeerProbe(ctx context.Context, name, key, targetIP string) (M, error) {
	if !wgKey(key) {
		return nil, errors.New("Peer 公钥无效")
	}
	profile, err := readWG(name)
	if err != nil {
		return nil, err
	}
	for _, peer := range profile.Peers {
		if peer.PublicKey != key || peer.Disabled {
			continue
		}
		peer.Probe.Target = targetIP
		target, err := wgProbeTarget(profile, peer)
		if err != nil {
			return nil, err
		}
		child, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		latency, ok, err := wgLiveProbe(child, n.app.runner, profile, peer, target)
		if err != nil {
			return nil, err
		}
		return M{"ok": ok, "latency": latency}, nil
	}
	return nil, errors.New("请先保存并启用此 Peer")
}
