package gateway

// Called while warpMu is held. This is an observed connection duration,
// not the operating system service lifetime or an inferred historic start.
func (a *App) noteWarpConnection(m M) {
	if !yes(m["connected"]) {
		a.warpConnectedSince = 0
		a.warpConnectionKey = ""
		return
	}
	key := str(first(m["endpoint_address"], first(m["endpoint"], m["override"])))
	if a.warpConnectedSince == 0 || (key != "" && a.warpConnectionKey != "" && key != a.warpConnectionKey) {
		a.warpConnectedSince = now()
	}
	if key != "" {
		a.warpConnectionKey = key
	}
	m["connected_since"] = a.warpConnectedSince
	m["connection_time_source"] = "observed"
}
