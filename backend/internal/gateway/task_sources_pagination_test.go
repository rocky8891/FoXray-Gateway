package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestTaskSourcesPersistInHistory(t *testing.T) {
	a, _ := fixture(t)
	manual, err := a.submit("手动检测", "probe.latency", func(_ context.Context, j *Job) error { a.stage(j, "手动详细结果"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := a.submitAutomatic("自动订阅更新", "subscription.update", func(_ context.Context, j *Job) error { a.stage(j, "自动详细结果"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	found := map[string]M{}
	for time.Now().Before(deadline) {
		for _, r := range a.taskHistory("")["items"].([]M) {
			found[str(r["id"])] = r
		}
		if len(found) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if str(found[manual]["source"]) != "manual" || str(found[automatic]["source"]) != "automatic" {
		t.Fatal("manual/automatic source not persisted")
	}
	if str(found[manual]["title"]) != "手动检测" || str(found[manual]["status"]) != "success" {
		t.Fatal("history metadata lost")
	}
	a.jobs.mu.Lock()
	a.jobs.items = nil
	a.jobs.mu.Unlock()
	if len(a.taskHistory("")["items"].([]M)) != 2 {
		t.Fatal("history depends on in-memory job list")
	}
}

func TestTaskHistoryLegacyAndEqualTimestampPagination(t *testing.T) {
	a, _ := fixture(t)
	for i := 0; i < 55; i++ {
		d, _ := json.Marshal(M{"job_id": fmt.Sprintf("job-%02d", i), "results": []string{"详情"}})
		if _, err := a.logdb.Exec("INSERT INTO operations VALUES(?,?,?,?,?,?,?)", fmt.Sprintf("row-%02d", i), 10, "info", "probe.latency", "success", "旧检测：完成", string(d)); err != nil {
			t.Fatal(err)
		}
	}
	first := a.taskHistory("")
	rows := first["items"].([]M)
	if len(rows) != 50 || str(first["next"]) == "" {
		t.Fatal("first history page incorrect")
	}
	if str(rows[0]["source"]) != "unknown" || str(rows[0]["title"]) != "旧检测" {
		t.Fatal("legacy source guessed or title lost")
	}
	second := a.taskHistory(str(first["next"]))["items"].([]M)
	if len(second) != 5 {
		t.Fatal("equal-timestamp pagination skipped history", len(second))
	}
	seen := map[string]bool{}
	for _, row := range append(rows, second...) {
		id := str(row["id"])
		if seen[id] {
			t.Fatal("duplicate task", id)
		}
		seen[id] = true
	}
}
