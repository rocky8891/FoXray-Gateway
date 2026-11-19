package gateway

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMonitorItemsDefaultOnAndPersistedIndependently(t *testing.T) {
	a, _ := fixture(t)
	for module, keys := range monitorItems {
		for _, key := range keys {
			if !a.monitorEnabled(module, key) {
				t.Fatal("default off", module, key)
			}
		}
	}
	if err := a.saveMonitorItems("xray", M{"traffic": true, "latency": false, "connections": false}); err != nil {
		t.Fatal(err)
	}
	if a.monitorEnabled("xray", "connections") || !a.monitorEnabled("warp", "connections") {
		t.Fatal("module isolation failed")
	}
	if err := a.saveMonitorItems("xray", M{"traffic": 1, "latency": true, "connections": true}); err == nil {
		t.Fatal("nonboolean switch accepted")
	}
	stored := M{}
	if err := readJSON(a.base+"/monitoring-items.json", &stored); err != nil || yes(obj(stored["xray"])["latency"]) {
		t.Fatal(stored, err)
	}
}
func TestServerConnectionProtocolCounts(t *testing.T) {
	value := serverConnectionSummary("ipv4 2 tcp 6 300 ESTABLISHED src=192.0.2.1 dst=198.51.100.1\nipv4 2 udp 17 60 src=192.0.2.1 dst=198.51.100.1\nipv4 2 icmp 1 30 src=192.0.2.1 dst=198.51.100.1\nipv6 10 icmpv6 58 30 src=::1 dst=::2\nipv4 2 gre 47 30 src=192.0.2.1 dst=198.51.100.1", nil)
	if num(value["count"]) != 5 {
		t.Fatal(value)
	}
	for _, protocol := range []string{"tcp", "udp", "icmp", "icmpv6", "other"} {
		if num(obj(value["protocols"])[protocol]) != 1 {
			t.Fatal(value)
		}
	}
	if yes(serverConnectionSummary("", errors.New("unavailable"))["available"]) {
		t.Fatal("missing collector became zero")
	}
}
func TestLazyTargetsRoundTripAndRejectStaleReference(t *testing.T) {
	a, _ := fixture(t)
	row := M{"name": "test", "enabled": true, "action": "direct", "type": "domain", "targets": []any{"example.com", "example.org"}, "network": "tcp,udp", "port": ""}
	a.cfg["rule_sets"] = []any{M{"id": "s1", "name": "test", "profile": "bypass_cn", "rules": []any{row}}}
	old := clone(a.cfg)
	compact := a.publicConfigLocked()
	publicRow := obj(arr(obj(arr(compact["rule_sets"])[0])["rules"])[0])
	raw, _ := json.Marshal(compact)
	if strings.Contains(string(raw), "example.org") || num(publicRow["targets_count"]) != 2 || publicRow["targets_preview"] != "example.com" {
		t.Fatal("targets leaked into polling payload")
	}
	if len(a.ruleTargets(str(publicRow["targets_ref"]))["items"].([]any)) != 2 {
		t.Fatal("lazy lookup failed")
	}
	if err := hydrateRuleTargets(compact, old); err != nil {
		t.Fatal(err)
	}
	if fingerprint(compact["rule_sets"]) != fingerprint(old["rule_sets"]) {
		t.Fatal("unchanged rule contents lost")
	}
	stale := a.publicConfigLocked()
	obj(arr(obj(arr(stale["rule_sets"])[0])["rules"])[0])["targets_ref"] = "stale"
	if err := hydrateRuleTargets(stale, old); err == nil {
		t.Fatal("stale target reference accepted")
	}
}
func TestConnectionReadersRemainResponsiveDuringHistoryWrite(t *testing.T) {
	a, _ := fixture(t)
	m := a.connections
	// Hold the only SQLite connection to emulate an in-flight writer.
	connection, err := a.histdb.Conn(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.saveBusinessHistory(nil, now()); close(done) }()
	time.Sleep(20 * time.Millisecond)
	read := make(chan struct{})
	go func() { m.snapshot("xray"); close(read) }()
	blocked := false
	select {
	case <-read:
	case <-time.After(300 * time.Millisecond):
		blocked = true
	}
	connection.Close()
	<-done
	if blocked {
		t.Fatal("database wait blocked in-memory connection snapshot")
	}
}

func TestServerSummaryCountSurvivesBusinessCollection(t *testing.T) {
	a, f := fixture(t)
	a.components = M{}
	f.responses = map[string]string{"conntrack -L -o extended": "ipv4 2 tcp 6 100 ESTABLISHED src=192.0.2.1 dst=198.51.100.2 sport=1234 dport=443"}
	a.connections.collect()
	summary := obj(a.connections.snapshot("")["summaries"])["host"]
	if num(obj(summary)["count"]) != 1 {
		t.Fatal("server count overwritten by business rows", summary)
	}
}
