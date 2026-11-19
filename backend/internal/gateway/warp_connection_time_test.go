package gateway

import "testing"

func TestWarpObservedConnectionTime(t *testing.T) {
	a := &App{}
	first := M{"connected": true, "endpoint": "192.0.2.1"}
	a.noteWarpConnection(first)
	if num(first["connected_since"]) <= 0 || first["connection_time_source"] != "observed" {
		t.Fatal("connection observation missing")
	}
	a.warpConnectedSince = 100
	repeated := M{"connected": true, "endpoint": "192.0.2.1"}
	a.noteWarpConnection(repeated)
	if repeated["connected_since"] != float64(100) {
		t.Fatal("poll reset duration")
	}
	unknown := M{"connected": true}
	a.noteWarpConnection(unknown)
	if unknown["connected_since"] != float64(100) {
		t.Fatal("missing endpoint reset duration")
	}
	changed := M{"connected": true, "endpoint": "192.0.2.2"}
	a.noteWarpConnection(changed)
	if num(changed["connected_since"]) <= 100 {
		t.Fatal("changed endpoint retained duration")
	}
	a.noteWarpConnection(M{"connected": false})
	if a.warpConnectedSince != 0 {
		t.Fatal("disconnection did not clear time")
	}
	reconnected := M{"connected": true, "endpoint": "192.0.2.2"}
	a.noteWarpConnection(reconnected)
	if num(reconnected["connected_since"]) <= 0 {
		t.Fatal("reconnect time missing")
	}
}
