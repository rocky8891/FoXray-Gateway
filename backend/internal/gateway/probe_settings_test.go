package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestBandwidthUsesConfiguredURLAndLimit(t *testing.T) {
	path := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(strings.Repeat("x", 1024*1024+100)))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	p, _ := strconv.Atoi(u.Port())
	r := bandwidthWithPolicy(context.Background(), p, "http", M{"speed_url": "http://probe.invalid/custom-file", "speed_timeout": 2, "speed_size_mb": 1})
	if !yes(r["ok"]) || path != "/custom-file" || num(r["bytes"]) != 1024*1024 {
		t.Fatal("configured source/limit ignored", r, path)
	}
}
