package gateway

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type WGPeer struct {
	PublicKey       string   `json:"public_key"`
	AllowedIPs      []string `json:"allowed_ips"`
	Endpoint        string   `json:"endpoint"`
	Keepalive       int      `json:"keepalive"`
	PresharedKey    string   `json:"-"`
	HasPresharedKey bool     `json:"has_preshared_key"`
}
type WGProfile struct {
	Remark        string     `json:"remark"`
	Network       *WGNetwork `json:"network,omitempty"`
	PublicKey     string     `json:"public_key"`
	Name          string     `json:"name"`
	Addresses     []string   `json:"addresses"`
	DNS           []string   `json:"dns"`
	ListenPort    int        `json:"listen_port"`
	MTU           int        `json:"mtu"`
	Table         string     `json:"table"`
	Peers         []WGPeer   `json:"peers"`
	HasPrivateKey bool       `json:"has_private_key"`
	PrivateKey    string     `json:"-"`
	extras        []string
}

func readWG(name string) (*WGProfile, error) {
	if e := iface(name); e != nil {
		return nil, e
	}
	if !regular("/etc/wireguard/" + name + ".conf") {
		return nil, errors.New("配置不存在或不是受管普通文件")
	}
	raw, e := os.ReadFile("/etc/wireguard/" + name + ".conf")
	if e != nil {
		return nil, errors.New("配置不存在")
	}
	return parseWG(name, string(raw))
}
func parseWG(name, raw string) (*WGProfile, error) {
	p := &WGProfile{Name: name, Remark: readWGRemark(raw), Addresses: []string{}, DNS: []string{}, Peers: []WGPeer{}}
	section := ""
	peer := -1
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, wgMetadataPrefix) {
			var v WGNetwork
			if json.Unmarshal([]byte(strings.TrimPrefix(line, wgMetadataPrefix)), &v) == nil {
				p.Network = &v
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(line)
			if section == "[peer]" {
				p.Peers = append(p.Peers, WGPeer{AllowedIPs: []string{}})
				peer = len(p.Peers) - 1
			}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
		if section == "[interface]" {
			switch key {
			case "privatekey":
				p.PrivateKey = value
				p.HasPrivateKey = value != ""
			case "address":
				p.Addresses = splitComma(value)
			case "dns":
				p.DNS = splitComma(value)
			case "listenport":
				p.ListenPort, _ = strconv.Atoi(value)
			case "mtu":
				p.MTU, _ = strconv.Atoi(value)
			case "table":
				p.Table = value
			default:
				if line != "PostUp = /opt/foxray/foxray --wireguard-network up --interface %i" && line != "PreDown = /opt/foxray/foxray --wireguard-network down --interface %i" {
					p.extras = append(p.extras, line)
				}
			}
		} else if section == "[peer]" && peer >= 0 {
			v := &p.Peers[peer]
			switch key {
			case "publickey":
				v.PublicKey = value
			case "presharedkey":
				v.PresharedKey = value
				v.HasPresharedKey = value != ""
			case "allowedips":
				v.AllowedIPs = splitComma(value)
			case "endpoint":
				v.Endpoint = value
			case "persistentkeepalive":
				v.Keepalive, _ = strconv.Atoi(value)
			}
		}
	}
	p.PublicKey = publicWGKey(p.PrivateKey)
	return p, nil
}
func splitComma(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func (p *WGProfile) render() string {
	var b strings.Builder
	if p.Remark != "" {
		value, _ := json.Marshal(p.Remark)
		b.WriteString(wgRemarkPrefix + string(value) + "\n")
	}
	if p.Network != nil {
		meta, _ := json.Marshal(p.Network)
		b.WriteString(wgMetadataPrefix + string(meta) + "\n")
	}
	b.WriteString("[Interface]\nPrivateKey = " + p.PrivateKey + "\n")
	if len(p.Addresses) > 0 {
		b.WriteString("Address = " + strings.Join(p.Addresses, ", ") + "\n")
	}
	if p.ListenPort > 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", p.ListenPort)
	}
	if p.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", p.MTU)
	}
	if p.Table != "" {
		b.WriteString("Table = " + p.Table + "\n")
	}
	if len(p.DNS) > 0 {
		b.WriteString("DNS = " + strings.Join(p.DNS, ", ") + "\n")
	}
	if p.Network != nil && (p.Network.RouteMode == "manual" || p.Network.SNAT) {
		b.WriteString("PostUp = /opt/foxray/foxray --wireguard-network up --interface %i\nPreDown = /opt/foxray/foxray --wireguard-network down --interface %i\n")
	}
	for _, line := range p.extras {
		b.WriteString(line + "\n")
	}
	for _, v := range p.Peers {
		b.WriteString("\n[Peer]\nPublicKey = " + v.PublicKey + "\n")
		if v.PresharedKey != "" {
			b.WriteString("PresharedKey = " + v.PresharedKey + "\n")
		}
		b.WriteString("AllowedIPs = " + strings.Join(v.AllowedIPs, ", ") + "\n")
		if v.Endpoint != "" {
			b.WriteString("Endpoint = " + v.Endpoint + "\n")
		}
		if v.Keepalive > 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", v.Keepalive)
		}
	}
	return b.String()
}
func wgKey(s string) bool {
	b, e := base64.StdEncoding.DecodeString(s)
	return e == nil && len(b) == 32
}
func (n *Network) planWireGuard(plan *NetworkPlan, d M) error {
	item := obj(d["item"])
	name, op := str(item["name"]), str(d["operation"])
	if e := iface(name); e != nil {
		return e
	}
	if op == "remark" {
		return n.planWGRemarkFile(plan, name, "/etc/wireguard/"+name+".conf", item["remark"])
	}
	if name == "wg-home" && !yes(d["allow_protected"]) {
		return errors.New("wg-home 为内置保护接口，须明确确认本次接口操作")
	}
	a := n.app
	path := "/etc/wireguard/" + name + ".conf"
	state := a.service("wg-quick@" + name)
	active := str(state["state"]) == "active"
	live, _ := a.command(3, "", "wg", "show", "interfaces")
	for _, running := range strings.Fields(live) {
		if running == name && !active {
			return errors.New("此接口由其他网络管理器运行，暂不使用 wg-quick 覆盖")
		}
	}
	info, statErr := os.Lstat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return errors.New("无法确认接口配置状态")
	}
	if statErr == nil && !info.Mode().IsRegular() {
		return errors.New("接口配置必须为普通文件，不接受符号链接")
	}
	oldBytes, readErr := os.ReadFile(path)
	exists := statErr == nil
	if exists && readErr != nil {
		return errors.New("无法读取现有配置，拒绝覆盖")
	}
	if op == "start" || op == "stop" || op == "restart" || op == "enable" || op == "disable" {
		if !exists {
			return errors.New("没有可用的 wg-quick 配置")
		}
		restore := "stop"
		if active {
			restore = "start"
		}
		if op == "restart" && active {
			restore = "restart"
		}
		if op == "enable" || op == "disable" {
			restore = "disable"
			if yes(state["autostart"]) {
				restore = "enable"
			}
		}
		if op == "start" || op == "restart" {
			plan.applyFiles = a.refreshWGGuard
		}
		plan.steps = []Step{{Args: []string{"systemctl", op, "wg-quick@" + name}}}
		plan.undo = []Step{{Args: []string{"systemctl", restore, "wg-quick@" + name}}}
		plan.Summary = "WireGuard " + name + " · " + op
		plan.Warnings = append(plan.Warnings, "只操作此接口；停止或重启会中断此 VPN 自身连接")
		return nil
	}
	if active {
		return errors.New("请先单独停止该接口，再修改地址、Peer 或删除配置；其他 VPN 保持运行")
	}
	if op == "add" && exists {
		return errors.New("同名接口配置已存在")
	}
	if op != "add" && !exists {
		return errors.New("接口配置不存在")
	}
	backup := filepath.Join(a.base, "private", "network-backups", plan.ID+".conf")
	plan.restoreFile = &FileRestore{Target: path, Backup: backup, Existed: exists, OldHash: fingerprint(string(oldBytes))}
	plan.precondition = func() error {
		current, e := os.ReadFile(path)
		if exists {
			if e != nil || fingerprint(string(current)) != fingerprint(string(oldBytes)) {
				return errors.New("接口配置已变化，请重新预览")
			}
		} else if e == nil || !os.IsNotExist(e) {
			return errors.New("同名配置已经存在或无法确认")
		}
		return nil
	}
	plan.undoFiles = func() error {
		if exists {
			return atomicFile(path, oldBytes, 0600)
		}
		return os.Remove(path)
	}
	if op == "delete" {
		if yes(state["autostart"]) {
			plan.steps = []Step{{Args: []string{"systemctl", "disable", "wg-quick@" + name}}}
			plan.undo = []Step{{Args: []string{"systemctl", "enable", "wg-quick@" + name}}}
		}
		plan.applyFiles = func() error {
			if e := atomicFile(backup, oldBytes, 0600); e != nil {
				return e
			}
			plan.restoreFile.ExpectedHash = "delete"
			if e := n.writeSpec(plan, 0); e != nil {
				return e
			}
			return os.Remove(path)
		}
		plan.Summary = "删除 WireGuard 配置 · " + name
		return nil
	}
	p := &WGProfile{Name: name, Addresses: []string{}, DNS: []string{}, Peers: []WGPeer{}, Table: str(item["table"])}
	old := &WGProfile{}
	if !exists {
		p.extras = []string{"FwMark = 65743"}
	}
	if exists {
		var e error
		old, e = parseWG(name, string(oldBytes))
		if e != nil {
			return e
		}
		p.PrivateKey = old.PrivateKey
		p.extras = old.extras
		p.Network = old.Network
		p.Remark = old.Remark
	}
	if value, present := item["remark"]; present {
		remark, err := validateWGRemark(value)
		if err != nil {
			return err
		}
		p.Remark = remark
	}
	if !exists && str(item["key_id"]) == "" {
		return errors.New("请先为新增接口生成独立密钥对")
	}
	if !exists && str(item["private_key"]) != "" {
		return errors.New("新增接口只使用独立生成的密钥，不读取或复用其他私钥")
	}
	if input := str(item["private_key"]); input != "" {
		if !wgKey(input) {
			return errors.New("私钥格式无效")
		}
		p.PrivateKey = input
	}
	for _, v := range arr(item["addresses"]) {
		s := str(v)
		if !strings.Contains(s, "/") {
			return errors.New("接口地址须包含前缀长度")
		}
		if e := cidr(s); e != nil {
			return e
		}
		p.Addresses = append(p.Addresses, s)
	}
	if len(p.Addresses) == 0 {
		return errors.New("至少填写一个接口地址")
	}
	for _, v := range arr(item["dns"]) {
		s := str(v)
		if e := cidr(s); e != nil {
			return e
		}
		if net.ParseIP(s) == nil {
			return errors.New("DNS 仅接受 IP 地址")
		}
		p.DNS = append(p.DNS, s)
	}
	var e error
	p.ListenPort, e = integer(first(item["listen_port"], 0), 0, 65535)
	if e != nil {
		return e
	}
	p.MTU, e = integer(first(item["mtu"], 1420), 0, 9000)
	if e != nil {
		return e
	}
	if p.MTU > 0 && p.MTU < 576 {
		return errors.New("MTU 须为0（自动）或576至9000")
	}
	if p.Table != "" && p.Table != "auto" && p.Table != "off" {
		if _, e := strconv.ParseUint(p.Table, 10, 31); e != nil {
			return errors.New("路由表须为 auto、off 或数字")
		}
	}
	if p.ListenPort > 0 {
		for _, other := range n.wireguard() {
			if str(other["name"]) != name && fmt.Sprint(other["listen_port"]) == strconv.Itoa(p.ListenPort) {
				return errors.New("监听端口已被另一 WireGuard 接口使用")
			}
		}
	}
	seen := map[string]bool{}
	for _, v := range arr(item["peers"]) {
		m := obj(v)
		peer := WGPeer{PublicKey: str(m["public_key"]), Endpoint: str(m["endpoint"]), Keepalive: int(num(m["keepalive"]))}
		if !wgKey(peer.PublicKey) || seen[peer.PublicKey] {
			return errors.New("Peer 公钥无效或重复")
		}
		seen[peer.PublicKey] = true
		if strings.ContainsAny(peer.Endpoint, "\n\r\t ;") || peer.Keepalive < 0 || peer.Keepalive > 65535 {
			return errors.New("Peer 参数无效")
		}
		if peer.Endpoint != "" {
			h, p, e := net.SplitHostPort(peer.Endpoint)
			if e != nil || h == "" || strings.ContainsAny(h, "/@?&\\") {
				return errors.New("Endpoint 须为主机:端口或 [IPv6]:端口")
			}
			number, e := strconv.Atoi(p)
			if e != nil || number < 1 || number > 65535 {
				return errors.New("Endpoint 端口无效")
			}
		}
		for _, oldPeer := range old.Peers {
			if oldPeer.PublicKey == peer.PublicKey {
				peer.PresharedKey = oldPeer.PresharedKey
			}
		}
		if value := str(m["preshared_key"]); value != "" {
			if !wgKey(value) {
				return errors.New("预共享密钥格式无效")
			}
			peer.PresharedKey = value
		}
		for _, ip := range arr(m["allowed_ips"]) {
			if str(ip) == "all" || str(ip) == "default" {
				return errors.New("AllowedIPs 须为 IP 或 CIDR")
			}
			value := str(ip)
			if e := cidr(value); e != nil {
				return e
			}
			if (value == "0.0.0.0/0" || value == "::/0") && !yes(d["allow_protected"]) {
				return errors.New("默认路由隧道可能影响网关与 VPN，请明确确认")
			}
			if ip := net.ParseIP(value); ip != nil {
				bits := 128
				if ip.To4() != nil {
					bits = 32
				}
				value = ip.String() + "/" + strconv.Itoa(bits)
			}
			peer.AllowedIPs = append(peer.AllowedIPs, value)
		}
		for _, prefix := range peer.AllowedIPs {
			if (prefix == "0.0.0.0/0" || prefix == "::/0") && (p.Table == "" || p.Table == "auto") {
				return errors.New("多出口网关的默认路由需使用独立路由表或 Table=off")
			}
		}
		if len(peer.AllowedIPs) == 0 {
			return errors.New("Peer 缺少 AllowedIPs")
		}
		p.Peers = append(p.Peers, peer)
	}
	if item["network"] != nil || !exists {
		network, e := wgNetworkFromInput(obj(item["network"]))
		if e != nil {
			return e
		}
		network.RouteTable = firstNonempty(str(obj(item["network"])["route_table"]), str(item["table"]), "254")
		network, e = validateWGNetwork(network)
		if e != nil {
			return e
		}
		p.Network = &network
		if network.RouteMode == "manual" || network.RouteMode == "off" {
			p.Table = "off"
		}
		if network.RouteMode == "manual" && len(network.Routes) == 0 {
			return errors.New("手动路由模式至少填写一条路由")
		}
		for _, dst := range network.Routes {
			ip, netw, _ := net.ParseCIDR(dst)
			covered := false
			_, bits := netw.Mask.Size()
			if (dst == "0.0.0.0/0" || dst == "::/0") && network.RouteTable == "254" {
				return errors.New("默认路由必须使用独立路由表，避免接管网关与 VPN")
			}
			for _, peer := range p.Peers {
				for _, allowed := range peer.AllowedIPs {
					_, n, e := net.ParseCIDR(allowed)
					if e != nil {
						continue
					}
					ones, b := n.Mask.Size()
					wanted, _ := netw.Mask.Size()
					if b == bits && ones <= wanted && n.Contains(ip) {
						covered = true
					}
				}
			}
			if !covered {
				return errors.New("手动路由未被任何 Peer 的 AllowedIPs 覆盖")
			}
		}
		if network.SNAT {
			plan.Warnings = append(plan.Warnings, "SNAT 仅匹配指定 LAN 网段与该 VPN 出接口；回程可无需配置 LAN 路由。已有连接停止后会中断，其他 VPN 不变")
		}
	}
	plan.applyFiles = func() error {
		if exists {
			if e := atomicFile(backup, oldBytes, 0600); e != nil {
				return e
			}
		}
		if p.PrivateKey == "" && str(item["key_id"]) != "" {
			value, e := a.consumeWGKey(str(item["key_id"]), name)
			if e != nil {
				return e
			}
			p.PrivateKey = value
		}
		if p.PrivateKey == "" {
			value, e := a.command(5, "", "wg", "genkey")
			if e != nil {
				return e
			}
			p.PrivateKey = strings.TrimSpace(value)
		}
		payload := []byte(p.render())
		plan.restoreFile.ExpectedHash = fingerprint(string(payload))
		if e := n.writeSpec(plan, 0); e != nil {
			return e
		}
		var e error
		if exists {
			e = atomicFile(path, payload, 0600)
		} else {
			e = atomicNewFile(path, payload, 0600)
		}
		p.PrivateKey = ""
		if e == nil {
			a.forgetWGKey(str(item["key_id"]))
		}
		return e
	}
	plan.Preview = append(plan.Preview, "写入 /etc/wireguard/"+name+".conf（密钥仅保存在本机受限配置文件）")
	plan.Summary = "WireGuard 配置 " + op + " · " + name
	plan.Warnings = append(plan.Warnings, "保存配置不会自动启动接口；需要单独启动并确认路由")
	return nil
}
