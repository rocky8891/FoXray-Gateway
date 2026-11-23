package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCrossBundleMatchesManagedPolicyAndAvoidsCLI(t *testing.T) {
	a, f := fixture(t)
	item, err := normalizeCrossZone(obj(crossRequest(false)["item"]))
	if err != nil {
		t.Fatal(err)
	}
	item["id"] = id()
	name := crossZoneName(item)
	a.network.managed["firewall"] = []any{item}
	rules := []any{}
	for _, rule := range crossZoneRules(item) {
		rules = append(rules, rule)
	}
	settings := M{"target": "CONTINUE", "priority": -1, "ingress_zones": []any{"public"}, "egress_zones": []any{"home-vpn"}, "rich_rules": rules, "masquerade": false, "disable": false, "version": "1.0", "description": "managed"}
	raw, _ := json.Marshal(M{"zones": []any{"public", "home-vpn"}, "runtime": M{name: settings}, "permanent": M{name: settings}})
	f.responses = map[string]string{strings.Join([]string{"/usr/bin/python3", "-c", firewallCrossBundleScript}, " "): string(raw)}
	snapshot := a.network.CrossZoneSnapshot()
	if !yes(snapshot["complete"]) {
		t.Fatal(snapshot)
	}
	row := snapshot["items"].([]M)[0]
	if !yes(row["runtime"]) || !yes(row["permanent"]) || yes(row["drift"]) {
		t.Fatal("batch snapshot differs from canonical policy", row)
	}
	for _, call := range f.calls {
		if call[0] == "firewall-cmd" {
			t.Fatal("extra CLI process despite complete bundle")
		}
	}
}

type crossCancelRunner struct {
	Runner
	started chan struct{}
}

func (r *crossCancelRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	if args[0] == "/usr/bin/python3" {
		close(r.started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	return r.Runner.Run(ctx, args, input)
}
func TestCrossBundleCancellationDoesNotStartFallback(t *testing.T) {
	a, f := fixture(t)
	r := &crossCancelRunner{Runner: f, started: make(chan struct{})}
	a.runner = r
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan M, 1)
	go func() { done <- a.network.CrossZoneSnapshot(ctx) }()
	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("batch read did not start")
	}
	cancel()
	select {
	case snapshot := <-done:
		if yes(snapshot["complete"]) {
			t.Fatal("cancelled snapshot shown complete")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled read kept running")
	}
	for _, call := range f.calls {
		if call[0] == "firewall-cmd" {
			t.Fatal("cancelled request started expensive fallback")
		}
	}
}
