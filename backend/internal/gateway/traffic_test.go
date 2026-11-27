package gateway

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type trafficTransport func(*http.Request) (*http.Response, error)

func (f trafficTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTrafficResetKeepsTotalsAndFailureDoesNotBecomeZeroSample(t *testing.T) {
	a, _ := fixture(t)
	obj(a.monitorData["defaults"])["interval"] = 2
	a.components["warp"] = false
	a.native["nodes"] = []any{M{"id": "n-test", "name": "test", "enabled": true}}
	_ = writeJSON(filepath.Join(a.base, "private", "traffic-policy.json"), M{"enabled": true})
	a.traffic.client.Transport = trafficTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"stats":{"outbound":{"out-n-test":{"uplink":100,"downlink":200}},"user":{"ignored":{"uplink":999}}}}`))}, nil
	})
	a.traffic.rows["n-test"] = &trafficReading{ID: "n-test", TotalUp: 50, TotalDown: 60, previous: trafficCounter{Up: 1000, Down: 2000, Available: true}, previousAt: time.Now().Add(-2 * time.Second)}
	a.traffic.collect()
	r := a.traffic.rows["n-test"]
	if r.TotalUp != 150 || r.TotalDown != 260 || !r.Available || r.UploadRate <= 0 {
		t.Fatal("counter reset lost totals")
	}
	if e := a.traffic.flushLocked(); e != nil {
		t.Fatal(e)
	}
	var before int
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples").Scan(&before)
	a.traffic.client.Transport = trafficTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("test unavailable") })
	a.traffic.collect()
	if r.Available || r.TotalUp != 150 || r.pendingSeconds != 0 {
		t.Fatal("failure changed measured totals")
	}
	_ = a.traffic.flushLocked()
	var after int
	_ = a.histdb.QueryRow("SELECT COUNT(*) FROM traffic_samples").Scan(&after)
	if after != before {
		t.Fatal("failed collection generated zero history")
	}
}
func TestTrafficConfigurationIsOptInAndLoopbackOnly(t *testing.T) {
	a, _ := fixture(t)
	a.cfg["endpoints"] = []any{}
	engine, e := a.buildEngine(a.cfg, a.native, "direct")
	if e != nil {
		t.Fatal(e)
	}
	if engine["metrics"] != nil {
		t.Fatal("stats changed migration before opt-in")
	}
	_ = writeJSON(filepath.Join(a.base, "private", "traffic-policy.json"), M{"enabled": true})
	engine, e = a.buildEngine(a.cfg, a.native, "direct")
	if e != nil || str(obj(engine["metrics"])["listen"]) != "127.0.0.1:10086" {
		t.Fatal("statistics not restricted to loopback")
	}
}
