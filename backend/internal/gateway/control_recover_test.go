package gateway

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanceledOrFailedSnapshotDoesNotClobberVerifiedState(t *testing.T) {
	a, f := fixture(t)
	a.readonly = false
	path := filepath.Join(a.base, "private", "control-state.json")
	before := M{"engine_pid": "42", "active": "n1", "applied": true}
	if err := writeJSON(path, before); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	cancel()
	a.maintainControlState()
	after := M{}
	_ = readJSON(path, &after)
	if fingerprint(after) != fingerprint(before) {
		t.Fatal("canceled shutdown clobbered snapshot")
	}
	a.ctx = context.Background()
	f.fail = "systemctl show foxray-engine"
	a.maintainControlState()
	_ = readJSON(path, &after)
	if fingerprint(after) != fingerprint(before) {
		t.Fatal("failed PID lookup clobbered snapshot")
	}
	f.fail = ""
	f.responses = map[string]string{"systemctl show foxray-engine -p MainPID --value": "42\n"}
	a.recoveryIssue = "unverified control state"
	a.maintainControlState()
	_ = readJSON(path, &after)
	if fingerprint(after) != fingerprint(before) {
		t.Fatal("unverified recovery state clobbered snapshot")
	}
}
func TestRecoverControlOnlyAdoptsVerifiedRunningEngine(t *testing.T) {
	a, f := fixture(t)
	a.readonly = false
	a.cfg["enabled"] = true
	a.cfg["selected"] = "n1"
	a.recoveryIssue = "stale snapshot"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	engine, err := a.buildEngine(a.cfg, a.native, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if err = writeJSON(filepath.Join(a.base, "engine.json"), engine); err != nil {
		t.Fatal(err)
	}
	f.responses = map[string]string{"systemctl show foxray-engine -p MainPID --value": "42\n"}
	a.cfg["enabled"] = false
	if err = a.recoverControl(M{"confirmed": true, "engine_pid": "41"}); err == nil {
		t.Fatal("changed PID accepted")
	}
	if err = a.recoverControl(M{"confirmed": true, "engine_pid": "42"}); err != nil {
		t.Fatal(err)
	}
	if !a.applied || !yes(a.cfg["enabled"]) || a.active != "n1" || a.recoveryIssue != "" {
		t.Fatal("verified state not restored")
	}
	for _, call := range f.calls {
		line := strings.Join(call, " ")
		if strings.Contains(line, "restart") || strings.Contains(line, "nft -f") || strings.Contains(line, "ip route") {
			t.Fatal("recovery changed running system", line)
		}
	}
}
