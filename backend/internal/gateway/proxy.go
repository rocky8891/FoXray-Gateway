package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

func canonicalOutbound(o M, tag string) (M, error) {
	o = clone(o)
	kind := str(o["type"])
	stream := M{"network": "raw", "sockopt": outboundSocket(true)}
	tls := obj(o["tls"])
	host := str(o["server"])
	port := o["server_port"]
	if yes(tls["enabled"]) {
		serverName := str(tls["server_name"])
		if serverName == "" {
			serverName = host
		}
		if reality := obj(tls["reality"]); yes(reality["enabled"]) {
			stream["security"] = "reality"
			stream["realitySettings"] = M{"serverName": serverName, "fingerprint": first(obj(tls["utls"])["fingerprint"], "chrome"), "publicKey": reality["public_key"], "shortId": first(reality["short_id"], ""), "spiderX": "/"}
		} else {
			stream["security"] = "tls"
			t := M{"serverName": serverName, "allowInsecure": yes(tls["insecure"])}
			if tls["alpn"] != nil {
				t["alpn"] = tls["alpn"]
			}
			if obj(tls["utls"])["fingerprint"] != nil {
				t["fingerprint"] = obj(tls["utls"])["fingerprint"]
			}
			stream["tlsSettings"] = t
		}
	}
	tr := obj(o["transport"])
	switch str(tr["type"]) {
	case "", "tcp", "raw":
	case "ws":
		headers := obj(tr["headers"])
		if headers["Host"] == nil {
			headers["Host"] = host
		}
		stream["network"] = "ws"
		stream["wsSettings"] = M{"path": first(tr["path"], "/"), "headers": headers}
	case "grpc":
		stream["network"] = "grpc"
		stream["grpcSettings"] = M{"serviceName": first(tr["service_name"], ""), "authority": host}
	case "xhttp", "httpupgrade":
		k := str(tr["type"])
		stream["network"] = k
		stream[k+"Settings"] = M{"path": first(tr["path"], "/"), "host": first(tr["host"], host)}
		if k == "xhttp" {
			obj(stream[k+"Settings"])["mode"] = first(tr["mode"], "auto")
		}
	default:
		return nil, errors.New("暂不支持该传输方式")
	}
	server := M{"address": host, "port": port}
	settings := M{}
	switch kind {
	case "vless", "vmess":
		u := M{"id": o["uuid"]}
		if kind == "vless" {
			u["encryption"] = "none"
			if o["flow"] != nil {
				u["flow"] = o["flow"]
			}
		} else {
			u["security"] = first(o["security"], "auto")
			u["alterId"] = first(o["alter_id"], 0)
		}
		server["users"] = []any{u}
		settings["vnext"] = []any{server}
	case "trojan", "shadowsocks":
		server["password"] = o["password"]
		if kind == "shadowsocks" {
			server["method"] = o["method"]
		}
		settings["servers"] = []any{server}
	case "hysteria2":
		kind = "hysteria"
		settings = M{"version": 2, "address": host, "port": port}
		stream["network"] = "hysteria"
		stream["hysteriaSettings"] = M{"version": 2, "auth": o["password"]}
		if obfs := obj(o["obfs"]); obfs["password"] != nil {
			stream["finalmask"] = M{"udp": []any{M{"type": "salamander", "settings": M{"password": obfs["password"]}}}}
		}
	case "socks", "http":
		if str(o["username"]) != "" || str(o["password"]) != "" {
			server["users"] = []any{M{"user": first(o["username"], ""), "pass": first(o["password"], "")}}
		}
		settings["servers"] = []any{server}
	default:
		return nil, errors.New("节点协议暂不支持")
	}
	return M{"tag": tag, "protocol": kind, "settings": settings, "streamSettings": stream}, nil
}
func (a *App) buildEngine(c, native M, selected string) (M, error) {
	ins := []any{}
	outs := []any{}
	rules := []any{}
	eps := []M{}
	for _, v := range arr(c["endpoints"]) {
		e := obj(v)
		if yes(e["enabled"]) && str(e["id"]) != "warp" {
			eps = append(eps, e)
		}
	}
	if len(eps) == 0 {
		return a.engineTelemetry(M{"log": M{"access": "none", "error": "none", "loglevel": "none"}, "inbounds": ins, "outbounds": []any{M{"tag": "out-direct", "protocol": "freedom", "settings": M{}}}, "routing": M{"rules": rules}}), nil
	}
	ins = append(ins, M{"tag": "tun", "protocol": "tun", "settings": M{"name": "vpm-tun", "mtu": 1280, "gateway": []string{"10.253.254.1/30"}, "autoSystemRoutingTable": []any{}, "autoOutboundsInterface": "", "autoSystemDnsToGateway": false}})
	for i, e := range eps {
		identity := str(e["id"])
		tag := "out-" + identity
		var out M
		var er error
		if identity == "warp" {
			out = M{"tag": tag, "protocol": "freedom", "settings": M{"domainStrategy": "UseIPv4"}, "streamSettings": M{"sockopt": M{"interface": "CloudflareWARP"}}}
		} else if str(e["kind"]) == "native" {
			var node M
			for _, v := range arr(native["nodes"]) {
				if obj(v)["id"] == identity {
					node = obj(v)
				}
			}
			if node == nil {
				return nil, errors.New("节点不存在")
			}
			canonical := clone(obj(node["outbound"]))
			host := str(canonical["server"])
			if net.ParseIP(host) == nil {
				ip, err := a.resolveEntry(host)
				if err != nil {
					return nil, err
				}
				tls := obj(canonical["tls"])
				if yes(tls["enabled"]) && str(tls["server_name"]) == "" {
					tls["server_name"] = host
					canonical["tls"] = tls
				}
				t := obj(canonical["transport"])
				if str(t["type"]) == "ws" && obj(t["headers"])["Host"] == nil {
					h := obj(t["headers"])
					h["Host"] = host
					t["headers"] = h
				}
				if (str(t["type"]) == "xhttp" || str(t["type"]) == "httpupgrade") && str(t["host"]) == "" {
					t["host"] = host
				}
				canonical["server"] = ip
				out, er = canonicalOutbound(canonical, tag)
				if er == nil && str(obj(out["streamSettings"])["network"]) == "grpc" {
					obj(obj(out["streamSettings"])["grpcSettings"])["authority"] = host
				}
			} else {
				out, er = canonicalOutbound(canonical, tag)
			}
			if er != nil {
				return nil, er
			}
		} else {
			out = M{"tag": tag, "protocol": "socks", "settings": M{"servers": []any{M{"address": "127.0.0.1", "port": e["port"]}}}}
		}
		outs = append(outs, out)
		ins = append(ins, M{"tag": "probe-" + identity, "listen": "127.0.0.1", "port": 16100 + i, "protocol": "http", "settings": M{}})
		rules = append(rules, M{"type": "field", "inboundTag": []string{"probe-" + identity}, "outboundTag": tag})
	}
	if selected == "direct" {
		outs = append(outs, M{"tag": "out-direct", "protocol": "freedom", "settings": M{"domainStrategy": "UseIPv4"}, "streamSettings": M{"sockopt": outboundSocket(false)}})
	}
	dnsServers, err := directDNSServers(c)
	if err != nil {
		return nil, err
	}
	if yes(c["dns_force_proxy"]) {
		rules = append(rules, M{"type": "field", "inboundTag": []string{"tun"}, "port": "53", "network": "tcp,udp", "outboundTag": "out-" + selected})
	}
	if len(dnsServers) > 0 && !yes(c["dns_force_proxy"]) {
		rules = append(rules, M{"type": "field", "inboundTag": []string{"tun"}, "ip": dnsServers, "port": "53", "network": "tcp,udp", "outboundTag": "out-direct"})
		if selected != "direct" {
			outs = append(outs, M{"tag": "out-direct", "protocol": "freedom", "settings": M{"domainStrategy": "UseIPv4"}, "streamSettings": M{"sockopt": outboundSocket(false)}})
		}
	}
	if str(c["route_profile"]) == "gfwlist" {
		data := M{}
		if e := readJSON(filepath.Join(a.base, "rules", "gfwlist_domains.json"), &data); e != nil {
			return nil, errors.New("GFWList 规则不存在")
		}
		obj(ins[0])["sniffing"] = M{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}
		if (len(dnsServers) == 0 || yes(c["dns_force_proxy"])) && selected != "direct" {
			outs = append(outs, M{"tag": "out-direct", "protocol": "freedom", "settings": M{"domainStrategy": "UseIPv4"}, "streamSettings": M{"sockopt": outboundSocket(true)}})
		}
		for _, k := range []string{"direct", "proxy"} {
			domains := []string{}
			for _, d := range arr(data[k]) {
				domains = append(domains, "domain:"+str(d))
			}
			if len(domains) > 0 {
				tag := "out-" + selected
				if k == "direct" {
					tag = "out-direct"
				}
				rules = append(rules, M{"type": "field", "inboundTag": []string{"tun"}, "domain": domains, "outboundTag": tag})
			}
		}
		rules = append(rules, M{"type": "field", "inboundTag": []string{"tun"}, "outboundTag": "out-direct"})
	} else {
		rules = append(rules, M{"type": "field", "inboundTag": []string{"tun"}, "outboundTag": "out-" + selected})
	}
	custom := customEngineRules(c, selected)
	if domains := arr(customVPNExclusions(c)["domains"]); len(domains) > 0 {
		custom = append([]any{M{"type": "field", "inboundTag": []string{"tun"}, "domain": domains, "outboundTag": "out-direct"}}, custom...)
	}
	if len(custom) > 0 {
		rules = append(custom, rules...)
		tags := map[string]bool{}
		for _, v := range outs {
			tags[str(obj(v)["tag"])] = true
		}
		if !tags["out-direct"] {
			outs = append(outs, M{"tag": "out-direct", "protocol": "freedom", "settings": M{"domainStrategy": "UseIPv4"}, "streamSettings": M{"sockopt": outboundSocket(false)}})
		}
		if !tags["out-block"] {
			outs = append(outs, M{"tag": "out-block", "protocol": "blackhole", "settings": M{}})
		}
		obj(ins[0])["sniffing"] = M{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}
	}
	if yes(c["domain_identification"]) {
		obj(ins[0])["sniffing"] = M{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "metadataOnly": false, "routeOnly": true}
	}
	engine := M{"log": M{"access": "none", "error": "none", "loglevel": "none"}, "inbounds": ins, "outbounds": outs, "routing": M{"domainStrategy": "AsIs", "rules": rules}}
	if yes(c["lan_dns_intercept"]) && (yes(c["capture_lan"]) || yes(c["capture_host"])) {
		configureLANResolverEngine(engine, selected)
	}
	return a.engineTelemetry(engine), nil
}
func (a *App) engineTelemetry(engine M) M {
	a.attachRoutingWebhooks(engine)
	traffic := M{}
	_ = readJSON(filepath.Join(a.base, "private", "traffic-policy.json"), &traffic)
	if yes(traffic["enabled"]) {
		engine["stats"] = M{}
		engine["policy"] = M{"system": M{"statsOutboundUplink": true, "statsOutboundDownlink": true}}
		engine["metrics"] = M{"listen": trafficMetricsAddress}
	}
	return engine
}

func (a *App) resolveEntry(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}
	resolver := directResolver()
	ctx, cancel := contextTimeout(a.ctx, 3*time.Second)
	defer cancel()
	ips, e := resolver.LookupIP(ctx, "ip4", host)
	if e == nil && len(ips) > 0 {
		return ips[0].String(), nil
	}
	cache := M{}
	if readJSON(filepath.Join(a.base, "auto-bypass.json"), &cache) == nil {
		for _, v := range arr(obj(obj(cache["cache"])[host])["ips"]) {
			if ip := net.ParseIP(strings.Split(str(v), "/")[0]); ip != nil {
				return ip.String(), nil
			}
		}
	}
	return "", errors.New("节点入口直连解析失败，保持当前出口")
}
func (a *App) nativeBypass() error {
	u, e := user.Lookup("vpm-engine")
	if e != nil {
		return errors.New("核心服务用户不存在")
	}
	text := "table inet foxray_native_bypass { chain output { type route hook output priority -170; policy accept; meta skuid " + u.Uid + " meta mark set 0x100cf ct mark set 0x100cf ip dscp set 0; }\n}\n"
	if _, e := a.command(4, "", "nft", "list", "table", "inet", "foxray_native_bypass"); e == nil {
		text = "delete table inet foxray_native_bypass\n" + text
	}
	_, e = a.command(5, text, "nft", "-f", "-")
	return e
}
func (a *App) startEngine(c, native M, target string) error {
	desired, e := a.buildEngine(c, native, target)
	if e != nil {
		return e
	}
	path := filepath.Join(a.base, "engine.json")
	old := M{}
	_ = readJSON(path, &old)
	b, _ := json.Marshal(desired)
	ob, _ := json.Marshal(old)
	if string(b) == string(ob) && str(a.service("foxray-engine")["state"]) == "active" {
		return nil
	}
	if e = a.nativeBypass(); e != nil {
		return e
	}
	candidate := filepath.Join(a.base, "private", "engine-candidate.json")
	if e = atomicFile(candidate, b, 0640); e != nil {
		return e
	}
	if group, e := user.LookupGroup("vpm-engine"); e == nil {
		gid, _ := strconv.Atoi(group.Gid)
		_ = os.Chown(candidate, 0, gid)
	}
	if _, e = a.command(12, "", filepath.Join(a.base, "xray"), "run", "-test", "-c", candidate); e != nil {
		return errors.New("核心配置校验失败，原配置保持")
	}
	oldBytes, _ := os.ReadFile(path)
	if e = atomicFile(path, b, 0640); e != nil {
		return e
	}
	if group, e := user.LookupGroup("vpm-engine"); e == nil {
		gid, _ := strconv.Atoi(group.Gid)
		_ = os.Chown(path, 0, gid)
	}
	if _, e = a.command(20, "", "systemctl", "restart", "foxray-engine"); e != nil {
		_ = atomicFile(path, oldBytes, 0640)
		return e
	}
	for i := 0; i < 30; i++ {
		if _, e := os.Stat("/sys/class/net/vpm-tun"); e == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("核心 TUN 接口未就绪")
}
func (a *App) vpnDevices() []string {
	names := []string{"vone0", "wg-home"}
	s, _ := a.command(3, "", "wg", "show", "interfaces")
	names = append(names, strings.Fields(s)...)
	return uniq(names)
}
func (a *App) captureRules(c M) (string, error) {
	a.mu.RLock()
	native := clone(a.native)
	a.mu.RUnlock()
	return a.captureRulesWithNative(c, native)
}
func (a *App) captureRulesWithNative(c, native M) (string, error) {
	u, e := user.Lookup("vpm-engine")
	if e != nil {
		return "", errors.New("核心用户不存在")
	}
	return a.captureRulesForEngine(c, native, u.Uid)
}
func (a *App) captureRulesForEngine(c, native M, uid string) (string, error) {
	excluded := append([]string{}, defaultCaptureExclusions...)
	if lan := currentTopology().LAN; lan != "" {
		excluded = append(excluded, lan)
	}
	exclusion := customVPNExclusions(c)
	for _, v := range arr(exclusion["cidrs"]) {
		if ip, _, err := net.ParseCIDR(str(v)); err == nil && ip.To4() != nil {
			excluded = append(excluded, str(v))
		}
	}
	for _, v := range arr(c["bypass_ips"]) {
		if e := cidr(str(v)); e != nil {
			return "", e
		}
		excluded = append(excluded, str(v))
	}
	for _, v := range arr(native["nodes"]) {
		host := str(obj(obj(v)["outbound"])["server"])
		if host != "" {
			if ip, e := a.resolveEntry(host); e == nil {
				excluded = append(excluded, ip+"/32")
			}
		}
	}
	sort.Strings(excluded)
	skip := "  fib daddr type local return\n  ip daddr { " + strings.Join(uniq(excluded), ", ") + " } return\n  udp dport { 53, 67, 68 } return\n  tcp dport 53 return\n"
	if str(c["route_profile"]) == "bypass_cn" {
		skip += "  meta mark 0x100cf return\n  ct mark 0x100cf return\n"
	}
	devs := []string{}
	devices := a.vpnDevices()
	for _, v := range arr(exclusion["interfaces"]) {
		devices = append(devices, str(v))
	}
	for _, dev := range uniq(devices) {
		if safeName.MatchString(dev) {
			devs = append(devs, strconv.Quote(dev))
		}
	}
	deviceSet := strings.Join(devs, ", ")
	customInputSkip := ""
	if len(arr(exclusion["interfaces"])) > 0 {
		names := []string{}
		for _, v := range arr(exclusion["interfaces"]) {
			names = append(names, strconv.Quote(str(v)))
		}
		customInputSkip = "  iifname { " + strings.Join(names, ", ") + " } return\n"
	}
	dns := "ip daddr != { " + strings.Join(uniq(excluded), ", ") + " } meta l4proto { tcp, udp } th dport 53 counter ip dscp set 1 meta mark set 0x100cf return\n"
	mark := "  meta l4proto { tcp, udp } counter ip dscp set 1 meta mark set 0x100cf\n"
	pre := "  return\n"
	if topology := currentTopology(); yes(c["capture_lan"]) && topology.Interface != "" && topology.LAN != "" {
		pre = fmt.Sprintf("  iifname != %q return\n  ip saddr != %s return\n  fib daddr oifname { %s } return\n  ", topology.Interface, topology.LAN, deviceSet) + dns + skip + mark
	}
	out := ""
	if yes(c["capture_lan"]) || yes(c["capture_host"]) {
		if du, e := user.Lookup("dnsmasq"); e == nil {
			out = "  meta skuid " + du.Uid + " oifname != { " + deviceSet + " } " + dns
		}
	}
	if yes(c["capture_host"]) {
		out += "  meta skuid " + uid + " return\n  oifname { " + deviceSet + " } return\n  socket mark 0x100cf return\n"
		if _, e := os.Stat("/sys/fs/cgroup/system.slice/vone-linux.service"); e == nil {
			out += "  socket cgroupv2 level 2 \"system.slice/vone-linux.service\" return comment \"foxray-vone-bypass\"\n"
		}
		out += "  ct direction reply return\n" + dns + skip + mark
	} else {
		out += "  return\n"
	}
	pre = customInputSkip + pre
	smartConfig := clone(c)
	dnsDevices := []any{}
	for _, device := range uniq(devices) {
		dnsDevices = append(dnsDevices, device)
	}
	smartConfig["_dns_vpn_devices"] = dnsDevices
	smart := lanDNSCaptureRules(smartConfig, uid)
	return "table inet vpm {\n" + smart + " chain prerouting {\n type filter hook prerouting priority -130; policy accept;\n" + pre + " }\n chain output {\n type route hook output priority -130; policy accept;\n" + out + " }\n}\n", nil
}
func (a *App) attach(c M) error {
	a.mu.RLock()
	native := clone(a.native)
	a.mu.RUnlock()
	return a.attachConfig(c, native)
}
func (a *App) attachConfig(c, native M) error {
	text, e := a.captureRulesWithNative(c, native)
	if e != nil {
		return e
	}
	if _, e := a.command(4, "", "nft", "list", "table", "inet", "vpm"); e == nil {
		text = "delete table inet vpm\n" + text
	}
	if _, e = a.command(5, "", "ip", "route", "replace", "default", "dev", "vpm-tun", "table", "16080"); e != nil {
		return e
	}
	rules, _ := a.commandJSON("ip", "-j", "rule")
	found := false
	for _, v := range rules {
		r := obj(v)
		if num(r["priority"]) == 16080 {
			if fmt.Sprint(r["table"]) != "16080" {
				return errors.New("代理路由优先级已占用")
			}
			found = true
		}
	}
	if !found {
		if _, e = a.command(4, "", "ip", "rule", "add", "pref", "16080", "fwmark", "0x100cf", "tos", "0x4", "lookup", "16080"); e != nil {
			return e
		}
	}
	if _, e = a.command(5, text, "nft", "--check", "-f", "-"); e != nil {
		return e
	}
	_, e = a.command(5, text, "nft", "-f", "-")
	return e
}
func (a *App) proxyControl(data M, uiAction ...string) error {
	a.mutate.Lock()
	defer a.mutate.Unlock()
	return a.proxyControlLocked(data, uiAction...)
}
func (a *App) proxyControlLocked(data M, uiAction ...string) error {
	return a.proxyControlConfigLocked(data, nil, uiAction...)
}
func (a *App) proxyControlConfigLocked(data M, prepared M, uiAction ...string) error {
	a.mu.RLock()
	c, native := clone(a.cfg), clone(a.native)
	oldApplied, oldActive := a.applied, a.active
	a.mu.RUnlock()
	if prepared != nil {
		c = clone(prepared)
	}
	for _, k := range []string{"enabled", "route_profile", "capture_host", "capture_lan", "selected", "mode", "vpn_exclusions", "dns_direct_servers", "domain_identification", "dns_force_proxy", "lan_dns_intercept", "dns_proxy_servers"} {
		if v, ok := data[k]; ok {
			c[k] = v
		}
	}
	if e := validateConfig(c); e != nil {
		return e
	}
	target := str(c["selected"])
	if yes(c["enabled"]) {
		if _, err := CheckTopology(); err != nil {
			return err
		}
		if target == "warp" || target == "direct" {
			return errors.New("启用 Xray 请先选择原生节点")
		}
		valid := false
		for _, v := range arr(c["endpoints"]) {
			e := obj(v)
			if e["id"] == target && yes(e["enabled"]) {
				valid = true
			}
		}
		if !valid {
			return errors.New("目标节点不存在或已停用")
		}
		if yes(c["lan_dns_intercept"]) && (yes(c["capture_lan"]) || yes(c["capture_host"])) && !a.readonly {
			if err := a.lanDNS.start(); err != nil {
				return err
			}
		}
		if e := a.startEngine(c, native, target); e != nil {
			return e
		}
		if e := a.attach(c); e != nil {
			a.mu.RLock()
			oldCfg, oldNative := clone(a.cfg), clone(a.native)
			a.mu.RUnlock()
			if oldApplied {
				_ = a.startEngine(oldCfg, oldNative, oldActive)
				_ = a.attach(oldCfg)
			}
			return e
		}
	} else {
		_, _ = a.command(4, "", "nft", "delete", "table", "inet", "vpm")
		target = "direct"
		if yes(a.components["warp"]) {
			target = "warp"
		}
	}
	a.mu.Lock()
	c["fallback_active"] = false
	a.cfg = c
	a.applied = yes(c["enabled"])
	a.active = target
	a.recoveryIssue = ""
	a.settings["proxy_paused"] = false
	_ = writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings)
	a.lastSwitch = now()
	e := a.persist()
	a.mu.Unlock()
	if e == nil {
		a.lanDNS.invalidate()
		if !yes(c["enabled"]) || (!yes(c["capture_lan"]) && !yes(c["capture_host"])) || !yes(c["lan_dns_intercept"]) {
			a.lanDNS.stop()
		}
		a.routes.setDomainCollection(yes(c["domain_identification"]))
		a.switchRecord(oldActive, target, str(first(data["method"], "manual")), true, "", uiAction...)
		a.audit("proxy.updated", "代理状态已更新", true, M{"active": target, "enabled": c["enabled"], "route_profile": c["route_profile"]}, uiAction...)
	}
	return e
}
func validateConfig(c M) error {
	if err := validateObservationPolicy(c); err != nil {
		return err
	}
	if _, err := normalizeVPNExclusions(c["vpn_exclusions"]); err != nil {
		return err
	}
	if err := validateRuleSets(c); err != nil {
		return err
	}
	if err := validateCustomRules(c); err != nil {
		return err
	}
	if v, ok := c["speed_enabled"]; ok {
		if _, valid := v.(bool); !valid {
			return errors.New("速度检测开关无效")
		}
	}
	for _, f := range []struct {
		k        string
		min, max int
	}{{"speed_timeout", 1, 120}, {"speed_size_mb", 1, 500}, {"speed_concurrency", 1, 4}, {"latency_concurrency", 1, 10}, {"score_latency_window_minutes", 1, 1440}, {"score_latency_min_samples", 1, 20}} {
		if v, ok := c[f.k]; ok {
			if _, err := integer(v, f.min, f.max); err != nil {
				labels := map[string]string{"score_latency_window_minutes": "评分延迟统计窗口（分钟）", "score_latency_min_samples": "最少成功样本数（次）"}
				label := firstNonempty(labels[f.k], f.k)
				return fmt.Errorf("%s：%w", label, err)
			}
		}
	}
	if v := str(c["speed_url"]); v != "" {
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("测速地址必须为不含认证信息的 HTTP(S) 地址")
		}
	}

	if value, exists := c["latency_enabled"]; exists {
		if _, ok := value.(bool); !ok {
			return errors.New("真连接检测开关必须为布尔值")
		}
	}
	if p := str(c["route_profile"]); p != "bypass_cn" && p != "global" && p != "gfwlist" {
		return errors.New("分流模式无效")
	}
	if mode := str(c["mode"]); mode != "manual" && mode != "auto" {
		return errors.New("选择模式无效")
	}
	if yes(c["enabled"]) && !yes(c["capture_host"]) && !yes(c["capture_lan"]) {
		return errors.New("至少选择一种接管范围")
	}
	if _, e := integer(c["interval"], 0, 3600); e != nil {
		return e
	}
	if _, e := integer(c["timeout"], 1, 60); e != nil {
		return e
	}
	weights := obj(c["score_weights"])
	sum := 0.
	for _, k := range []string{"latency", "loss", "speed"} {
		v := num(weights[k])
		if v < 0 || v > 100 {
			return errors.New("评分权重无效")
		}
		sum += v
	}
	if sum != 100 {
		return errors.New("权重合计必须为100")
	}
	metrics := arr(c["score_metrics"])
	if len(metrics) == 0 {
		return errors.New("至少选择一个评分指标")
	}
	for _, v := range metrics {
		if str(v) != "latency" && str(v) != "loss" && str(v) != "speed" {
			return errors.New("评分指标无效")
		}
		if num(weights[str(v)]) <= 0 {
			return errors.New("选中指标权重须大于0")
		}
	}
	return nil
}
func (a *App) switchRecord(source, target, method string, ok bool, message string, uiAction ...string) {
	if ok {
		a.noteExitSwitch(source, target)
	}
	status := "success"
	if !ok {
		status = "error"
	}
	a.audit("switch.result", message, ok, M{"time": now(), "source": source, "target": target, "method": method, "status": status, "error": message, "duration": 0}, uiAction...)
}
func fingerprint(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	raw := strings.TrimSuffix(buf.String(), "\n")
	var canonical strings.Builder
	for _, r := range raw {
		if r < 128 {
			canonical.WriteRune(r)
		} else if r <= 0xffff {
			fmt.Fprintf(&canonical, "\\u%04x", r)
		} else {
			a, b := utf16.EncodeRune(r)
			fmt.Fprintf(&canonical, "\\u%04x\\u%04x", a, b)
		}
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}
func decode64(s string) (string, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, e := enc.DecodeString(s); e == nil {
			return string(b), nil
		}
	}
	return "", errors.New("Base64 格式无效")
}
func parseNodeURI(raw string) (M, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme == "" {
		return nil, errors.New("节点链接无效")
	}
	kind := strings.ToLower(u.Scheme)
	if kind == "hy2" {
		kind = "hysteria2"
	}
	if kind == "ss" {
		kind = "shadowsocks"
	}
	q := u.Query()
	o := M{"type": kind, "server": u.Hostname(), "server_port": 443}
	if p := u.Port(); p != "" {
		n, er := strconv.Atoi(p)
		if er != nil || n < 1 || n > 65535 {
			return nil, errors.New("端口无效")
		}
		o["server_port"] = n
	}
	name, _ := url.PathUnescape(u.Fragment)
	if name == "" {
		name = u.Hostname()
	}
	auth := ""
	if u.User != nil {
		auth = u.User.Username()
	}
	password, hasPassword := "", false
	if u.User != nil {
		password, hasPassword = u.User.Password()
	}
	if kind == "vmess" {
		decoded, er := decode64(strings.TrimPrefix(raw, "vmess://"))
		if er != nil {
			return nil, er
		}
		v := M{}
		if json.Unmarshal([]byte(decoded), &v) != nil {
			return nil, errors.New("VMess 格式无效")
		}
		o = M{"type": "vmess", "server": v["add"], "server_port": num(v["port"]), "uuid": v["id"], "security": first(v["scy"], "auto"), "alter_id": first(v["aid"], 0)}
		if s := str(v["port"]); s != "" {
			o["server_port"], _ = strconv.Atoi(s)
		}
		name = str(v["ps"])
		if str(v["tls"]) != "" {
			q.Set("security", "tls")
			q.Set("sni", str(v["sni"]))
		}
		q.Set("type", str(v["net"]))
		q.Set("path", str(v["path"]))
		q.Set("host", str(v["host"]))
	} else {
		switch kind {
		case "vless":
			o["uuid"] = auth
			if f := q.Get("flow"); f != "" {
				o["flow"] = f
			}
		case "trojan", "hysteria2":
			o["password"] = auth
			if hasPassword {
				o["password"] = auth + ":" + password
			}
			if q.Get("obfs") == "salamander" {
				o["obfs"] = M{"type": "salamander", "password": q.Get("obfs-password")}
			}
		case "shadowsocks":
			value := auth
			if hasPassword {
				value = auth + ":" + password
			} else {
				decoded, er := decode64(auth)
				if er != nil {
					return nil, er
				}
				value = decoded
			}
			parts := strings.SplitN(value, ":", 2)
			if len(parts) != 2 {
				return nil, errors.New("Shadowsocks 认证格式无效")
			}
			o["method"], o["password"] = parts[0], parts[1]
		case "socks5", "socks", "http":
			if kind == "socks5" {
				o["type"] = "socks"
			}
			o["username"], o["password"] = auth, password
		default:
			return nil, errors.New("节点协议不支持")
		}
	}
	host := str(o["server"])
	if host == "" || strings.ContainsAny(host, " \r\n\t") {
		return nil, errors.New("节点入口无效")
	}
	security := q.Get("security")
	if security == "" {
		security = q.Get("tls")
	}
	if security != "" && security != "none" && security != "0" || kind == "trojan" || kind == "hysteria2" {
		t := M{"enabled": true, "server_name": firstNonempty(q.Get("sni"), q.Get("peer"), host), "insecure": q.Get("allowInsecure") == "1"}
		if q.Get("alpn") != "" {
			t["alpn"] = strings.Split(q.Get("alpn"), ",")
		}
		if security == "reality" {
			if q.Get("pbk") == "" {
				return nil, errors.New("REALITY 公钥缺失")
			}
			t["reality"] = M{"enabled": true, "public_key": q.Get("pbk"), "short_id": q.Get("sid")}
			t["utls"] = M{"enabled": true, "fingerprint": firstNonempty(q.Get("fp"), "chrome")}
		}
		o["tls"] = t
	}
	switch q.Get("type") {
	case "ws":
		o["transport"] = M{"type": "ws", "path": firstNonempty(q.Get("path"), "/"), "headers": M{"Host": firstNonempty(q.Get("host"), host)}}
	case "grpc":
		o["transport"] = M{"type": "grpc", "service_name": q.Get("serviceName")}
	case "xhttp", "httpupgrade":
		o["transport"] = M{"type": q.Get("type"), "path": firstNonempty(q.Get("path"), "/"), "host": q.Get("host"), "mode": firstNonempty(q.Get("mode"), "auto")}
	}
	if _, e := canonicalOutbound(o, "validate"); e != nil {
		return nil, e
	}
	return M{"name": label(name), "outbound": o}, nil
}
func firstNonempty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
