package gateway

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const lanDNSPort = 1053
const lanDNSDirectPort = 16953
const lanDNSProxyPort = 16954

type LANResolver struct {
	app                         *App
	mu                          sync.Mutex
	udp                         *net.UDPConn
	tcp                         net.Listener
	issue                       string
	queries, answers, failovers uint64
	slots                       chan struct{}
	policy                      M
	policyGeneration            uint64
	cooldown                    map[string]time.Time
	exchange                    func(context.Context, string, string, []byte) ([]byte, error)
}

func newLANResolver(a *App) *LANResolver {
	d := &LANResolver{app: a, slots: make(chan struct{}, 64), cooldown: map[string]time.Time{}}
	d.exchange = exchangeLANQuery
	return d
}
func (d *LANResolver) invalidate() {
	d.mu.Lock()
	d.policy = nil
	d.policyGeneration++
	d.cooldown = map[string]time.Time{}
	d.mu.Unlock()
}
func (d *LANResolver) policySnapshot() M {
	d.mu.Lock()
	cached := d.policy
	generation := d.policyGeneration
	d.mu.Unlock()
	if cached != nil {
		return cached
	}
	d.app.mu.RLock()
	c := M{"dns_direct_servers": clone(d.app.cfg["dns_direct_servers"]), "dns_proxy_servers": clone(d.app.cfg["dns_proxy_servers"]), "dns_force_proxy": d.app.cfg["dns_force_proxy"], "custom_rules": clone(activeRules(d.app.cfg))}
	d.app.mu.RUnlock()
	domains := M{}
	_ = readJSON(filepath.Join(d.app.base, "rules", "gfwlist_domains.json"), &domains)
	c["_direct_domains"] = domains["direct"]
	c["_proxy_domains"] = domains["proxy"]
	d.mu.Lock()
	if d.policyGeneration != generation {
		d.mu.Unlock()
		return d.policySnapshot()
	}
	d.policy = c
	d.mu.Unlock()
	return c
}
func (d *LANResolver) status() M {
	d.mu.Lock()
	defer d.mu.Unlock()
	return M{"ready": d.udp != nil && d.tcp != nil, "error": d.issue, "queries": d.queries, "answers": d.answers, "failovers": d.failovers}
}
func (d *LANResolver) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.udp != nil {
		d.udp.Close()
		d.udp = nil
	}
	if d.tcp != nil {
		d.tcp.Close()
		d.tcp = nil
	}
	d.issue = ""
}
func (d *LANResolver) start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.udp != nil && d.tcp != nil {
		return nil
	}
	tcp, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(lanDNSPort)))
	if err != nil {
		d.issue = "智能DNS监听端口不可用，原转发设置保持"
		return errors.New(d.issue)
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: lanDNSPort})
	if err != nil {
		tcp.Close()
		d.issue = "智能DNS UDP监听不可用，原转发设置保持"
		return errors.New(d.issue)
	}
	if err = enableDNSOriginalDestination(udp); err != nil {
		udp.Close()
		tcp.Close()
		d.issue = "智能DNS原始目标读取不可用"
		return errors.New(d.issue)
	}
	d.tcp, d.udp, d.issue = tcp, udp, ""
	go func() { <-d.app.ctx.Done(); udp.Close(); tcp.Close() }()
	go d.serveUDP(udp)
	go d.serveTCP(tcp)
	return nil
}
func lanDNSClientAllowed(ip net.IP) bool {
	v := ip.To4()
	return ip.IsLoopback() || v != nil && v[0] == 192 && v[1] == 168 && v[2] == 1
}
func (d *LANResolver) serveUDP(conn *net.UDPConn) {
	for {
		raw := make([]byte, 65535)
		oob := make([]byte, 256)
		n, on, _, peer, err := conn.ReadMsgUDP(raw, oob)
		if err != nil {
			return
		}
		if !lanDNSClientAllowed(peer.IP) {
			continue
		}
		original := dnsOriginalUDP(oob[:on])
		local := dnsLocalUDP(oob[:on])
		select {
		case d.slots <- struct{}{}:
		default:
			continue
		}
		go func(raw []byte, peer *net.UDPAddr, original, local string) {
			defer func() { <-d.slots }()
			ctx := context.WithValue(d.app.ctx, dnsOriginalPeerKey{}, peer.String())
			response := d.resolve(ctx, raw, original, "udp")
			if len(response) > dnsUDPSize(raw) {
				response = dnsErrorReply(raw, 0, true)
			}
			if len(response) > 0 {
				writeDNSReplyUDP(conn, response, peer, local)
			}
		}(append([]byte{}, raw[:n]...), peer, original, local)
	}
}
func (d *LANResolver) serveTCP(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		peer, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		if !lanDNSClientAllowed(net.ParseIP(peer)) {
			conn.Close()
			continue
		}
		select {
		case d.slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		go func(conn net.Conn) {
			defer conn.Close()
			defer func() { <-d.slots }()
			original := dnsOriginalTCP(conn)
			for count := 0; count < 32; count++ {
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				size := make([]byte, 2)
				if _, err := io.ReadFull(conn, size); err != nil {
					return
				}
				n := int(binary.BigEndian.Uint16(size))
				if n < 12 {
					return
				}
				raw := make([]byte, n)
				if _, err := io.ReadFull(conn, raw); err != nil {
					return
				}
				response := d.resolve(d.app.ctx, raw, original, "tcp")
				if len(response) == 0 {
					return
				}
				binary.BigEndian.PutUint16(size, uint16(len(response)))
				if _, err := conn.Write(append(size, response...)); err != nil {
					return
				}
			}
		}(conn)
	}
}
func (d *LANResolver) resolve(ctx context.Context, raw []byte, original, network string) []byte {
	question, err := parseDNSQuestion(raw)
	if err != nil || raw[2]&0x80 != 0 {
		return dnsErrorReply(raw, 1, false)
	}
	d.app.mu.RLock()
	applied := d.app.applied
	enabled := (yes(d.app.cfg["capture_lan"]) || yes(d.app.cfg["capture_host"])) && yes(d.app.cfg["lan_dns_intercept"])
	d.app.mu.RUnlock()
	c := d.policySnapshot()
	if !applied || !enabled {
		return dnsErrorReply(raw, 5, false)
	}
	if peer, ok := ctx.Value(dnsOriginalPeerKey{}).(string); ok && dnsNeedsOriginalIP(c) {
		resolved := dnsOriginalConntrack(peer)
		if resolved == "" {
			return dnsErrorReply(raw, 2, false)
		}
		original = resolved
	}
	d.mu.Lock()
	d.queries++
	d.mu.Unlock()
	action, fallbackProxy := d.app.lanDNSDecision(c, question.name, original, network)
	if action == "block" {
		return dnsErrorReply(raw, 5, false)
	}
	proxyServers, _ := resolverServers(c, "dns_proxy_servers", defaultProxyDNSServers())
	servers := proxyServers
	if action == "direct" {
		servers, _ = directDNSServers(c)
	}
	candidates := []M{}
	for _, server := range servers {
		candidates = append(candidates, M{"action": action, "server": server})
	}
	if action == "direct" && fallbackProxy {
		for _, server := range proxyServers {
			candidates = append(candidates, M{"action": "proxy", "server": server})
		}
	}
	if len(candidates) == 0 {
		return dnsErrorReply(raw, 2, false)
	}
	// Temporarily move failed upstreams behind healthy alternatives. If all
	// candidates are cooling down, still retry them instead of permanently failing.
	d.mu.Lock()
	healthy, failed := []M{}, []M{}
	for _, candidate := range candidates {
		key := str(candidate["action"]) + "|" + str(candidate["server"])
		if time.Now().Before(d.cooldown[key]) {
			failed = append(failed, candidate)
		} else {
			healthy = append(healthy, candidate)
		}
	}
	candidates = append(healthy, failed...)
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	for i, candidate := range candidates {
		child, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
		response, err := d.exchange(child, str(candidate["action"]), str(candidate["server"]), raw)
		cancel()
		key := str(candidate["action"]) + "|" + str(candidate["server"])
		if err != nil || !validDNSReply(raw, response) {
			d.mu.Lock()
			d.cooldown[key] = time.Now().Add(30 * time.Second)
			d.mu.Unlock()
			continue
		}
		code := response[3] & 15
		if code == 2 || code == 5 {
			d.mu.Lock()
			d.cooldown[key] = time.Now().Add(30 * time.Second)
			d.mu.Unlock()
			continue
		}
		if trace, ok := ctx.Value(dnsDiagnosticTraceKey{}).(*dnsDiagnosticTrace); ok {
			trace.mu.Lock()
			trace.success = len(trace.flows) - 1
			trace.mu.Unlock()
		}
		d.mu.Lock()
		delete(d.cooldown, key)
		d.answers++
		if i > 0 {
			d.failovers++
		}
		d.mu.Unlock()
		return response
	}
	return dnsErrorReply(raw, 2, false)
}
func (a *App) lanDNSAction(c M, domain, original, network string) string {
	action, _ := a.lanDNSDecision(c, domain, original, network)
	return action
}
func (a *App) lanDNSDecision(c M, domain, original, network string) (string, bool) {
	for _, v := range activeRules(c) {
		r := obj(v)
		// Generic IP bypass rules describe the original resolver's transport, not
		// the queried name. Only explicitly scoped DNS IP rules override smart DNS.
		if r["type"] == "ip" && str(r["port"]) == "" {
			continue
		}
		if !yes(r["enabled"]) || !dnsRulePort(str(r["port"])) {
			continue
		}
		if n := str(r["network"]); n != "tcp,udp" && n != network {
			continue
		}
		matched := false
		for _, value := range arr(r["targets"]) {
			if r["type"] == "domain" {
				matched = matched || dnsDomainMatches(domain, str(value))
			} else if r["type"] == "ip" {
				ip := net.ParseIP(original)
				if _, cidr, e := net.ParseCIDR(str(value)); e == nil {
					matched = matched || cidr.Contains(ip)
				} else {
					matched = matched || ip != nil && ip.Equal(net.ParseIP(str(value)))
				}
			}
		}
		if matched {
			return str(r["action"]), false
		}
	}
	if yes(c["dns_force_proxy"]) {
		return "proxy", false
	}
	if strings.HasSuffix(domain, ".cn") || domain == "cn" {
		return "direct", true
	}
	for _, v := range arr(c["_direct_domains"]) {
		if dnsDomainMatches(domain, str(v)) {
			return "direct", true
		}
	}
	for _, v := range arr(c["_proxy_domains"]) {
		if dnsDomainMatches(domain, str(v)) {
			return "proxy", false
		}
	}
	return "direct", true
}
func dnsRulePort(value string) bool {
	if value == "" {
		return true
	}
	for _, v := range strings.Split(value, ",") {
		bounds := strings.Split(v, "-")
		lo, e := strconv.Atoi(bounds[0])
		if e != nil {
			continue
		}
		hi := lo
		if len(bounds) == 2 {
			hi, _ = strconv.Atoi(bounds[1])
		}
		if lo <= 53 && 53 <= hi {
			return true
		}
	}
	return false
}
func dnsDomainMatches(domain, rule string) bool {
	rule = strings.TrimSpace(rule)
	if strings.HasPrefix(rule, "regexp:") {
		re, err := regexp.Compile(strings.TrimPrefix(rule, "regexp:"))
		return err == nil && re.MatchString(domain)
	}
	rule = strings.ToLower(strings.TrimSuffix(rule, "."))
	if strings.HasPrefix(rule, "keyword:") {
		return strings.Contains(domain, strings.TrimPrefix(rule, "keyword:"))
	}
	if strings.HasPrefix(rule, "full:") {
		return domain == strings.TrimPrefix(rule, "full:")
	}
	rule = strings.TrimPrefix(strings.TrimPrefix(rule, "domain:"), "*.")
	return domain == rule || strings.HasSuffix(domain, "."+rule)
}
func exchangeLANQuery(ctx context.Context, action, server string, query []byte) ([]byte, error) {
	port := lanDNSProxyPort
	if action == "direct" {
		port = lanDNSDirectPort
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	target := net.JoinHostPort(server, "53")
	if trace, ok := ctx.Value(dnsDiagnosticTraceKey{}).(*dnsDiagnosticTrace); ok {
		flow := M{"network": "tcp", "source": conn.LocalAddr().String(), "target": target}
		trace.routes.expectProbe("tcp", str(flow["source"]), target)
		trace.mu.Lock()
		trace.flows = append(trace.flows, flow)
		trace.mu.Unlock()
	}
	if _, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		return nil, errors.New("DNS上游代理连接未建立")
	}
	size := make([]byte, 2)
	binary.BigEndian.PutUint16(size, uint16(len(query)))
	if _, err = conn.Write(append(size, query...)); err != nil {
		return nil, err
	}
	if _, err = io.ReadFull(reader, size); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(size))
	if n < 12 {
		return nil, errors.New("DNS应答长度无效")
	}
	raw := make([]byte, n)
	_, err = io.ReadFull(reader, raw)
	return raw, err
}
func configureLANResolverEngine(engine M, selected string) {
	engine["inbounds"] = append(arr(engine["inbounds"]), M{"tag": "smartdns-direct", "listen": "127.0.0.1", "port": lanDNSDirectPort, "protocol": "http", "settings": M{}}, M{"tag": "smartdns-proxy", "listen": "127.0.0.1", "port": lanDNSProxyPort, "protocol": "http", "settings": M{}})
	outbounds := arr(engine["outbounds"])
	direct := false
	for _, v := range outbounds {
		if obj(v)["tag"] == "out-direct" {
			direct = true
		}
	}
	if !direct {
		outbounds = append(outbounds, M{"tag": "out-direct", "protocol": "freedom", "settings": M{"domainStrategy": "UseIPv4"}, "streamSettings": M{"sockopt": M{"interface": "ens16", "mark": 65743}}})
	}
	engine["outbounds"] = outbounds
	routing := obj(engine["routing"])
	routing["rules"] = append([]any{M{"type": "field", "inboundTag": []string{"smartdns-direct"}, "outboundTag": "out-direct"}, M{"type": "field", "inboundTag": []string{"smartdns-proxy"}, "outboundTag": "out-" + selected}}, arr(routing["rules"])...)
}
func lanDNSCaptureRules(c M, engineUID ...string) string {
	if !yes(c["lan_dns_intercept"]) || (!yes(c["capture_lan"]) && !yes(c["capture_host"])) {
		return ""
	}
	text := ""
	lan := yes(c["capture_lan"])
	for _, v := range arr(customVPNExclusions(c)["interfaces"]) {
		if str(v) == "ens16" {
			lan = false
		}
	}
	if lan {
		text += fmt.Sprintf(" chain dns_prerouting { type nat hook prerouting priority -100; policy accept; iifname \"ens16\" ip saddr 192.168.1.0/24 meta l4proto { tcp, udp } th dport 53 redirect to :%d; }\n", lanDNSPort)
	}
	if yes(c["capture_host"]) && len(engineUID) > 0 {
		if _, err := strconv.Atoi(engineUID[0]); err == nil {
			skip := ""
			devices := []string{}
			for _, v := range arr(c["_dns_vpn_devices"]) {
				if safeName.MatchString(str(v)) {
					devices = append(devices, strconv.Quote(str(v)))
				}
			}
			if len(devices) > 0 {
				skip = "oifname { " + strings.Join(devices, ", ") + " } return; "
			}
			text += fmt.Sprintf(" chain dns_output { type nat hook output priority -100; policy accept; meta skuid %s return; socket mark 0x100cf return; meta l4proto { tcp, udp } th sport %d return; %smeta nfproto ipv4 meta l4proto { tcp, udp } th dport 53 redirect to :%d; }\n", engineUID[0], lanDNSPort, skip, lanDNSPort)
		}
	}
	allowLAN := ""
	if lan {
		allowLAN = fmt.Sprintf("iifname \"ens16\" ip saddr 192.168.1.0/24 meta l4proto { tcp, udp } th dport %d accept; ", lanDNSPort)
	}
	text += fmt.Sprintf(" chain dns_input { type filter hook input priority -5; policy accept; iifname \"lo\" meta nfproto ipv4 meta l4proto { tcp, udp } th dport %d accept; %smeta nfproto ipv4 meta l4proto { tcp, udp } th dport %d drop; }\n", lanDNSPort, allowLAN, lanDNSPort)
	return text
}

type dnsOriginalPeerKey struct{}

func dnsNeedsOriginalIP(c M) bool {
	for _, v := range activeRules(c) {
		r := obj(v)
		if yes(r["enabled"]) && r["type"] == "ip" && str(r["port"]) != "" && dnsRulePort(str(r["port"])) {
			return true
		}
	}
	return false
}
