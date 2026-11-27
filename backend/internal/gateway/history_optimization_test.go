package gateway

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestHistoryPageSearchAndStableCursor(t *testing.T) {
	app, _ := fixture(t)
	stamp := now()
	app.connections.saveBusinessHistory(nil, stamp)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := app.histdb.Exec("INSERT INTO business_flows VALUES(?,?,?,?,?)", id, "xray", stamp-5, stamp, `{"source":"needle","protocol":"tcp","extra":"detail only"}`); err != nil {
			t.Fatal(err)
		}
	}
	q := url.Values{"module": {"xray"}, "limit": {"2"}, "query": {"needle"}}
	first := app.connections.historicalConnectionPage(context.Background(), q, stamp-10, stamp+10)
	items := first["items"].([]M)
	if len(items) != 2 || items[0]["id"] != "c" || items[1]["id"] != "b" || items[0]["extra"] != nil || first["next_cursor"] == "" {
		t.Fatalf("unexpected first page: %v", first)
	}
	q.Set("cursor", str(first["next_cursor"]))
	second := app.connections.historicalConnectionPage(context.Background(), q, stamp-10, stamp+10)
	items = second["items"].([]M)
	if len(items) != 1 || items[0]["id"] != "a" || second["next_cursor"] != "" {
		t.Fatalf("unexpected second page: %v", second)
	}
	if obj(app.connections.historicalConnectionDetail(context.Background(), "xray", "a")["item"])["extra"] != "detail only" {
		t.Fatal("missing full detail")
	}
}

func TestHistoryTrafficPeakAndAverageHaveDistinctSemantics(t *testing.T) {
	app, _ := fixture(t)
	stamp := (float64(int64(now())/60) * 60) - 120
	for i, peak := range []int{25, 400} {
		if _, err := app.histdb.Exec("INSERT INTO traffic_samples(node,t,up,down,seconds,up_peak,down_peak) VALUES(?,?,?,?,?,?,?)", "peak", stamp+float64(i+1), 100, 200, 10, peak, peak); err != nil {
			t.Fatal(err)
		}
	}
	view := app.traffic.history(url.Values{"node": {"peak"}, "window": {"60"}})
	rows := view["items"].([]M)
	if len(rows) != 1 || num(rows[0]["upload_bps"]) != 10 || num(rows[0]["upload_peak_bps"]) != 400 || num(rows[0]["upload"]) != 200 {
		t.Fatalf("unexpected bucket: %v", view)
	}
}

func TestTrafficFlushPersistsObservedPeaks(t *testing.T) {
	app, _ := fixture(t)
	app.traffic.rows["test-peak"] = &trafficReading{ID: "test-peak", pendingUp: 100, pendingDown: 200, pendingSeconds: 10, pendingUploadPeak: 999, pendingDownloadPeak: 888, lastStoredAt: time.Now().Add(-time.Minute)}
	if err := app.traffic.flushLocked(); err != nil {
		t.Fatal(err)
	}
	var up, down float64
	if err := app.histdb.QueryRow("SELECT up_peak,down_peak FROM traffic_samples WHERE node='test-peak'").Scan(&up, &down); err != nil || up != 999 || down != 888 {
		t.Fatalf("peak persisted as %v/%v: %v", up, down, err)
	}
}
