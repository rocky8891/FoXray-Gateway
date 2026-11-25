package gateway

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
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
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := encryptConfigBackup(raw, &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptConfigBackup(encrypted, key)
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
	if _, err = decryptConfigBackup(encrypted, key); err == nil {
		t.Fatal("tampered backup accepted")
	}
	if backupAllowed("system/etc/wireguard/../shadow") {
		t.Fatal("traversal accepted")
	}
}
