package gateway

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func meterFixture(t businessTarget, family, proto, direction string, bytes float64) M {
	return M{"set": M{"name": t.prefix() + "_" + family + "_" + proto + "_" + direction, "elem": []any{M{"elem": M{"val": M{"concat": []any{"192.0.2.2", 12345, "198.51.100.5", 443}}, "expires": 598, "counter": M{"bytes": bytes}}}}}}
}

func TestBusinessMetersJoinBothDirectionsWithoutInventingNode(t *testing.T) {
	target := businessTarget{"xray", "", "vpm-tun"}
	raw, _ := json.Marshal(M{"nftables": []any{meterFixture(target, "ip", "tcp", "up", 123), meterFixture(target, "ip", "tcp", "down", 456)}})
	rows, err := parseBusinessMeters(string(raw), []businessTarget{target}, 1000)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	r := rows[0]
	if r["source"] != "192.0.2.2:12345" || num(r["upload"]) != 123 || num(r["download"]) != 456 || r["node_id"] != "" || num(r["last_packet"]) != 998 {
		t.Fatal(r)
	}
	broken := strings.Replace(string(raw), "192.0.2.2", "unknown verdict", 1)
	if _, err = parseBusinessMeters(broken, []businessTarget{target}, 1000); err == nil {
		t.Fatal("corrupt kernel tuple accepted")
	}
}

func TestBusinessReadOnlyAndForeignTableNeverMutate(t *testing.T) {
	a, f := fixture(t)
	target := businessTarget{"xray", "", "vpm-tun"}
	f.responses = map[string]string{"ip -j link show dev vpm-tun": `[{"ifname":"vpm-tun"}]`, "nft -j list table inet foxray_business_v1": `{"nftables":[{"table":{"comment":"foreign"}}]}`}
	_, summary := a.connections.businessCollect(a.ctx, []businessTarget{target})
	if yes(obj(summary["xray"])["available"]) {
		t.Fatal(summary)
	}
	for _, call := range f.calls {
		if strings.Join(call, " ") == "nft -f -" {
			t.Fatal("foreign table modified")
		}
	}
	f.fail = "nft"
	delete(f.responses, "nft -j list table inet foxray_business_v1")
	f.calls = nil
	_, summary = a.connections.businessCollect(a.ctx, []businessTarget{target})
	if yes(obj(summary["xray"])["available"]) {
		t.Fatal("readonly installed table")
	}
	for _, call := range f.calls {
		if strings.Join(call, " ") == "nft -f -" {
			t.Fatal("readonly mutation")
		}
	}
}

func TestBusinessHistoryFiltersRangeAndModule(t *testing.T) {
	a, _ := fixture(t)
	m := a.connections
	stamp := now()
	m.summaries = M{"xray": M{"available": true}, "warp": M{"available": true}}
	rows := []M{{"id": "one", "module": "xray", "interface": "vpm-tun", "protocol": "tcp", "node_id": "", "source": "192.0.2.1:1234", "destination": "198.51.100.1:443", "first_seen": stamp - 30, "time": stamp, "last_packet": stamp, "active": true, "upload": 123, "download": 456, "upload_bps": 10, "download_bps": 20}, {"id": "two", "module": "warp", "interface": "CloudflareWARP", "protocol": "udp", "node_id": "warp", "first_seen": stamp - 30, "time": stamp, "last_packet": stamp, "active": true, "upload_bps": 50, "download_bps": 60}}
	m.saveBusinessHistory(rows, stamp)
	if m.historyError != "" {
		t.Fatal(m.historyError)
	}
	q := url.Values{"module": {"xray"}, "scope": {"history"}, "hours": {"1"}, "protocol": {"tcp"}}
	view := m.query(q)
	if len(view["items"].([]M)) != 1 {
		t.Fatal(view)
	}
	chart := m.businessHistory(q)
	items := chart["items"].([]M)
	if len(items) != 1 || num(items[0]["count"]) != 1 || num(items[0]["upload"]) != 10 {
		t.Fatal(chart)
	}
	q.Set("protocol", "udp")
	if len(m.query(q)["items"].([]M)) != 0 || len(m.businessHistory(q)["items"].([]M)) != 0 {
		t.Fatal("protocol filter ignored")
	}
	q.Set("start", "100")
	q.Set("end", "50")
	if m.query(q)["error"] == nil {
		t.Fatal("invalid range accepted")
	}
}

func TestBusinessRatesPreserveExpiredOppositeDirectionAndReset(t *testing.T) {
	old := M{"time": 100., "first_seen": 90., "upload": 500., "download": 200., "_raw_upload": 500., "_raw_download": 200.}
	row := M{"upload": 550., "download": 0., "_observed_upload": true, "last_packet": 102.}
	updateBusinessRates(row, old, 102)
	if num(row["download"]) != 200 || num(row["upload_bps"]) != 25 || num(row["download_bps"]) != 0 {
		t.Fatal(row)
	}
	next := M{"upload": 570., "download": 30., "_observed_upload": true, "_observed_download": true, "last_packet": 104.}
	updateBusinessRates(next, row, 104)
	if num(next["download"]) != 230 || num(next["download_bps"]) != 15 || num(next["first_seen"]) != 90 {
		t.Fatal(next)
	}
}

func TestHostDefaultMetricValidationAndPersistence(t *testing.T) {
	a, _ := fixture(t)
	if err := a.host.configure(M{"default_load": "load15", "default_cpu": "cpu_hi", "default_memory": "memory_free", "default_partition": "/boot"}); err != nil {
		t.Fatal(err)
	}
	if a.host.policy["default_load"] != "load15" || a.host.policy["default_partition"] != "/boot" {
		t.Fatal(a.host.policy)
	}
	for _, invalid := range []M{{"default_cpu": "invalid"}, {"default_memory": "swap"}, {"default_partition": "relative"}} {
		if err := a.host.configure(invalid); err == nil {
			t.Fatal("invalid default accepted", invalid)
		}
	}
}

func TestConnectionCountsDoNotReenterApplicationReadLock(t *testing.T) {
	a, _ := fixture(t)
	a.connections.summaries = M{"xray": M{"available": true}}
	a.mu.RLock()
	waiting, writerDone := make(chan struct{}), make(chan struct{})
	go func() { close(waiting); a.mu.Lock(); a.mu.Unlock(); close(writerDone) }()
	<-waiting
	time.Sleep(20 * time.Millisecond)
	done := make(chan struct{})
	go func() { a.connections.counts(); close(done) }()
	blocked := false
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		blocked = true
	}
	a.mu.RUnlock()
	<-writerDone
	<-done
	if blocked {
		t.Fatal("connection snapshot reentered application lock while a writer waited")
	}
}
