//go:build linux

package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKernelLANDNSTransparentUDPAndTCPWithCoreFallback(t *testing.T) {
	if os.Getenv("FOXRAY_LANDNS_CHILD") == "1" {
		runLANDNSKernelChild(t)
		return
	}
	if os.Getenv("FOXRAY_KERNEL_TESTS") != "1" || os.Getenv("FOXRAY_XRAY_BINARY") == "" {
		t.Skip("isolated kernel and actual Xray test require explicit flags")
	}
	gateway, client := "foxray-dns-g-"+id()[:6], "foxray-dns-c-"+id()[:6]
	run := func(args ...string) {
		t.Helper()
		out, err := adminRun(args, "")
		if err != nil {
			t.Fatal(args, out, err)
		}
	}
	run("ip", "netns", "add", gateway)
	t.Cleanup(func() {
		raw, _ := exec.Command("ip", "netns", "pids", gateway).Output()
		for _, value := range strings.Fields(string(raw)) {
			number, _ := strconv.Atoi(value)
			if number > 0 {
				if process, e := os.FindProcess(number); e == nil {
					process.Kill()
				}
			}
		}
		adminRun([]string{"ip", "netns", "del", gateway}, "")
	})
	run("ip", "netns", "add", client)
	t.Cleanup(func() { adminRun([]string{"ip", "netns", "del", client}, "") })
	gdev, cdev := "fdg"+id()[:5], "fdc"+id()[:5]
	run("ip", "link", "add", gdev, "type", "veth", "peer", "name", cdev)
	run("ip", "link", "set", gdev, "netns", gateway)
	run("ip", "link", "set", cdev, "netns", client)
	run("ip", "-n", gateway, "link", "set", gdev, "name", "ens16")
	run("ip", "-n", gateway, "addr", "add", "192.168.1.1/24", "dev", "ens16")
	run("ip", "-n", gateway, "link", "set", "ens16", "up")
	run("ip", "-n", gateway, "link", "set", "lo", "up")
	run("ip", "-n", client, "addr", "add", "192.168.1.2/24", "dev", cdev)
	run("ip", "-n", client, "link", "set", cdev, "up")
	run("ip", "-n", client, "link", "set", "lo", "up")
	run("ip", "-n", client, "route", "add", "default", "via", "192.168.1.1")
	run("ip", "-n", gateway, "route", "add", "default", "via", "192.168.1.2")
	temp := t.TempDir()
	done := filepath.Join(temp, "finished")
	childTemp := filepath.Join(temp, "child")
	if err := os.Mkdir(childTemp, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", gateway, os.Args[0], "-test.run=^TestKernelLANDNSTransparentUDPAndTCPWithCoreFallback$", "-test.v")
	cmd.Env = append(os.Environ(), "FOXRAY_LANDNS_CHILD=1", "FOXRAY_LANDNS_DONE="+done, "TMPDIR="+childTemp)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(pipe)
	ready := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "DNS_FIXTURE_READY") {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("isolated DNS fixture did not start")
	}
	python := `import socket,struct
for server in ['192.168.1.1','114.114.114.114','8.8.8.8','203.0.113.250']:
 for transport in ['udp','tcp']:
  for name in ['local.test','foreign.test']:
   q=b'\x12\x34\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00'+b''.join(bytes([len(x)])+x.encode() for x in name.split('.'))+b'\x00\x00\x01\x00\x01'
   s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM if transport=='udp' else socket.SOCK_STREAM);s.settimeout(5)
   if transport=='udp':
    s.sendto(q,(server,53));r,peer=s.recvfrom(4096);assert peer==(server,53),(server,peer)
   else:
    s.connect((server,53));s.sendall(struct.pack('!H',len(q))+q)
    def read(n):
     data=b''
     while len(data)<n:
      x=s.recv(n-len(data));assert x;data+=x
     return data
    n=struct.unpack('!H',read(2))[0];r=read(n)
   s.close();assert r[:2]==q[:2] and r[3]&15==0 and r[-4:]==bytes([203,0,113,8 if name=='local.test' or server=='8.8.8.8' else 9]),(server,transport,name,r)
print('16 transparent queries passed; arbitrary resolver replies preserve original address')`
	outBytes, err := exec.Command("ip", "netns", "exec", client, "python3", "-c", python).CombinedOutput()
	out := string(outBytes)
	if err != nil {
		t.Fatal(out, err)
	}
	t.Log(strings.TrimSpace(out))
	if err = os.WriteFile(done, []byte("done"), 0600); err != nil {
		t.Fatal(err)
	}
	for scanner.Scan() {
		t.Log(scanner.Text())
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatal("isolated child failed", err)
	}
}
func runLANDNSKernelChild(t *testing.T) {
	a, _ := fixture(t)
	if out, err := adminRun([]string{"sysctl", "-qw", "net.ipv4.conf.all.rp_filter=2"}, ""); err != nil {
		t.Fatal(out, err)
	}
	a.applied = true
	a.cfg["enabled"] = true
	a.cfg["capture_lan"] = true
	a.cfg["lan_dns_intercept"] = true
	a.cfg["dns_direct_servers"] = []any{"127.0.0.4", "127.0.0.2"}
	a.cfg["dns_proxy_servers"] = []any{"127.0.0.4", "127.0.0.3"}
	a.cfg["custom_rules"] = []any{M{"enabled": true, "type": "ip", "network": "tcp,udp", "port": "53", "targets": []any{"8.8.8.8/32"}, "action": "direct"}}
	if err := os.MkdirAll(filepath.Join(a.base, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	writeJSON(filepath.Join(a.base, "rules", "gfwlist_domains.json"), M{"direct": []any{"local.test"}, "proxy": []any{"foreign.test"}})
	for _, address := range []string{"127.0.0.2:53", "127.0.0.3:53"} {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		last := byte(8)
		if strings.Contains(address, "0.3:") {
			last = 9
		}
		go func(listener net.Listener, last byte) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				go func() {
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(3 * time.Second))
					head := make([]byte, 2)
					if _, err := io.ReadFull(conn, head); err != nil {
						return
					}
					raw := make([]byte, binary.BigEndian.Uint16(head))
					if _, err := io.ReadFull(conn, raw); err != nil {
						return
					}
					reply := dnsFixtureReply(raw, false)
					reply[len(reply)-1] = last
					binary.BigEndian.PutUint16(head, uint16(len(reply)))
					conn.Write(append(head, reply...))
				}()
			}
		}(listener, last)
	}

	a.routes.setDomainCollection(true)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event routeEvent
		if json.NewDecoder(r.Body).Decode(&event) != nil {
			w.WriteHeader(400)
			return
		}
		a.routes.mu.RLock()
		_, expected := a.routes.probes[eventKey(event)]
		a.routes.mu.RUnlock()
		if !expected && (event.Inbound == "smartdns-direct" || event.Inbound == "smartdns-proxy") {
			event.Inbound = "tun"
		}
		if !a.routes.record(event) {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	targetHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer targetHTTP.Close()
	targetAddress := strings.TrimPrefix(targetHTTP.URL, "http://")
	targetHost, targetPort, _ := net.SplitHostPort(targetAddress)
	portNumber, _ := strconv.Atoi(targetPort)
	reserve, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	inboundAddress := reserve.Addr().String()
	_, inboundPort, _ := net.SplitHostPort(inboundAddress)
	inboundNumber, _ := strconv.Atoi(inboundPort)
	reserve.Close()
	engine := M{"log": M{"loglevel": "none", "access": "none", "error": "none"}, "inbounds": []any{}, "outbounds": []any{M{"tag": "out-direct", "protocol": "freedom", "settings": M{}, "streamSettings": M{"sockopt": M{"mark": 65743}}}, M{"tag": "out-n1", "protocol": "freedom", "settings": M{}, "streamSettings": M{"sockopt": M{"mark": 65743}}}}, "routing": M{"rules": []any{}}}
	engine["inbounds"] = []any{M{"tag": "tun", "listen": "127.0.0.1", "port": inboundNumber, "protocol": "dokodemo-door", "settings": M{"address": targetHost, "port": portNumber, "network": "tcp"}, "sniffing": M{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "metadataOnly": false, "routeOnly": true}}}
	configureLANResolverEngine(engine, "n1")
	obj(engine["routing"])["rules"] = append(arr(obj(engine["routing"])["rules"]), M{"type": "field", "inboundTag": []string{"tun"}, "outboundTag": "out-direct"})
	for _, r := range arr(obj(engine["routing"])["rules"]) {
		obj(r)["webhook"] = M{"url": receiver.URL + "/routing", "deduplication": 0}
	}
	path := filepath.Join(a.base, "core.json")
	if err := writeJSON(path, engine); err != nil {
		t.Fatal(err)
	}
	core := exec.Command(os.Getenv("FOXRAY_XRAY_BINARY"), "run", "-c", path)
	core.Stdout = io.Discard
	core.Stderr = io.Discard
	if err := core.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { core.Process.Kill(); core.Wait() }()
	for deadline := time.Now().Add(5 * time.Second); ; {
		c, e := net.DialTimeout("tcp", "127.0.0.1:16953", 100*time.Millisecond)
		if e == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("core DNS proxy not listening")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := a.lanDNS.start(); err != nil {
		t.Fatal(err)
	}
	defer a.lanDNS.stop()
	text, err := a.captureRulesForEngine(a.cfg, M{}, "65534")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := adminRun([]string{"nft", "-f", "-"}, text); err != nil {
		t.Fatal(out, err)
	}
	defer adminRun([]string{"nft", "delete", "table", "inet", "vpm"}, "")
	for _, domain := range []string{"web.example.test", "tls.example.test"} {
		conn, e := net.DialTimeout("tcp", inboundAddress, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		source := conn.LocalAddr().String()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if strings.HasPrefix(domain, "web.") {
			fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", domain)
			response, e := http.ReadResponse(bufio.NewReader(conn), nil)
			if e != nil || response.StatusCode != 204 {
				t.Fatal("HTTP sniff fixture failed", e)
			}
			response.Body.Close()
		} else {
			client := tls.Client(conn, &tls.Config{ServerName: domain})
			_ = client.Handshake()
			client.Close()
		}
		conn.Close()
		key := socketMetadataKey("tcp", source, targetAddress)
		for deadline := time.Now().Add(2 * time.Second); ; {
			if a.routes.domainHints()[key].domain == domain {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("actual core did not emit sniffed domain", domain, a.routes.domainHints())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	fmt.Println("DNS_FIXTURE_READY")
	for deadline := time.Now().Add(30 * time.Second); ; {
		if _, err := os.Stat(os.Getenv("FOXRAY_LANDNS_DONE")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("parent did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, tcpOnly := range []bool{false, true} {
		started := time.Now()
		result, err := a.measureDiagnosticLANDNS(a.ctx, "114.114.114.114", 53, "foreign.test", tcpOnly)
		if err != nil || !yes(result["dns_response_verified"]) {
			t.Fatal("smart LAN DNS diagnostic failed", result, err)
		}
		event, ok := a.diagnosticRouteCandidates(a.ctx, arr(result["probe_connections"]), started)
		if !ok || event.Inbound != "smartdns-proxy" || event.Outbound != "out-n1" {
			t.Fatal("smart DNS diagnostic not confirmed by actual core", event, ok)
		}
		path := filepath.Join(a.base, "engine.json")
		writeJSON(path, engine)
		node := a.observedDiagnosticNode(event)
		if !strings.Contains(diagnosticObservedPath(node, "foreign.test", 53), "LAN DNS解析器") {
			t.Fatal("smart DNS path mislabeled as TUN")
		}
	}
	for _, tcpOnly := range []bool{false, true} {
		result, err := measureDiagnosticDNS(a.ctx, "203.0.113.250", 53, "foreign.test", tcpOnly)
		if err != nil || !yes(result["dns_response_verified"]) {
			t.Fatal("host DNS bypassed smart resolver", result, err, a.lanDNS.status())
		}
	}
	if num(a.lanDNS.status()["answers"]) != 20 || num(a.lanDNS.status()["failovers"]) < 2 {
		t.Fatal("fallback or answers missing", a.lanDNS.status())
	}
	tags := map[string]bool{}
	for _, tag := range a.routes.hints() {
		tags[tag] = true
	}
	if !tags["out-direct"] || !tags["out-n1"] {
		t.Fatal("actual core did not confirm both DNS paths", tags)
	}
}

func TestKernelLANDNSRulesLoadInIsolatedNamespace(t *testing.T) {
	a, _ := kernelFixture(t)
	c := clone(a.cfg)
	c["capture_lan"] = true
	c["lan_dns_intercept"] = true
	text, err := a.captureRulesForEngine(c, M{}, "0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.command(5, text, "nft", "--check", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.command(5, text, "nft", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.command(5, "", "nft", "delete", "table", "inet", "vpm"); err != nil {
		t.Fatal(err)
	}
}
