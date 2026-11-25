package gateway

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
	if got := request("POST", "/api/management/users", M{"old_name": "", "username": "new-fixture", "password": "synthetic password"}, cookie); got.Code != 200 {
		t.Fatal(got)
	}
	if got := request("POST", "/api/management/users", M{"old_name": "fixture", "username": "renamed", "password": "synthetic changed"}, cookie).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/management/users", nil, cookie).Code; got != 401 {
		t.Fatal(got)
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
