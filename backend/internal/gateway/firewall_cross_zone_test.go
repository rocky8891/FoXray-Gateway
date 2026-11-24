package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type cancelledInventoryRunner struct {
	Runner
	started chan struct{}
}

func (r *cancelledInventoryRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	if args[0] != "firewall-cmd" {
		return r.Runner.Run(ctx, args, input)
	}
	r.started <- struct{}{}
	<-ctx.Done()
	return "", ctx.Err()
}
func TestSwitchingAwayCancelsZoneInventoryProcesses(t *testing.T) {
	a, f := fixture(t)
	r := &cancelledInventoryRunner{Runner: f, started: make(chan struct{}, 10)}
	a.runner = r
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan M, 1)
	go func() { done <- a.network.firewallSnapshotWithState("public", M{"state": "active"}, ctx) }()
	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("inventory did not start")
	}
	cancel()
	select {
	case result := <-done:
		if yes(result["complete"]) {
			t.Fatal("cancelled inventory shown as complete")
		}
	case <-time.After(time.Second):
		t.Fatal("hidden inventory kept subprocesses running")
	}
}

// Model both scopes, including a command that mutates and then reports failure.
// Rollback runs through the durable file, independent of the in-memory planner.
type crossRunner struct {
	Runner
	mu                  sync.Mutex
	live, saved         map[string]crossZonePolicy
	zoneLive, zoneSaved map[string][]string
	mutations, failAt   int
	ignore              bool
}

func copyCrossPolicies(source map[string]crossZonePolicy) map[string]crossZonePolicy {
	out := map[string]crossZonePolicy{}
	for name, p := range source {
		p.Ingress = slices.Clone(p.Ingress)
		p.Egress = slices.Clone(p.Egress)
		p.Rules = slices.Clone(p.Rules)
		out[name] = p
	}
	return out
}
func crossPolicyText(source map[string]crossZonePolicy) string {
	names := []string{}
	for name := range source {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, name := range names {
		p := source[name]
		fmt.Fprintf(&out, "%s\n  priority: %d\n  target: %s\n  ingress-zones: %s\n  egress-zones: %s\n  rich rules:\n", name, p.Priority, p.Target, strings.Join(p.Ingress, " "), strings.Join(p.Egress, " "))
		for _, rule := range p.Rules {
			fmt.Fprintf(&out, "    %s\n", rule)
		}
	}
	return out.String()
}
func (r *crossRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	out, err := r.Runner.Run(ctx, args, input)
	if err != nil {
		return out, err
	}
	if args[0] != "firewall-cmd" {
		return out, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	permanent := slices.Contains(args, "--permanent")
	target := r.live
	if permanent {
		target = r.saved
	}
	if slices.Contains(args, "--list-all-policies") {
		return crossPolicyText(target), nil
	}
	zones := r.zoneLive
	if permanent {
		zones = r.zoneSaved
	}
	if zones != nil && slices.Contains(args, "--list-all-zones") {
		names := []string{}
		for name := range zones {
			names = append(names, name)
		}
		sort.Strings(names)
		var output strings.Builder
		for _, name := range names {
			fmt.Fprintf(&output, "%s\n  target: default\n  rich rules:\n", name)
			for _, rule := range zones[name] {
				fmt.Fprintf(&output, "    %s\n", rule)
			}
		}
		return output.String(), nil
	}
	mutate := false
	if slices.Contains(args, "--reload") {
		mutate = true
		if !r.ignore {
			r.live = copyCrossPolicies(r.saved)
			if r.zoneSaved != nil {
				r.zoneLive = clone(r.zoneSaved)
			}
		}
	}
	zone := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "--zone=") {
			zone = strings.TrimPrefix(arg, "--zone=")
		}
	}
	if zone != "" && zones != nil {
		for _, arg := range args {
			if rule, ok := strings.CutPrefix(arg, "--add-rich-rule="); ok {
				mutate = true
				if !r.ignore && !slices.Contains(zones[zone], rule) {
					zones[zone] = append(zones[zone], rule)
					sort.Strings(zones[zone])
				}
			}
			if rule, ok := strings.CutPrefix(arg, "--remove-rich-rule="); ok {
				mutate = true
				if !r.ignore {
					zones[zone] = slices.DeleteFunc(zones[zone], func(v string) bool { return v == rule })
					if len(zones[zone]) == 0 {
						zones[zone] = nil
					}
				}
			}
		}
	}
	name := ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "--policy=") {
			name = strings.TrimPrefix(arg, "--policy=")
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--new-policy=") {
			mutate = true
			if !r.ignore {
				target[strings.TrimPrefix(arg, "--new-policy=")] = crossZonePolicy{Priority: -1, Target: "CONTINUE"}
			}
		}
		if strings.HasPrefix(arg, "--delete-policy=") {
			mutate = true
			if !r.ignore {
				delete(target, strings.TrimPrefix(arg, "--delete-policy="))
			}
		}
		if name == "" {
			continue
		}
		p := target[name]
		if value, ok := strings.CutPrefix(arg, "--set-priority="); ok {
			mutate = true
			if !r.ignore {
				fmt.Sscanf(value, "%d", &p.Priority)
				target[name] = p
			}
		}
		for _, field := range []struct {
			key    string
			values *[]string
		}{{"ingress-zone", &p.Ingress}, {"egress-zone", &p.Egress}, {"rich-rule", &p.Rules}} {
			for _, operation := range []string{"add", "remove"} {
				prefix := "--" + operation + "-" + field.key + "="
				if !strings.HasPrefix(arg, prefix) {
					continue
				}
				mutate = true
				if r.ignore {
					continue
				}
				value := strings.TrimPrefix(arg, prefix)
				if operation == "add" {
					if !slices.Contains(*field.values, value) {
						*field.values = append(*field.values, value)
					}
				} else {
					*field.values = slices.DeleteFunc(*field.values, func(v string) bool { return v == value })
				}
				if len(*field.values) == 0 {
					*field.values = nil
				}
				sort.Strings(*field.values)
			}
		}
		if mutate && !r.ignore {
			target[name] = p
		}
	}
	if mutate {
		r.mutations++
		if r.failAt == r.mutations {
			return "", errors.New("synthetic partial command failure")
		}
	}
	return out, nil
}
func crossFixture(t *testing.T) (*App, *fakeRunner, *crossRunner) {
	a, f := fixture(t)
	f.responses = map[string]string{"firewall-cmd --get-zones": "public home-vpn work", "firewall-cmd --list-all-zones": "public\n  target: default\nhome-vpn\n  target: default\n", "firewall-cmd --permanent --list-all-zones": "public\n  target: default\nhome-vpn\n  target: default\n"}
	r := &crossRunner{Runner: f, live: map[string]crossZonePolicy{}, saved: map[string]crossZonePolicy{}}
	a.runner = r
	return a, f, r
}
func crossRequest(immediate bool) M {
	mode := "permanent"
	if immediate {
		mode = "immediate"
	}
	return M{"kind": "firewall", "operation": "add", "allow_protected": true, "item": M{"kind": "cross-zone", "driver": "firewalld", "name": "LAN to VPN", "ingress_zone": "public", "egress_zone": "home-vpn", "source": "192.0.2.10", "destination": "10.24.255.3", "protocol": "icmp", "enabled": true, "action": "accept", "apply_mode": mode, "reload_ack": true}}
}
func TestCrossZoneValidation(t *testing.T) {
	for _, bad := range []M{{"source": ""}, {"destination": "bad"}, {"destination": "::1"}, {"ingress_zone": "ANY"}, {"egress_zone": "public"}, {"action": "ACCEPT"}, {"protocol": "shell"}, {"enabled": "yes"}, {"id": "short"}, {"protocol": "tcp", "port": "65536"}} {
		d := crossRequest(false)
		for key, value := range bad {
			obj(d["item"])[key] = value
		}
		if _, err := normalizeCrossZone(obj(d["item"])); err == nil {
			t.Fatal("accepted invalid input", bad)
		}
	}
	d := obj(crossRequest(false)["item"])
	d["protocol"] = "tcpudp"
	d["port"] = "80-81"
	item, err := normalizeCrossZone(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(crossZoneRules(item)) != 2 || str(item["source"]) != "192.0.2.10/32" || str(item["zone"]) != "" {
		t.Fatal(item)
	}
	d["source"] = "2001:db8::1"
	d["destination"] = "2001:db8::2"
	d["protocol"] = "icmp"
	item, err = normalizeCrossZone(d)
	if err != nil || !strings.Contains(crossZoneRules(item)[0], "ipv6-icmp") {
		t.Fatal(item, err)
	}
}
func TestCrossZonePermanentAndImmediateDurableRollback(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		t.Run(fmt.Sprint(immediate), func(t *testing.T) {
			a, f, r := crossFixture(t)
			request := crossRequest(immediate)
			obj(request["item"])["priority"] = -15
			p, err := a.network.Preview(request)
			if err != nil {
				t.Fatal(err)
			}
			if r.mutations != 0 {
				t.Fatal("preview mutated firewall")
			}
			if err = a.network.Apply(p.ID); err != nil {
				t.Fatal(err)
			}
			if len(r.saved) != 1 || len(r.live) != map[bool]int{false: 0, true: 1}[immediate] {
				t.Fatal("scope mismatch")
			}
			if err = rollbackFile(a.network.specPath(p), r); err != nil {
				t.Fatal(err)
			}
			if len(r.saved) != 0 || len(r.live) != 0 {
				t.Fatal("rollback retained new policy")
			}
			reloads := 0
			for _, call := range f.calls {
				if slices.Contains(call, "--reload") {
					reloads++
				}
			}
			if !immediate && reloads != 0 || immediate && reloads != 2 {
				t.Fatal("unexpected reload", reloads)
			}
		})
	}
}
func TestCrossZonePartialFailuresRollbackEveryStage(t *testing.T) {
	for failAt := 1; failAt <= 5; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			a, _, r := crossFixture(t)
			p, err := a.network.Preview(crossRequest(true))
			if err != nil {
				t.Fatal(err)
			}
			r.failAt = failAt
			if a.network.Apply(p.ID) == nil {
				t.Fatal("failure was ignored")
			}
			if len(r.saved) != 0 || len(r.live) != 0 || p.Status != "rolled_back" {
				t.Fatal("partial apply not restored", p.Status, r.saved, r.live)
			}
		})
	}
}
func TestCrossZoneDeleteDisableAndExternalDrift(t *testing.T) {
	for _, operation := range []string{"delete", "disable", "rename"} {
		t.Run(operation, func(t *testing.T) {
			a, _, r := crossFixture(t)
			p, err := a.network.Preview(crossRequest(true))
			if err != nil {
				t.Fatal(err)
			}
			if err = a.network.Apply(p.ID); err != nil {
				t.Fatal(err)
			}
			if err = a.network.Confirm(p.ID); err != nil {
				t.Fatal(err)
			}
			oldLive, oldSaved := copyCrossPolicies(r.live), copyCrossPolicies(r.saved)
			item := clone(obj(p.persist["item"]))
			op := "replace"
			if operation == "delete" {
				op = "delete"
			} else if operation == "disable" {
				item["enabled"] = false
			} else {
				item["name"] = "renamed"
			}
			item["apply_mode"] = "immediate"
			next, err := a.network.Preview(M{"kind": "firewall", "operation": op, "allow_protected": true, "item": item})
			if err != nil {
				t.Fatal(err)
			}
			if err = a.network.Apply(next.ID); err != nil {
				t.Fatal(err)
			}
			if err = rollbackFile(a.network.specPath(next), r); err != nil {
				t.Fatal(err)
			}
			if fingerprint(r.live) != fingerprint(oldLive) || fingerprint(r.saved) != fingerprint(oldSaved) {
				t.Fatal("previous scopes not restored")
			}
			for name, policy := range r.live {
				policy.Rules = append(policy.Rules, "rule family=\"ipv4\" source address=\"198.51.100.0/24\" accept")
				r.live[name] = policy
			}
			if _, err = a.network.Preview(M{"kind": "firewall", "operation": "replace", "allow_protected": true, "item": item}); err == nil {
				t.Fatal("external runtime edit overwritten")
			}
		})
	}
}
func TestCrossZoneSnapshotReadOnlyAndReloadGuards(t *testing.T) {
	a, f, r := crossFixture(t)
	r.saved["external"] = crossZonePolicy{Priority: -1, Target: "ACCEPT", Ingress: []string{"public"}, Egress: []string{"home-vpn"}}
	snapshot := a.network.CrossZoneSnapshot()
	rows := snapshot["items"].([]M)
	if !yes(snapshot["complete"]) || len(rows) != 1 || yes(obj(rows[0])["managed"]) || yes(obj(rows[0])["runtime"]) {
		t.Fatal(snapshot)
	}
	if _, err := a.network.Preview(crossRequest(true)); err == nil {
		t.Fatal("runtime/permanent drift permitted reload")
	}
	r.saved = map[string]crossZonePolicy{}
	d := crossRequest(true)
	obj(d["item"])["reload_ack"] = false
	if _, err := a.network.Preview(d); err == nil {
		t.Fatal("missing reload acknowledgement")
	}
	p, err := a.network.Preview(crossRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	r.live["external"] = crossZonePolicy{Priority: -1, Target: "ACCEPT"}
	if p.precondition() == nil {
		t.Fatal("stale preview accepted")
	}
	r.live = map[string]crossZonePolicy{}
	f.fail = "--list-all-policies"
	if yes(a.network.CrossZoneSnapshot()["complete"]) {
		t.Fatal("read failure presented as empty valid inventory")
	}
}
func TestCrossZonePostconditionAndBatchGuards(t *testing.T) {
	a, _, r := crossFixture(t)
	p, err := a.network.Preview(crossRequest(false))
	if err != nil {
		t.Fatal(err)
	}
	r.ignore = true
	if a.network.Apply(p.ID) == nil {
		t.Fatal("ignored mutations accepted")
	}
	if _, err = a.network.Preview(M{"kind": "firewall", "operation": "disable", "items": []any{obj(crossRequest(false)["item"])}}); err == nil {
		t.Fatal("batch cross-zone allowed")
	}
}

func TestBatchCrossZoneAppliesAndRestoresOnlySelectedPolicies(t *testing.T) {
	a, _, runner := crossFixture(t)
	var rows []any
	var selection []any
	for _, identity := range []string{id(), id(), id()} {
		rule, err := normalizeCrossZone(obj(crossRequest(true)["item"]))
		if err != nil {
			t.Fatal(err)
		}
		rule["id"] = identity
		rows = append(rows, rule)
		name := crossZoneName(rule)
		policy := crossZonePolicy{Priority: -1, Target: "CONTINUE", Ingress: []string{"public"}, Egress: []string{"home-vpn"}, Rules: crossZoneRules(rule)}
		runner.live[name], runner.saved[name] = policy, clone(policy)
		selection = append(selection, M{"id": identity})
	}
	a.network.managed["firewall"] = rows
	request := M{"kind": "firewall", "scope": "cross-zone", "operation": "disable", "apply_mode": "immediate", "items": selection[:2], "allow_protected": true}
	plan, err := a.network.Preview(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.undoEnds) != len(plan.steps) || len(arr(plan.persist["changes"])) != 2 {
		t.Fatal("incomplete rollback or persistence plan")
	}
	if err = a.network.Apply(plan.ID); err != nil {
		t.Fatal(err)
	}
	for i, value := range rows {
		name := crossZoneName(obj(value))
		if keep := i == 2; (len(runner.live[name].Rules) > 0) != keep || (len(runner.saved[name].Rules) > 0) != keep {
			t.Fatalf("unexpected policy state %d live=%v saved=%v planned=%v", i, runner.live[name], runner.saved[name], plan.Preview)
		}
	}
	if err = rollbackFile(a.network.specPath(plan), runner); err != nil {
		t.Fatal(err)
	}
	for _, value := range rows {
		name := crossZoneName(obj(value))
		if len(runner.live[name].Rules) == 0 || len(runner.saved[name].Rules) == 0 {
			t.Fatal("policy not restored")
		}
	}
}
