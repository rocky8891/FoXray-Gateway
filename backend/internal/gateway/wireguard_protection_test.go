package gateway

import (
	"path/filepath"
	"strings"
	"testing"
)

func protectionFixture(t *testing.T) (*App, *fakeRunner) {
	a, f := fixture(t)
	f.responses = map[string]string{"wg show interfaces": "wg-home wg-work liquan"}
	return a, f
}
func TestWireGuardProtectionPersistsWithoutNetworkMutations(t *testing.T) {
	a, f := protectionFixture(t)
	n := a.network
	if !n.wireguardProtected("wg-home") || n.wireguardProtected("wg-work") {
		t.Fatal("legacy defaults changed")
	}
	beforeCalls := len(f.calls)
	for _, input := range []M{{"interfaces": []any{"wg-home"}, "protected": false}, {"interfaces": []any{"wg-work", "liquan"}, "protected": true}} {
		if _, err := n.setWireGuardProtection(input); err != nil {
			t.Fatal(err)
		}
	}
	if n.wireguardProtected("wg-home") || !n.wireguardProtected("wg-work") || !n.wireguardProtected("liquan") {
		t.Fatal("explicit flags not applied")
	}
	for _, call := range f.calls[beforeCalls:] {
		if strings.Join(call, " ") != "wg show interfaces" {
			t.Fatal("protection mutation issued a network/service command", call)
		}
	}
	recovered := NewNetwork(a)
	if recovered.wireguardProtected("wg-home") || !recovered.wireguardProtected("wg-work") || recovered.wireguardProtectionSource("wg-home") != "user" {
		t.Fatal("flags did not survive reload")
	}
	var saved M
	if err := readJSON(filepath.Join(a.base, "private/network-managed.json"), &saved); err != nil || obj(saved[wgProtectionKey])["wg-home"] != false {
		t.Fatal("explicit false was lost", err)
	}
}
func TestWireGuardProtectionBatchIsAtomicAndValidated(t *testing.T) {
	a, _ := protectionFixture(t)
	n := a.network
	for _, input := range []M{
		{"interfaces": []any{"wg-work", "missing"}, "protected": true},
		{"interfaces": []any{"wg-work", "wg-work"}, "protected": true},
		{"interfaces": []any{"bad name"}, "protected": true},
		{"interfaces": []any{}, "protected": true},
		{"interfaces": []any{"wg-work"}, "protected": "true"},
	} {
		before := fingerprint(n.managed)
		if _, err := n.setWireGuardProtection(input); err == nil || before != fingerprint(n.managed) {
			t.Fatal("invalid batch changed metadata", input, err)
		}
	}
	n.plans["pending"] = &NetworkPlan{ID: "pending", Status: "awaiting_confirmation"}
	if _, err := n.setWireGuardProtection(M{"interfaces": []any{"wg-home"}, "protected": false}); err == nil {
		t.Fatal("changed protection during pending network change")
	}
}
func TestWireGuardProtectionControlsGuardsAndStalePlans(t *testing.T) {
	a, _ := protectionFixture(t)
	n := a.network
	original := fingerprint(n.managed)
	n.plans["preview"] = &NetworkPlan{ID: "preview", Status: "preview", Created: now(), revision: original}
	if _, err := n.setWireGuardProtection(M{"interfaces": []any{"wg-work"}, "protected": true}); err != nil {
		t.Fatal(err)
	}
	if err := n.Apply("preview"); err == nil || !strings.Contains(err.Error(), "规则已变化") {
		t.Fatal("stale preview bypassed new protection", err)
	}
	err := n.planWireGuard(&NetworkPlan{}, M{"operation": "peer-disable", "item": M{"name": "wg-work"}})
	if err == nil || !strings.Contains(err.Error(), "开启保护") {
		t.Fatal("custom interface bypassed protection", err)
	}
	if _, err := n.setWireGuardProtection(M{"interfaces": []any{"wg-home"}, "protected": false}); err != nil {
		t.Fatal(err)
	}
	err = n.planWireGuard(&NetworkPlan{}, M{"operation": "stop", "item": M{"name": "wg-home"}})
	if err != nil && strings.Contains(err.Error(), "开启保护") {
		t.Fatal("disabled default protection still blocked interface", err)
	}
}
func TestWireGuardProtectionRenameSurvivesPendingPlanRecovery(t *testing.T) {
	a, _ := protectionFixture(t)
	n := a.network
	change := &wgProtectionRename{From: "wg-home", To: "renamed", Protected: true}
	p := &NetworkPlan{ID: id(), Kind: "wireguard", Status: "awaiting_confirmation", Expires: now() + 120, wgProtectionRename: change}
	if err := n.writeSpec(p, 0); err != nil {
		t.Fatal(err)
	}
	recovered := NewNetwork(a)
	plan := recovered.plans[p.ID]
	if plan == nil || plan.wgProtectionRename == nil {
		t.Fatal("pending rename lost protection")
	}
	if err := recovered.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	if !recovered.wireguardProtected("renamed") {
		t.Fatal("renamed interface lost protection")
	}
	flags := obj(recovered.managed[wgProtectionKey])
	if _, found := flags["wg-home"]; found {
		t.Fatal("old override left behind")
	}
	reloaded := NewNetwork(a)
	if !reloaded.wireguardProtected("renamed") {
		t.Fatal("confirmed rename was not persisted")
	}
}
