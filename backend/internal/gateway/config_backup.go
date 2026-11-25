package gateway

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const backupMagic = "FOXRAY-CONFIG-1\n"
const backupLimit = 64 << 20
const backupPublicKey = "private/config-backup-public.pem"

var appBackupFiles = strings.Fields(`config.json components.json engine.json core.json manager-settings.json service-preferences.json auto-bypass.json host-monitor-policy.json icmp-policy.json log-retention-policy.json monitor-data-policy.json monitoring-items.json traffic-history-policy.json traffic-monitor-modules.json traffic-monitor-policy.json warp-endpoints.json private/nodes.json private/network-managed.json private/traffic-policy.json private/warp-auto.json`)
var systemBackupFiles = strings.Fields(`etc/foxray/topology.json etc/foxray/warp-netns-resolv.conf etc/netns/foxray-warp/resolv.conf etc/systemd/system/foxray-engine.service etc/systemd/system/foxray-warp-netns.service etc/systemd/system/foxray-warp-network.service etc/systemd/system/komari.service etc/nftables.conf etc/firewalld/firewalld.conf etc/sysconfig/nftables.conf etc/sysconfig/iptables etc/sysconfig/ip6tables var/lib/cloudflare-warp/conf.json var/lib/cloudflare-warp/consumer-settings.json var/lib/cloudflare-warp/final-overrides-settings.json var/lib/cloudflare-warp/reg.json var/lib/cloudflare-warp/settings.json var/lib/cloudflare-warp/warp.db var/lib/foxray-scout/account.json opt/komari/data/komari.db opt/komari/data/plugin/state.json`)
var systemBackupPatterns = []string{
	"etc/wireguard/*.conf", "etc/wireguard/*.private", "etc/wireguard/*.public", "etc/wireguard/*.key",
	"etc/NetworkManager/system-connections/*.nmconnection",
	"etc/firewalld/zones/*.xml", "etc/firewalld/policies/*.xml", "etc/firewalld/ipsets/*.xml", "etc/firewalld/services/*.xml", "etc/firewalld/helpers/*.xml", "etc/firewalld/icmptypes/*.xml", "etc/firewalld/direct.xml",
	"etc/systemd/system/foxray-engine.service.d/*.conf", "etc/systemd/system/warp-svc.service.d/*.conf", "etc/systemd/system/komari.service.d/*.conf", "etc/sysctl.d/*foxray*.conf",
}

func backupKey(raw []byte) (*rsa.PublicKey, error) {
	block, rest := pem.Decode(raw)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || block.Type != "PUBLIC KEY" {
		return nil, errors.New("需要 PEM 格式的 RSA 公钥")
	}
	decoded, err := x509.ParsePKIXPublicKey(block.Bytes)
	key, ok := decoded.(*rsa.PublicKey)
	if err != nil || !ok || key.N.BitLen() < 3072 || key.N.BitLen() > 8192 {
		return nil, errors.New("需要 3072 至 8192 位的 RSA 公钥")
	}
	return key, nil
}

func SetBackupPublicKey(base, filename string) error {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	if _, err = backupKey(raw); err != nil {
		return err
	}
	target := filepath.Join(base, backupPublicKey)
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	return atomicFile(target, raw, 0600)
}

func backupAllowed(name string) bool {
	if strings.ContainsAny(name, "\\:\x00") || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	if name == "manifest.json" {
		return true
	}
	if strings.HasPrefix(name, "app/") {
		for _, rel := range appBackupFiles {
			if name == "app/"+rel {
				return true
			}
		}
		return false
	}
	rel, ok := strings.CutPrefix(name, "system/")
	if !ok {
		return false
	}
	for _, item := range systemBackupFiles {
		if rel == item {
			return true
		}
	}
	for _, pattern := range systemBackupPatterns {
		if match, _ := path.Match(pattern, rel); match {
			return true
		}
	}
	return false
}

func readBackupFile(root *os.Root, name string, remaining int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("配置路径不是普通文件")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) {
		return nil, errors.New("配置文件在读取前发生变化")
	}
	if info.Size() > remaining {
		return nil, errors.New("配置备份超过 64 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, remaining+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || int64(len(data)) != info.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, errors.New("配置文件正在更新，请重试")
	}
	return data, nil
}

func backupSQLite(filename string, remaining int64) ([]byte, error) {
	// VACUUM INTO snapshots a live SQLite database including committed WAL entries.
	dir, err := os.MkdirTemp("", "foxray-backup-db-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	snapshot := filepath.Join(dir, "snapshot.db")
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err = db.Exec("VACUUM INTO ?", snapshot); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readBackupFile(root, "snapshot.db", remaining)
}

func buildConfigArchive(base, systemRoot string, serviceState func([]string) any) ([]byte, error) {
	app, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer app.Close()
	system, err := os.OpenRoot(systemRoot)
	if err != nil {
		return nil, err
	}
	defer system.Close()
	var buf bytes.Buffer
	out := zip.NewWriter(&buf)
	defer out.Close()
	var names []string
	var remaining int64 = backupLimit
	add := func(root *os.Root, scope, name string) error {
		full := scope + "/" + name
		if !backupAllowed(full) || len(names) >= 2048 {
			return errors.New("配置备份路径或数量无效")
		}
		var raw []byte
		var e error
		if scope == "system" && strings.HasSuffix(name, ".db") && systemRoot == "/" {
			info, statErr := root.Lstat(name)
			e = statErr
			if e == nil {
				resolved, resolveErr := filepath.EvalSymlinks("/" + name)
				if resolveErr != nil || resolved != "/"+name || !info.Mode().IsRegular() || info.Size() > remaining {
					return fmt.Errorf("无法备份数据库 %s", name)
				}
				raw, e = backupSQLite(resolved, remaining)
			}
		} else {
			raw, e = readBackupFile(root, name, remaining)
		}
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return fmt.Errorf("无法备份 %s，请检查文件状态或容量后重试", name)
		}
		remaining -= int64(len(raw))
		w, e := out.Create(full)
		if e == nil {
			_, e = w.Write(raw)
		}
		if e == nil {
			names = append(names, full)
		}
		return e
	}
	for _, name := range appBackupFiles {
		if err = add(app, "app", name); err != nil {
			return nil, err
		}
	}
	for _, name := range systemBackupFiles {
		if err = add(system, "system", name); err != nil {
			return nil, err
		}
	}
	for _, pattern := range systemBackupPatterns {
		matches, e := filepath.Glob(filepath.Join(systemRoot, filepath.FromSlash(pattern)))
		if e != nil {
			return nil, e
		}
		for _, filename := range matches {
			name, e := filepath.Rel(systemRoot, filename)
			if e != nil {
				return nil, e
			}
			if err = add(system, "system", filepath.ToSlash(name)); err != nil {
				return nil, err
			}
		}
	}
	meta := M{"format": 1, "version": Version, "time": time.Now().UTC().Format(time.RFC3339), "files": names}
	if serviceState != nil {
		meta["services"] = serviceState(names)
	}
	w, err := out.Create("manifest.json")
	if err == nil {
		err = json.NewEncoder(w).Encode(meta)
	}
	if err != nil {
		return nil, err
	}
	if err = out.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encryptConfigBackup(raw []byte, key *rsa.PublicKey) ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	wrapped, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, secret, []byte(backupMagic))
	if err != nil {
		return nil, err
	}
	gcm, err := backupAEAD(secret)
	if err != nil {
		return nil, err
	}
	header := binary.BigEndian.AppendUint16([]byte(backupMagic), uint16(len(wrapped)))
	header = append(header, wrapped...)
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	header = append(header, nonce...)
	return append(header, gcm.Seal(nil, nonce, raw, header)...), nil
}

func backupAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func decryptConfigBackup(data []byte, key *rsa.PrivateKey) ([]byte, error) {
	start := len(backupMagic) + 2
	if len(data) < start || !bytes.HasPrefix(data, []byte(backupMagic)) {
		return nil, errors.New("未知备份格式")
	}
	n := int(binary.BigEndian.Uint16(data[start-2 : start]))
	end := start + n + 12
	if n != key.Size() || end+16 > len(data) {
		return nil, errors.New("无效备份或私钥不匹配")
	}
	secret, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, data[start:start+n], []byte(backupMagic))
	if err != nil {
		return nil, errors.New("备份与私钥不匹配")
	}
	gcm, err := backupAEAD(secret)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, data[start+n:end], data[end:], data[:end])
}

func extractConfigBackup(raw []byte, dest string) error {
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	var total uint64
	seen := map[string]bool{}
	if len(z.File) > 2049 {
		return errors.New("无效备份目录")
	}
	for _, file := range z.File {
		if !backupAllowed(file.Name) || !file.Mode().IsRegular() || seen[file.Name] || file.UncompressedSize64 > backupLimit-total {
			return errors.New("无效备份目录")
		}
		total += file.UncompressedSize64
		seen[file.Name] = true
	}
	if !seen["manifest.json"] {
		return errors.New("备份缺少清单")
	}
	if err = os.Mkdir(dest, 0700); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dest)
		}
	}()
	for _, file := range z.File {
		filename := filepath.Join(dest, filepath.FromSlash(file.Name))
		if err = os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			return err
		}
		reader, e := file.Open()
		if e != nil {
			return e
		}
		target, e := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			reader.Close()
			return e
		}
		n, e := io.Copy(target, io.LimitReader(reader, int64(file.UncompressedSize64)+1))
		reader.Close()
		closeErr := target.Close()
		if e != nil || closeErr != nil || n != int64(file.UncompressedSize64) {
			return errors.New("备份文件校验失败")
		}
	}
	complete = true
	return nil
}

func StageConfigBackup(backup, keyPath, dest string) error {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(keyData)
	if block == nil {
		return errors.New("需要 RSA PEM 私钥")
	}
	var key *rsa.PrivateKey
	if block.Type == "RSA PRIVATE KEY" {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if block.Type == "PRIVATE KEY" {
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = parsed.(*rsa.PrivateKey)
	}
	if err != nil || key == nil {
		return errors.New("无法读取 RSA 私钥")
	}
	f, err := os.Open(backup)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, backupLimit+(4<<20)+1))
	if err != nil || len(raw) > backupLimit+(4<<20) {
		return errors.New("备份超出读取限制")
	}
	archive, err := decryptConfigBackup(raw, key)
	if err != nil {
		return err
	}
	return extractConfigBackup(archive, dest)
}

func (a *App) exportConfigBackup(w http.ResponseWriter) {
	raw, err := os.ReadFile(filepath.Join(a.base, backupPublicKey))
	if err != nil {
		writeResponse(w, 409, M{"error": "请先在服务器登记备份公钥"})
		return
	}
	key, err := backupKey(raw)
	if err != nil {
		writeResponse(w, 409, M{"error": err.Error()})
		return
	}
	if !a.mutate.TryLock() {
		writeResponse(w, 409, M{"error": "配置正在变更，请稍后重试"})
		return
	}
	archive, err := buildConfigArchive(a.base, "/", func(files []string) any {
		names := []string{"foxray.service", "foxray-engine.service", "foxray-warp-network.service", "foxray-warp-netns.service", "warp-svc.service", "komari.service", "firewalld.service", "nftables.service", "NetworkManager.service"}
		for _, file := range files {
			if strings.HasPrefix(file, "system/etc/wireguard/") && strings.HasSuffix(file, ".conf") {
				names = append(names, "wg-quick@"+strings.TrimSuffix(path.Base(file), ".conf")+".service")
			}
		}
		result := M{}
		args := append([]string{"systemctl", "show", "--property=Id,ActiveState,UnitFileState", "--"}, names...)
		if info, _ := a.command(8, "", args...); info != "" {
			for _, block := range strings.Split(strings.TrimSpace(info), "\n\n") {
				values := map[string]string{}
				for _, line := range strings.Split(block, "\n") {
					if k, v, ok := strings.Cut(line, "="); ok {
						values[k] = v
					}
				}
				if id := values["Id"]; id != "" {
					result[id] = M{"active": values["ActiveState"], "enabled": values["UnitFileState"]}
				}
			}
		}
		return result
	})
	a.mutate.Unlock()
	if err != nil {
		writeResponse(w, 409, M{"error": err.Error()})
		return
	}
	encrypted, err := encryptConfigBackup(archive, key)
	if err != nil {
		writeResponse(w, 500, M{"error": "无法加密配置备份"})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="foxray-config-`+time.Now().UTC().Format("20060102-150405")+`.foxbackup"`)
	_, _ = w.Write(encrypted)
}
