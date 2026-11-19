package gateway

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type clockTransport func(*http.Request) (*http.Response, error)

func (f clockTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDisplayTimeFallbackCacheAndCustomServer(t *testing.T) {
	displayClock.at = time.Time{}
	displayClock.stamp = time.Time{}
	calls := []string{}
	client := &http.Client{Transport: clockTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Host)
		status := 200
		if r.URL.Host == "www.cloudflare.com" {
			status = 403
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Date": []string{time.Now().UTC().Format(http.TimeFormat)}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	first := displayTimeWithClient("internet", "", client)
	if first["internet_time"] == nil || first["time_server"] != defaultClockServers[1] || len(calls) != 2 {
		t.Fatalf("fallback failed: %v, calls %v", first, calls)
	}
	displayTimeWithClient("internet", "", client)
	if len(calls) != 2 {
		t.Fatal("cache not reused")
	}
	custom := displayTimeWithClient("internet", "https://clock.example/", client)
	if custom["time_server"] != "https://clock.example/" || len(calls) != 3 {
		t.Fatal("custom server reused another source cache")
	}
	displayTimeWithClient("server", "", client)
	if len(calls) != 3 {
		t.Fatal("server time accessed internet")
	}
}

func TestClockErrorsAndSettings(t *testing.T) {
	a, _ := fixture(t)
	if e := a.savePresentation(M{"clock_server": "https://clock.example/"}); e != nil {
		t.Fatal(e)
	}
	for _, target := range []string{"http://clock.example/", "not a URL", "https://clock.example/#part", "https://clock.example/?x=1"} {
		if a.savePresentation(M{"clock_server": target}) == nil {
			t.Fatalf("invalid target accepted: %q", target)
		}
	}
	client := &http.Client{Transport: clockTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	result := displayTimeWithClient("internet", "https://missing-date.example/", client)
	if result["internet_time"] != nil || !strings.Contains(str(result["error"]), "Date") {
		t.Fatalf("missing Date not reported: %v", result)
	}
}
