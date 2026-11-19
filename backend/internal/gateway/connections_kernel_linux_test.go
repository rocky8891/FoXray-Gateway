//go:build linux

package gateway

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestKernelBusinessMetersCountRealTCPUDPBothDirections(t *testing.T) {
	a, r := kernelFixture(t)
	peer := r.name + "-peer"
	if _, err := adminRun([]string{"ip", "netns", "add", peer}, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminRun([]string{"ip", "netns", "del", peer}, "") })
	steps := [][]string{
		{"ip", "-n", r.name, "link", "add", "meter0", "type", "veth", "peer", "name", "other0"},
		{"ip", "-n", r.name, "link", "set", "other0", "netns", peer},
		{"ip", "-n", r.name, "addr", "add", "198.18.0.1/30", "dev", "meter0"},
		{"ip", "-n", peer, "addr", "add", "198.18.0.2/30", "dev", "other0"},
		{"ip", "-n", r.name, "addr", "add", "fd42:410::1/126", "dev", "meter0", "nodad"},
		{"ip", "-n", peer, "addr", "add", "fd42:410::2/126", "dev", "other0", "nodad"},
		{"ip", "-n", r.name, "link", "set", "meter0", "up"},
		{"ip", "-n", peer, "link", "set", "other0", "up"},
	}
	for _, step := range steps {
		if _, err := adminRun(step, ""); err != nil {
			t.Fatal(err)
		}
	}
	target := businessTarget{"wireguard", "", "meter0"}
	script := businessRules([]businessTarget{target}, map[string]bool{}, true)
	if _, err := r.Run(context.Background(), []string{"nft", "-f", "-"}, script); err != nil {
		t.Fatal(err)
	}
	server := exec.Command("ip", "netns", "exec", peer, "python3", "-u", "-c", `
import socket,threading,time
def serve(family,kind,host):
 s=socket.socket(family,kind);s.bind((host,32123))
 if kind==socket.SOCK_STREAM:
  s.listen();c,_=s.accept();c.sendall(c.recv(1024));c.close()
 else:
  data,addr=s.recvfrom(1024);s.sendto(data,addr)
 s.close()
for family,host in [(socket.AF_INET,'198.18.0.2'),(socket.AF_INET6,'fd42:410::2')]:
 for kind in [socket.SOCK_STREAM,socket.SOCK_DGRAM]: threading.Thread(target=serve,args=(family,kind,host),daemon=True).start()
print('ready',flush=True)
time.sleep(15)
`)
	pipe, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Process.Kill(); _ = server.Wait() }()
	ready := make([]byte, 6)
	if _, err = pipe.Read(ready); err != nil {
		t.Fatal(err)
	}
	// The readiness marker follows thread creation; bound sockets are retried.
	client := `import socket,time
for family,host in [(socket.AF_INET,'198.18.0.2'),(socket.AF_INET6,'fd42:410::2')]:
 for kind in [socket.SOCK_STREAM,socket.SOCK_DGRAM]:
  for attempt in range(20):
   try:
    s=socket.socket(family,kind);s.settimeout(1);s.connect((host,32123));s.sendall(b'hello-meter');assert s.recv(1024)==b'hello-meter';s.close();break
   except OSError:
    s.close();time.sleep(.05)
  else: raise RuntimeError('echo failed')`
	if out, err := adminRun([]string{"ip", "netns", "exec", r.name, "python3", "-c", client}, ""); err != nil {
		t.Fatal(err, out)
	}
	raw, err := r.Run(context.Background(), []string{"nft", "-j", "list", "table", "inet", businessTable}, "")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseBusinessMeters(raw, []businessTarget{target}, now())
	if err != nil || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if num(row["upload"]) <= 0 || num(row["download"]) <= 0 {
			t.Fatal("bidirectional byte counter missing", row)
		}
		if row["protocol"] == "udp" {
			expected := 39.
			if strings.Contains(str(row["source"]), "fd42:") {
				expected = 59
			}
			if num(row["upload"]) != expected || num(row["download"]) != expected {
				t.Fatal("UDP bytes/tuple incorrect", row)
			}
		}
	}
	// No destructive statements or routing changes exist in the meter batch.
	if strings.Contains(script, "drop") || strings.Contains(script, "mark set") || strings.Contains(script, "flush") {
		t.Fatal("non-passive meter")
	}
	_ = a
	time.Sleep(10 * time.Millisecond)
}
