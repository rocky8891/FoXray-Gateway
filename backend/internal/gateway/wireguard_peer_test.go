package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func peerTestKey(n byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{n}, 32))
}

func TestWGPeerDisableRoundTripPreservesOtherPeersAndCustomFields(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		key, other := peerTestKey(1), peerTestKey(2)
		raw := strings.ReplaceAll("[Interface]\nPrivateKey = synthetic-private\nAddress = 192.0.2.1/24\nSaveConfig = true\nPostUp = preserve-hook\n\n[Peer]\nPublicKey = "+key+"\nPresharedKey = synthetic-psk\nAllowedIPs = 192.0.2.2/32, 2001:db8::2/128\n# user note\nCustomOption = preserve\n\n[Peer]\nPublicKey = "+other+"\nAllowedIPs = 192.0.2.3/32\n", "\n", ending)
		disabled, err := rewriteWGPeerState(raw, key, true)
		if err != nil {
			t.Fatal(err)
		}
		parsed, _ := parseWG("wg-test", disabled)
		if len(parsed.Peers) != 2 || !parsed.Peers[0].Disabled || parsed.Peers[1].Disabled || parsed.Peers[0].PresharedKey != "synthetic-psk" {
			t.Fatal("peer state or saved configuration lost")
		}
		if !strings.Contains(disabled, "[Peer]"+ending+"PublicKey = "+other) || strings.Contains(disabled, "SaveConfig = true") {
			t.Fatal("other peer changed or SaveConfig still enabled")
		}
		rendered, _ := parseWG("wg-test", parsed.render())
		if len(rendered.Peers) != 2 || !rendered.Peers[0].Disabled {
			t.Fatal("disabled state lost on later edit")
		}
		enabled, err := rewriteWGPeerState(disabled, key, false)
		expected := string(wgStopConfiguration([]byte(raw)))
		if err != nil || enabled != expected {
			t.Fatal("round trip changed original directives or comments")
		}
		for _, invalid := range []string{raw + "[Peer]" + ending + "PublicKey = " + key + ending, strings.ReplaceAll(raw, key, other)} {
			if _, err := rewriteWGPeerState(invalid, key, true); err == nil {
				t.Fatal("missing or duplicate peer accepted")
			}
		}
	}
}

type peerStateRunner struct {
	*fakeRunner
	peerInput string
	failAdd   bool
}

func (r *peerStateRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	if len(args) > 1 && args[0] == "wg" && args[1] == "addconf" {
		r.peerInput = input
		if r.failAdd {
			return "", errors.New("synthetic failure")
		}
	}
	return r.fakeRunner.Run(ctx, args, input)
}

func TestWGPeerSyncOnlyMutatesSelectedPeerAndRejectsPrefixTheft(t *testing.T) {
	key, other := peerTestKey(1), peerTestKey(2)
	r := &peerStateRunner{fakeRunner: &fakeRunner{responses: map[string]string{"wg show wg-test allowed-ips": other + "\t192.0.2.3/32\n"}}}
	p := &WGProfile{PrivateKey: "synthetic-private", Peers: []WGPeer{{PublicKey: key, AllowedIPs: []string{"192.0.2.2/32"}, PresharedKey: "synthetic-psk", Disabled: true}, {PublicKey: other, AllowedIPs: []string{"192.0.2.3/32"}}}}
	if err := syncWGPeer(context.Background(), r, "wg-test", key, p); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.calls[0], " ") != "wg set wg-test peer "+key+" remove" {
		t.Fatal("disable did not target only one peer")
	}
	p.Peers[0].Disabled = false
	if err := syncWGPeer(context.Background(), r, "wg-test", key, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.peerInput, "PresharedKey = synthetic-psk") || strings.Contains(r.peerInput, "synthetic-private") || strings.Contains(r.peerInput, other) {
		t.Fatal("enable payload contains interface or other peer configuration")
	}
	r.peerInput = ""
	p.Peers[0].AllowedIPs = []string{"192.0.2.3/32"}
	if err := syncWGPeer(context.Background(), r, "wg-test", key, p); err == nil || r.peerInput != "" {
		t.Fatal("another peer's prefix was stolen")
	}
	p.Peers[0].AllowedIPs = []string{"192.0.2.2/32"}
	r.failAdd = true
	if err := syncWGPeer(context.Background(), r, "wg-test", key, p); err == nil {
		t.Fatal("live failure ignored")
	}
	for _, args := range r.calls {
		if args[0] == "systemctl" {
			t.Fatal("peer operation restarted interface")
		}
	}
}

func TestWGPeerInventoryKeepsDisabledPeersWithoutSecrets(t *testing.T) {
	key, other := peerTestKey(1), peerTestKey(2)
	rows := mergeWGPeerRows([]WGPeer{{PublicKey: key, Disabled: true, PresharedKey: "synthetic-psk"}, {PublicKey: other, Endpoint: "example.invalid:51820"}}, []M{{"public_key": other, "endpoint": "192.0.2.5:51820", "handshake": "123"}})
	if len(rows) != 2 || !yes(rows[0]["disabled"]) || yes(rows[0]["runtime_present"]) || str(rows[1]["endpoint"]) != "192.0.2.5:51820" {
		t.Fatal("inventory lost saved disabled peer or live endpoint")
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "synthetic-psk") {
		t.Fatal("secret exposed in inventory")
	}
}

func TestWGPeerPlanArmsRollbackBeforeMutationAndRejectsStaleConfig(t *testing.T) {
	a, r := fixture(t)
	key := peerTestKey(1)
	r.responses = map[string]string{"wg show interfaces": "wg-test"}
	path := filepath.Join(t.TempDir(), "wg-test.conf")
	original := "[Interface]\nAddress = 192.0.2.1/24\n[Peer]\nPublicKey = " + key + "\nAllowedIPs = 192.0.2.2/32\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	plan := &NetworkPlan{ID: id(), Kind: "wireguard", Status: "armed"}
	if err := a.network.planWGPeerStateFile(plan, "wg-test", key, true, path); err != nil {
		t.Fatal(err)
	}
	if err := plan.precondition(); err != nil {
		t.Fatal(err)
	}
	if err := plan.postcondition(); err != nil {
		t.Fatal(err)
	}
	r.responses["wg show wg-test peers"] = key
	if err := plan.postcondition(); err == nil {
		t.Fatal("incorrect live state accepted")
	}
	delete(r.responses, "wg show wg-test peers")
	if len(plan.steps) != 1 || len(plan.afterRestore) != 1 || len(plan.beforeRestore) != 0 {
		t.Fatal("peer rollback lifecycle incorrect")
	}
	if err := plan.applyFiles(); err != nil {
		t.Fatal(err)
	}
	var spec rollbackSpec
	if err := readJSON(a.network.specPath(plan), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.File.ExpectedHash == "" || len(spec.AfterFile) != 1 {
		t.Fatal("rollback not durable before live command")
	}
	if err := plan.precondition(); err == nil {
		t.Fatal("stale plan allowed")
	}
	if err := restoreManagedFile(spec.File); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(path)
	if string(restored) != original {
		t.Fatal("rollback failed to preserve original")
	}
	if err := runRollbackStep(r, spec.AfterFile[0]); err != nil {
		t.Fatal(err)
	}
	unsafe := spec.AfterFile[0]
	unsafe.Args = append(unsafe.Args, "--initialize")
	if err := runRollbackStep(r, unsafe); err == nil {
		t.Fatal("unexpected helper flags accepted in rollback")
	}
}
