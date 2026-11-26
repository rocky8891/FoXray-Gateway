package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementAuthentication(t *testing.T) {
	app, _ := fixture(t)
	app.readonly = false
	guard, err := NewManagementSecurity(app, "127.0.0.1:18880")
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	salt := []byte("fixture-salt-123")
	guard.users = map[string]managementUser{"fixture": {Salt: salt, Digest: passwordBackupKey("synthetic original", salt)}}
	request := func(method, target string, body M, cookie *http.Cookie) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "http://127.0.0.1"+target, bytes.NewReader(raw))
		r.RemoteAddr = "127.0.0.1:49200"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://127.0.0.1")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		return w
	}
	if got := request("GET", "/api/state", nil, nil).Code; got != 401 {
		t.Fatal(got)
	}
	if got := request("POST", "/api/config-backup/export", M{}, nil).Code; got != 401 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/health", nil, nil).Code; got != 200 {
		t.Fatal(got)
	}
	login := request("POST", "/api/management/login", M{"username": "fixture", "password": "synthetic original"}, nil)
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("insecure cookie")
	}
	if got := request("GET", "/api/management/users", nil, cookie).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request("POST", "/api/management/users", M{"old_name": "", "username": "new-fixture", "password": "x"}, cookie); got.Code != 200 {
		t.Fatal(got)
	}
	if got := request("POST", "/api/management/users", M{"old_name": "fixture", "username": "renamed", "password": "synthetic changed"}, cookie).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/management/users", nil, cookie).Code; got != 401 {
		t.Fatal(got)
	}
	login = request("POST", "/api/management/login", M{"username": "renamed", "password": "synthetic changed"}, nil)
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	cookie = login.Result().Cookies()[0]
	if got := request("POST", "/api/management/login-mode", M{"mode": "password", "password_account": "new-fixture"}, cookie).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/management/users", nil, cookie).Code; got != 401 {
		t.Fatal(got)
	}
	login = request("POST", "/api/management/login", M{"password": "x"}, nil)
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	cookie = login.Result().Cookies()[0]
	if got := request("POST", "/api/management/login-mode", M{"mode": "off"}, cookie).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/management/users", nil, nil).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request("POST", "/api/management/login-mode", M{"mode": "account"}, nil).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/management/users", nil, nil).Code; got != 401 {
		t.Fatal(got)
	}

}

func TestManagementAuthenticationAfterHTTPSDisabled(t *testing.T) {
	app, _ := fixture(t)
	guard, err := NewManagementSecurity(app, "127.0.0.1:18880")
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	salt := []byte("fixture-salt-123")
	guard.users = map[string]managementUser{"fixture": {Salt: salt, Digest: passwordBackupKey("synthetic fixture", salt)}}
	request := func(scheme, method, path string, body M, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		origin := scheme + "://192.168.1.8"
		r := httptest.NewRequest(method, origin+path, bytes.NewReader(raw))
		r.RemoteAddr = "192.168.1.10:49200"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		return w
	}
	login := func(scheme string) *http.Cookie {
		w := request(scheme, "POST", "/api/management/login", M{"username": "fixture", "password": "synthetic fixture"})
		cookies := w.Result().Cookies()
		if w.Code != http.StatusOK || len(cookies) != 1 {
			t.Fatalf("%s login returned %d with %d cookies", scheme, w.Code, len(cookies))
		}
		return cookies[0]
	}
	httpsCookie := login("https")
	httpCookie := login("http")
	// Browsers reject an HTTP cookie that would overwrite an HTTPS cookie.
	if !httpsCookie.Secure || httpCookie.Secure || httpsCookie.Name == httpCookie.Name {
		t.Fatal("HTTP login would conflict with the existing HTTPS cookie")
	}
	if got := request("http", "GET", "/api/state", nil, httpsCookie).Code; got != http.StatusUnauthorized {
		t.Fatalf("HTTP accepted the HTTPS cookie: %d", got)
	}
	if got := request("http", "GET", "/api/state", nil, httpCookie).Code; got != http.StatusOK {
		t.Fatalf("HTTP rejected its session: %d", got)
	}
	logout := request("http", "POST", "/api/management/logout", nil, httpCookie)
	cleared := logout.Result().Cookies()
	if logout.Code != http.StatusOK || len(cleared) != 1 || cleared[0].Name != httpCookie.Name || cleared[0].MaxAge >= 0 {
		t.Fatal("HTTP logout did not clear its session cookie")
	}
	if got := request("http", "GET", "/api/state", nil, httpCookie).Code; got != http.StatusUnauthorized {
		t.Fatalf("HTTP accepted a logged-out session: %d", got)
	}
	if got := request("https", "GET", "/api/state", nil, httpCookie, httpsCookie).Code; got != http.StatusOK {
		t.Fatalf("HTTPS rejected its session: %d", got)
	}
}

func TestManagementSelfSignedHTTPS(t *testing.T) {
	pem, err := selfSignedManagement("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair([]byte(pem), []byte(pem))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || cert.VerifyHostname("127.0.0.1") != nil {
		t.Fatal("invalid certificate SAN", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	manager := newManagementTLS(t.TempDir(), "127.0.0.1:18880", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	manager.address = address
	defer manager.close()
	manager.mu.Lock()
	err = manager.applyLocked(managementTLSConfig{Mode: "self", Name: "127.0.0.1", PEM: pem}, true)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	response, err := client.Get("https://" + address)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal(response.StatusCode)
	}
}

func TestManagementHTTPTransition(t *testing.T) {
	manager := &managementTLS{address: "127.0.0.1:18443", server: &http.Server{}, config: managementTLSConfig{Port: 19443, Redirect: 18080}}
	request := func(method string, port int) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.test/?q=1", nil)
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}))
		w := httptest.NewRecorder()
		if !manager.handlePlainHTTP(w, r) {
			t.Fatal("HTTP was allowed while TLS was active")
		}
		return w
	}
	if w := request(http.MethodGet, 18080); w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "https://example.test:19443/?q=1" {
		t.Fatal(w.Code, w.Header())
	}
	if w := request(http.MethodGet, 18880); w.Code != http.StatusUpgradeRequired {
		t.Fatal(w.Code)
	}
	if w := request(http.MethodPost, 18080); w.Code != http.StatusUpgradeRequired {
		t.Fatal(w.Code)
	}
	manager.config.Redirect = 0
	if w := request(http.MethodGet, 18080); w.Code != http.StatusUpgradeRequired {
		t.Fatal(w.Code)
	}
}
func TestManagementKeepsCertificateWhenSavingRenewalInterval(t *testing.T) {
	certPEM, err := selfSignedManagement("certificate.example.test")
	if err != nil {
		t.Fatal(err)
	}
	manager := newManagementTLS(t.TempDir(), "127.0.0.1:18880", http.NotFoundHandler())
	defer manager.close()
	manager.config = managementTLSConfig{Mode: "cloudflare", Name: "certificate.example.test", Email: "fixture@example.test", Port: 18443, PEM: certPEM}
	manager.server = &http.Server{}
	request := httptest.NewRequest(http.MethodPost, "https://127.0.0.1/api/management/https", strings.NewReader(`{"mode":"keep","port":"18443","renew_hours":"24","cloudflare_token":"synthetic_fixture"}`))
	response := httptest.NewRecorder()
	manager.serve(response, request, false)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code)
	}
	var saved managementTLSConfig
	if err = readJSON(manager.configFile(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Mode != "cloudflare" || saved.Name != "certificate.example.test" || saved.Email != "fixture@example.test" || saved.PEM != certPEM || saved.RenewHours != 24 {
		t.Fatal("interval change altered the saved certificate")
	}
	response = httptest.NewRecorder()
	manager.serve(response, httptest.NewRequest(http.MethodGet, "/api/management/https", nil), false)
	path, err := manager.cloudflareCredential(saved.Name)
	if err != nil || path != manager.httpsCloudflareCredential(saved.Name) {
		t.Fatal("independent credential not selected")
	}
	if strings.Contains(response.Body.String(), "synthetic_fixture") {
		t.Fatal("credential returned by HTTPS status")
	}
	var info M
	if err = json.Unmarshal(response.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if str(info["mode"]) != "cloudflare" || num(info["renew_hours"]) != 24 || str(info["subject"]) != "certificate.example.test" || info["next_renew_check"] == nil {
		t.Fatal("HTTPS status omitted saved settings")
	}
}

func TestManagementCloudflareTokenRevealRequiresHTTPSAndSession(t *testing.T) {
	app, _ := fixture(t)
	app.readonly = false
	guard, err := NewManagementSecurity(app, "127.0.0.1:18880")
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	const domain = "certificate.example.test"
	const fixtureValue = "synthetic_fixture_only"
	guard.tls.config = managementTLSConfig{Mode: "cloudflare", Name: domain}
	if err := atomicFile(guard.tls.httpsCloudflareCredential(domain), []byte("dns_cloudflare_api_token = "+fixtureValue+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	salt := []byte("fixture-salt-123")
	guard.users = map[string]managementUser{"fixture": {Salt: salt, Digest: passwordBackupKey("synthetic password", salt)}}
	request := func(scheme, endpoint string, body M, cookie *http.Cookie) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, scheme+"://127.0.0.1"+endpoint, bytes.NewReader(raw))
		r.RemoteAddr = "127.0.0.1:49200"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", scheme+"://127.0.0.1")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		return w
	}
	const endpoint = "/api/management/https/cloudflare-token"
	body := M{"domain": domain}
	if got := request("https", endpoint, body, nil).Code; got != 401 {
		t.Fatal(got)
	}
	login := func(scheme string) *http.Cookie {
		w := request(scheme, "/api/management/login", M{"username": "fixture", "password": "synthetic password"}, nil)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		return w.Result().Cookies()[0]
	}
	secure := login("https")
	if got := request("http", endpoint, body, login("http")).Code; got != 403 {
		t.Fatal(got)
	}
	if got := request("https", endpoint, M{"domain": "other.example.test"}, secure).Code; got != 404 {
		t.Fatal(got)
	}
	w := request("https", endpoint, body, secure)
	var payload M
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &payload) != nil || payload["token"] != fixtureValue {
		t.Fatal("authenticated reveal did not return the non-cacheable fixture")
	}
	guard.mode = "off"
	if got := request("https", endpoint, body, nil).Code; got != 403 {
		t.Fatal(got)
	}
}
