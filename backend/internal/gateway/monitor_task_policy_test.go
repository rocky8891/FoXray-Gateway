package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"
)

func TestRetiredPartitionDoesNotChangeOtherMonitorChoices(t *testing.T) {
	a, _ := fixture(t)
	p := clone(obj(defaultMonitorItems()["host"]))
	p["cpu"] = false
	p["partition"] = true
	p["layout"] = "single"
	p["order"] = []any{"partition", "connections", "memory", "cpu", "load", "disk", "network"}
	if err := a.saveMonitorItems("host", p); err != nil {
		t.Fatal(err)
	}
	got := obj(a.monitorItemsSnapshot()["host"])
	if got["partition"] != nil || got["cpu"] != false || got["layout"] != "single" || len(arr(got["order"])) != 6 {
		t.Fatal("retired item lost unrelated settings")
	}
	if got["order"].([]any)[0] != "connections" {
		t.Fatal("relative order changed")
	}
}
func TestPeriodicNodeTasksDoNotPersistWhileManualChecksDo(t *testing.T) {
	a, _ := fixture(t)
	periodic, err := a.submitAutomatic("周期节点检测", "probe.latency", func(_ context.Context, _ *Job) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	manual, err := a.submit("手动节点检测", "probe.latency", func(_ context.Context, _ *Job) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(a.jobs.gate) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	found := false
	for _, r := range a.taskHistory("")["items"].([]M) {
		if str(r["id"]) == periodic {
			t.Fatal("periodic job polluted history")
		}
		found = found || str(r["id"]) == manual
	}
	if !found {
		t.Fatal("manual check was hidden")
	}
}
func TestTaskCenterFiltersPeriodicRowsBeforePagination(t *testing.T) {
	a, _ := fixture(t)
	for i := 0; i < 120; i++ {
		source, group := "automatic", "probe.latency"
		if i < 55 {
			source = "manual"
		}
		d, _ := json.Marshal(M{"job_id": fmt.Sprintf("job-%03d", i), "source": source, "title": "任务"})
		if _, err := a.logdb.Exec("INSERT INTO operations VALUES(?,?,?,?,?,?,?)", fmt.Sprintf("row-%03d", i), float64(i), "info", group, "success", "完成", string(d)); err != nil {
			t.Fatal(err)
		}
	}
	first := a.taskCenterHistory("")
	items := first["items"].([]M)
	if len(items) != 50 || str(items[0]["id"]) != "job-054" {
		t.Fatal("periodic rows consumed a page")
	}
	second := a.taskCenterHistory(str(first["next"]))["items"].([]M)
	if len(second) != 5 || str(second[4]["id"]) != "job-000" {
		t.Fatal("pagination lost manual history")
	}
}
func TestTrafficWindowUsesByteWeightedRateAndKeepsTotals(t *testing.T) {
	a, _ := fixture(t)
	a.traffic.saveHistoryPolicy("xray", M{"storage_interval": 1})
	base := float64(int64(now())/60*60 - 120)
	for i, r := range []struct{ bytes, seconds float64 }{{100, 1}, {900, 3}} {
		if _, err := a.histdb.Exec("INSERT INTO traffic_samples VALUES(?,?,?,?,?)", "weighted", base+10+float64(i)*10, 0, r.bytes, r.seconds); err != nil {
			t.Fatal(err)
		}
	}
	q := url.Values{"node": {"weighted"}, "module": {"xray"}, "start": {fmt.Sprint(base)}, "end": {fmt.Sprint(base + 60)}, "window": {"60"}}
	result := a.traffic.history(q)
	rows := result["items"].([]M)
	if len(rows) != 1 || num(rows[0]["download"]) != 1000 || num(rows[0]["download_bps"]) != 250 {
		t.Fatal("display aggregation changed totals or used unweighted averaging")
	}
	q.Set("window", "1")
	fine := a.traffic.history(q)["items"].([]M)
	if len(fine) != 2 || num(fine[0]["download"])+num(fine[1]["download"]) != 1000 {
		t.Fatal("fine window lost original bytes")
	}
}
