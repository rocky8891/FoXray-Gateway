package gateway

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func routeTestEngine(rules ...any) M {
	return M{"outbounds": []any{M{"tag": "out-n1", "protocol": "vless"}, M{"tag": "out-direct", "protocol": "freedom"}, M{"tag": "out-block", "protocol": "blackhole"}}, "routing": M{"rules": rules}}
}
func routeTestRow() M {
	return M{"id": "flow", "module": "xray", "protocol": "tcp", "source": "192.0.2.2:12345", "destination": "198.51.100.5:443", "active": true}
}
func TestXrayBusinessRouteFiltering(t *testing.T) {
	for _, tag := range []string{"out-n1", "out-direct", "out-block", ""} {
		row := routeTestRow()
		hints := map[string]string{}
		if tag != "" {
			hints[socketMetadataKey("tcp", str(row["source"]), str(row["destination"]))] = tag
		}
		class, _, _ := classifyXrayRoute(row, routeTestEngine(), hints)
		want := map[string]string{"out-n1": "proxy", "out-direct": "direct", "out-block": "blocked", "": "unconfirmed"}[tag]
		if class != want {
			t.Fatalf("%s got %s want %s", tag, class, want)
		}
		rows, stats := filterXrayBusiness([]M{row}, routeTestEngine(), hints)
		if (want == "direct" || want == "blocked") && len(rows) != 0 {
			t.Fatal("nonproxy flow retained")
		}
		if want == "unconfirmed" && (num(stats["unclassified_count"]) != 1 || proxyConnectionCounted(rows[0])) {
			t.Fatal("unknown counted")
		}
	}
	configured := routeTestEngine(M{"type": "field", "outboundTag": "out-direct"})
	class, _, _ := classifyXrayRoute(routeTestRow(), configured, nil)
	if class != "unconfirmed" {
		t.Fatal("configuration substituted for actual event")
	}
}
func TestXrayRouteAccessEvidenceRequiresExactTuple(t *testing.T) {
	stamp := time.Now()
	prefix := stamp.Format("2006/01/02 15:04:05")
	raw := fmt.Sprintf("%s from tcp:192.0.2.2:12345 accepted tcp:198.51.100.5:443 [tun -> out-direct]", prefix)
	hints := parseAccessRoutes(raw, float64(stamp.Unix()))
	class, _, _ := classifyXrayRoute(routeTestRow(), routeTestEngine(), hints)
	if class != "direct" {
		t.Fatal(class)
	}
	row := routeTestRow()
	row["destination"] = "198.51.100.6:443"
	class, _, _ = classifyXrayRoute(row, routeTestEngine(), hints)
	if class != "unconfirmed" {
		t.Fatal("different IP inherited direct route")
	}
	ambiguous := raw + "\n" + fmt.Sprintf("%s from tcp:192.0.2.2:12345 accepted tcp:198.51.100.5:443 [tun -> out-n1]", prefix)
	if len(parseAccessRoutes(ambiguous, float64(stamp.Unix()))) != 0 {
		t.Fatal("ambiguous route accepted")
	}
	if len(parseAccessRoutes(raw, float64(stamp.Unix())+61)) != 0 {
		t.Fatal("stale route accepted")
	}
}
func TestXrayProxyHistoryDoesNotMixOldTotalsOrUnknown(t *testing.T) {
	a, _ := fixture(t)
	m := a.connections
	stamp := now()
	m.summaries = M{"xray": M{"available": true}}
	row := routeTestRow()
	row["first_seen"], row["last_packet"], row["proxy_counted"] = stamp-1, stamp, true
	unknown := clone(row)
	unknown["id"], unknown["proxy_counted"] = "unknown", false
	m.saveBusinessHistory([]M{row, unknown}, stamp)
	if m.historyError != "" {
		t.Fatal(m.historyError)
	}
	_, err := a.histdb.Exec("INSERT INTO business_samples VALUES(?,?,?,?,?,?,?,?)", int64(stamp), "xray", "vpm-tun", "tcp", "", 99, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	chart := m.businessHistory(url.Values{"module": {"xray"}, "hours": {"1"}})
	items := chart["items"].([]M)
	if len(items) != 1 || num(items[0]["count"]) != 1 {
		t.Fatal(chart)
	}
}

func TestXrayCollectorExcludesDirectFromSummaryAndHistory(t *testing.T) {
	a, f := fixture(t)
	a.components = M{"xray": true}
	engine := routeTestEngine(M{"type": "field", "ip": []any{"198.51.100.5"}, "outboundTag": "out-direct"}, M{"type": "field", "ip": []any{"198.51.100.6"}, "outboundTag": "out-n1"}, M{"type": "field", "domain": []any{"domain:example.com"}, "outboundTag": "out-direct"})
	if err := writeJSON(filepath.Join(a.base, "engine.json"), engine); err != nil {
		t.Fatal(err)
	}
	meter := meterFixture(businessTarget{"xray", "", "vpm-tun"}, "ip", "tcp", "up", 100)
	set := obj(meter["set"])
	elements := []any{}
	for _, ip := range []string{"198.51.100.5", "198.51.100.6", "198.51.100.7"} {
		elements = append(elements, M{"elem": M{"val": M{"concat": []any{"192.0.2.2", 12345, ip, 443}}, "expires": 598, "counter": M{"bytes": 100}}})
	}
	set["elem"] = elements
	itemsForNFT := []any{M{"table": M{"comment": businessOwner}}, meter}
	for _, family := range []string{"ip", "ip6"} {
		for _, proto := range []string{"tcp", "udp"} {
			for _, direction := range []string{"up", "down"} {
				if family == "ip" && proto == "tcp" && direction == "up" {
					continue
				}
				empty := meterFixture(businessTarget{"xray", "", "vpm-tun"}, family, proto, direction, 0)
				obj(empty["set"])["elem"] = []any{}
				itemsForNFT = append(itemsForNFT, empty)
			}
		}
	}
	raw, _ := json.Marshal(M{"nftables": itemsForNFT})
	f.responses = map[string]string{"ip -j link show dev vpm-tun": `[{"ifname":"vpm-tun"}]`, "nft -j list table inet foxray_business_v1": string(raw)}
	a.routes.record(routeEvent{Timestamp: time.Now().Unix(), Inbound: "tun", Outbound: "out-direct", Network: "tcp", Source: "192.0.2.2:12345", Original: "tcp:198.51.100.5:443"})
	a.routes.record(routeEvent{Timestamp: time.Now().Unix(), Inbound: "tun", Outbound: "out-n1", Network: "tcp", Source: "192.0.2.2:12345", Original: "tcp:198.51.100.6:443"})
	a.connections.collect()
	summary := obj(a.connections.summaries["xray"])
	if num(summary["count"]) != 1 || num(summary["direct_excluded"]) != 1 || num(summary["unclassified_count"]) != 1 {
		t.Fatal(summary)
	}
	if len(a.connections.rows) != 2 {
		t.Fatal("direct shown in active proxy list", a.connections.rows)
	}
	chart := a.connections.businessHistory(url.Values{"module": {"xray"}, "hours": {"1"}})
	items := chart["items"].([]M)
	if len(items) != 1 || num(items[0]["count"]) != 1 {
		t.Fatal(chart)
	}
}
