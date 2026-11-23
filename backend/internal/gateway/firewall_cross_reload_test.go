package gateway

import (
	"fmt"
	"strings"
	"testing"
)

func TestCrossReloadOnlyProtectsAdditionalRuntimeRichRules(t *testing.T) {
	base := "public\n  target: default\n  interfaces: ens16 wg0\n  rich rules:\n    rule family=\"ipv4\" source address=\"192.0.2.10/32\" accept\n"
	permanent := "public\n  target: default\n  interfaces: wg0 ens16\n  rich rules:\n"
	protection, err := protectableZoneRules(base, permanent)
	if err != nil || len(protection) != 1 || protection[0].Zone != "public" {
		t.Fatal(protection, err)
	}
	for _, bad := range []string{strings.Replace(permanent, "default", "ACCEPT", 1), strings.Replace(permanent, "ens16", "ens17", 1), permanent + "work\n  target: default\n", permanent + "    rule family=ipv4 source address=198.51.100.1/32 drop\n"} {
		if _, err = protectableZoneRules(base, bad); err == nil {
			t.Fatal("unsafe drift accepted", bad)
		}
	}
	reordered := "public (active)\n  interfaces: wg0 ens16\n  target: default\n  rich rules:\n    rule family=ipv4 source address=192.0.2.10/32 accept\n"
	if reloadObjectFingerprint(base, "") != reloadObjectFingerprint(reordered, "") {
		t.Fatal("display order caused false conflict")
	}
}

func preparedSavedOnlyCrossPolicy(t *testing.T) (*App, *crossRunner, M) {
	t.Helper()
	a, _, r := crossFixture(t)
	initial, err := a.network.Preview(crossRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	if err = a.network.Apply(initial.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.network.Confirm(initial.ID); err != nil {
		t.Fatal(err)
	}
	r.zoneLive = map[string][]string{"public": {"rule family=ipv4 source address=192.0.2.0/24 destination address=10.24.255.2/32 accept", "rule family=ipv4 source address=192.0.2.0/24 destination address=10.24.255.3/32 accept"}, "home-vpn": nil}
	r.zoneSaved = map[string][]string{"public": nil, "home-vpn": nil}
	r.mutations = 0
	item := clone(obj(initial.persist["item"]))
	item["apply_mode"] = "immediate"
	item["reload_ack"] = true
	return a, r, M{"kind": "firewall", "operation": "replace", "allow_protected": true, "item": item}
}

func TestSavedOnlyCrossPolicyActivatesWithoutLosingRuntimeRules(t *testing.T) {
	a, r, request := preparedSavedOnlyCrossPolicy(t)
	oldLive, oldSaved, oldPolicies := clone(r.zoneLive), clone(r.zoneSaved), copyCrossPolicies(r.saved)
	p, err := a.network.Preview(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.steps) != 5 || len(p.undoStates) != len(p.steps) {
		t.Fatal("missing protected reload phases", p.Preview)
	}
	if err = a.network.Apply(p.ID); err != nil {
		t.Fatal(err)
	}
	if fingerprint(r.zoneLive) != fingerprint(oldLive) || fingerprint(r.zoneSaved) != fingerprint(oldSaved) {
		t.Fatal("unrelated runtime or permanent rules changed")
	}
	name := crossZoneName(obj(request["item"]))
	if fingerprint(r.live[name]) != fingerprint(r.saved[name]) {
		t.Fatal("policy not active")
	}
	if err = rollbackFile(a.network.specPath(p), r); err != nil {
		t.Fatal(err)
	}
	if fingerprint(r.zoneLive) != fingerprint(oldLive) || fingerprint(r.zoneSaved) != fingerprint(oldSaved) || fingerprint(r.saved) != fingerprint(oldPolicies) {
		t.Fatal("standalone rollback changed original scopes")
	}
	shell := r.live[name]
	if len(shell.Ingress) != 0 || len(shell.Egress) != 0 || shell.Target != "CONTINUE" {
		t.Fatal("rollback left effective forwarding policy", shell)
	}
	// The inactive shell left by rollback can be activated incrementally on retry.
	retry, err := a.network.Preview(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range retry.steps {
		if strings.Contains(strings.Join(step.Args, " "), "--reload") {
			t.Fatal("retry unnecessarily reloaded")
		}
	}
	if err = a.network.Apply(retry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedCrossReloadPartialFailureAtEveryStage(t *testing.T) {
	for stage := 1; stage <= 5; stage++ {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			a, r, request := preparedSavedOnlyCrossPolicy(t)
			oldLive, oldSaved, oldPolicies := clone(r.zoneLive), clone(r.zoneSaved), copyCrossPolicies(r.saved)
			p, err := a.network.Preview(request)
			if err != nil {
				t.Fatal(err)
			}
			r.failAt = stage
			if a.network.Apply(p.ID) == nil {
				t.Fatal("partial failure ignored")
			}
			if p.Status != "rolled_back" || fingerprint(r.zoneLive) != fingerprint(oldLive) || fingerprint(r.zoneSaved) != fingerprint(oldSaved) || fingerprint(r.saved) != fingerprint(oldPolicies) {
				t.Fatal("partial failure lost original state", p.Status)
			}
			name := crossZoneName(obj(request["item"]))
			if active, ok := r.live[name]; ok && (len(active.Ingress) > 0 || len(active.Egress) > 0) {
				t.Fatal("failed activation remained effective")
			}
		})
	}
}

func TestProtectedCrossReloadDetectsChangedRuntimeBeforeApply(t *testing.T) {
	a, r, request := preparedSavedOnlyCrossPolicy(t)
	p, err := a.network.Preview(request)
	if err != nil {
		t.Fatal(err)
	}
	r.zoneLive["public"] = append(r.zoneLive["public"], "rule family=ipv4 source address=198.51.100.0/24 drop")
	if err = p.precondition(); err == nil {
		t.Fatal("stale protection list accepted")
	}
	if r.mutations != 0 {
		t.Fatal("preview or stale check modified firewall")
	}
}

func TestBrandNewProtectedCrossPolicyRollsBackEveryStage(t *testing.T) {
	for failure := 0; failure <= 7; failure++ {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			a, _, r := crossFixture(t)
			r.zoneLive = map[string][]string{"public": {"rule family=ipv4 source address=192.0.2.10/32 accept"}, "home-vpn": nil}
			r.zoneSaved = map[string][]string{"public": nil, "home-vpn": nil}
			oldLive, oldSaved := clone(r.zoneLive), clone(r.zoneSaved)
			p, err := a.network.Preview(crossRequest(true))
			if err != nil {
				t.Fatal(err)
			}
			if len(p.steps) != 7 {
				t.Fatal("unexpected reload stages", len(p.steps))
			}
			r.failAt = failure
			err = a.network.Apply(p.ID)
			if failure == 0 {
				if err != nil {
					t.Fatal(err)
				}
				if err = rollbackFile(a.network.specPath(p), r); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("partial failure accepted")
			}
			if len(r.live) != 0 || len(r.saved) != 0 || fingerprint(r.zoneLive) != fingerprint(oldLive) || fingerprint(r.zoneSaved) != fingerprint(oldSaved) {
				t.Fatal("new policy rollback did not restore original scopes")
			}
		})
	}
}
