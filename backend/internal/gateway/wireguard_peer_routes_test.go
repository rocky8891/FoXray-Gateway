package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestPeerRoutesPreserveSharedAndUnrelatedRoutes(t *testing.T) {
	peer := WGPeer{PublicKey: "selected", AllowedIPs: []string{"192.0.2.0/24", "2001:db8::/64", "198.51.100.0/24"}, Disabled: true}
	profile := &WGProfile{Name: "wg-test", Table: "2000"}
	r := &fakeRunner{responses: map[string]string{
		"wg show wg-test allowed-ips":                                   "other\t198.51.100.0/24\n",
		"ip -j -4 route show table all exact 192.0.2.0/24 dev wg-test":  `[{"dst":"192.0.2.0/24","dev":"wg-test","table":2000,"protocol":"static","metric":20},{"dst":"192.0.2.0/24","table":"2000","scope":"link","flags":[]}]`,
		"ip -j -6 route show table all exact 2001:db8::/64 dev wg-test": `[{"dst":"2001:db8::/64","dev":"wg-test","table":2000,"protocol":"kernel"}]`,
	}}
	if err := syncWGPeerRoutes(context.Background(), r, profile, peer); err != nil {
		t.Fatal(err)
	}
	var mutations []string
	for _, args := range r.calls {
		if args[0] == "ip" && args[1] != "-j" {
			mutations = append(mutations, strings.Join(args, " "))
		}
	}
	if len(mutations) != 1 || mutations[0] != "ip -4 route del 192.0.2.0/24 dev wg-test table 2000 proto boot" {
		t.Fatal(mutations)
	}
	delete(r.responses, "ip -j -4 route show table all exact 192.0.2.0/24 dev wg-test")
	peer.Disabled = false
	r.calls = nil
	if err := syncWGPeerRoutes(context.Background(), r, profile, peer); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.calls[2], " "); got != "ip -4 route add 192.0.2.0/24 dev wg-test table 2000 proto boot" {
		t.Fatal(got)
	}
}

func TestManualWireGuardStartupSkipsRoutesForDisabledPeer(t *testing.T) {
	p := &WGProfile{Name: "wg-test", Network: &WGNetwork{Routes: []string{"192.0.2.0/24", "198.51.100.0/25"}, RouteMode: "manual", RouteTable: "2000"}, Peers: []WGPeer{{AllowedIPs: []string{"192.0.2.0/24", "198.51.100.0/24"}, Disabled: true}, {AllowedIPs: []string{"198.51.100.0/25"}}}}
	steps, err := wgHelperSteps(p)
	if err != nil || len(steps) != 1 || steps[0].Args[4] != "198.51.100.0/25" {
		t.Fatal(steps, err)
	}
}
