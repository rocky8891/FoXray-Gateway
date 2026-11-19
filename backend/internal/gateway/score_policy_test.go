package gateway

import (
	"path/filepath"
	"testing"
)

func TestScoreLatencyWindowAndMinimum(t *testing.T) {
	a, _ := fixture(t)
	a.cfg["endpoints"] = []any{M{"id": "n1", "enabled": true}}
	stamp := now() - 1
	a.results["n1"] = M{"time": stamp, "ok": true, "latency": 100}
	a.history["n1"] = []M{
		{"time": stamp - 2400, "ok": true, "latency": 900},
		{"time": stamp - 90, "ok": true, "latency": 200},
		{"time": stamp - 30, "ok": false, "latency": -1},
		{"time": stamp, "ok": true, "latency": 999},
		{"time": stamp + 3600, "ok": true, "latency": 5000},
	}
	row := a.scores()[0]
	if !yes(row["eligible"]) || num(row["latency_average_ms"]) != 150 || num(row["latency_average_samples"]) != 2 || num(row["latency_average_window_minutes"]) != 30 {
		t.Fatal("default window, duplicate latest or failed/future sample handling", row)
	}
	a.cfg["score_latency_window_minutes"] = 1
	a.cfg["score_latency_min_samples"] = 2
	row = a.scores()[0]
	if yes(row["eligible"]) || row["score"] != nil || num(row["latency_average_ms"]) != 100 || num(row["latency_average_samples"]) != 1 {
		t.Fatal("insufficient samples must not score", row)
	}
	a.cfg["score_latency_window_minutes"] = 30
	row = a.scores()[0]
	if !yes(row["eligible"]) || num(row["latency_average_ms"]) != 150 {
		t.Fatal("expanding window did not restore score", row)
	}
	a.cfg["score_metrics"] = []any{"speed"}
	a.cfg["score_latency_min_samples"] = 20
	a.results["n1"]["last_speed"] = 2048
	if !yes(a.scores()[0]["eligible"]) {
		t.Fatal("latency threshold affected speed-only scoring")
	}
}

func TestScorePolicyValidationPersistenceAndNoCoreReload(t *testing.T) {
	a, runner := fixture(t)
	before := fingerprint(a.cfg)
	for _, policy := range []M{
		{"score_latency_window_minutes": 0}, {"score_latency_window_minutes": 1441}, {"score_latency_window_minutes": 1.5},
		{"score_latency_min_samples": 0}, {"score_latency_min_samples": 21}, {"score_latency_min_samples": 1.5},
	} {
		if a.saveXraySettings(policy) == nil || fingerprint(a.cfg) != before || len(runner.calls) > 0 {
			t.Fatal("invalid policy changed configuration", policy)
		}
	}
	a.applied = true
	if err := a.saveXraySettings(M{"score_latency_window_minutes": 60, "score_latency_min_samples": 3}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) > 0 {
		t.Fatal("score-only save issued system command", runner.calls)
	}
	stored := M{}
	if err := readJSON(filepath.Join(a.base, "config.json"), &stored); err != nil {
		t.Fatal(err)
	}
	if num(stored["score_latency_window_minutes"]) != 60 || num(stored["score_latency_min_samples"]) != 3 {
		t.Fatal("policy not persisted")
	}
}
