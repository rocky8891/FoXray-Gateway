package gateway

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWarpRestoreVerifiesConnectionAndSettings(t *testing.T) {
	a, f := fixture(t)
	a.components["warp"] = true
	f.responses = map[string]string{
		"warp-cli --accept-tos status":       "Status update: Connected",
		"warp-cli --accept-tos settings":     "WARP tunnel protocol: MASQUE\nHTTP Version: HTTP/2\nOverride WARP endpoint: 192.0.2.1:443",
		"warp-cli --accept-tos tunnel stats": "Endpoints: 192.0.2.1\nProtocol: HTTPS via TCP",
	}
	old := M{"connected": true, "protocol": "MASQUE", "masque_options": "h2-only", "override": "192.0.2.1:443"}
	if err := a.restoreWarpConnection(context.Background(), &Job{}, old); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "systemctl restart") || strings.Contains(joined, "xray") {
			t.Fatal("restore touched another service", joined)
		}
	}
	f.responses["warp-cli --accept-tos settings"] = "WARP tunnel protocol: MASQUE\nHTTP Version: HTTP/2\nOverride WARP endpoint: 192.0.2.2:443"
	if err := a.restoreWarpConnection(context.Background(), &Job{}, old); err == nil {
		t.Fatal("connected at wrong endpoint was accepted as rollback success")
	}
}

func TestWarpRestoreRejectsPartialCommandFailure(t *testing.T) {
	a, f := fixture(t)
	f.fail = "tunnel endpoint"
	f.responses = map[string]string{"warp-cli --accept-tos status": "Status update: Connected"}
	if err := a.restoreWarpConnection(context.Background(), &Job{}, M{"connected": true, "override": "192.0.2.1:443"}); err == nil {
		t.Fatal("failed endpoint restore was reported as success")
	}
}

func TestWarpEndpointConfirmationNeverTrustsOverrideAlone(t *testing.T) {
	if warpEndpointConfirmed(M{"connected": true, "override": "192.0.2.1:443", "endpoint": "192.0.2.2"}, "192.0.2.1:443") {
		t.Fatal("configured target mistaken for actual endpoint")
	}
	if warpEndpointConfirmed(M{"connected": true, "override": "192.0.2.1:443"}, "192.0.2.1:443") {
		t.Fatal("missing tunnel endpoint accepted")
	}
	if !warpEndpointConfirmed(M{"connected": true, "endpoint": "192.0.2.1"}, "192.0.2.1:443") {
		t.Fatal("verified address rejected")
	}
}

func TestWarpWaitStopsAfterExplicitNativeHandshakeFailure(t *testing.T) {
	a, f := fixture(t)
	f.responses = map[string]string{"warp-cli --accept-tos status": "Status update: Unable\nReason: Failed to perform happy eyeballs"}
	start := time.Now()
	err := a.waitWarp(context.Background(), &Job{}, 45)
	if err == nil || !strings.Contains(err.Error(), "握手失败") {
		t.Fatal("explicit client failure hidden as generic timeout", err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatal("terminal native failure waited full connection timeout")
	}
}
