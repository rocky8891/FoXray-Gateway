package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestXrayStartReportsSkippedServiceInTaskAndAudit(t *testing.T) {
	const statusCommand = "systemctl show foxray-engine -p ActiveState -p SubState -p ConditionResult -p Result -p ExecMainStatus"
	for _, tc := range []struct {
		name, state, condition, status, message string
		configured                              bool
	}{
		{"missing config", "", "", "error", "请先在 Xray 设置中", false},
		{"systemd skipped", "inactive", "no", "error", "systemd 跳过了服务", true},
		{"runtime failure", "failed", "yes", "error", "未进入运行状态", true},
		{"running", "active", "yes", "success", "已运行", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, runner := fixture(t)
			if tc.configured {
				if err := os.WriteFile(filepath.Join(a.base, "engine.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			runner.responses = map[string]string{
				statusCommand: "ActiveState=" + tc.state + "\nSubState=running\nConditionResult=" + tc.condition + "\nResult=success\nExecMainStatus=0\n",
			}
			if !tc.configured {
				runner.fail = "systemctl start foxray-engine"
			}
			id, err := a.submitUI(testUIAction, "服务操作 · xray", "service.start", func(ctx context.Context, j *Job) error {
				return a.serviceAction(ctx, j, "xray", "start", M{})
			})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				var auditStatus, message, uiAction string
				err := a.logdb.QueryRow("SELECT status, message, json_extract(details,'$.ui_action') FROM operations WHERE json_extract(details,'$.job_id')=?", id).Scan(&auditStatus, &message, &uiAction)
				if err == nil {
					if auditStatus != tc.status || !strings.Contains(message, tc.message) || uiAction != testUIAction {
						t.Fatalf("unexpected audit: %s %s %s", auditStatus, message, uiAction)
					}
					a.jobs.mu.RLock()
					defer a.jobs.mu.RUnlock()
					for _, task := range a.jobs.items {
						if task.ID == id {
							if task.Status != tc.status || !strings.Contains(task.Message, tc.message) || len(task.Details) == 0 {
								t.Fatalf("unexpected task: %+v", task)
							}
							return
						}
					}
					t.Fatal("task missing")
				}
				if time.Now().After(deadline) {
					t.Fatal(errors.New("task audit missing"), err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
