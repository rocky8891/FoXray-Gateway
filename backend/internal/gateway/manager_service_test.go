package gateway

import (
	"strings"
	"testing"
)

func TestManagerServiceSchedulesSelfStopAndRestartOnly(t *testing.T) {
	a, runner := fixture(t)
	a.readonly = false
	for _, action := range []string{"stop", "restart"} {
		result, err := a.managerServiceAction(M{"action": action, "confirmed": true})
		if err != nil {
			t.Fatal(err)
		}
		command := strings.Join(runner.calls[len(runner.calls)-1], " ")
		if !yes(result["scheduled"]) || !strings.Contains(command, "systemd-run") || !strings.Contains(command, "--on-active=3s") || !strings.HasSuffix(command, "/usr/bin/systemctl "+action+" foxray.service") {
			t.Fatal("self action must be delayed and restricted", command)
		}
		if strings.Contains(command, "foxray-engine") || strings.Contains(command, "warp-svc") {
			t.Fatal("unexpected component change")
		}
	}
	for _, action := range []string{"start", "enable", "disable"} {
		if _, err := a.managerServiceAction(M{"action": action}); err != nil {
			t.Fatal(err)
		}
		if command := strings.Join(runner.calls[len(runner.calls)-1], " "); command != "systemctl "+action+" foxray.service" {
			t.Fatal("unexpected manager command", command)
		}
	}
}

func TestManagerServiceRejectsReadonlyUnknownAndUnconfirmedActions(t *testing.T) {
	a, runner := fixture(t)
	if _, err := a.managerServiceAction(M{"action": "restart", "confirmed": true}); err == nil {
		t.Fatal("readonly action accepted")
	}
	a.readonly = false
	for _, input := range []M{{"action": "stop"}, {"action": "restart"}, {"action": "reboot"}, {"action": "enable", "unit": "foxray-engine.service"}} {
		if _, err := a.managerServiceAction(input); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if len(runner.calls) != 0 {
		t.Fatal("rejected actions executed commands")
	}
	runner.fail = "systemd-run"
	if _, err := a.managerServiceAction(M{"action": "restart", "confirmed": true}); err == nil {
		t.Fatal("failed scheduling reported as success")
	}
}
