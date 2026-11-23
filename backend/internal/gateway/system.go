package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func adminRun(args []string, input string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return (SystemRunner{}).Run(ctx, args, input)
}
func VPNGuard(refresh bool) error {
	if _, err := CheckTopology(); err != nil {
		return err
	}
	company := ""
	if _, e := os.Stat("/sys/fs/cgroup/system.slice/vone-linux.service"); e == nil {
		company = " socket cgroupv2 level 2 \"system.slice/vone-linux.service\" meta mark set 0x100cf ct mark set 0x100cf ip dscp set 0\n"
	}
	rule := "table inet foxray_vpn_guard {\n chain output {\n type route hook output priority -165; policy accept;\n" + company + " udp sport 8045 meta mark set 0x100cf ct mark set 0x100cf\n }\n chain prerouting {\n type filter hook prerouting priority -165; policy accept;\n iifname " + strconv.Quote(uplink()) + " udp dport 8045 ct mark set 0x100cf meta mark set 0x100cf\n iifname \"CloudflareWARP\" ip saddr 10.254.253.2 meta mark set 0x100cf ct mark set 0x100cf\n }\n chain nat {\n type nat hook postrouting priority 99; policy accept;\n oifname " + strconv.Quote(uplink()) + " ip saddr 10.254.253.2 masquerade\n }\n}\n"
	if _, e := adminRun([]string{"nft", "list", "table", "inet", "foxray_vpn_guard"}, ""); e == nil {
		rule = "delete table inet foxray_vpn_guard\n" + rule
	}
	if _, e := adminRun([]string{"nft", "--check", "-f", "-"}, rule); e != nil {
		return e
	}
	if _, e := adminRun([]string{"nft", "-f", "-"}, rule); e != nil {
		return e
	}
	if !refresh {
		return nil
	}
	raw, e := adminRun([]string{"nft", "-j", "-a", "list", "chain", "inet", "vpm", "output"}, "")
	if e != nil {
		return nil
	}
	data := M{}
	if json.Unmarshal([]byte(raw), &data) != nil {
		return errors.New("现有接管规则无法读取")
	}
	script := ""
	for _, v := range arr(data["nftables"]) {
		r := obj(obj(v)["rule"])
		if str(r["comment"]) == "foxray-vone-bypass" {
			script += "delete rule inet vpm output handle " + intstr(r["handle"]) + "\n"
		}
	}
	script += "insert rule inet vpm output socket cgroupv2 level 2 \"system.slice/vone-linux.service\" return comment \"foxray-vone-bypass\"\n"
	_, e = adminRun([]string{"nft", "-f", "-"}, script)
	return e
}
func SetupWarpNetwork() error {
	if _, err := CheckTopology(); err != nil {
		return err
	}
	if _, e := os.Stat("/run/netns/foxray-warp"); e != nil {
		if _, e = adminRun([]string{"ip", "netns", "add", "foxray-warp"}, ""); e != nil {
			return e
		}
	}
	if _, e := os.Stat("/sys/class/net/CloudflareWARP"); e != nil {
		if _, e = adminRun([]string{"ip", "link", "add", "CloudflareWARP", "type", "veth", "peer", "name", "fw-uplink"}, ""); e != nil {
			return e
		}
		if _, e = adminRun([]string{"ip", "link", "set", "fw-uplink", "netns", "foxray-warp"}, ""); e != nil {
			return e
		}
	} else {
		raw, e := adminRun([]string{"ip", "-j", "-d", "link", "show", "CloudflareWARP"}, "")
		if e != nil {
			return e
		}
		var rows []M
		if json.Unmarshal([]byte(raw), &rows) != nil || len(rows) == 0 || str(obj(rows[0]["linkinfo"])["info_kind"]) != "veth" {
			return errors.New("主机 WARP 接口不是隔离 veth，未接管")
		}
	}
	commands := [][]string{{"ip", "addr", "replace", "10.254.253.1/30", "dev", "CloudflareWARP"}, {"ip", "link", "set", "CloudflareWARP", "mtu", "1500", "up"}, {"sysctl", "-qw", "net.ipv4.ip_forward=1"}, {"sysctl", "-qw", "net.ipv4.conf.CloudflareWARP.rp_filter=0"}, {"ip", "route", "replace", "default", "via", "10.254.253.2", "dev", "CloudflareWARP", "src", "10.254.253.1", "mtu", "1280", "table", "65743"}}
	for _, c := range commands {
		if _, e := adminRun(c, ""); e != nil {
			return e
		}
	}
	ns := func(args ...string) (string, error) {
		return adminRun(append([]string{"ip", "netns", "exec", "foxray-warp"}, args...), "")
	}
	for _, c := range [][]string{{"ip", "addr", "replace", "10.254.253.2/30", "dev", "fw-uplink"}, {"ip", "link", "set", "lo", "up"}, {"ip", "link", "set", "fw-uplink", "mtu", "1500", "up"}, {"ip", "route", "replace", "default", "via", "10.254.253.1", "dev", "fw-uplink"}, {"sysctl", "-qw", "net.ipv4.ip_forward=1"}, {"sysctl", "-qw", "net.ipv4.conf.all.rp_filter=0"}, {"sysctl", "-qw", "net.ipv4.conf.fw-uplink.rp_filter=0"}, {"ip", "route", "replace", "unreachable", "default", "table", "16082"}} {
		if _, e := ns(c...); e != nil {
			return e
		}
	}
	raw, e := ns("ip", "-j", "rule")
	if e != nil {
		return e
	}
	var rows []M
	if json.Unmarshal([]byte(raw), &rows) != nil {
		return errors.New("隔离空间策略规则无效")
	}
	found := false
	for _, r := range rows {
		if num(r["priority"]) == 20000 {
			if strOrNumber(r["table"]) != "16082" {
				return errors.New("隔离空间规则优先级被占用")
			}
			found = true
		}
	}
	if !found {
		if _, e = ns("ip", "rule", "add", "pref", "20000", "from", "10.254.253.1/32", "table", "16082"); e != nil {
			return e
		}
	}
	nat := "table ip foxray_warp_nat { chain postrouting { type nat hook postrouting priority srcnat; policy accept; oifname \"CloudflareWARP\" masquerade; }\n}\n"
	if _, e = ns("nft", "list", "table", "ip", "foxray_warp_nat"); e == nil {
		nat = "delete table ip foxray_warp_nat\n" + nat
	}
	if _, e = adminRun([]string{"ip", "netns", "exec", "foxray-warp", "nft", "-f", "-"}, nat); e != nil {
		return e
	}
	if e = VPNGuard(false); e != nil {
		return e
	}
	return configureWarpHostPolicy(adminRun)
}

// Only local sockets explicitly bound to the WARP veth should use its route table.
// Ordinary host traffic, including replies to remote management clients, uses its existing routing policy.
func configureWarpHostPolicy(run func([]string, string) (string, error)) error {
	raw, err := run([]string{"ip", "-j", "-4", "rule"}, "")
	if err != nil {
		return err
	}
	var rules []M
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return err
	}
	const priority = 16078
	// ip rule del matches some omitted selectors loosely. Leave ambiguous priorities untouched.
	counts := map[int]int{}
	for _, rule := range rules {
		counts[int(num(rule["priority"]))]++
	}
	selector := []string{"pref", strconv.Itoa(priority), "oif", "CloudflareWARP", "lookup", "65743"}
	var present bool
	for _, rule := range rules {
		if num(rule["priority"]) != priority {
			continue
		}
		if present || str(rule["oif"]) != "CloudflareWARP" || strOrNumber(rule["table"]) != "65743" || !unrestrictedWarpRule(rule, "oif") {
			return errors.New("WARP 专用路由规则优先级 16078 已被占用")
		}
		present = true
	}
	if !present {
		if _, err := run(append([]string{"ip", "-4", "rule", "add"}, selector...), ""); err != nil {
			return err
		}
	}
	// Remove only the two exact rules written by earlier FoXray versions.
	for _, priority := range []int{16079, 16070} {
		if counts[priority] != 1 {
			continue
		}
		for _, rule := range rules {
			if num(rule["priority"]) != float64(priority) {
				continue
			}
			var old []string
			switch priority {
			case 16079:
				_, inverted := rule["not"]
				mark, err := strconv.ParseUint(strOrNumber(rule["fwmark"]), 0, 32)
				mask := strOrNumber(rule["fwmask"])
				if inverted && err == nil && mark == 65743 && (rule["fwmask"] == nil || mask == "0xffffffff" || mask == "4294967295") && strOrNumber(rule["table"]) == "65743" && unrestrictedWarpRule(rule, "not", "fwmark", "fwmask") {
					old = []string{"not", "fwmark", "0x100cf/0xffffffff", "lookup", "65743"}
				}
			case 16070:
				prefix, ok := rule["suppress_prefixlen"].(float64)
				if ok && prefix == 0 && (strOrNumber(rule["table"]) == "main" || strOrNumber(rule["table"]) == "254") && unrestrictedWarpRule(rule, "suppress_prefixlen") {
					old = []string{"lookup", "main", "suppress_prefixlength", "0"}
				}
			}
			if len(old) != 0 {
				args := []string{"ip", "-4", "rule", "del", "pref", strconv.Itoa(priority), "from", "all", "to", "all"}
				if _, err := run(append(args, old...), ""); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func unrestrictedWarpRule(rule M, selectors ...string) bool {
	for key, value := range rule {
		switch key {
		case "priority", "table", "protocol":
			continue
		case "src", "dst":
			if text := str(value); text == "all" || text == "0.0.0.0/0" {
				continue
			}
		}
		if !slices.Contains(selectors, key) {
			return false
		}
	}
	return true
}

func strOrNumber(v any) string {
	if s := str(v); s != "" {
		return s
	}
	return intstr(v)
}
func Initialize(base string, warp, komari bool) error {
	return InitializeModules(base, true, warp, false, komari)
}
func InitializeModules(base string, xray, warp, wireguard, komari bool) error {
	if e := os.MkdirAll(filepath.Join(base, "private"), 0700); e != nil {
		return e
	}
	if !regular(filepath.Join(base, "components.json")) {
		if e := writeJSON(filepath.Join(base, "components.json"), M{"version": 2, "xray": xray, "warp": warp, "wireguard": wireguard, "komari": komari, "komari_url": ""}); e != nil {
			return e
		}
	}
	if !regular(filepath.Join(base, "config.json")) {
		endpoints := []any{}
		selected := "direct"
		if warp {
			selected = "warp"
			endpoints = append(endpoints, M{"id": "warp", "name": "Cloudflare WARP", "kind": "warp", "enabled": true, "priority": 30, "port": 0})
		}
		if e := writeJSON(filepath.Join(base, "config.json"), M{"enabled": false, "capture_lan": false, "mode": "manual", "selected": selected, "endpoints": endpoints}); e != nil {
			return e
		}
	}
	return nil
}

var _ = strings.TrimSpace
