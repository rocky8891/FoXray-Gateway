package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommunityRuleSourcesAndDecode(t *testing.T) {
	c := M{"rule_sets": []any{M{"predefined": true, "profile": "bypass_cn", "source": "https://raw.githubusercontent.com/rocky8891/FoXray-Gateway/main/rules/sets/bypass_cn.json"}, M{"predefined": false, "profile": "gfwlist", "source": "https://example.invalid/custom"}, M{"predefined": true, "profile": "global", "source": "https://raw.githubusercontent.com/rocky8891/FoXray-Gateway/main/rules/sets/global.json", "auto_update": true}}}
	migratePresetSources(c)
	rows := arr(c["rule_sets"])
	if obj(rows[0])["source"] != communityDirect || obj(rows[1])["source"] != "https://example.invalid/custom" || obj(rows[2])["source"] != "" || yes(obj(rows[2])["auto_update"]) {
		t.Fatal("migration changed custom source")
	}
	raw := "# community\nexample.com\nfull:exact.example\nregexp:^test[0-9]+\\.example$\nexample.com\n"
	rules, e := decodeRuleSource([]byte(raw), M{"source_format": "domain-list", "profile": "bypass_cn"})
	if e != nil || len(rules) != 1 || len(arr(obj(rules[0])["targets"])) != 3 || obj(rules[0])["action"] != "direct" {
		t.Fatal(rules, e)
	}
	if _, e = decodeRuleSource([]byte("<html>error</html>"), M{"source_format": "domain-list"}); e == nil {
		t.Fatal("HTML accepted")
	}
	if _, e = decodeRuleSource([]byte("regexp:["), M{"source_format": "domain-list"}); e == nil {
		t.Fatal("invalid regex accepted")
	}
	if _, e = decodeRuleSource([]byte(`[{"enabled":true,"action":"proxy","type":"domain","targets":["example.com"],"network":"tcp,udp"}]`), M{}); e != nil {
		t.Fatal(e)
	}
}
func TestConnectionSocketAndTrackedParsers(t *testing.T) {
	raw := `tcp ESTAB 0 0 192.0.2.1:30000 198.51.100.1:443 users:(("xray",pid=12,fd=1)) bytes_sent:100 bytes_acked:90 bytes_received:250
udp ESTAB 0 0 192.0.2.1:30001 198.51.100.2:443 users:(("other",pid=2,fd=1))`
	rows := parseSocketConnections(raw, map[string][]string{"198.51.100.1:443": {"n1"}})
	if len(rows) != 1 || rows[0]["node_id"] != "n1" || num(rows[0]["upload"]) != 100 || num(rows[0]["download"]) != 250 {
		t.Fatal(rows)
	}
	rows = parseSocketConnections(raw, map[string][]string{"198.51.100.1:443": {"n1", "n2"}})
	if rows[0]["node_id"] != "" {
		t.Fatal("ambiguous node guessed")
	}
	ct := `ipv4 2 tcp 6 100 ESTABLISHED src=192.0.2.2 dst=198.51.100.9 sport=30000 dport=443 packets=2 bytes=100 src=198.51.100.9 dst=192.0.2.2 sport=443 dport=30000 packets=4 bytes=200 [ASSURED] mark=0 use=1
ipv4 2 udp 17 10 src=192.0.2.2 dst=198.51.100.9 sport=53 dport=53 [UNREPLIED] src=198.51.100.9 dst=192.0.2.2 sport=53 dport=53 mark=0`
	rows = parseTrackedConnections(ct, "warp")
	if len(rows) != 1 || num(rows[0]["upload"]) != 100 || rows[0]["node_id"] != "warp" {
		t.Fatal(rows)
	}
	missing := parseTrackedConnections(strings.ReplaceAll(strings.ReplaceAll(ct, " bytes=100", ""), " bytes=200", ""), "warp")
	if missing[0]["upload"] != nil {
		t.Fatal("missing counter became zero")
	}
}
func TestConnectionCollectorKeepsTransportSeparateFromBusiness(t *testing.T) {
	a, f := fixture(t)
	a.components = M{"xray": true}
	a.native["nodes"] = []any{M{"id": "n1", "name": "test", "outbound": M{"server": "198.51.100.1", "server_port": 443}}}
	f.responses = map[string]string{"ss -HatunpieO": `tcp ESTAB 0 0 192.0.2.1:30000 198.51.100.1:443 users:(("xray",pid=12,fd=1)) bytes_sent:100 bytes_received:200`}
	a.connections.collect()
	snap := a.connections.snapshot("xray")
	if len(snap["items"].([]M)) != 0 || len(a.connections.transport) != 1 {
		t.Fatal("transport shown as business", snap)
	}
	for _, call := range f.calls {
		if strings.Join(call, " ") == "nft -f -" {
			t.Fatal("readonly monitor mutated network")
		}
	}
}

func TestDNSPreviewIsScopedReversibleAndValidated(t *testing.T) {
	a, f := fixture(t)
	uuid := "11111111-2222-3333-4444-555555555555"
	f.responses = map[string]string{"nmcli --escape no -g GENERAL.CON-UUID device show eth0": uuid, "nmcli --escape no -g ipv4.dns,ipv4.ignore-auto-dns,ipv6.dns,ipv6.ignore-auto-dns connection show " + uuid: "9.9.9.9\nno\n\nno\n", "nmcli --escape no -g IP4.DNS,IP6.DNS device show eth0": "9.9.9.9\n"}
	p, e := a.network.Preview(M{"kind": "dns", "operation": "replace", "allow_protected": true, "item": M{"scope": "host", "interface": "eth0", "servers": []any{"1.1.1.1", "2001:4860:4860::8888"}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) != 2 || len(p.undo) != 2 {
		t.Fatal("DNS apply/undo pair missing")
	}
	if !strings.Contains(strings.Join(p.steps[1].Args, " "), "device modify eth0") || !strings.Contains(strings.Join(p.undo[1].Args, " "), "9.9.9.9") {
		t.Fatal(p.steps, p.undo)
	}
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), "modify") {
			t.Fatal("preview mutated DNS")
		}
	}
	for _, servers := range []any{[]any{}, []any{"bad;command"}, []any{"0.0.0.0"}} {
		if _, e := dnsServers(servers); e == nil {
			t.Fatal("invalid DNS accepted")
		}
	}
	if _, e := a.network.Preview(M{"kind": "dns", "operation": "replace", "allow_protected": true, "item": M{"scope": "host", "interface": "wg-home", "servers": []any{"1.1.1.1"}}}); e == nil {
		t.Fatal("protected VPN DNS accepted")
	}
}
func TestDNSInPlaceRollbackKeepsOriginalFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "resolver.conf")
	backup := filepath.Join(t.TempDir(), "old.conf")
	old := []byte("nameserver 9.9.9.9\n")
	updated := []byte("nameserver 1.1.1.1\n")
	_ = os.WriteFile(target, updated, 0644)
	_ = os.WriteFile(backup, old, 0600)
	before, _ := os.Stat(target)
	if e := restoreManagedFile(&FileRestore{Target: target, Backup: backup, Existed: true, OldHash: fingerprint(string(old)), ExpectedHash: fingerprint(string(updated)), InPlace: true}); e != nil {
		t.Fatal(e)
	}
	after, _ := os.Stat(target)
	if !os.SameFile(before, after) {
		t.Fatal("binding inode replaced")
	}
}

func TestPublicRuleFixtureCompatibility(t *testing.T) {
	directory := os.Getenv("FOXRAY_PUBLIC_RULE_FIXTURE")
	if directory == "" {
		t.Skip("optional downloaded upstream fixture")
	}
	for _, name := range []string{"direct-list.txt", "proxy-list.txt"} {
		raw, e := os.ReadFile(filepath.Join(directory, name))
		if e != nil {
			t.Fatal(e)
		}
		rules, e := decodeRuleSource(raw, M{"source_format": "domain-list", "profile": "bypass_cn"})
		if e != nil {
			t.Fatal(name, e)
		}
		if len(rules) == 0 {
			t.Fatal("no parsed rules")
		}
	}
}

func TestDefaultICMPIsOffAndSavedPreferenceSurvives(t *testing.T) {
	a, _ := fixture(t)
	if yes(a.icmp["enabled"]) {
		t.Fatal("new installation enabled ICMP")
	}
	base, ui := t.TempDir(), t.TempDir()
	if e := writeJSON(filepath.Join(base, "icmp-policy.json"), M{"enabled": true, "interval": 300, "count": 20, "size": 56, "timeout": 2}); e != nil {
		t.Fatal(e)
	}
	saved, e := New(base, ui, true)
	if e != nil {
		t.Fatal(e)
	}
	defer saved.Close()
	if !yes(saved.icmp["enabled"]) {
		t.Fatal("saved preference overwritten")
	}
}

func TestXrayUnconnectedUDPCorrelatesSocketAndTrackedPeer(t *testing.T) {
	sockets := `udp UNCONN 0 0 0.0.0.0%ens16:34567 *:* users:(("xray",pid=12,fd=7))`
	tracked := `ipv4 2 udp 17 100 src=192.0.2.1 dst=198.51.100.4 sport=34567 dport=443 packets=4 bytes=100 src=198.51.100.4 dst=192.0.2.1 sport=443 dport=34567 packets=5 bytes=200 [ASSURED] mark=0 use=1`
	rows := xrayUDPConnections(sockets, tracked, map[string][]string{"198.51.100.4:443": {"n1"}})
	if len(rows) != 1 || rows[0]["node_id"] != "n1" || num(rows[0]["download"]) != 200 {
		t.Fatal(rows)
	}
	if len(xrayUDPConnections(strings.ReplaceAll(sockets, "xray", "other"), tracked, map[string][]string{"198.51.100.4:443": {"n1"}})) != 0 {
		t.Fatal("another process was attributed to Xray")
	}
	if len(xrayUDPConnections(sockets, strings.ReplaceAll(tracked, "sport=34567", "sport=22222"), map[string][]string{"198.51.100.4:443": {"n1"}})) != 0 {
		t.Fatal("different socket port attributed")
	}
}
