package gateway

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var dnsInterface = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$`)
var dnsUUID = regexp.MustCompile(`^[a-fA-F0-9-]{36}$`)

func dnsServers(value any) ([]string, error) {
	out := []string{}
	for _, v := range arr(value) {
		ip := net.ParseIP(str(v))
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, errors.New("DNS服务器须为有效IPv4或IPv6地址")
		}
		out = append(out, ip.String())
	}
	if len(out) == 0 || len(out) > 6 {
		return nil, errors.New("请填写1～6个DNS服务器地址")
	}
	return uniq(out), nil
}
func parseResolver(raw string) M {
	servers, search := []string{}, []string{}
	for _, line := range textLines(raw) {
		v := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(v) < 2 {
			continue
		}
		if v[0] == "nameserver" && net.ParseIP(v[1]) != nil {
			servers = append(servers, v[1])
		}
		if v[0] == "search" {
			search = append(search, v[1:]...)
		}
	}
	return M{"servers": servers, "search": search}
}
func (n *Network) dnsProfile(iface string) (M, error) {
	if !dnsInterface.MatchString(iface) {
		return nil, errors.New("DNS接口名称无效")
	}
	uuid, e := n.app.command(4, "", "nmcli", "--escape", "no", "-g", "GENERAL.CON-UUID", "device", "show", iface)
	uuid = strings.TrimSpace(uuid)
	if e != nil || !dnsUUID.MatchString(uuid) {
		return nil, errors.New("此接口没有可管理的NetworkManager活动配置")
	}
	fields, e := n.app.command(4, "", "nmcli", "--escape", "no", "-g", "ipv4.dns,ipv4.ignore-auto-dns,ipv6.dns,ipv6.ignore-auto-dns", "connection", "show", uuid)
	lines := strings.Split(strings.TrimSuffix(fields, "\n"), "\n")
	if e != nil || len(lines) != 4 {
		return nil, errors.New("无法读取接口DNS配置，未生成变更")
	}
	runtime, e := n.app.command(4, "", "nmcli", "--escape", "no", "-g", "IP4.DNS,IP6.DNS", "device", "show", iface)
	if e != nil {
		return nil, errors.New("无法读取当前生效DNS")
	}
	return M{"interface": iface, "uuid": uuid, "dns4": lines[0], "ignore4": lines[1], "dns6": lines[2], "ignore6": lines[3], "runtime_servers": strings.Fields(runtime)}, nil
}
func warpResolverTarget() (string, error) {
	target, e := filepath.EvalSymlinks("/etc/netns/foxray-warp/resolv.conf")
	if e != nil || target != "/etc/netns/foxray-warp/resolv.conf" && target != "/etc/foxray/warp-netns-resolv.conf" {
		return "", errors.New("WARP DNS未指向受管隔离文件，保持只读")
	}
	info, e := os.Lstat(target)
	if e != nil || !info.Mode().IsRegular() {
		return "", errors.New("WARP隔离DNS文件不可编辑")
	}
	return target, nil
}
func (n *Network) DNSView(scope, iface string) M {
	if scope == "warp" {
		raw, e := os.ReadFile("/etc/netns/foxray-warp/resolv.conf")
		result := parseResolver(string(raw))
		result["scope"] = "warp"
		_, managedErr := warpResolverTarget()
		result["editable"] = e == nil && managedErr == nil
		result["backend"] = "WARP独立网络空间"
		if e != nil {
			result["error"] = "WARP隔离DNS文件尚未创建；请先完成WARP安装"
		}
		return result
	}
	raw, e := os.ReadFile("/etc/resolv.conf")
	result := parseResolver(string(raw))
	result["scope"] = "host"
	result["backend"] = "系统解析器 / NetworkManager"
	result["editable"] = false
	if e != nil {
		result["error"] = "系统DNS配置不可读取"
	}
	links, err := n.app.commandJSON("ip", "-j", "address", "show")
	names := []string{}
	if err == nil {
		for _, v := range links {
			name := str(obj(v)["ifname"])
			if name != "lo" && name != "vone0" && name != "wg-home" && dnsInterface.MatchString(name) {
				names = append(names, name)
			}
		}
	}
	result["interfaces"] = names
	if iface != "" {
		profile, e := n.dnsProfile(iface)
		if e == nil {
			result["profile"] = profile
			result["editable"] = true
		} else {
			result["error"] = e.Error()
		}
	}
	result["note"] = "仅更改所选接口的DNS，不启停连接、不修改VPN接口；需预览、应用并在120秒内确认。"
	return result
}
func (n *Network) planDNS(p *NetworkPlan, data M) error {
	if str(data["operation"]) != "replace" {
		return errors.New("DNS只支持预览替换配置")
	}
	item := obj(data["item"])
	servers, e := dnsServers(item["servers"])
	if e != nil {
		return e
	}
	scope := str(item["scope"])
	if !yes(data["allow_protected"]) {
		return errors.New("请确认本次DNS变更与独立回退")
	}
	if scope == "warp" {
		target, e := warpResolverTarget()
		if e != nil {
			return e
		}
		old, e := os.ReadFile(target)
		if e != nil {
			return errors.New("无法备份WARP DNS")
		}
		payload := []byte("# Managed by FoXray DNS\n")
		for _, server := range servers {
			payload = append(payload, []byte("nameserver "+server+"\n")...)
		}
		backup := filepath.Join(n.app.base, "private", "dns-backups", p.ID+".conf")
		if e = os.MkdirAll(filepath.Dir(backup), 0700); e != nil {
			return e
		}
		if e = atomicFile(backup, old, 0600); e != nil {
			return e
		}
		p.restoreFile = &FileRestore{Target: target, Backup: backup, Existed: true, OldHash: fingerprint(string(old)), ExpectedHash: fingerprint(string(payload)), InPlace: true}
		p.precondition = func() error {
			current, e := os.ReadFile(target)
			if e != nil || fingerprint(string(current)) != fingerprint(string(old)) {
				return errors.New("WARP DNS已变化，请重新预览")
			}
			return nil
		}
		p.applyFiles = func() error { return os.WriteFile(target, payload, 0644) }
		p.Preview = append(p.Preview, "WARP独立DNS → "+strings.Join(servers, ", "))
		p.Summary = "更新WARP独立DNS → " + strings.Join(servers, "、")
		p.Warnings = append(p.Warnings, "仅更新WARP隔离解析文件，保留原文件inode供运行中的绑定挂载读取；未确认将恢复原内容。")
		return nil
	}
	if scope != "host" {
		return errors.New("DNS管理范围无效")
	}
	iface := str(item["interface"])
	if iface == "wg-home" || iface == "vone0" {
		return errors.New("受保护VPN接口不通过系统DNS入口修改")
	}
	profile, e := n.dnsProfile(iface)
	if e != nil {
		return e
	}
	uuid := str(profile["uuid"])
	v4, v6 := []string{}, []string{}
	for _, ip := range servers {
		if net.ParseIP(ip).To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	fresh := []string{"ipv4.dns", strings.Join(v4, ","), "ipv4.ignore-auto-dns", "yes", "ipv6.dns", strings.Join(v6, ","), "ipv6.ignore-auto-dns", "yes"}
	original := []string{"ipv4.dns", str(profile["dns4"]), "ipv4.ignore-auto-dns", str(profile["ignore4"]), "ipv6.dns", str(profile["dns6"]), "ipv6.ignore-auto-dns", str(profile["ignore6"])}
	runtime4, runtime6 := []string{}, []string{}
	for _, v := range profile["runtime_servers"].([]string) {
		ip := net.ParseIP(strings.SplitN(str(v), "%", 2)[0])
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			runtime4 = append(runtime4, ip.String())
		} else {
			runtime6 = append(runtime6, str(v))
		}
	}
	runtime := []string{"ipv4.dns", strings.Join(runtime4, ","), "ipv4.ignore-auto-dns", str(profile["ignore4"]), "ipv6.dns", strings.Join(runtime6, ","), "ipv6.ignore-auto-dns", str(profile["ignore6"])}
	p.steps = []Step{{Args: append([]string{"nmcli", "connection", "modify", uuid}, fresh...)}, {Args: append([]string{"nmcli", "device", "modify", iface}, fresh...)}}
	p.undo = []Step{{Args: append([]string{"nmcli", "connection", "modify", uuid}, original...)}, {Args: append([]string{"nmcli", "device", "modify", iface}, runtime...)}}
	p.precondition = func() error {
		current, e := n.dnsProfile(iface)
		if e != nil || fingerprint(current) != fingerprint(profile) {
			return errors.New("接口DNS或活动配置已变化，请重新预览")
		}
		return nil
	}
	p.Summary = fmt.Sprintf("更新 %s DNS → %s", iface, strings.Join(servers, "、"))
	p.Warnings = append(p.Warnings, "只修改所选配置与活动设备DNS字段，不执行连接up/down或设备reapply。", "未在120秒内确认将恢复配置字段与原生效DNS地址；DHCP DNS的后续变动仍由NetworkManager管理。")
	return nil
}
