package gateway

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHostLayoutPolicyValidatesAndPreservesOtherModules(t *testing.T) {
	a, _ := fixture(t)
	previous := clone(obj(a.monitoringItems["xray"]))
	for _, layout := range []string{"single", "double"} {
		p := clone(obj(defaultMonitorItems()["host"]))
		p["layout"] = layout
		p["order"] = []any{"connections", "disk", "network", "partition", "memory", "cpu", "load"}
		if err := a.saveMonitorItems("host", p); err != nil {
			t.Fatal(err)
		}
		if str(obj(a.monitorItemsSnapshot()["host"])["layout"]) != layout {
			t.Fatal("layout not saved")
		}
	}
	p := clone(obj(defaultMonitorItems()["host"]))
	p["layout"] = "top"
	if a.saveMonitorItems("host", p) == nil {
		t.Fatal("invalid layout accepted")
	}
	if len(obj(a.monitorItemsSnapshot()["xray"])) != len(previous) {
		t.Fatal("another module changed")
	}
}
func TestHistoryCompressionNegotiationAndExactPayload(t *testing.T) {
	data := M{"items": strings.Repeat("sample", 2000), "complete": true}
	expected, _ := json.Marshal(data)
	for _, encoding := range []string{"gzip", "br, gzip;q=0.5", "gzip;q=0", "br", "gzip;q=invalid", ""} {
		r := httptest.NewRequest("GET", "/api/traffic/history", nil)
		r.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		writeHistoryResponse(w, r, data)
		raw := w.Body.Bytes()
		if w.Header().Get("Content-Encoding") == "gzip" {
			g, err := gzip.NewReader(strings.NewReader(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			raw, err = io.ReadAll(g)
			if err != nil {
				t.Fatal(err)
			}
			g.Close()
			if len(w.Body.Bytes()) >= len(expected)/3 {
				t.Fatal("history compression ineffective")
			}
		} else if encoding == "gzip" || encoding == "br, gzip;q=0.5" {
			t.Fatal("gzip not negotiated")
		}
		if string(raw) != string(expected) {
			t.Fatal("history payload changed")
		}
	}
}
func TestTrafficCanceledHistoryIsIncomplete(t *testing.T) {
	a, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value := a.traffic.historyContext(ctx, url.Values{"module": {"xray"}, "hours": {"1"}})
	if yes(value["complete"]) {
		t.Fatal("canceled query reported complete")
	}
}
func TestSocketMetadataNormalizesMappedIPv6WithoutGuessingApplication(t *testing.T) {
	rows := []M{{"protocol": "tcp", "source": "192.0.2.1:1234", "destination": "198.51.100.2:443"}, {"protocol": "tcp", "source": "192.0.2.1:1235", "destination": "198.51.100.2:443"}}
	enrichConnectionMetadata(rows, `tcp ESTAB 0 0 [::ffff:192.0.2.1]:1234 [::ffff:198.51.100.2]:443 users:(("curl",pid=10,fd=2))`, nil)
	if str(rows[0]["application"]) != "curl" || str(rows[1]["application"]) != "" {
		t.Fatal("exact local process matching failed")
	}
	if str(rows[0]["domain"]) != "" {
		t.Fatal("fabricated domain")
	}
}

func TestTrafficHistoryUsesCoveringIndexAndPreservesTotals(t *testing.T) {
	a, _ := fixture(t)
	stamp := now() - 10
	if _, err := a.histdb.Exec("INSERT INTO traffic_samples(node,t,up,down,seconds) VALUES(?,?,?,?,?)", "n-cover", stamp, 123, 456, 2); err != nil {
		t.Fatal(err)
	}
	plan, err := a.histdb.Query("EXPLAIN QUERY PLAN SELECT node,CAST(t/2 AS INTEGER),MIN(t),SUM(up),SUM(down),SUM(seconds) FROM traffic_samples WHERE t>=? AND t<=? AND node!='warp' AND node NOT LIKE 'wg:%' GROUP BY node,2 ORDER BY node,2", stamp-1, stamp+1)
	if err != nil {
		t.Fatal(err)
	}
	covered := false
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		covered = covered || strings.Contains(detail, "COVERING INDEX traffic_history_cover")
	}
	plan.Close()
	if !covered {
		t.Fatal("history query requires table lookups")
	}
	result := a.traffic.history(url.Values{"module": {"xray"}, "hours": {"1"}})
	var up, down float64
	for _, r := range result["items"].([]M) {
		up += num(r["upload"])
		down += num(r["download"])
	}
	if up != 123 || down != 456 || !yes(result["complete"]) {
		t.Fatal("aggregation changed byte totals")
	}
}
