package gateway

import (
	"bytes"
	"context"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationBackupRoundTrip(t *testing.T) {
	app, system := t.TempDir(), t.TempDir()
	write := func(root, name string, content []byte) {
		t.Helper()
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte("synthetic fixture")
	write(app, "private/nodes.json", data)
	write(app, "private/config-backup-public.pem", data)
	write(system, "etc/wireguard/example.conf", data)
	write(system, "etc/shadow", data)
	raw, err := buildConfigArchive(app, system, nil)
	if err != nil {
		t.Fatal(err)
	}
	const password = "synthetic fixture only"
	encrypted, err := sealPasswordBackup(raw, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = openPasswordBackup(encrypted, "wrong fixture"); err == nil {
		t.Fatal("incorrect password accepted")
	}
	unencrypted, err := sealPasswordBackup(raw, "")
	if err != nil || !bytes.Equal(raw, unencrypted) {
		t.Fatal("unencrypted archive changed", err)
	}
	plain, err := openPasswordBackup(encrypted, password)
	if err != nil || !bytes.Equal(raw, plain) {
		t.Fatal("backup round trip failed", err)
	}
	dest := filepath.Join(t.TempDir(), "staging")
	if err = extractConfigBackup(plain, dest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app/private/nodes.json", "system/etc/wireguard/example.conf"} {
		got, e := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if e != nil || !bytes.Equal(got, data) {
			t.Fatal(name, e)
		}
	}
	for _, name := range []string{"system/etc/shadow", "app/private/config-backup-public.pem"} {
		if _, e := os.Stat(filepath.Join(dest, filepath.FromSlash(name))); !os.IsNotExist(e) {
			t.Fatal("unexpected file", name, e)
		}
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = openPasswordBackup(encrypted, password); err == nil {
		t.Fatal("tampered backup accepted")
	}
	if backupAllowed("system/etc/wireguard/../shadow") {
		t.Fatal("traversal accepted")
	}
}

func TestBackupPasswordKDF(t *testing.T) {
	const want = "ea2fba9e6dd796f161e3ef5b9cc905ba176d7004514f76637e319844ab6530e4"
	if hex.EncodeToString(passwordBackupKey("fixture", []byte("0123456789abcdef"))) != want {
		t.Fatal("PBKDF2 output differs from reference")
	}
}

func TestBackupRequiresLoopback(t *testing.T) {
	for _, tc := range []struct {
		peer, local, host string
		allowed           bool
	}{
		{"127.0.0.1:1234", "127.0.0.1", "localhost:18880", true},
		{"192.168.1.99:1234", "127.0.0.1", "localhost:18880", false},
		{"127.0.0.1:1234", "192.168.1.8", "localhost:18880", false},
		{"127.0.0.1:1234", "127.0.0.1", "remote.invalid:18880", false},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/api/config-backup/export", nil)
		r.RemoteAddr = tc.peer
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP(tc.local), Port: 18880}))
		if localBackupRequest(r) != tc.allowed {
			t.Fatal(tc)
		}
	}
}
