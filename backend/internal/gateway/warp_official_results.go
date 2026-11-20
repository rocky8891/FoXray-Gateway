package gateway

import "path/filepath"

func (a *App) recordWarpOfficial(protocol, endpoint string, connected bool) {
	if endpoint == "" {
		return
	}
	rows := M{}
	_ = readJSON(filepath.Join(a.base, "warp-official-results.json"), &rows)
	rows[protocol+":"+endpoint] = M{"connected": connected, "checked_at": now()}
	_ = writeJSON(filepath.Join(a.base, "warp-official-results.json"), rows)
}

func officialWarpCoolingDown(row M) bool {
	value, exists := row["official_ok"].(bool)
	return exists && !value && now()-num(row["official_at"]) < 900
}
