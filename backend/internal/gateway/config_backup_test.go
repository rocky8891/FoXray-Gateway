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
	"runtime"
	"strings"
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

func TestConfigurationBackupWarpResolverAlias(t *testing.T) {
	app, system := t.TempDir(), t.TempDir()
	const alias = "etc/netns/foxray-warp/resolv.conf"
	const managed = "etc/foxray/warp-netns-resolv.conf"
	aliasPath := filepath.Join(system, filepath.FromSlash(alias))
	managedPath := filepath.Join(system, filepath.FromSlash(managed))
	for _, file := range []string{aliasPath, managedPath} {
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
	}
	content := []byte("nameserver 192.0.2.53\n")
	if err := os.WriteFile(managedPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	target := "/" + managed
	if runtime.GOOS == "windows" {
		target = filepath.FromSlash("../../foxray/warp-netns-resolv.conf")
	}
	if err := os.Symlink(target, aliasPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	archive, err := buildConfigArchive(app, system, nil)
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(t.TempDir(), "staged")
	if err := extractConfigBackup(archive, staging); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{alias, managed} {
		filename := filepath.Join(staging, "system", filepath.FromSlash(name))
		got, err := os.ReadFile(filename)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("unexpected staged DNS file %s: %v", name, err)
		}
		info, err := os.Lstat(filename)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("expected a regular file at %s", name)
		}
	}
	if err := os.Remove(aliasPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/resolv.conf", aliasPath); err != nil {
		t.Fatal(err)
	}
	if _, err := buildConfigArchive(app, system, nil); err == nil || !strings.Contains(err.Error(), "/"+alias) {
		t.Fatalf("expected an error containing the full system path, got %v", err)
	}
}
