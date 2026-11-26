package gateway

import (
	"strings"
	"testing"
)

func TestWireGuardServiceErrorKeepsSafeSystemdReason(t *testing.T) {
	app, _ := fixture(t)
	const unit = "wg-quick@wg-test"
	const command = "systemctl enable " + unit
	err := app.wireGuardServiceStepError(command, unit, &commandFailure{
		name:   "systemctl",
		stderr: "unstructured fixture output\nFailed to enable unit: Unit file " + unit + ".service is masked.",
	})
	if !strings.Contains(err.Error(), command) || !strings.Contains(err.Error(), "is masked") || strings.Contains(err.Error(), "unstructured") {
		t.Fatal("expected the command and safe systemd error only")
	}
	err = app.wireGuardServiceStepError(command, unit, &commandFailure{name: "systemctl", stderr: "unstructured fixture output"})
	if !strings.Contains(err.Error(), command) || strings.Contains(err.Error(), "unstructured") {
		t.Fatal("unexpected output disclosure")
	}
}
