package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testUIAction = "0123456789abcdef0123456789abcdef"

func TestUISettingsAuditUsesRequestMarkerWithoutChangingConfiguration(t *testing.T) {
	for _, marker := range []string{testUIAction, "", "not-a-valid-action"} {
		t.Run(marker, func(t *testing.T) {
			a, _ := fixture(t)
			a.readonly = false
			r := httptest.NewRequest("POST", "http://127.0.0.1/api/settings", strings.NewReader(`{"log_retention_days":30,"measurement_retention_days":30,"log_max_rows":10000}`))
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Foxray-UI-Action", marker)
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			items := a.operations("", "")["items"].([]M)
			if len(items) != 1 {
				t.Fatalf("unexpected audit count: %d", len(items))
			}
			if got := str(obj(obj(items[0])["details"])["ui_action"]); got != uiActionID(marker) {
				t.Fatalf("unexpected UI marker: %q", got)
			}
			if _, exists := a.settings["ui_action"]; exists {
				t.Fatal("request marker entered persistent configuration")
			}
			if err := a.audit("network.conflict", "background", false, nil); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := a.logdb.QueryRow("SELECT COUNT(*) FROM operations WHERE action='network.conflict' AND json_extract(details,'$.ui_action') IS NULL").Scan(&count); err != nil || count != 1 {
				t.Fatal("background audit acquired a UI marker", err)
			}
		})
	}
}

func TestUITaskAuditsRemainIsolatedFromConcurrentAutomaticTask(t *testing.T) {
	a, _ := fixture(t)
	release := make(chan struct{})
	uiID, err := a.submitUI(testUIAction, "UI operation", "ui.test", func(ctx context.Context, j *Job) error {
		<-release
		if err := a.audit("ui.inner", "UI failure", false, nil, uiActionFromContext(ctx)); err != nil {
			return err
		}
		return errors.New("synthetic UI failure")
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.submitAutomatic("background", "background.test", func(ctx context.Context, j *Job) error {
		close(release)
		return nil
	})
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var count int
		_ = a.logdb.QueryRow("SELECT COUNT(*) FROM operations WHERE action IN ('ui.test','background.test','ui.inner')").Scan(&count)
		if count == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tasks did not write expected audits")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var uiCount, automaticCount int
	_ = a.logdb.QueryRow("SELECT COUNT(*) FROM operations WHERE action IN ('ui.test','ui.inner') AND json_extract(details,'$.ui_action')=?", testUIAction).Scan(&uiCount)
	_ = a.logdb.QueryRow("SELECT COUNT(*) FROM operations WHERE action='background.test' AND json_extract(details,'$.ui_action') IS NULL").Scan(&automaticCount)
	if uiCount != 2 || automaticCount != 1 {
		t.Fatalf("incorrect UI/background attribution: %d/%d", uiCount, automaticCount)
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/tasks", nil)
	w := httptest.NewRecorder()
	a.getAPI(w, r)
	var live struct {
		Tasks []Job `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range live.Tasks {
		if task.ID == uiID && task.UIAction == testUIAction {
			found = true
		}
	}
	if !found {
		t.Fatal("live task endpoint omitted UI marker")
	}
	for _, row := range a.taskHistory("")["items"].([]M) {
		if task := obj(row); str(task["id"]) == uiID {
			if str(task["ui_action"]) != testUIAction || task["status"] != "error" {
				t.Fatal("UI marker or failure status lost in task history")
			}
			return
		}
	}
	t.Fatal("UI task missing from history")
}

func TestUIAuditDoesNotMutateCallerDetails(t *testing.T) {
	a, _ := fixture(t)
	details := M{"job_id": "test-job"}
	if err := a.audit("test.operation", "done", true, details, testUIAction); err != nil {
		t.Fatal(err)
	}
	if _, ok := details["ui_action"]; ok {
		t.Fatal("UI annotation mutated caller details")
	}
	encoded, _ := json.Marshal(&Job{ID: "test-job", UIAction: testUIAction})
	if !strings.Contains(string(encoded), `"ui_action":"`+testUIAction+`"`) {
		t.Fatal("live task response omitted UI marker")
	}
}
