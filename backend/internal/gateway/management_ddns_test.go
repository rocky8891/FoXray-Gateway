package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type ddnsTestTransport func(*http.Request) (*http.Response, error)

func (f ddnsTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestManagementDDNSCloudflareAndDuckDNS(t *testing.T) {
	d, err := newManagementDDNS(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	const fixtureToken = "synthetic_fixture"
	submit := func(body string) int {
		w := httptest.NewRecorder()
		d.serve(w, httptest.NewRequest(http.MethodPost, "/api/management/ddns", strings.NewReader(body)), false)
		return w.Code
	}
	if code := submit(`{"operation":"save","provider":"cloudflare","domain":"Lab.Example.test","token":"synthetic_fixture","ipv4":"203.0.113.7","enabled":false}`); code != 200 {
		t.Fatal(code)
	}
	if d.cloudflareToken("LAB.example.test") != fixtureToken || d.cloudflareToken("other.example.test") != "" {
		t.Fatal("incorrect ACME account lookup")
	}
	if code := submit(`{"operation":"save","provider":"cloudflare","domain":"lab.example.test","ipv4":"203.0.113.8","enabled":false}`); code != 200 {
		t.Fatal(code)
	}
	if d.cloudflareToken("lab.example.test") != fixtureToken {
		t.Fatal("existing token lost on edit")
	}
	w := httptest.NewRecorder()
	d.serve(w, httptest.NewRequest(http.MethodGet, "/api/management/ddns", nil), false)
	if strings.Contains(w.Body.String(), fixtureToken) || !strings.Contains(w.Body.String(), "lab.example.test") {
		t.Fatal("unexpected DDNS response")
	}
	info, err := os.Stat(d.configPath())
	if err != nil || info.IsDir() {
		t.Fatal(err)
	}
	if code := submit(`{"operation":"save","provider":"duckdns","domain":"fixture.duckdns.org","token":"synthetic_fixture","ipv4":"203.0.113.7","enabled":false}`); code != 200 {
		t.Fatal(code)
	}
	if code := submit(`{"operation":"save","provider":"duckdns","domain":"example.test","token":"synthetic_fixture","enabled":false}`); code != 400 {
		t.Fatal(code)
	}
	records := false
	updates := 0
	d.client.Transport = ddnsTestTransport(func(r *http.Request) (*http.Response, error) {
		respond := func(body string) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if r.URL.Host == "www.duckdns.org" {
			if r.URL.Path != "/update" || r.URL.Query().Get("domains") != "fixture" || r.URL.Query().Get("ip") != "203.0.113.7" {
				t.Fatal("incorrect DuckDNS update")
			}
			return respond("OK\n")
		}
		if r.URL.Host != "api.cloudflare.com" || r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			t.Fatal("incorrect Cloudflare request")
		}
		if r.URL.Path == "/client/v4/zones" {
			if r.URL.Query().Get("name") == "example.test" {
				return respond(`{"success":true,"result":[{"id":"zone","name":"example.test"}]}`)
			}
			return respond(`{"success":true,"result":[]}`)
		}
		if r.Method == http.MethodGet {
			if records {
				return respond(`{"success":true,"result":[{"id":"record","name":"lab.example.test","content":"203.0.113.7"}]}`)
			}
			return respond(`{"success":true,"result":[]}`)
		}
		var fields M
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil || fields["content"] != "203.0.113.8" {
			t.Fatal("incorrect Cloudflare address")
		}
		if !records && r.Method == http.MethodPost && fields["proxied"] == false {
			records = true
			updates++
			return respond(`{"success":true}`)
		}
		if records && r.Method == http.MethodPatch && r.URL.Path == "/client/v4/zones/zone/dns_records/record" && len(fields) == 1 {
			updates++
			return respond(`{"success":true}`)
		}
		t.Fatal("incorrect Cloudflare mutation")
		return nil, nil
	})
	for _, domain := range []string{"lab.example.test", "lab.example.test", "fixture.duckdns.org"} {
		if err := d.update(context.Background(), domain, true); err != nil {
			t.Fatal(err)
		}
	}
	if updates != 2 {
		t.Fatal(updates)
	}
}
