package gateway

import (
	"context"
	"errors"
	"testing"
)

func TestWGPeerHealthRequiresConsecutiveSamples(t *testing.T) {
	var h wgPeerHealth
	h.observe(true, nil, 9)
	for range wgMissLimit - 1 {
		h.observe(false, nil, -1)
	}
	if h.state == "失联" || !h.demonstrated {
		t.Fatalf("premature failure: %+v", h)
	}
	h.observe(false, errors.New("no valid probe path"), -1)
	if h.state != "无法探测" || h.failure != 0 || h.recovery != 0 {
		t.Fatalf("command error counted as missing reply: %+v", h)
	}
	for range wgMissLimit {
		h.observe(false, nil, -1)
	}
	if h.state != "失联" {
		t.Fatalf("did not mark lost peer: %+v", h)
	}
	for range wgRecoveryLimit - 1 {
		h.observe(true, nil, 9)
	}
	if h.state != "失联" {
		t.Fatalf("premature recovery: %+v", h)
	}
	h.observe(true, nil, 9)
	if h.state != "正常" {
		t.Fatalf("did not mark recovered peer: %+v", h)
	}
}

func TestWGPeerRouteChangesRequireMatchingReadback(t *testing.T) {
	peer := WGPeer{AllowedIPs: []string{"192.0.2.0/24"}}
	profile := &WGProfile{Name: "wg-test"}
	const read = "ip -j -4 route show table 254 exact 192.0.2.0/24"
	runner := &fakeRunner{responses: map[string]string{read: `[{"dev":"wg-test"}]`}}
	state, err := wgGuardRouteState(context.Background(), runner, profile, peer, "192.0.2.0/24", "withdraw")
	if state != "撤销失败" || err == nil || len(runner.calls) != 3 {
		t.Fatalf("unchanged route incorrectly accepted: %s, %v, %v", state, err, runner.calls)
	}
	runner.calls = nil
	runner.responses[read] = `[{"dev":"wg-test"},{"dev":"eth0","metric":20}]`
	_, err = wgGuardRouteState(context.Background(), runner, profile, peer, "192.0.2.0/24", "withdraw")
	if err == nil || len(runner.calls) != 1 {
		t.Fatalf("ambiguous route mutation attempted: %v, %v", err, runner.calls)
	}
}

func TestWGPeerRemoteProbeKeepsSpecificHostRoute(t *testing.T) {
	peer := WGPeer{PublicKey: "peer", AllowedIPs: []string{"192.0.2.0/24"}, Probe: WGPeerProbe{Enabled: true, Target: "192.0.2.9", AutoWithdraw: true, Routes: []string{"192.0.2.0/24"}}}
	profile := &WGProfile{Name: "wg-test", Peers: []WGPeer{peer}}
	if err := validateWGPeerProbes(profile); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{responses: map[string]string{
		"ip -j -4 route get 192.0.2.9":                     `[{"dev":"wg-test"}]`,
		"ip -j -4 route show table 254 exact 192.0.2.9/32": `[]`,
	}}
	if err := ensureWGProbePath(context.Background(), runner, profile, peer); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 || runner.calls[2][4] != "192.0.2.9/32" {
		t.Fatalf("missing probe host route: %v", runner.calls)
	}
}

func TestWGPeerRouteInventoryShowsConnectedCoverage(t *testing.T) {
	a, _ := fixture(t)
	profile := &WGProfile{Name: "wg-test"}
	peer := WGPeer{AllowedIPs: []string{"10.24.255.2/32", "10.24.255.3/32", "192.0.2.7/32"}}
	inventory := map[string][]M{"-4|254": {
		{"dst": "10.24.255.0/24", "dev": "wg-test", "protocol": "kernel", "scope": "link"},
		{"dst": "10.24.255.3", "dev": "wg-test"},
		{"dst": "192.0.2.0/24", "dev": "eth0", "protocol": "kernel", "scope": "link"},
	}}
	rows := a.network.wgPeerRouteInventory(profile, peer, inventory)
	if len(rows) != 3 || rows[0]["status"] != "由直连网段覆盖" || rows[0]["covering_prefix"] != "10.24.255.0/24" || rows[1]["status"] != "已安装" || rows[2]["status"] != "未安装精确路由" {
		t.Fatal(rows)
	}
}
