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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var certificateName = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

type managementTLSConfig struct {
	Mode        string `json:"mode"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	PEM         string `json:"pem"`
	Port        int    `json:"port,omitempty"`
	Redirect    int    `json:"redirect_port,omitempty"`
	Description string `json:"description,omitempty"`
}

type managementTLS struct {
	base, address, httpAddress string
	handler                    http.Handler
	mu                         sync.Mutex
	config                     managementTLSConfig
	server                     *http.Server
	redirectServer             *http.Server
	busy                       bool
	warning                    string
	cancel                     context.CancelFunc
}

func newManagementTLS(base, address string, handler http.Handler) *managementTLS {
	ctx, cancel := context.WithCancel(context.Background())
	t := &managementTLS{base: base, address: managementPort(address), httpAddress: address, handler: handler, cancel: cancel}
	t.mu.Lock()
	defer t.mu.Unlock()
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
	if t.redirectServer != nil {
		_ = t.redirectServer.Close()
	}
}

func (t *managementTLS) httpsAddress(port int) string {
	if port == 0 {
		return t.address
	}
	host, _, _ := net.SplitHostPort(t.address)
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func (t *managementTLS) applyLocked(next managementTLSConfig, persist bool) error {
	var tlsListener, redirectListener net.Listener
	oldTLS, oldRedirect := t.server, t.redirectServer
	httpHost, httpPortStr, _ := net.SplitHostPort(t.httpAddress)
	httpPort, _ := strconv.Atoi(httpPortStr)
	nextAddress := t.httpsAddress(next.Port)
	if next.Mode != "" {
		_, portStr, _ := net.SplitHostPort(nextAddress)
		port, _ := strconv.Atoi(portStr)
		if port == httpPort || port == next.Redirect || next.Redirect < 0 || next.Redirect > 65535 || next.Port < 0 || next.Port > 65535 {
			return errors.New("HTTPS 端口和 HTTP 端口必须不同")
		}
		pair, err := tls.X509KeyPair([]byte(next.PEM), []byte(next.PEM))
		if err != nil || len(pair.Certificate) == 0 {
			return errors.New("证书和私钥格式无效或不匹配")
		}
		if oldTLS == nil || nextAddress != t.httpsAddress(t.config.Port) {
			tlsListener, err = net.Listen("tcp", nextAddress)
			if err != nil {
				return errors.New("无法监听指定的 HTTPS 端口")
			}
		}
		if next.Redirect != 0 && next.Redirect != httpPort && (oldRedirect == nil || next.Redirect != t.config.Redirect) {
			redirectListener, err = net.Listen("tcp", net.JoinHostPort(httpHost, strconv.Itoa(next.Redirect)))
			if err != nil {
				if tlsListener != nil {
					_ = tlsListener.Close()
				}
				return errors.New("无法监听指定的 HTTP 跳转端口")
			}
		}
	}
	if persist {
		raw, err := json.Marshal(next)
		if err == nil {
			err = atomicFile(t.configFile(), raw, 0600)
		}
		if err != nil {
			if tlsListener != nil {
				_ = tlsListener.Close()
			}
			if redirectListener != nil {
				_ = redirectListener.Close()
			}
			return errors.New("无法保存 HTTPS 配置")
		}
	}
	t.config = next
	t.warning = ""
	if next.Mode == "" {
		t.server = nil
	}
	if next.Mode == "" || next.Redirect == 0 || next.Redirect == httpPort {
		t.redirectServer = nil
	}
	if tlsListener != nil {
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
		go func() { _ = server.ServeTLS(tlsListener, "", "") }()
	}
	if redirectListener != nil {
		server := &http.Server{Handler: t.handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
		t.redirectServer = server
		go func() { _ = server.Serve(redirectListener) }()
	}
	for _, server := range []*http.Server{oldTLS, oldRedirect} {
		if server != nil && server != t.server && server != t.redirectServer {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if server.Shutdown(ctx) != nil {
					_ = server.Close()
				}
			}()
		}
	}
	return nil
}

func (t *managementTLS) handlePlainHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.TLS != nil {
		return false
	}
	t.mu.Lock()
	enabled := t.server != nil
	cfg := t.config
	t.mu.Unlock()
	if !enabled {
		return false
	}
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	port := 0
	if local != nil {
		_, value, _ := net.SplitHostPort(local.String())
		port, _ = strconv.Atoi(value)
	}
	host := r.URL.Hostname()
	if host == "" {
		host = (&url.URL{Host: r.Host}).Hostname()
	}
	if net.ParseIP(host) == nil && !certificateName.MatchString(host) {
		host = cfg.Name
	}
	_, httpsPort, _ := net.SplitHostPort(t.httpsAddress(cfg.Port))
	if cfg.Redirect != 0 && port == cfg.Redirect && (r.Method == http.MethodGet || r.Method == http.MethodHead) && host != "" {
		target := url.URL{Scheme: "https", Host: net.JoinHostPort(host, httpsPort), Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		http.Redirect(w, r, target.String(), http.StatusTemporaryRedirect)
		return true
	}
	writeResponse(w, http.StatusUpgradeRequired, M{"error": "请使用 HTTPS 管理地址", "https_port": httpsPort})
	return true
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

func (t *managementTLS) acmeCertificate(ctx context.Context, mode, name, email string) (string, error) {
	if !certificateName.MatchString(name) || !strings.Contains(name, ".") || net.ParseIP(name) != nil || strings.Contains(name, "..") || !strings.Contains(email, "@") || len(email) > 254 {
		return "", errors.New("请输入有效域名和电子邮箱")
	}
	state := filepath.Join(t.base, "private", "acme")
	bin, err := exec.LookPath("certbot")
	if err != nil {
		return "", errors.New("未找到 Certbot，请先安装 Certbot")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	config := filepath.Join(state, "config")
	certName := "foxray-" + strings.ToLower(name)
	args := []string{"certonly", "--server", "https://acme-v02.api.letsencrypt.org/directory", "--config-dir", config, "--work-dir", filepath.Join(state, "work"), "--logs-dir", filepath.Join(state, "logs"), "--non-interactive", "--agree-tos", "--email", email, "--cert-name", certName, "--keep-until-expiring", "-d", name}
	if mode == "cloudflare" {
		credentials := filepath.Join(state, "cloudflare.ini")
		info, err := os.Lstat(credentials)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", errors.New("请配置权限为 0600 的 cloudflare.ini 并安装 Certbot Cloudflare 插件")
		}
		args = append(args, "--dns-cloudflare", "--dns-cloudflare-credentials", credentials, "--dns-cloudflare-propagation-seconds", "30")
	} else {
		webroot := filepath.Join(state, "webroot")
		if err := os.MkdirAll(webroot, 0700); err != nil {
			return "", errors.New("无法创建 HTTP 验证目录")
		}
		_, initialPort, _ := net.SplitHostPort(t.httpAddress)
		t.mu.Lock()
		alreadyListening := initialPort == "80" || t.config.Redirect == 80 && t.redirectServer != nil
		t.mu.Unlock()
		if !alreadyListening {
			host, _, _ := net.SplitHostPort(t.httpAddress)
			listener, err := net.Listen("tcp", net.JoinHostPort(host, "80"))
			if err != nil {
				return "", errors.New("80 端口已被占用，无法启动 HTTP 验证")
			}
			challenge := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !t.serveChallenge(w, r) {
					http.NotFound(w, r)
				}
			}), ReadHeaderTimeout: 5 * time.Second}
			go func() { _ = challenge.Serve(listener) }()
			defer challenge.Close()
		}
		args = append(args, "--webroot", "--webroot-path", webroot)
	}
	if err := exec.CommandContext(ctx, bin, args...).Run(); err != nil {
		return "", errors.New("Let’s Encrypt 签发失败，请检查域名、验证条件及 Certbot 日志")
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

var challengeToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func (t *managementTLS) serveChallenge(w http.ResponseWriter, r *http.Request) bool {
	token, matched := strings.CutPrefix(r.URL.Path, "/.well-known/acme-challenge/")
	if !matched {
		return false
	}
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || !challengeToken.MatchString(token) {
		http.NotFound(w, r)
		return true
	}
	root, err := os.OpenRoot(filepath.Join(t.base, "private", "acme", "webroot", ".well-known", "acme-challenge"))
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	defer root.Close()
	data, err := readBackupFile(root, token, 4096)
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
	return true
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
	if t.busy || snapshot.Mode != "cloudflare" && snapshot.Mode != "acme-http" {
		t.mu.Unlock()
		return
	}
	t.busy = true
	t.mu.Unlock()
	pem, err := t.acmeCertificate(ctx, snapshot.Mode, snapshot.Name, snapshot.Email)
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
		port := t.config.Port
		if port == 0 {
			port = 18443
		}
		response := M{"enabled": t.server != nil, "mode": t.config.Mode, "domain": t.config.Name, "port": port, "redirect_port": t.config.Redirect, "description": t.config.Description, "busy": t.busy, "warning": t.warning}
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
	next := managementTLSConfig{Mode: mode, Name: strings.TrimSpace(str(input["domain"])), Email: strings.TrimSpace(str(input["email"])), Description: strings.TrimSpace(str(input["description"]))}
	if mode != "off" {
		next.Port, err = strconv.Atoi(str(input["port"]))
		if err != nil || next.Port < 1 || next.Port > 65535 {
			writeResponse(w, 400, M{"error": "HTTPS 端口应为 1～65535"})
			return
		}
		if value := str(input["redirect_port"]); value != "" {
			next.Redirect, err = strconv.Atoi(value)
			if err != nil || next.Redirect < 1 || next.Redirect > 65535 {
				writeResponse(w, 400, M{"error": "HTTP 跳转端口应为 1～65535"})
				return
			}
		}
	}
	switch mode {
	case "keep":
		t.mu.Lock()
		previous := t.config
		t.mu.Unlock()
		if previous.Mode == "" {
			writeResponse(w, 400, M{"error": "没有可保留的证书"})
			return
		}
		previous.Port = next.Port
		previous.Redirect = next.Redirect
		next = previous
	}

	switch mode {
	case "keep":
	case "off":
		next = managementTLSConfig{}
	case "self":
		next.PEM, err = selfSignedManagement(next.Name)
	case "import":
		next.PEM = str(input["certificate"]) + "\n" + str(input["key"])
	case "cloudflare", "acme-http":
		next.PEM, err = t.acmeCertificate(r.Context(), mode, next.Name, next.Email)
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
	_, httpPortText, _ := net.SplitHostPort(t.httpAddress)
	httpPort, _ := strconv.Atoi(httpPortText)
	writeResponse(w, 200, M{"ok": true, "enabled": next.Mode != "", "port": next.Port, "http_port": httpPort})
}
