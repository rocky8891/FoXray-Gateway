package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WGNetwork contains no authentication material and is stored in a config comment.
type WGNetwork struct {
	Role             string   `json:"role"`
	RouteMode        string   `json:"route_mode"`
	Routes           []string `json:"routes"`
	AdvertisedRoutes []string `json:"advertised_routes"`
	LANForward       *bool    `json:"lan_forward,omitempty"`
	SNAT             bool     `json:"snat"`
	LANSources       []string `json:"lan_sources"`
	LANInterface     string   `json:"lan_interface"`
	ServerEndpoint   string   `json:"server_endpoint"`
	RouteTable       string   `json:"route_table"`
}

func (v WGNetwork) allowsLANForward() bool {
	if v.LANForward == nil {
		return v.SNAT
	}
	return *v.LANForward
}

func wgTableName(name string) string { return "foxray_wg_" + fingerprint(name)[:16] }

const wgMetadataPrefix = "# FoXray-Network: "

func validateWGNetwork(v WGNetwork) (WGNetwork, error) {
	if v.Role == "" {
		v.Role = "client"
	}
	if v.Role != "client" && v.Role != "server" {
		return v, errors.New("WireGuard 角色无效")
	}
	if v.RouteMode == "" {
		v.RouteMode = "auto"
	}
	if v.RouteMode != "auto" && v.RouteMode != "manual" && v.RouteMode != "off" {
		return v, errors.New("路由模式无效")
	}
	for _, rows := range [][]string{v.Routes, v.AdvertisedRoutes, v.LANSources} {
		if len(rows) > 100 {
			return v, errors.New("每组网段最多100条")
		}
		for _, s := range rows {
			if _, _, e := net.ParseCIDR(s); e != nil {
				return v, errors.New("路由与 LAN 网段须使用 CIDR 格式")
			}
		}
	}
	for _, s := range v.LANSources {
		ip, network, _ := net.ParseCIDR(s)
		if ip.To4() == nil || network.String() == "0.0.0.0/0" {
			return v, errors.New("SNAT 来源须为明确的 IPv4 LAN 网段")
		}
	}
	if (v.SNAT || v.allowsLANForward()) && (len(v.LANSources) == 0 || v.LANInterface == "") {
		return v, errors.New("允许 LAN 转发或开启 SNAT 需要填写来源网段与 LAN 接口")
	}
	if v.LANInterface != "" {
		if e := iface(v.LANInterface); e != nil {
			return v, e
		}
	}
	if v.ServerEndpoint != "" {
		h, p, e := net.SplitHostPort(v.ServerEndpoint)
		number, err := strconv.Atoi(p)
		if e != nil || err != nil || number < 1 || number > 65535 || !validHost(h) {
			return v, errors.New("服务端公网入口须为域名或IP:端口")
		}
	}
	if v.RouteTable == "" || v.RouteTable == "auto" {
		v.RouteTable = "254"
	}
	if v.RouteMode == "manual" && v.RouteTable == "off" {
		return v, errors.New("手动路由需要数字路由表，不能使用 off")
	}
	if v.RouteTable != "off" {
		n, e := routeTable(v.RouteTable)
		if e != nil || n == 253 || n == 255 || n == 16080 || n == 16082 || n == 65743 {
			return v, errors.New("WireGuard 路由表不可使用系统或代理专用表")
		}
		v.RouteTable = strconv.Itoa(n)
	}
	return v, nil
}
func validHost(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func wgNetworkFromInput(item M) (WGNetwork, error) {
	b, _ := json.Marshal(item)
	var v WGNetwork
	if e := json.Unmarshal(b, &v); e != nil {
		return v, errors.New("WireGuard 网络参数格式无效")
	}
	return validateWGNetwork(v)
}
func wgHelperSteps(p *WGProfile) ([]Step, error) {
	if p.Network == nil {
		return nil, nil
	}
	v, e := validateWGNetwork(*p.Network)
	if e != nil {
		return nil, e
	}
	steps := []Step{}
	if v.RouteMode == "manual" {
		var active, disabled []string
		for _, peer := range p.Peers {
			if peer.Disabled {
				disabled = append(disabled, peer.AllowedIPs...)
			} else {
				active = append(active, peer.AllowedIPs...)
			}
		}
		for _, dst := range v.Routes {
			_, prefix, err := canonicalSource(dst)
			if err != nil {
				return nil, err
			}
			if wgCovers(prefix, disabled) && !wgCovers(prefix, active) {
				continue
			}
			family := "-4"
			if strings.Contains(dst, ":") {
				family = "-6"
			}
			steps = append(steps, Step{Args: []string{"ip", family, "route", "add", dst, "dev", p.Name, "table", v.RouteTable, "proto", "187"}})
		}
	}
	if v.SNAT {
		name := wgTableName(p.Name)
		outInterface := p.Name
		if v.Role == "server" {
			outInterface = v.LANInterface
		}
		sources := strings.Join(v.LANSources, ", ")
		script := "table ip " + name + " { chain postrouting { type nat hook postrouting priority 98; policy accept; ip saddr { " + sources + " } oifname \"" + outInterface + "\" masquerade; }\n}\n"
		steps = append(steps, Step{Args: []string{"nft", "-f", "-"}, Input: script})
	}
	return steps, nil
}

type wgRuntime struct {
	Name string `json:"name"`
	Undo []Step `json:"undo"`
}

func WireGuardNetwork(action, name string) error {
	if e := iface(name); e != nil {
		return e
	}
	if action != "up" && action != "down" {
		return errors.New("操作无效")
	}
	dir := "/run/foxray-wireguard"
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	path := filepath.Join(dir, name+".json")
	unlock, e := lockPlan(path + ".lock")
	if e != nil {
		return e
	}
	defer unlock()
	state := wgRuntime{Name: name}
	_ = readJSON(path, &state)
	undo := func() error {
		failed := []Step{}
		for i := len(state.Undo) - 1; i >= 0; i-- {
			step := state.Undo[i]
			var err error
			if len(step.Args) == 11 && step.Args[0] == "ip" && step.Args[2] == "route" && step.Args[3] == "del" && step.Args[10] == "187" {
				_, prefix, parseErr := canonicalSource(step.Args[4])
				err = parseErr
				if err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					err = syncWGRoute(ctx, SystemRunner{}, step.Args[1], name, prefix, step.Args[8], "187", true)
					cancel()
				}
			} else {
				_, err = adminRun(step.Args, step.Input)
			}
			if err != nil {
				failed = append([]Step{step}, failed...)
			}
		}
		state.Undo = failed
		if len(failed) > 0 {
			_ = writeJSON(path, state)
			return errors.New("接口网络规则未全部撤销，请检查该接口日志")
		}
		_ = os.Remove(path)
		return nil
	}

	if action == "down" {
		profile, err := readWG(name)
		if err == nil && wgProfileAutoWithdraw(profile) {
			for _, peer := range profile.Peers {
				if !peer.Probe.AutoWithdraw {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := purgeWGPeerGuards(ctx, SystemRunner{}, profile.Name, peer)
				cancel()
				if err != nil {
					return err
				}
			}
		}
		return undo()
	}
	if len(state.Undo) > 0 {
		return errors.New("该接口仍有待清理的网络规则，请先停止接口")
	}
	p, e := readWG(name)
	if e != nil {
		return e
	}
	steps, e := wgHelperSteps(p)
	if e != nil {
		return e
	}
	if p.Network != nil && (p.Network.SNAT || p.Network.allowsLANForward()) {
		raw, e := adminRun([]string{"sysctl", "-n", "net.ipv4.ip_forward"}, "")
		if e != nil || strings.TrimSpace(raw) != "1" {
			return errors.New("LAN 转发需要先启用 IPv4 forwarding；未自动改动全局开关")
		}
	}
	for _, step := range steps {
		reverse := Step{}
		if step.Args[0] == "ip" {
			reverse.Args = append([]string{}, step.Args...)
			reverse.Args[3] = "del"
		} else {
			reverse.Args = []string{"nft", "delete", "table", "ip", wgTableName(name)}
		}
		if _, e = adminRun(step.Args, step.Input); e != nil {
			_ = undo()
			return e
		}
		state.Undo = append(state.Undo, reverse)
		if e = writeJSON(path, state); e != nil {
			_ = undo()
			return e
		}
	}
	// firewalld rich rules are scoped to the LAN ingress zone and tunnel destinations.
	if p.Network != nil && p.Network.allowsLANForward() {
		ingress := p.Network.LANInterface
		if p.Network.Role == "server" {
			ingress = p.Name
		}
		zone, e := adminRun([]string{"firewall-cmd", "--get-zone-of-interface=" + ingress}, "")
		if e == nil && strings.TrimSpace(zone) == "" {
			zone, e = adminRun([]string{"firewall-cmd", "--get-default-zone"}, "")
		}
		if p.Network.LANForward != nil && (e != nil || !firewallZoneName.MatchString(strings.TrimSpace(zone)) || strings.TrimSpace(zone) == "no") {
			_ = undo()
			return errors.New("无法核对 LAN 转发的入口区域，请检查 firewalld 与接口归属")
		}
		if e == nil && firewallZoneName.MatchString(strings.TrimSpace(zone)) {
			destinations := []string{}
			for _, peer := range p.Peers {
				if !peer.Disabled {
					destinations = append(destinations, peer.AllowedIPs...)
				}
			}
			if p.Network.Role == "server" {
				destinations = p.Network.AdvertisedRoutes
			}
			for _, source := range p.Network.LANSources {
				for _, dst := range uniq(destinations) {
					ip, _, err := net.ParseCIDR(dst)
					if err != nil || ip.To4() == nil {
						continue
					}
					rule := "rule family=\"ipv4\" source address=\"" + source + "\" destination address=\"" + dst + "\" accept"
					args := []string{"firewall-cmd", "--zone=" + strings.TrimSpace(zone)}
					found, _ := adminRun(append(args, "--query-rich-rule="+rule), "")
					if strings.TrimSpace(found) == "yes" {
						continue
					}
					if _, err = adminRun(append(args, "--add-rich-rule="+rule), ""); err != nil {
						_ = undo()
						return err
					}
					state.Undo = append(state.Undo, Step{Args: append(args, "--remove-rich-rule="+rule)})
					if err = writeJSON(path, state); err != nil {
						_ = undo()
						return err
					}
				}
			}
		}
	}
	return nil
}
func (p *WGProfile) clientTemplate(peer int) (string, error) {
	if p.Network == nil || p.Network.Role != "server" {
		return "", errors.New("此接口未配置为服务端")
	}
	if peer < 0 || peer >= len(p.Peers) {
		return "", errors.New("请选择已有 Peer")
	}
	if p.Network.ServerEndpoint == "" {
		return "", errors.New("先填写服务端公网入口")
	}
	routes := p.Network.AdvertisedRoutes
	if len(routes) == 0 {
		return "", errors.New("先填写客户端访问网段")
	}
	addresses := []string{}
	for _, prefix := range p.Peers[peer].AllowedIPs {
		_, n, e := net.ParseCIDR(prefix)
		if e == nil {
			ones, bits := n.Mask.Size()
			if ones == bits {
				addresses = append(addresses, prefix)
			}
		}
	}
	address := strings.Join(addresses, ", ")
	if address == "" {
		address = "<填写客户端隧道地址/CIDR>"
	}
	return "# 在客户端本地生成私钥，并将对应公钥配置到服务端此 Peer\n[Interface]\nPrivateKey = <在客户端本地生成>\nAddress = " + address + "\n\n[Peer]\nPublicKey = " + p.PublicKey + "\nEndpoint = " + p.Network.ServerEndpoint + "\nAllowedIPs = " + strings.Join(routes, ", ") + "\nPersistentKeepalive = 25\n", nil
}
