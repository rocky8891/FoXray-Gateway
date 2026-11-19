package gateway

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPresentationRangeLayoutAndIsolation(t *testing.T) {
	a, f := fixture(t)
	before := fingerprint(a.cfg)
	icmp := fingerprint(a.icmp)
	for unit, factor := range rangeUnits {
		p := defaultPresentation()
		p["range_value"] = 525600 / factor
		p["range_unit"] = unit
		p["overview_layout"] = "sidebar"
		p["overview_cards"] = []any{"connections", "info"}
		if err := a.savePresentation(p); err != nil {
			t.Fatal(unit, err)
		}
		if str(a.presentationPolicy()["overview_layout"]) != "sidebar" {
			t.Fatal("not saved")
		}
	}
	for _, p := range []M{{"range_value": 0}, {"range_value": 2, "range_unit": "year"}, {"range_unit": "second"}, {"overview_cards": []any{"info", "info"}}, {"overview_cards": "info"}, {"overview_layout": "unknown"}, {"unrecognized": 1}} {
		saved := fingerprint(a.settings)
		if a.savePresentation(p) == nil || fingerprint(a.settings) != saved {
			t.Fatal("invalid presentation changed settings", p)
		}
	}
	if fingerprint(a.cfg) != before || fingerprint(a.icmp) != icmp || len(f.calls) != 0 {
		t.Fatal("presentation touched probes or core")
	}
}
func TestMonitorOrderingValidationAndPersistence(t *testing.T) {
	a, _ := fixture(t)
	p := clone(obj(defaultMonitorItems()["host"]))
	p["order"] = []any{"connections", "disk", "network", "partition", "memory", "cpu", "load"}
	if err := a.saveMonitorItems("host", p); err != nil {
		t.Fatal(err)
	}
	if str(arr(obj(a.monitorItemsSnapshot()["host"])["order"])[0]) != "connections" {
		t.Fatal("order not retained")
	}
	p["order"] = []any{"cpu", "cpu", "network", "partition", "memory", "disk", "load"}
	if a.saveMonitorItems("host", p) == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestMinuteAndYearHistoryRanges(t *testing.T) {
	a, _ := fixture(t)
	for _, hours := range []string{"0.016666666666666666", "0.08333333333333333", "0.25", "8760"} {
		q := url.Values{"hours": {hours}}
		start, end, valid := connectionRange(q)
		if !valid || end-start < 59.99 {
			t.Fatal("connection range rejected", hours)
		}
		for _, r := range []M{a.host.history(q), a.traffic.history(q), a.latencyHistory(q), a.connections.history(q)} {
			if num(r["end"])-num(r["start"]) > end-start+2 {
				t.Fatal("range silently reverted to 24h", hours, r["start"], r["end"])
			}
		}
	}
}
func TestConnectionMetadataNeverInventsDomainOrRemoteApplication(t *testing.T) {
	stamp := float64(time.Date(2026, 11, 15, 20, 0, 30, 0, time.Local).Unix())
	raw := "2026/11/15 20:00:01 from tcp:192.0.2.1:3210 accepted tcp:example.org:443 [tun -> proxy]\n"
	domains := parseAccessDomains(raw, stamp)
	rows := []M{{"module": "xray", "protocol": "tcp", "source": "192.0.2.1:3210", "destination": "198.51.100.1:443"}, {"module": "wireguard", "protocol": "tcp", "source": "192.0.2.2:3210", "destination": "198.51.100.1:443"}}
	enrichConnectionMetadata(rows, `tcp ESTAB 0 0 192.0.2.1:3210 198.51.100.1:443 users:(("curl",pid=1,fd=2))`, domains)
	if rows[0]["domain"] != "example.org" || rows[0]["application"] != "curl" {
		t.Fatal(rows[0])
	}
	if rows[1]["domain"] != "" || rows[1]["application"] != "" {
		t.Fatal("remote app inferred")
	}
	ambiguous := parseAccessDomains(raw+strings.ReplaceAll(raw, "example.org", "other.example"), stamp)
	if len(ambiguous) != 0 || len(parseAccessDomains(raw, stamp+61)) != 0 {
		t.Fatal("ambiguous or expired domain retained")
	}
}

func TestMonitorSnapshotsSkipLargeRoutingAndPreserveProbeFields(t *testing.T) {
	a, _ := fixture(t)
	a.cfg["rule_sets"] = []any{M{"rules": []any{M{"targets": []any{strings.Repeat("x", 2*1024*1024)}}}}}
	a.cfg["speed_enabled"] = false
	p := a.probeConfigLocked()
	if p["rule_sets"] != nil || p["endpoints"] == nil || p["speed_enabled"] != false || num(p["timeout"]) != num(a.cfg["timeout"]) {
		t.Fatal("probe snapshot lost policy or retained routing")
	}
	path := filepath.Join(t.TempDir(), "engine.json")
	raw := `{"inbounds":[{"tag":"probe-test","port":1234}],"log":{"access":"none"},"routing":{"rules":"` + strings.Repeat("x", 2*1024*1024) + `"}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	var rows []M
	if readEngineField(path, "inbounds", &rows) != nil || len(rows) != 1 || num(rows[0]["port"]) != 1234 {
		t.Fatal("small section required reading full engine")
	}
}

func TestRuleMetadataCacheInvalidatesAfterWriteAndDoesNotAlias(t *testing.T) {
	a, _ := fixture(t)
	a.cfg["custom_rules"] = []any{M{"targets": []any{"one.example"}, "name": "one"}}
	first := a.publicConfigLocked()
	obj(arr(first["custom_rules"])[0])["name"] = "tampered"
	if obj(arr(a.publicConfigLocked()["custom_rules"])[0])["name"] != "one" {
		t.Fatal("cached metadata aliases output")
	}
	a.cfg["custom_rules"] = []any{M{"targets": []any{"two.example"}, "name": "two"}}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	if obj(arr(a.publicConfigLocked()["custom_rules"])[0])["targets_preview"] != "two.example" {
		t.Fatal("cached target reference stale after write")
	}
}
func TestExistingBusinessMetersReadOnceWithoutMutation(t *testing.T) {
	a, f := fixture(t)
	target := businessTarget{"xray", "", "vpm-tun"}
	items := []any{M{"table": M{"comment": businessOwner}}}
	for _, family := range []string{"ip", "ip6"} {
		for _, proto := range []string{"tcp", "udp"} {
			for _, dir := range []string{"up", "down"} {
				items = append(items, meterFixture(target, family, proto, dir, 123))
			}
		}
	}
	raw, _ := json.Marshal(M{"nftables": items})
	f.responses = map[string]string{"ip -j link show dev vpm-tun": `[{}]`, "nft -j list table inet " + businessTable: string(raw)}
	rows, summaries := a.connections.businessCollect(a.ctx, []businessTarget{target})
	if len(rows) == 0 || !yes(obj(summaries["xray"])["available"]) {
		t.Fatal("meter snapshot lost")
	}
	count := 0
	for _, call := range f.calls {
		joined := strings.Join(call, " ")
		if joined == "nft -j list table inet "+businessTable {
			count++
		}
		if strings.Contains(joined, "nft -f") {
			t.Fatal("existing counters reinstalled")
		}
	}
	if count != 1 {
		t.Fatal("redundant meter dump", count)
	}
}
