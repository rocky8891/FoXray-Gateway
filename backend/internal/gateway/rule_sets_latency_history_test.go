package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRuleSetValidationAndActiveCompilation(t *testing.T) {
	c := M{"custom_rules": []any{M{"enabled": true, "type": "domain", "action": "block", "targets": []any{"old.example"}, "network": "tcp,udp"}}, "rule_sets": []any{M{"id": "one", "name": "set", "profile": "global", "rules": []any{M{"enabled": true, "type": "domain", "action": "direct", "targets": []any{"new.example"}, "network": "tcp,udp"}}}}, "active_rule_set": "one"}
	if e := validateRuleSets(c); e != nil {
		t.Fatal(e)
	}
	rules := customEngineRules(c, "n1")
	if len(rules) != 1 || str(obj(rules[0])["outboundTag"]) != "out-direct" {
		t.Fatal(rules)
	}
	c["active_rule_set"] = "missing"
	if validateRuleSets(c) == nil {
		t.Fatal("missing active set accepted")
	}
}
func TestRuleSetUpdateRejectsMissingRulesAndPreservesConfig(t *testing.T) {
	a, _ := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	a.cfg["endpoints"] = []any{M{"id": "n1", "enabled": true}}
	a.active = "n1"
	_ = writeJSON(filepath.Join(a.base, "engine.json"), M{"inbounds": []any{M{"tag": "probe-n1", "listen": "127.0.0.1", "port": port}}})
	a.cfg["rule_sets"] = []any{M{"id": "one", "name": "one", "profile": "global", "source": "http://source.invalid/rules", "rules": []any{}}}
	before := fingerprint(a.cfg)
	if e := a.updateRuleSet(context.Background(), &Job{}, "one"); e == nil {
		t.Fatal("invalid empty document accepted")
	}
	if before != fingerprint(a.cfg) {
		t.Fatal("invalid update changed config")
	}
}
func TestLatencyHistorySupportsSixMonths(t *testing.T) {
	a, _ := fixture(t)
	r := a.latencyHistory(url.Values{"hours": {"4320"}})
	if r["error"] != nil || num(r["end"])-num(r["start"]) < 4319*3600 {
		t.Fatal(r)
	}
}
func TestServiceEnableChecksActualAutostart(t *testing.T) {
	a, r := fixture(t)
	r.responses = map[string]string{"systemctl show foxray-engine -p ActiveState -p SubState -p MainPID -p UnitFileState": "ActiveState=active\nUnitFileState=static\n"}
	// The non-mutating service reader must distinguish a static legacy unit.
	s := a.service("foxray-engine")
	if yes(s["autostart"]) || str(s["unit_file_state"]) != "static" {
		t.Fatal(s)
	}
	r.responses = nil
	if e := a.serviceAction(context.Background(), &Job{}, "xray", "enable", M{}); e != nil {
		t.Fatal(e)
	}
	for _, call := range r.calls {
		if strings.Contains(strings.Join(call, " "), "restart") {
			t.Fatal("autostart restarted core")
		}
	}
}
func TestRuleSetValidInactiveUpdateDoesNotReloadCore(t *testing.T) {
	a, r := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rules":[{"name":"test","enabled":true,"action":"direct","type":"domain","targets":["example.com"],"network":"tcp,udp","port":""}]}`))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	a.cfg["endpoints"] = []any{M{"id": "n1", "enabled": true}}
	a.active = "n1"
	a.applied = true
	_ = writeJSON(filepath.Join(a.base, "engine.json"), M{"inbounds": []any{M{"tag": "probe-n1", "listen": "127.0.0.1", "port": port}}})
	a.cfg["rule_sets"] = []any{M{"id": "one", "name": "one", "profile": "bypass_cn", "source": "http://source.invalid/rules", "rules": []any{}}}
	if e := a.updateRuleSet(context.Background(), &Job{}, "one"); e != nil {
		t.Fatal(e)
	}
	updated := obj(arr(a.cfg["rule_sets"])[0])
	if len(arr(updated["rules"])) != 1 || num(updated["updated"]) == 0 {
		t.Fatal(updated)
	}
	for _, call := range r.calls {
		for _, word := range call {
			if word == "restart" || word == "nft" {
				t.Fatal("inactive rule update reloaded core", call)
			}
		}
	}
}
