package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type targetRunner struct {
	Runner
	live, saved                         string
	drift, failReloadOnce, ignoreSetter bool
}

func (r *targetRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	out, err := r.Runner.Run(ctx, args, input)
	if err != nil {
		return out, err
	}
	joined := strings.Join(args, " ")
	permanent := strings.Contains(joined, "--permanent")
	target := r.live
	if permanent {
		target = r.saved
	}
	if strings.Contains(joined, "--list-all-zones") {
		out = "public\n  target: " + target + "\n  services: ssh\n"
		if r.drift && !permanent {
			out += "  ports: 12345/tcp\n"
		}
		return out, nil
	}
	if strings.Contains(joined, "--list-all") && !strings.Contains(joined, "policies") {
		return strings.Replace(policyText("no", "no", ""), "target: default", "target: "+target, 1), nil
	}
	if strings.Contains(joined, "--info-service=ssh") {
		return "ssh\n  ports: 22/tcp\n  includes:\n  helpers:\n", nil
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--set-target=") && !r.ignoreSetter {
			r.saved = strings.TrimPrefix(arg, "--set-target=")
		}
	}
	if joined == "firewall-cmd --reload" {
		r.live = r.saved
		if r.failReloadOnce {
			r.failReloadOnce = false
			return "", errors.New("synthetic partial reload failure")
		}
	}
	return out, nil
}
func targetFixture(t *testing.T) (*App, *fakeRunner, *targetRunner) {
	a, f := policyFixture(t)
	r := &targetRunner{Runner: f, live: "default", saved: "default"}
	a.runner = r
	return a, f, r
}
func targetRequest(immediate bool) M {
	d := policyRequest()
	item := obj(d["item"])
	item["icmp_blocks"] = []any{}
	item["target"] = "DROP"
	item["target_ack"] = true
	item["target_apply"] = "permanent"
	if immediate {
		item["target_apply"] = "immediate"
		item["reload_ack"] = true
	}
	return d
}
func assertTargets(t *testing.T, r *targetRunner, live, saved string) {
	t.Helper()
	if r.live != live || r.saved != saved {
		t.Fatalf("targets live=%s saved=%s; want %s/%s", r.live, r.saved, live, saved)
	}
}
func TestTargetPermanentPreviewApplyAndStandaloneRollback(t *testing.T) {
	a, f, r := targetFixture(t)
	r.live = "ACCEPT" // A permanent-only edit must retain this distinct original runtime value.
	p, e := a.network.Preview(targetRequest(false))
	if e != nil {
		t.Fatal(e)
	}
	assertTargets(t, r, "ACCEPT", "default")
	if len(p.steps) != 1 || len(p.Rollback) != 1 || p.Details["reload_required"] != false {
		t.Fatal("unexpected target plan")
	}
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), "--set-target") {
			t.Fatal("preview mutated target")
		}
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	assertTargets(t, r, "ACCEPT", "DROP")
	if p.Status != "awaiting_confirmation" {
		t.Fatal(p.Status)
	}
	if e = rollbackFile(a.network.specPath(p), r); e != nil {
		t.Fatal(e)
	}
	assertTargets(t, r, "ACCEPT", "default")
	for _, call := range f.calls {
		if strings.Join(call, " ") == "firewall-cmd --reload" {
			t.Fatal("permanent-only change reloaded")
		}
	}
}
func TestTargetImmediateRollbackRestoresTargetBeforeReload(t *testing.T) {
	a, f, r := targetFixture(t)
	p, e := a.network.Preview(targetRequest(true))
	if e != nil {
		t.Fatal(e)
	}
	if len(p.steps) != 2 || len(p.undo) != 3 || len(p.undoEnds) != 2 {
		t.Fatal("reload rollback grouping absent")
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	assertTargets(t, r, "DROP", "DROP")
	start := len(f.calls)
	if e = rollbackFile(a.network.specPath(p), r); e != nil {
		t.Fatal(e)
	}
	assertTargets(t, r, "default", "default")
	undo := f.calls[start:]
	if len(undo) != 3 || !strings.Contains(strings.Join(undo[0], " "), "--set-target=default") || strings.Join(undo[1], " ") != "firewall-cmd --reload" {
		t.Fatal("old target not restored before rollback reload", undo)
	}
}
func TestTargetImmediatePartialReloadFailureRestoresBothScopes(t *testing.T) {
	a, _, r := targetFixture(t)
	p, e := a.network.Preview(targetRequest(true))
	if e != nil {
		t.Fatal(e)
	}
	r.failReloadOnce = true
	if a.network.Apply(p.ID) == nil {
		t.Fatal("reload failure hidden")
	}
	assertTargets(t, r, "default", "default")
	if p.Status != "rolled_back" {
		t.Fatal(p.Status)
	}
}
func TestTargetSetterFailureCannotReloadOrLoseOriginal(t *testing.T) {
	a, f, r := targetFixture(t)
	p, e := a.network.Preview(targetRequest(true))
	if e != nil {
		t.Fatal(e)
	}
	f.fail = "--set-target=DROP"
	if a.network.Apply(p.ID) == nil {
		t.Fatal("setter failure hidden")
	}
	assertTargets(t, r, "default", "default")
	for _, call := range f.calls {
		if strings.Join(call, " ") == "firewall-cmd --reload" {
			t.Fatal("reload ran after first setter failed")
		}
	}
}
func TestTargetConfirmedChangeCannotBeRolledBack(t *testing.T) {
	a, _, r := targetFixture(t)
	p, e := a.network.Preview(targetRequest(true))
	if e != nil {
		t.Fatal(e)
	}
	if e = a.network.Apply(p.ID); e != nil {
		t.Fatal(e)
	}
	if e = a.network.Confirm(p.ID); e != nil {
		t.Fatal(e)
	}
	if e = rollbackFile(a.network.specPath(p), r); e != nil {
		t.Fatal(e)
	}
	assertTargets(t, r, "DROP", "DROP")
}
func TestTargetRuntimeDriftAndStaleGlobalConfigurationBlockReload(t *testing.T) {
	a, f, r := targetFixture(t)
	r.drift = true
	if _, e := a.network.Preview(targetRequest(true)); e == nil {
		t.Fatal("runtime-only rules accepted")
	}
	r.drift = false
	p, e := a.network.Preview(targetRequest(true))
	if e != nil {
		t.Fatal(e)
	}
	r.drift = true
	start := len(f.calls)
	if a.network.Apply(p.ID) == nil {
		t.Fatal("stale global config accepted")
	}
	for _, call := range f.calls[start:] {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "--set-target=") || strings.Contains(joined, "systemd-run") {
			t.Fatal("stale plan armed or applied")
		}
	}
}
func TestTargetInputAcknowledgementsAndWriteVerification(t *testing.T) {
	a, _, r := targetFixture(t)
	for _, change := range []M{{"target": "bad"}, {"target": "DROP --reload"}, {"target": 42}, {"target_ack": false}, {"target_apply": "unknown"}} {
		d := targetRequest(false)
		for k, v := range change {
			obj(d["item"])[k] = v
		}
		if _, e := a.network.Preview(d); e == nil {
			t.Fatal("invalid target request accepted", change)
		}
	}
	d := targetRequest(true)
	delete(obj(d["item"]), "reload_ack")
	if _, e := a.network.Preview(d); e == nil {
		t.Fatal("reload acknowledgement missing")
	}
	p, e := a.network.Preview(targetRequest(false))
	if e != nil {
		t.Fatal(e)
	}
	r.ignoreSetter = true
	if a.network.Apply(p.ID) == nil {
		t.Fatal("unverified target setter accepted")
	}
	if p.Status != "rolled_back" {
		t.Fatal(p.Status)
	}
}
func TestReloadGuardPreservesRuleOrderAndZoneMembership(t *testing.T) {
	a := "public (active)\n  target: default\n  rule source address=192.0.2.1 accept\n  rule source address=192.0.2.1 drop\nwork\n  target: ACCEPT\n"
	b := strings.Replace(a, " (active)", "", 1)
	if canonicalFirewallObjects(a) != canonicalFirewallObjects(b) {
		t.Fatal("display active flag caused mismatch")
	}
	b = strings.Replace(b, "  rule source address=192.0.2.1 accept\n  rule source address=192.0.2.1 drop", "  rule source address=192.0.2.1 drop\n  rule source address=192.0.2.1 accept", 1)
	if canonicalFirewallObjects(a) == canonicalFirewallObjects(b) {
		t.Fatal("rule ordering lost")
	}
	if canonicalZoneTarget("%%REJECT%%") != "REJECT" || validZoneTarget("%%REJECT%%") {
		t.Fatal("reject normalization")
	}
}
