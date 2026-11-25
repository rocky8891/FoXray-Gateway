package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var certificateName = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

type managementTLSConfig struct {
	Mode  string `json:"mode"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	PEM   string `json:"pem"`
}

type managementTLS struct {
	base, address string
	handler       http.Handler
	mu            sync.Mutex
	config        managementTLSConfig
	server        *http.Server
	busy          bool
	warning       string
	cancel        context.CancelFunc
}

func newManagementTLS(base, address string, handler http.Handler) *managementTLS {
	ctx, cancel := context.WithCancel(context.Background())
	t := &managementTLS{base: base, address: managementPort(address), handler: handler, cancel: cancel}
	file, err := os.ReadFile(t.configFile())
	if err == nil {
		var saved managementTLSConfig
		if json.Unmarshal(file, &saved) != nil {
			t.warning = "HTTPS 配置文件无效"
		} else if e := t.applyLocked(saved, false); e != nil {
			t.warning = e.Error()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.warning = "无法读取 HTTPS 配置"
	}
	go t.scheduleRenewal(ctx)
	return t
}
func (t *managementTLS) configFile() string {
	return filepath.Join(t.base, "private", "management-https.json")
}
func (t *managementTLS) close() {
	t.cancel()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.server != nil {
		_ = t.server.Close()
	}
}

func (t *managementTLS) applyLocked(next managementTLSConfig, persist bool) error {
	var listener net.Listener
	if next.Mode != "" {
		if _, err := tls.X509KeyPair([]byte(next.PEM), []byte(next.PEM)); err != nil {
			return errors.New("证书和私钥格式无效或不匹配")
		}
		if t.server == nil {
			var err error
			listener, err = net.Listen("tcp", t.address)
			if err != nil {
				return errors.New("无法监听 HTTPS 端口 18443")
			}
		}
	}
	if persist {
		raw, err := json.Marshal(next)
		if err == nil {
			err = atomicFile(t.configFile(), raw, 0600)
		}
		if err != nil {
			if listener != nil {
				_ = listener.Close()
			}
			return errors.New("无法保存 HTTPS 配置")
		}
	}
	t.config = next
	t.warning = ""
	if next.Mode == "" && t.server != nil {
		old := t.server
		t.server = nil
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = old.Shutdown(ctx)
		}()
	}
	if listener != nil {
		server := &http.Server{Handler: t.handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384, TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				t.mu.Lock()
				snapshot := t.config
				t.mu.Unlock()
				cert, err := tls.X509KeyPair([]byte(snapshot.PEM), []byte(snapshot.PEM))
				return &cert, err
			},
		}}
		t.server = server
		go func() { _ = server.ServeTLS(listener, "", "") }()
	}
	return nil
}

func selfSignedManagement(name string) (string, error) {
	if net.ParseIP(name) == nil && !certificateName.MatchString(name) {
		return "", errors.New("请输入有效域名或 IP 地址")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", err
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip := net.ParseIP(name); ip != nil {
		cert.IPAddresses = []net.IP{ip}
	} else {
		cert.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) + string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), nil
}

func (t *managementTLS) cloudflareCertificate(ctx context.Context, name, email string) (string, error) {
	if !certificateName.MatchString(name) || !strings.Contains(name, ".") || net.ParseIP(name) != nil || strings.Contains(name, "..") || !strings.Contains(email, "@") || len(email) > 254 {
		return "", errors.New("请输入有效域名和电子邮箱")
	}
	state := filepath.Join(t.base, "private", "acme")
	credentials := filepath.Join(state, "cloudflare.ini")
	info, err := os.Lstat(credentials)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("请先安装 Certbot 的 Cloudflare 插件并配置权限为 0600 的 cloudflare.ini")
	}
	bin, err := exec.LookPath("certbot")
	if err != nil {
		return "", errors.New("未找到 Certbot，请安装 Certbot 和 DNS Cloudflare 插件")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	config := filepath.Join(state, "config")
	certName := "foxray-" + strings.ToLower(name)
	command := exec.CommandContext(ctx, bin, "certonly", "--dns-cloudflare", "--dns-cloudflare-credentials", credentials, "--dns-cloudflare-propagation-seconds", "30", "--config-dir", config, "--work-dir", filepath.Join(state, "work"), "--logs-dir", filepath.Join(state, "logs"), "--non-interactive", "--agree-tos", "--email", email, "--cert-name", certName, "--keep-until-expiring", "-d", name)
	if err = command.Run(); err != nil {
		return "", errors.New("Cloudflare DNS 验证或 ACME 签发失败，请检查域名、API 权限和 Certbot 日志")
	}
	dir := filepath.Join(config, "live", certName)
	cert, err := os.ReadFile(filepath.Join(dir, "fullchain.pem"))
	if err != nil {
		return "", errors.New("找不到已签发证书")
	}
	key, err := os.ReadFile(filepath.Join(dir, "privkey.pem"))
	if err != nil {
		return "", errors.New("找不到已签发证书的私钥")
	}
	return string(cert) + "\n" + string(key), nil
}

func (t *managementTLS) scheduleRenewal(ctx context.Context) {
	timer := time.NewTicker(12 * time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			t.renew(ctx)
		}
	}
}
func (t *managementTLS) renew(ctx context.Context) {
	t.mu.Lock()
	snapshot := t.config
	if t.busy || snapshot.Mode != "cloudflare" {
		t.mu.Unlock()
		return
	}
	t.busy = true
	t.mu.Unlock()
	pem, err := t.cloudflareCertificate(ctx, snapshot.Name, snapshot.Email)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.busy = false
	if err != nil {
		t.warning = err.Error()
		return
	}
	if snapshot.PEM == pem {
		t.warning = ""
		return
	}
	snapshot.PEM = pem
	if err = t.applyLocked(snapshot, true); err != nil {
		t.warning = err.Error()
	}
}

func (t *managementTLS) serve(w http.ResponseWriter, r *http.Request, readonly bool) {
	if r.Method == http.MethodGet {
		t.mu.Lock()
		defer t.mu.Unlock()
		response := M{"enabled": t.server != nil, "mode": t.config.Mode, "domain": t.config.Name, "port": 18443, "busy": t.busy, "warning": t.warning}
		if pair, err := tls.X509KeyPair([]byte(t.config.PEM), []byte(t.config.PEM)); err == nil && len(pair.Certificate) > 0 {
			if cert, err := x509.ParseCertificate(pair.Certificate[0]); err == nil {
				response["expires"] = cert.NotAfter
			}
		}
		writeResponse(w, 200, response)
		return
	}
	if r.Method != http.MethodPost || readonly {
		writeResponse(w, 405, M{"error": "当前不允许修改 HTTPS"})
		return
	}
	input, err := managementInput(w, r, 512*1024)
	if err != nil {
		writeResponse(w, 400, M{"error": "HTTPS 参数无效"})
		return
	}
	mode := str(input["mode"])
	t.mu.Lock()
	if t.busy {
		t.mu.Unlock()
		writeResponse(w, 409, M{"error": "证书操作正在进行"})
		return
	}
	t.busy = true
	t.mu.Unlock()
	defer func() { t.mu.Lock(); t.busy = false; t.mu.Unlock() }()
	next := managementTLSConfig{Mode: mode, Name: strings.TrimSpace(str(input["domain"])), Email: strings.TrimSpace(str(input["email"]))}
	switch mode {
	case "off":
		next = managementTLSConfig{}
	case "self":
		next.PEM, err = selfSignedManagement(next.Name)
	case "import":
		next.PEM = str(input["certificate"]) + "\n" + str(input["key"])
	case "cloudflare":
		next.PEM, err = t.cloudflareCertificate(r.Context(), next.Name, next.Email)
	default:
		err = errors.New("未知证书类型")
	}
	if err == nil {
		t.mu.Lock()
		err = t.applyLocked(next, true)
		t.mu.Unlock()
	}
	if err != nil {
		writeResponse(w, 400, M{"error": err.Error()})
		return
	}
	writeResponse(w, 200, M{"ok": true})
}
