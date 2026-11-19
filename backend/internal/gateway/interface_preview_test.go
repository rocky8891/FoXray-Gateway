package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotOnlyInterfacePlanner(t *testing.T) {
	var rows []any
	if json.Unmarshal([]byte(testInterfaceAddressJSON), &rows) != nil {
		t.Fatal("fixture")
	}
	snapshot := M{"complete": true, "items": rows, "routes_complete": true, "default_routes": M{"4": []any{}, "6": []any{}}}
	result, err := InterfacePreviewFromSnapshot(M{"kind": "interfaces", "operation": "replace", "allow_protected": true, "item": M{"name": "eth0", "address": "192.0.2.20", "prefix": 24, "original_address": "192.0.2.10/24"}}, snapshot)
	if err != nil || !yes(result["preview_only"]) || len(arr(result["preview"])) != 2 {
		t.Fatal(result, err)
	}
	if _, err := InterfacePreviewFromSnapshot(M{"kind": "interfaces", "operation": "snapshot"}, snapshot); err != nil {
		t.Fatal(err)
	}
	runner := interfacePreviewRunner{snapshot: snapshot}
	for _, args := range [][]string{{"ip", "link", "delete", "dev", "eth0"}, {"systemctl", "restart", "xray"}, {"sh", "-c", "true"}} {
		if _, err := runner.Run(context.Background(), args, ""); err == nil {
			t.Fatal("snapshot runner accepted mutation")
		}
	}
	for _, req := range []M{{"kind": "interfaces", "operation": "apply"}, {"kind": "routes", "operation": "add"}, {"kind": "interfaces", "operation": "add", "items": []any{M{}}}} {
		if _, err := InterfacePreviewFromSnapshot(req, snapshot); err == nil {
			t.Fatal("unsupported helper operation")
		}
	}
	blob, _ := json.Marshal(result)
	if strings.Contains(string(blob), "steps") || strings.Contains(string(blob), "undo") {
		t.Fatal("private implementation escaped")
	}
}
