package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func testDNSQuery(name string, kind uint16) []byte {
	raw := []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range strings.Split(name, ".") {
		raw = append(raw, byte(len(label)))
		raw = append(raw, []byte(label)...)
	}
	raw = append(raw, 0, byte(kind>>8), byte(kind), 0, 1)
	return raw
}
func TestLANDNSAnyResolverAndRecordTypesHaveValidatedFallback(t *testing.T) {
	for _, original := range []string{"192.168.1.8", "114.114.114.114", "8.8.8.8", "203.0.113.250"} {
		for _, network := range []string{"tcp", "udp"} {
			for _, kind := range []uint16{1, 28, 16, 65} {
				a, _ := fixture(t)
				a.applied = true
				a.cfg["lan_dns_intercept"] = true
				a.cfg["capture_lan"] = true
				a.cfg["dns_direct_servers"] = []any{"192.0.2.1", "192.0.2.2"}
				a.cfg["dns_proxy_servers"] = []any{"198.51.100.1", "198.51.100.2"}
				d := a.lanDNS
				attempts := []string{}
				d.exchange = func(ctx context.Context, action, server string, raw []byte) ([]byte, error) {
					attempts = append(attempts, action+"|"+server)
					if server == "192.0.2.1" {
						return nil, errors.New("synthetic primary outage")
					}
					return dnsErrorReply(raw, 0, false), nil
				}
				query := testDNSQuery("www.example.cn", kind)
				reply := d.resolve(a.ctx, query, original, network)
				if !validDNSReply(query, reply) || reply[3]&15 != 0 || len(attempts) != 2 || attempts[1] != "direct|192.0.2.2" {
					t.Fatal(original, network, kind, attempts, reply)
				}
				if num(d.status()["failovers"]) != 1 {
					t.Fatal("fallback not recorded")
				}
				attempts = nil
				d.resolve(a.ctx, query, original, network)
				if len(attempts) != 1 || attempts[0] != "direct|192.0.2.2" {
					t.Fatal("failed primary not cooled down", attempts)
				}
			}
		}
	}
}
func TestLANDNSCustomRulesForceAndFailureResults(t *testing.T) {
	a, _ := fixture(t)
	a.applied = true
	a.cfg["lan_dns_intercept"] = true
	a.cfg["capture_lan"] = true
	a.cfg["dns_force_proxy"] = true
	a.cfg["custom_rules"] = []any{M{"enabled": true, "type": "domain", "targets": []any{"domain:example.cn"}, "network": "tcp,udp", "action": "direct"}, M{"enabled": true, "type": "ip", "targets": []any{"8.8.8.8/32"}, "network": "tcp,udp", "port": "53", "action": "block"}}
	c := a.lanDNS.policySnapshot()
	if a.lanDNSAction(c, "www.example.cn", "8.8.8.8", "udp") != "direct" || a.lanDNSAction(c, "other.com", "8.8.8.8", "tcp") != "block" || a.lanDNSAction(c, "other.cn", "1.1.1.1", "udp") != "proxy" {
		t.Fatal("custom rule priority or force switch wrong")
	}
	queries := 0
	a.lanDNS.exchange = func(context.Context, string, string, []byte) ([]byte, error) {
		queries++
		return nil, errors.New("all upstreams failed")
	}
	blocked := a.lanDNS.resolve(a.ctx, testDNSQuery("other.com", 1), "8.8.8.8", "udp")
	if blocked[3]&15 != 5 || queries != 0 {
		t.Fatal("custom block bypassed")
	}
	failed := a.lanDNS.resolve(a.ctx, testDNSQuery("other.cn", 1), "1.1.1.1", "udp")
	if failed[3]&15 != 2 || queries == 0 {
		t.Fatal("failed DNS reported success")
	}
	query := testDNSQuery("other.cn", 1)
	a.lanDNS.exchange = func(_ context.Context, _, _ string, raw []byte) ([]byte, error) {
		r := dnsErrorReply(raw, 0, false)
		r[0]++
		return r, nil
	}
	if a.lanDNS.resolve(a.ctx, query, "1.1.1.1", "udp")[3]&15 != 2 {
		t.Fatal("mismatched transaction accepted")
	}
}
func TestLANDNSWireBoundsAndCaptureScope(t *testing.T) {
	useTopologyFixture(t)
	for _, raw := range [][]byte{nil, {1}, {0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0xc0, 0x0c}} {
		if _, err := parseDNSQuestion(raw); err == nil {
			t.Fatal("invalid DNS encoding accepted")
		}
	}
	query := testDNSQuery("www.example.com", 65)
	reply := dnsErrorReply(query, 0, false)
	if !validDNSReply(query, reply) {
		t.Fatal("valid response rejected")
	}
	reply[len(reply)-3]++
	if validDNSReply(query, reply) {
		t.Fatal("mismatched qtype accepted")
	}
	if dnsUDPSize(query) != 512 {
		t.Fatal("wrong nonEDNS budget")
	}
	binary.BigEndian.PutUint16(query[10:12], 1)
	query = append(query, 0, 0, 41, 0x10, 0, 0, 0, 0, 0, 0, 0)
	if dnsUDPSize(query) != 4096 {
		t.Fatal("EDNS budget missing")
	}
	c := M{"capture_lan": true, "lan_dns_intercept": true}
	rules := lanDNSCaptureRules(c)
	if !strings.Contains(rules, "172.18.0.0/16") || !strings.Contains(rules, "th dport 53 redirect to :1053") || !strings.Contains(rules, "th dport 1053 drop") {
		t.Fatal(rules)
	}
	c["capture_lan"] = false
	if lanDNSCaptureRules(c) != "" {
		t.Fatal("DNS intercept escaped LAN capture scope")
	}
}

func TestLANDNSRegexAndUnknownDomainFallback(t *testing.T) {
	if !dnsDomainMatches("abc.example", `regexp:^\D+\.example$`) {
		t.Fatal("regexp case semantics changed")
	}
	a, _ := fixture(t)
	a.applied = true
	a.cfg["lan_dns_intercept"] = true
	a.cfg["capture_lan"] = true
	a.cfg["dns_direct_servers"] = []any{"192.0.2.1"}
	a.cfg["dns_proxy_servers"] = []any{"198.51.100.1"}
	calls := []string{}
	a.lanDNS.exchange = func(_ context.Context, action, server string, raw []byte) ([]byte, error) {
		calls = append(calls, action)
		if action == "direct" {
			return nil, errors.New("direct upstream outage")
		}
		return dnsErrorReply(raw, 0, false), nil
	}
	reply := a.lanDNS.resolve(a.ctx, testDNSQuery("unknown.example", 28), "203.0.113.250", "udp")
	if reply[3]&15 != 0 || strings.Join(calls, ",") != "direct,proxy" {
		t.Fatal("direct group did not fall back to proxy", calls)
	}
}

func TestSmartDNSHostCaptureRespectsScopeAndCoreBypass(t *testing.T) {
	c := M{"capture_host": true, "capture_lan": false, "lan_dns_intercept": true, "_dns_vpn_devices": []any{"wg-home"}}
	text := lanDNSCaptureRules(c, "123")
	for _, part := range []string{"chain dns_output", "meta skuid 123 return", "socket mark 0x100cf return", "oifname { \"wg-home\" } return", "meta nfproto ipv4", "th dport 53 redirect to :1053"} {
		if !strings.Contains(text, part) {
			t.Fatal("missing host DNS protection", part, text)
		}
	}
	if strings.Contains(text, "chain dns_prerouting") {
		t.Fatal("host capture enabled LAN interception")
	}
	c["capture_host"] = false
	if lanDNSCaptureRules(c, "123") != "" {
		t.Fatal("DNS interception escaped selected capture scope")
	}
}

func TestSmartDNSDoesNotTreatGenericPrivateIPBypassAsDNSDomainPolicy(t *testing.T) {
	a, _ := fixture(t)
	c := M{"custom_rules": []any{M{"enabled": true, "type": "ip", "targets": []any{"127.0.0.0/8", "192.168.0.0/16"}, "action": "direct", "network": "tcp,udp", "port": ""}}, "_proxy_domains": []any{"google.com"}}
	if a.lanDNSAction(c, "www.google.com", "127.0.0.1", "udp") != "proxy" || a.lanDNSAction(c, "www.google.com", "192.168.1.8", "udp") != "proxy" {
		t.Fatal("gateway compatibility address changed queried-domain policy")
	}
}
