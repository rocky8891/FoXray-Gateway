package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWritesAcceptReceivingInterfaceAndRejectUnrelatedHosts(t *testing.T) {
	for _, tc := range []struct {
		name, host, localIP, origin, allowedHosts string
		status                                    int
	}{
		{"server interface", "10.20.30.40:18880", "10.20.30.40", "", "", 200},
		{"other server interface", "192.168.50.2:18880", "192.168.50.2", "", "", 200},
		{"IPv6 interface", "[fd00::2]:18880", "fd00::2", "", "", 200},
		{"loopback", "127.0.0.1:18880", "127.0.0.1", "", "", 200},
		{"unrelated IP", "192.168.1.8:18880", "10.20.30.40", "", "", 403},
		{"unlisted domain", "unknown.invalid:18880", "10.20.30.40", "", "", 403},
		{"explicit domain", "gateway.example:18880", "10.20.30.40", "", "gateway.example", 200},
		{"foreign origin", "10.20.30.40:18880", "10.20.30.40", "https://unknown.invalid", "", 403},
		{"wildcard is not a destination", "0.0.0.0:18880", "0.0.0.0", "", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FOXRAY_ALLOWED_HOSTS", tc.allowedHosts)
			a, _ := fixture(t)
			a.readonly = false
			r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/monitoring/display", strings.NewReader(`{"chart_refresh_seconds":5}`))
			r.RemoteAddr = "10.20.30.50:55000"
			r.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP(tc.localIP), Port: 18880}))
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}
