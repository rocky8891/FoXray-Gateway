package gateway

import "path/filepath"

func (a *App) loadExitRuntime() {
	a.activeSince = 0
	record := M{}
	if readJSON(filepath.Join(a.base, "private", "exit-runtime.json"), &record) == nil && str(record["active"]) == a.active && num(record["since"]) > 0 {
		a.activeSince = num(record["since"])
		return
	}
	rows := []M{}
	_ = readJSON(filepath.Join(a.base, "switch-history.json"), &rows)
	for _, row := range rows {
		if str(row["target"]) == a.active && str(row["source"]) != a.active && str(row["status"]) == "success" && num(row["time"]) > a.activeSince {
			a.activeSince = num(row["time"])
		}
	}
	if a.activeSince > 0 && !a.readonly {
		_ = writeJSON(filepath.Join(a.base, "private", "exit-runtime.json"), M{"active": a.active, "since": a.activeSince})
	}
}
func (a *App) noteExitSwitch(source, target string) {
	if source == target || a.readonly {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != target {
		return
	}
	a.activeSince = now()
	_ = writeJSON(filepath.Join(a.base, "private", "exit-runtime.json"), M{"active": target, "since": a.activeSince})
}
