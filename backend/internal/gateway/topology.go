package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Topology is shared by the manager and its standalone network helpers.
type Topology struct {
	Interface string `json:"interface"`
	LAN       string `json:"lan_cidr"`
	DNS       string `json:"upstream_dns"`
}

func defaultRouteInterface(raw string) string {
	selected, best := "", int(^uint(0)>>1)
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" || !safeName.MatchString(fields[0]) {
			continue
		}
		flags, flagErr := strconv.ParseUint(fields[3], 16, 32)
		metric, err := strconv.Atoi(fields[6])
		if flagErr == nil && flags&1 != 0 && err == nil && metric < best {
			selected, best = fields[0], metric
		}
	}
	return selected
}

func usableDNS(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && ip.IsGlobalUnicast()
}

var topologyCache = sync.OnceValues(func() (Topology, error) {
	var t Topology
	data, err := os.ReadFile("/etc/foxray/topology.json")
	if err == nil {
		if err := json.Unmarshal(data, &t); err != nil {
			return t, fmt.Errorf("读取拓扑配置: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return t, err
	}
	if t.Interface == "" {
		routes, _ := os.ReadFile("/proc/net/route")
		t.Interface = defaultRouteInterface(string(routes))
	}
	if t.Interface != "" && (!safeName.MatchString(t.Interface) || t.Interface == "lo") {
		return t, errors.New("无效的出口接口")
	}
	if t.LAN == "" && t.Interface != "" {
		if iface, err := net.InterfaceByName(t.Interface); err == nil {
			addresses, _ := iface.Addrs()
			for _, address := range addresses {
				ip, prefix, err := net.ParseCIDR(address.String())
				if err == nil && ip.To4() != nil && ip.IsGlobalUnicast() {
					t.LAN = prefix.String()
					break
				}
			}
		}
	}
	if t.LAN != "" {
		ip, prefix, err := net.ParseCIDR(t.LAN)
		if err != nil || ip.To4() == nil || prefix.String() == "0.0.0.0/0" {
			return t, errors.New("LAN 网段须为明确的 IPv4 CIDR")
		}
		t.LAN = prefix.String()
	}
	if t.DNS == "" {
		for _, path := range []string{"/etc/resolv.conf", "/run/systemd/resolve/resolv.conf"} {
			raw, _ := os.ReadFile(path)
			for _, line := range strings.Split(string(raw), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 1 && fields[0] == "nameserver" && usableDNS(fields[1]) {
					t.DNS = fields[1]
					break
				}
			}
			if t.DNS != "" {
				break
			}
		}
	} else if !usableDNS(t.DNS) {
		return t, errors.New("上游 DNS 须为非回环单播 IP")
	}
	return t, nil
})

func CheckTopology() (Topology, error) {
	t, err := topologyCache()
	if err != nil {
		return t, err
	}
	iface, err := net.InterfaceByName(t.Interface)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return t, errors.New("无可用 IPv4 默认出口，请核对主路由或 /etc/foxray/topology.json")
	}
	if t.LAN == "" {
		return t, errors.New("出口接口没有可识别的 IPv4 网段，请配置 LAN CIDR")
	}
	return t, nil
}

func currentTopology() Topology {
	t, err := topologyCache()
	if err != nil {
		return Topology{}
	}
	return t
}

func uplink() string { return currentTopology().Interface }

func outboundSocket(lookup bool) M {
	options := M{"mark": 65743}
	if device := uplink(); device != "" {
		options["interface"] = device
	}
	if lookup {
		options["domainStrategy"] = "UseIPv4"
	}
	return options
}
