package gateway

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func dnsFixtureReply(query []byte, truncated bool) []byte {
	if len(query) < 17 {
		return nil
	}
	end := 12
	for end < len(query) && query[end] != 0 {
		end += int(query[end]) + 1
	}
	end += 5
	if end > len(query) {
		return nil
	}
	reply := append([]byte{}, query[:end]...)
	reply[2] = 0x81
	reply[3] = 0x80
	binary.BigEndian.PutUint16(reply[6:8], 1)
	for i := 8; i < 12; i++ {
		reply[i] = 0
	}
	if truncated {
		reply[2] |= 2
		binary.BigEndian.PutUint16(reply[6:8], 0)
		return reply
	}
	return append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 203, 0, 113, 8)
}
func TestDNSDiagnosticsValidateUDPAndTCPResponseInsteadOfDial(t *testing.T) {
	for _, tcpOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "udp", true: "tcp"}[tcpOnly], func(t *testing.T) {
			a, _ := fixture(t)
			tcp, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer tcp.Close()
			udp, err := net.ListenPacket("udp", tcp.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer udp.Close()

			go func() {
				for {
					b := make([]byte, 2048)
					n, addr, e := udp.ReadFrom(b)
					if e != nil {
						return
					}
					time.Sleep(50 * time.Millisecond)
					udp.WriteTo(dnsFixtureReply(b[:n], false), addr)
				}
			}()
			go func() {
				for {
					c, e := tcp.Accept()
					if e != nil {
						return
					}
					go func() {
						defer c.Close()
						c.SetDeadline(time.Now().Add(3 * time.Second))
						head := make([]byte, 2)
						if _, e := io.ReadFull(c, head); e != nil {
							return
						}
						b := make([]byte, binary.BigEndian.Uint16(head))
						if _, e := io.ReadFull(c, b); e != nil {
							return
						}
						time.Sleep(50 * time.Millisecond)
						reply := dnsFixtureReply(b, false)
						binary.BigEndian.PutUint16(head, uint16(len(reply)))
						c.Write(append(head, reply...))
					}()
				}
			}()
			host, port, _ := net.SplitHostPort(udp.LocalAddr().String())
			number, _ := strconv.Atoi(port)
			result, err := measureDiagnosticDNS(context.Background(), host, number, "www.example.com", tcpOnly)
			if err != nil || !yes(result["dns_response_verified"]) || num(result["dns_query_ms"]) < 40 {
				t.Fatal(result, err)
			}
			answers, ok := result["dns_answers"].([]string)
			if !ok || len(answers) != 1 || answers[0] != "203.0.113.8" {
				t.Fatal(result)
			}
			flows := arr(result["probe_connections"])
			if len(flows) == 0 {
				t.Fatal("missing query tuple")
			}
			flow := obj(flows[0])
			network := str(flow["network"])
			if tcpOnly && network != "tcp" {
				t.Fatal(flow)
			}
			a.routes.record(routeEvent{Timestamp: time.Now().Unix(), Inbound: "tun", Outbound: "out-direct", Source: str(flow["source"]), Original: network + ":" + str(flow["target"]), Network: network})
			if _, ok := a.diagnosticRouteCandidates(a.ctx, flows, time.Now().Add(-time.Second)); !ok {
				t.Fatal("DNS route event not correlated")
			}
		})
	}
}
func TestDNSDiagnosticRejectsInvalidNamesAndNoReply(t *testing.T) {
	for _, name := range []string{"https://example.com", "bad name", "example.com:53"} {
		if _, err := measureDiagnosticDNS(context.Background(), "127.0.0.1", 53, name, false); err == nil {
			t.Fatal("invalid query accepted", name)
		}
	}
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	host, port, _ := net.SplitHostPort(udp.LocalAddr().String())
	number, _ := strconv.Atoi(port)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := measureDiagnosticDNS(ctx, host, number, "www.example.com", false)
	if err == nil || yes(result["dns_response_verified"]) {
		t.Fatal("UDP dial counted as valid DNS response", result)
	}
}
