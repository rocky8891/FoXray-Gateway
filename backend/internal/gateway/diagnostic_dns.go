package gateway

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The standard resolver validates DNS transaction IDs and questions, handles
// truncated UDP responses via TCP, and never uses the system resolver here.
func measureDiagnosticDNS(ctx context.Context, target string, port int, query string, tcpOnly bool) (M, error) {
	return measureDiagnosticDNSUsing(ctx, target, port, query, tcpOnly, nil)
}
func measureDiagnosticDNSUsing(ctx context.Context, target string, port int, query string, tcpOnly bool, override func(context.Context, string) (net.Conn, error)) (M, error) {
	result := M{"dns_query_name": query, "dns_server": net.JoinHostPort(target, strconv.Itoa(port)), "dns_response_verified": false}
	query = strings.TrimSuffix(strings.ToLower(query), ".")
	if !safeHostname(query) {
		return result, fmt.Errorf("DNS查询域名无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	started := time.Now()
	var mu sync.Mutex
	flows := []any{}
	resolver := net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		if tcpOnly {
			network = "tcp"
		}
		dial := net.Dialer{Timeout: 4 * time.Second}
		var conn net.Conn
		var err error
		if override != nil {
			conn, err = override(ctx, network)
		} else {
			conn, err = dial.DialContext(ctx, network, str(result["dns_server"]))
		}
		if err == nil {
			mu.Lock()
			flows = append(flows, M{"network": network, "source": conn.LocalAddr().String(), "target": str(result["dns_server"])})
			mu.Unlock()
		}
		return conn, err
	}}
	answers, err := resolver.LookupIP(ctx, "ip4", query+".")
	result["dns_query_ms"] = float64(time.Since(started)) / float64(time.Millisecond)
	mu.Lock()
	result["probe_connections"] = append([]any{}, flows...)
	mu.Unlock()
	if err != nil {
		return result, fmt.Errorf("DNS查询未获得有效A记录应答：%w", err)
	}
	values := []string{}
	for _, ip := range answers {
		values = append(values, ip.String())
	}
	if len(values) == 0 {
		return result, fmt.Errorf("DNS应答不包含A记录")
	}
	result["dns_answers"] = uniq(values)
	result["dns_response_verified"] = true
	return result, nil
}

type dnsDiagnosticTraceKey struct{}
type dnsDiagnosticTrace struct {
	mu      sync.Mutex
	routes  *routeObserver
	flows   []any
	success int
}

func (a *App) measureDiagnosticLANDNS(ctx context.Context, target string, port int, query string, tcpOnly bool) (M, error) {
	trace := &dnsDiagnosticTrace{routes: a.routes, success: -1}
	ctx = context.WithValue(ctx, dnsDiagnosticTraceKey{}, trace)
	network := "udp"
	if tcpOnly {
		network = "tcp"
	}
	result, err := measureDiagnosticDNSUsing(ctx, target, port, query, tcpOnly, func(child context.Context, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			stop := context.AfterFunc(child, func() { server.Close() })
			defer stop()
			if deadline, ok := child.Deadline(); ok {
				server.SetDeadline(deadline)
			}
			size := make([]byte, 2)
			if _, e := io.ReadFull(server, size); e != nil {
				return
			}
			raw := make([]byte, binary.BigEndian.Uint16(size))
			if _, e := io.ReadFull(server, raw); e != nil {
				return
			}
			reply := a.lanDNS.resolve(child, raw, target, network)
			binary.BigEndian.PutUint16(size, uint16(len(reply)))
			server.Write(append(size, reply...))
		}()
		return client, nil
	})
	trace.mu.Lock()
	if trace.success >= 0 && trace.success < len(trace.flows) {
		result["probe_connections"] = []any{trace.flows[trace.success]}
	} else {
		result["probe_connections"] = append([]any{}, trace.flows...)
	}
	trace.mu.Unlock()
	result["dns_routing_mode"] = "LAN智能DNS规则模拟；实际经过网关解析器与上游，不代表客户端实测"
	return result, err
}
