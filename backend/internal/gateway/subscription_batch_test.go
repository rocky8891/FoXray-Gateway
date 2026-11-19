package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func subscriptionsFixture(t *testing.T) (*App, *fakeRunner) {
	a, f := fixture(t)
	f.responses = map[string]string{"systemctl show foxray-engine -p ActiveState -p SubState -p MainPID -p UnitFileState": "ActiveState=inactive\n"}
	a.native = M{"nodes": []any{M{"id": "n1", "name": "one", "subscription": "s1", "enabled": true, "priority": 20, "outbound": M{"type": "socks", "server": "192.0.2.1", "server_port": 1080}}, M{"id": "n2", "name": "two", "subscription": "s2", "enabled": true, "priority": 20, "outbound": M{"type": "socks", "server": "192.0.2.2", "server_port": 1080}}}, "subscriptions": []any{M{"id": "s1", "name": "one", "auto_update": true, "interval": 3600}, M{"id": "s2", "name": "two", "auto_update": true, "interval": 3600}}, "excluded": M{}}
	return a, f
}
func TestSubscriptionBatchScopeAndStopUpdates(t *testing.T) {
	a, _ := subscriptionsFixture(t)
	if a.publicNative()["subscriptions"].([]M)[0]["enabled"] != true {
		t.Fatal("legacy disabled")
	}
	for _, op := range []string{"disable", "enable", "stop_updates"} {
		if _, e := a.subscriptionBatch(M{"ids": []any{"s1"}, "operation": op}); e != nil {
			t.Fatal(e)
		}
		n := obj(arr(a.native["nodes"])[0])
		sub := obj(arr(a.native["subscriptions"])[0])
		if yes(n["enabled"]) != (op != "disable") {
			t.Fatalf("node not linked %s", op)
		}
		if op == "stop_updates" && (yes(sub["auto_update"]) || sub["enabled"] == false) {
			t.Fatal("stop updates changed enabled")
		}
		if !yes(obj(arr(a.native["nodes"])[1])["enabled"]) || !yes(obj(arr(a.native["subscriptions"])[1])["auto_update"]) {
			t.Fatal("unselected changed")
		}
	}
}
func TestSubscriptionBatchRejectsInvalidSelectionAtomically(t *testing.T) {
	a, _ := subscriptionsFixture(t)
	before, _ := json.Marshal(a.native)
	for _, ids := range [][]any{nil, {"s1", "s1"}, {"s1", "missing"}, {""}} {
		if _, e := a.subscriptionBatch(M{"ids": ids, "operation": "disable"}); e == nil {
			t.Fatal("invalid accepted")
		}
		after, _ := json.Marshal(a.native)
		if string(before) != string(after) {
			t.Fatal("partial mutation")
		}
	}
}
func TestSubscriptionDeleteRetainsOrDeletesSelectedNodes(t *testing.T) {
	for _, remove := range []bool{false, true} {
		a, _ := subscriptionsFixture(t)
		if _, e := a.subscriptionBatch(M{"ids": []any{"s1"}, "operation": "delete", "delete_nodes": remove}); e != nil {
			t.Fatal(e)
		}
		if len(arr(a.native["subscriptions"])) != 1 || len(arr(a.native["nodes"])) != map[bool]int{false: 2, true: 1}[remove] {
			t.Fatal("wrong scope")
		}
		if !remove && obj(arr(a.native["nodes"])[0])["subscription"] != nil {
			t.Fatal("not detached")
		}
	}
}
func TestSubscriptionDisableCurrentWithoutFallbackRejected(t *testing.T) {
	a, _ := subscriptionsFixture(t)
	a.active = "n1"
	a.applied = true
	a.warpCache = M{"connected": false}
	before, _ := json.Marshal(a.native)
	if _, e := a.subscriptionBatch(M{"ids": []any{"s1"}, "operation": "disable"}); e == nil {
		t.Fatal("unverified fallback accepted")
	}
	after, _ := json.Marshal(a.native)
	if string(before) != string(after) {
		t.Fatal("failed disable committed")
	}
}
func TestSubscriptionUpdateRechecksAfterDownload(t *testing.T) {
	for _, op := range []string{"disable", "delete"} {
		a, _ := subscriptionsFixture(t)
		started, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { close(started) })
			<-release
			w.Write([]byte(`{"outbounds":[{"type":"socks","server":"192.0.2.3","server_port":1080}]}`))
		}))
		defer srv.Close()
		obj(arr(a.native["subscriptions"])[0])["url"] = srv.URL
		done := make(chan error, 1)
		go func() { done <- a.updateSubscription(context.Background(), &Job{}, "s1", "skip") }()
		<-started
		if _, e := a.subscriptionBatch(M{"ids": []any{"s1"}, "operation": op}); e != nil {
			close(release)
			t.Fatal(e)
		}
		before, _ := json.Marshal(a.native)
		close(release)
		if e := <-done; e == nil {
			t.Fatal("stale write accepted")
		}
		after, _ := json.Marshal(a.native)
		if string(before) != string(after) {
			t.Fatal("download overwrote latest state")
		}
	}
}

func TestSubscriptionBatchUpdateReportsPartialResults(t *testing.T) {
	a, _ := subscriptionsFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/failed" {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"outbounds":[{"type":"socks","server":"192.0.2.1","server_port":1080}]}`))
	}))
	defer server.Close()
	subs := arr(a.native["subscriptions"])
	obj(subs[0])["url"] = server.URL + "/good"
	obj(subs[1])["url"] = server.URL + "/failed"
	job := &Job{}
	if e := a.updateSubscriptions(context.Background(), job, M{"ids": []any{"s1", "s2"}}); e == nil {
		t.Fatal("partial failure hidden")
	}
	if len(job.Details) != 2 || !strings.Contains(job.Details[0], "成功") || !strings.Contains(job.Details[1], "失败") {
		t.Fatal("missing per-item results", job.Details)
	}
	if num(obj(arr(a.native["subscriptions"])[0])["updated"]) == 0 || num(obj(arr(a.native["subscriptions"])[1])["updated"]) != 0 {
		t.Fatal("incorrect partial commit")
	}
}
