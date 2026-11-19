package gateway

import (
	"net"
	"strings"
)

// Unconnected UDP sockets have no peer in ss. Correlate their local bind with
// conntrack before associating the peer with a configured node endpoint.
func xrayUDPConnections(socketRaw, trackedRaw string, endpoints map[string][]string) []M {
	binds := map[string]bool{}
	for _, line := range textLines(socketRaw) {
		f := strings.Fields(line)
		if len(f) < 6 || f[0] != "udp" || !strings.Contains(line, `("xray",`) {
			continue
		}
		host, port, e := net.SplitHostPort(f[4])
		if e == nil {
			binds[net.JoinHostPort(strings.SplitN(host, "%", 2)[0], port)] = true
		}
	}
	rows := []M{}
	for _, r := range parseTrackedConnections(trackedRaw, "xray") {
		if str(r["protocol"]) != "udp" {
			continue
		}
		host, port, e := net.SplitHostPort(str(r["source"]))
		if e != nil {
			continue
		}
		if !binds[net.JoinHostPort(host, port)] && !binds[net.JoinHostPort("*", port)] && !binds[net.JoinHostPort("0.0.0.0", port)] && !binds[net.JoinHostPort("::", port)] {
			continue
		}
		ids := uniq(endpoints[str(r["destination"])])
		if len(ids) == 0 {
			continue
		}
		r["node_id"] = ""
		if len(ids) == 1 {
			r["node_id"] = ids[0]
		}
		r["process"] = "Xray"
		r["kind"] = "节点UDP传输连接（内核跟踪）"
		r["attribution"] = "Xray本地UDP套接字与节点入口双向匹配"
		rows = append(rows, r)
	}
	return rows
}
