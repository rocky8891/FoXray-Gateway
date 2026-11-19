package gateway

import "testing"

func TestExitRuntimeSurvivesReloadAndSameNodeConfiguration(t *testing.T) {
	a, _ := fixture(t)
	a.readonly = false
	a.active = "n1"
	a.noteExitSwitch("n0", "n1")
	since := a.activeSince
	if since <= 0 {
		t.Fatal("switch time missing")
	}
	a.noteExitSwitch("n1", "n1")
	if a.activeSince != since {
		t.Fatal("same-node configuration reset uptime")
	}
	a.activeSince = 0
	a.loadExitRuntime()
	if a.activeSince != since {
		t.Fatal("reload reset uptime")
	}
	a.active = "n2"
	a.activeSince = 0
	a.loadExitRuntime()
	if a.activeSince != 0 {
		t.Fatal("unknown node time invented")
	}
}
