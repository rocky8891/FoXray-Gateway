package gateway

import (
	"path/filepath"
	"testing"
)

func TestVersionCheckPolicyDefaultsIsolationAndPersistence(t *testing.T) {
	a, _ := fixture(t)
	for _, service := range []string{"foxray", "xray", "warp", "wireguard"} {
		p := a.versionCheckPolicy(service)
		if !yes(p["enabled"]) || num(p["interval_hours"]) != 24 {
			t.Fatal("defaults changed", service)
		}
	}
	if err := a.saveVersionCheckPolicy(M{"service": "warp", "enabled": false, "interval_hours": 6}); err != nil {
		t.Fatal(err)
	}
	if yes(a.versionCheckPolicy("warp")["enabled"]) || num(a.versionCheckPolicy("warp")["interval_hours"]) != 6 {
		t.Fatal("policy not applied")
	}
	if !yes(a.versionCheckPolicy("xray")["enabled"]) || num(a.versionCheckPolicy("xray")["interval_hours"]) != 24 {
		t.Fatal("policy leaked across services")
	}
	stored := M{}
	if err := readJSON(filepath.Join(a.base, "manager-settings.json"), &stored); err != nil {
		t.Fatal(err)
	}
	if yes(obj(obj(stored["version_check_policies"])["warp"])["enabled"]) {
		t.Fatal("disabled state not persisted")
	}
	a.components["warp"] = true
	row := obj(obj(a.updatesSnapshot()["services"])["warp"])
	if yes(obj(row["check_policy"])["enabled"]) {
		t.Fatal("API missing policy")
	}
	for _, data := range []M{{"service": "bogus", "enabled": true, "interval_hours": 6}, {"service": "xray", "enabled": "true", "interval_hours": 6}, {"service": "xray", "enabled": true, "interval_hours": 0}, {"service": "xray", "enabled": true, "interval_hours": 169}, {"service": "xray", "enabled": true, "interval_hours": 1.5}, {"service": "xray", "enabled": true, "interval_hours": 6, "extra": 1}} {
		if err := a.saveVersionCheckPolicy(data); err == nil {
			t.Fatal("invalid policy accepted", data)
		}
	}
}
func TestVersionCheckDueRespectsEnabledAndFrequency(t *testing.T) {
	previous := M{"checked_at": 1000.0}
	if versionCheckDue(M{"enabled": false, "interval_hours": 1}, previous, 100000) {
		t.Fatal("disabled auto-check scheduled")
	}
	if versionCheckDue(M{"enabled": true, "interval_hours": 6}, previous, 1000+5*3600) {
		t.Fatal("check ran early")
	}
	if !versionCheckDue(M{"enabled": true, "interval_hours": 6}, previous, 1000+6*3600) {
		t.Fatal("due check not scheduled")
	}
	if !versionCheckDue(M{"enabled": true, "interval_hours": 1}, M{}, 100000) {
		t.Fatal("first check never scheduled")
	}
}
