package gateway

import (
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var socketApplication = regexp.MustCompile(`users:\(\("([^"\r\n]{1,64})",pid=`)
var accessDomain = regexp.MustCompile(` from (\S+) accepted (tcp|udp):([^\s]+)`)

// A domain comes only from an existing access record, never reverse DNS or an
// IP/port guess. The key must be unique within a short observation window.
func parseAccessDomains(raw string, stamp float64) map[string]string {
	result, ambiguous := map[string]string{}, map[string]bool{}
	for _, line := range textLines(raw) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		date, err := time.ParseInLocation("2006/01/02 15:04:05", strings.Split(fields[0]+" "+fields[1], ".")[0], time.Local)
		if err != nil || stamp-float64(date.Unix()) > 60 || float64(date.Unix()) > stamp+2 {
			continue
		}
		matches := accessDomain.FindStringSubmatch(line)
		if len(matches) != 4 {
			continue
		}
		source := strings.TrimPrefix(strings.TrimPrefix(matches[1], "tcp:"), "udp:")
		host, port, err := net.SplitHostPort(matches[3])
		if err != nil || net.ParseIP(host) != nil || !safeHostname(host) {
			continue
		}
		key := matches[2] + "|" + source + "|" + port
		if previous := result[key]; previous != "" && previous != host {
			ambiguous[key] = true
		}
		result[key] = host
	}
	for key := range ambiguous {
		delete(result, key)
	}
	return result
}
func safeHostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return strings.Contains(s, ".")
}
func (m *ConnectionMonitor) existingDomainHints(stamp float64) map[string]string {
	if stamp-m.metadataAt < 5 {
		return m.domainHints
	}
	m.metadataAt = stamp
	m.domainHints = map[string]string{}
	m.routeHints = map[string]string{}
	m.domainHintStatus = "Xray核心未配置可读取的文件访问日志；未自动开启日志或重载核心"
	// The manager's generated config normally disables access logging. Reading an
	// existing configured log does not enable logging or reload the core.
	var log struct {
		Access string `json:"access"`
	}
	if readEngineField(filepath.Join(m.app.base, "engine.json"), "log", &log) != nil {
		return m.domainHints
	}
	path := log.Access
	if !filepath.IsAbs(path) || path == "/dev/stdout" || path == "/dev/stderr" {
		return m.domainHints
	}
	m.domainHintStatus = "无法读取已有Xray文件访问日志；请检查日志文件和读取权限"
	f, err := os.Open(path)
	if err != nil {
		return m.domainHints
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return m.domainHints
	}
	start := max(int64(0), info.Size()-256*1024)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return m.domainHints
	}
	raw, err := io.ReadAll(io.LimitReader(f, 256*1024))
	if err != nil {
		return m.domainHints
	}
	if start > 0 {
		if index := strings.IndexByte(string(raw), '\n'); index >= 0 {
			raw = raw[index+1:]
		}
	}
	m.domainHints = parseAccessDomains(string(raw), stamp)
	m.routeHints = parseAccessRoutes(string(raw), stamp)
	m.domainHintStatus = "已读取已有Xray访问日志；近60秒内无来源、协议和目标端口唯一匹配的域名证据"
	return m.domainHints
}
func enrichConnectionMetadata(rows []M, sockets string, domains map[string]string) {
	applications := map[string]string{}
	for _, line := range textLines(sockets) {
		f := strings.Fields(line)
		match := socketApplication.FindStringSubmatch(line)
		if len(f) < 6 || len(match) != 2 {
			continue
		}
		key := socketMetadataKey(f[0], f[4], f[5])
		if old, found := applications[key]; found && old != match[1] {
			applications[key] = ""
		} else if !found {
			applications[key] = match[1]
		}
	}
	for _, r := range rows {
		if yes(r["_deferred"]) {
			continue
		}
		key := socketMetadataKey(str(r["protocol"]), str(r["source"]), str(r["destination"]))
		r["application"] = ""
		r["application_note"] = "转发流量无法从网关确定客户端应用；未进行端口猜测"
		if app := applications[key]; app != "" {
			r["application"] = app
			r["application_note"] = "本机套接字五元组精确匹配；仅进程名"
		}
		r["domain"] = ""
		r["domain_note"] = "无匹配域名证据；仅凭目标IP无法可靠确定域名"
		_, port, err := net.SplitHostPort(str(r["destination"]))
		if err == nil && r["module"] == "xray" {
			if domain := domains[str(r["protocol"])+"|"+str(r["source"])+"|"+port]; domain != "" {
				r["domain"] = domain
				r["domain_note"] = "已有Xray访问日志：60秒内来源、协议和目标端口唯一匹配；域名证据，不代表节点归属"
			}
		}
	}
}

func socketMetadataKey(protocol, source, destination string) string {
	normalize := func(s string) string {
		host, port, err := net.SplitHostPort(s)
		if err != nil { // ss can omit brackets around IPv6 endpoints.
			i := strings.LastIndex(s, ":")
			if i < 0 {
				return s
			}
			host, port = s[:i], s[i+1:]
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return s
		}
		return net.JoinHostPort(ip.Unmap().String(), port)
	}
	return strings.ToLower(protocol) + "|" + normalize(source) + "|" + normalize(destination)
}
