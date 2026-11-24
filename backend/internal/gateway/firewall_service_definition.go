package gateway

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

type firewallServiceDefinition struct {
	Ports []struct {
		Protocol string `xml:"protocol,attr"`
		Port     string `xml:"port,attr"`
	} `xml:"port"`
	Protocols []struct {
		Value string `xml:"value,attr"`
	} `xml:"protocol"`
	Includes []struct {
		Service string `xml:"service,attr"`
	} `xml:"include"`
}

// Disk definitions are for display. Diagnostics use the live service definition.
func firewallServiceDisplayPorts(name string, visiting map[string]bool) []string {
	if !firewallZoneName.MatchString(name) || visiting[name] || len(visiting) >= 16 {
		return nil
	}
	visiting[name] = true
	defer delete(visiting, name)
	for _, dir := range []string{"/etc/firewalld/services", "/usr/lib/firewalld/services"} {
		raw, err := os.ReadFile(filepath.Join(dir, name+".xml"))
		if os.IsNotExist(err) {
			continue
		}
		var definition firewallServiceDefinition
		if err != nil || xml.Unmarshal(raw, &definition) != nil {
			return nil
		}
		var values []string
		for _, p := range definition.Ports {
			values = append(values, strings.ToUpper(p.Protocol)+" / "+p.Port)
		}
		for _, proto := range definition.Protocols {
			values = append(values, strings.ToUpper(proto.Value))
		}
		for _, include := range definition.Includes {
			values = append(values, firewallServiceDisplayPorts(include.Service, visiting)...)
		}
		return uniq(values)
	}
	return nil
}

func firewallServiceMatch(details M, f firewallFlow) (matches, known bool) {
	for _, field := range []string{"ports", "protocols", "source-ports", "includes", "destination"} {
		if _, ok := details[field]; !ok {
			return false, false
		}
	}
	// These constraints require a fuller evaluation than the current input permits.
	constrained := str(details["source-ports"]) != "" || str(details["includes"]) != "" || str(details["destination"]) != ""
	for _, value := range strings.Fields(str(details["ports"])) {
		port, proto, ok := strings.Cut(value, "/")
		a, b, valid := firewallPortBounds(port)
		if ok && valid && proto == f.protocol && f.port >= a && f.port <= b {
			return !constrained, !constrained
		}
	}
	protocol := f.protocol
	if protocol == "icmp" && f.source.Is6() {
		protocol = "ipv6-icmp"
	}
	for _, value := range strings.Fields(str(details["protocols"])) {
		if value == protocol {
			return !constrained, !constrained
		}
	}
	return false, !constrained
}

func firewallActiveInterfaceZone(active, name string) string {
	zone := ""
	for _, line := range strings.Split(active, "\n") {
		value := strings.TrimSpace(line)
		if value != "" && value == line {
			zone = strings.Fields(value)[0]
			continue
		}
		interfaces, ok := strings.CutPrefix(value, "interfaces:")
		if !ok || !firewallZoneName.MatchString(zone) {
			continue
		}
		for _, device := range strings.Fields(interfaces) {
			if device == name {
				return zone
			}
		}
	}
	return ""
}
