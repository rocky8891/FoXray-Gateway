package gateway

import (
	"context"
	"testing"
	"time"
)

func TestVersionActionsPersistInOperationLogs(t *testing.T) {
	for _, group := range []string{"version.check.warp", "version.upgrade.foxray", "service.check", "service.upgrade", "service.rollback"} {
		t.Run(group, func(t *testing.T) {
			a, _ := fixture(t)
			job, err := a.submit("版本维护", group, func(_ context.Context, j *Job) error { a.stage(j, "完成"); return nil })
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			found := false
			for time.Now().Before(deadline) {
				for _, row := range a.operations("", "")["items"].([]M) {
					if str(row["action"]) == group && str(obj(row["details"])["job_id"]) == job {
						found = true
					}
				}
				if found {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !found {
				t.Fatal("version action absent from operation logs")
			}
			a.jobs.mu.Lock()
			a.jobs.items = nil
			a.jobs.mu.Unlock()
			if len(a.operations("", "")["items"].([]M)) != 1 {
				t.Fatal("operation log depends on task memory")
			}
		})
	}
}
