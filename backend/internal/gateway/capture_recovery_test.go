package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdoptVerifiedCapturePreservesExitWithoutRestart(t *testing.T) {
	a, f := fixture(t)
	a.cfg["enabled"], a.cfg["selected"] = true, "n1"
	f.responses = map[string]string{"systemctl show foxray-engine -p MainPID --value": "42\n"}
	ok, err := a.adoptControl(M{"active": "n1", "applied": true, "engine_pid": "42", "last_switch": 123})
	if err != nil || !ok || !a.applied || a.active != "n1" || a.lastSwitch != 123 {
		t.Fatal("verified capture not adopted", err)
	}
	for _, call := range f.calls {
		s := strings.Join(call, " ")
		if strings.Contains(s, "restart") || strings.Contains(s, "-f") {
			t.Fatal("verified startup changed live rules", s)
		}
	}
}

func TestMissingCaptureIsNotAdoptedOrRewrittenInReadOnlyMode(t *testing.T) {
	a, f := fixture(t)
	a.cfg["enabled"], a.cfg["selected"] = true, "n1"
	f.responses = map[string]string{"systemctl show foxray-engine -p MainPID --value": "42\n"}
	f.fail = "nft list table inet vpm"
	ok, err := a.adoptControl(M{"active": "n1", "applied": true, "engine_pid": "42"})
	if ok || err == nil {
		t.Fatal("missing rules silently adopted")
	}
	for _, call := range f.calls {
		if call[0] == "ip" || strings.Contains(strings.Join(call, " "), "-f") {
			t.Fatal("read-only startup wrote rules")
		}
	}
}

func TestSubscriptionCannotSucceedWhenCaptureRestoreFails(t *testing.T) {
	a, f := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Download directly, then model a node selected while the request was in flight.
		a.mu.Lock()
		a.active = str(a.cfg["selected"])
		a.mu.Unlock()
		_, _ = w.Write([]byte(`{"outbounds":[{"type":"socks","tag":"test","server":"203.0.113.9","server_port":1080}]}`))
	}))
	defer server.Close()
	parsed, err := parseDocument(`{"outbounds":[{"type":"socks","tag":"test","server":"203.0.113.9","server_port":1080}]}`)
	if err != nil {
		t.Fatal(err)
	}
	a.native["subscriptions"] = []any{M{"id": "sub1", "url": server.URL, "updated": 0}}
	if _, _, err = mergeNodes(a.native, parsed, "sub1", "skip"); err != nil {
		t.Fatal(err)
	}
	a.rebuildEndpoints(a.cfg, a.native)
	a.active = str(obj(arr(a.native["nodes"])[0])["id"])
	a.cfg["selected"], a.cfg["enabled"], a.applied = a.active, true, true
	engine, err := a.buildEngine(a.cfg, a.native, a.active)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeJSON(filepath.Join(a.base, "engine.json"), engine); err != nil {
		t.Fatal(err)
	}
	before := fingerprint(a.native)
	f.fail = "nft --check"
	a.active = "direct"
	err = a.updateSubscription(context.Background(), &Job{}, "sub1", "skip")
	if err == nil || !strings.Contains(err.Error(), "接管规则") {
		t.Fatal("subscription reported success with missing capture", err)
	}
	if fingerprint(a.native) != before {
		t.Fatal("failed reload committed subscription changes")
	}
}
