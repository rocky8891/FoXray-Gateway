package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWGRemarkValidationAndRoundTrip(t *testing.T) {
	for _, value := range []any{123, "bad\nline", "bad\x00", strings.Repeat("字", 201)} {
		if _, err := validateWGRemark(value); err == nil {
			t.Fatal("invalid remark accepted")
		}
	}
	raw := "# existing comment\r\n[Interface]\r\nAddress = 10.20.0.1/24\r\nSaveConfig = false\r\n"
	updated := rewriteWGRemark(raw, "回家 VPN · 测试")
	if readWGRemark(updated) != "回家 VPN · 测试" || rewriteWGRemark(updated, "") != raw {
		t.Fatal("round trip modified other configuration")
	}
	replaced := rewriteWGRemark(updated, "办公网络")
	if strings.Count(replaced, wgRemarkPrefix) != 1 || readWGRemark(replaced) != "办公网络" {
		t.Fatal("remark duplicated")
	}
	profile, err := parseWG("wg-test", replaced)
	if err != nil || profile.Remark != "办公网络" {
		t.Fatal("profile did not expose remark")
	}
	data, _ := json.Marshal(profile)
	if !strings.Contains(string(data), "办公网络") {
		t.Fatal("public profile lacks remark")
	}
	rendered := profile.render()
	if readWGRemark(rendered) != "办公网络" {
		t.Fatal("configuration render lost remark")
	}
}
func TestWGRemarkPlanPreservesRuntimeAndRollsBack(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "wg-note.conf")
	original := []byte("# user comment\n[Interface]\nAddress = 10.20.0.1/24\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	n := &Network{app: &App{base: base}}
	p := &NetworkPlan{ID: "remark-test", Kind: "wireguard"}
	if err := n.planWGRemarkFile(p, "wg-home", path, "回家 VPN"); err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 0 || len(p.undo) != 0 {
		t.Fatal("remark plan operates on services or routes")
	}
	if err := p.precondition(); err != nil {
		t.Fatal(err)
	}
	if err := p.applyFiles(); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(path)
	if readWGRemark(string(updated)) != "回家 VPN" {
		t.Fatal("remark not saved")
	}
	if err := restoreManagedFile(p.restoreFile); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(path)
	if string(restored) != string(original) {
		t.Fatal("rollback did not preserve original bytes")
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.precondition(); err == nil {
		t.Fatal("stale configuration accepted")
	}
}

func TestWGRemarkOperationDispatch(t *testing.T) {
	n := &Network{app: &App{base: t.TempDir()}, plans: map[string]*NetworkPlan{}, managed: M{}}
	_, err := n.Preview(M{"kind": "wireguard", "operation": "remark", "item": M{"name": "wg-test", "remark": strings.Repeat("x", 201)}})
	if err == nil || !strings.Contains(err.Error(), "备注") {
		t.Fatal("remark did not reach validation")
	}
	_, err = n.Preview(M{"kind": "routes", "operation": "remark", "item": M{}})
	if err == nil || !strings.Contains(err.Error(), "网络操作无效") {
		t.Fatal("remark accepted outside WireGuard")
	}
}
