package gateway

import (
	"errors"
	"net"
	"strings"
)

var defaultCaptureExclusions = []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/3"}

func defaultCaptureBypass(address string) bool {
	ip := net.ParseIP(address)
	if ip == nil || ip.To4() == nil {
		return true
	}
	for _, value := range defaultCaptureExclusions {
		_, subnet, _ := net.ParseCIDR(value)
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}
func defaultVPNExclusions() M {
	return M{"mode": "default", "interfaces": []any{}, "cidrs": []any{}, "domains": []any{}}
}
func normalizeVPNExclusions(value any) (M, error) {
	p := defaultVPNExclusions()
	if value == nil {
		return p, nil
	}
	input, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("VPN排除配置无效")
	}
	for key, v := range input {
		if _, ok := p[key]; !ok {
			return nil, errors.New("VPN排除包含未知字段")
		}
		p[key] = v
	}
	if mode := str(p["mode"]); mode != "default" && mode != "custom" {
		return nil, errors.New("VPN排除模式无效")
	}
	for _, key := range []string{"interfaces", "cidrs", "domains"} {
		rows, ok := p[key].([]any)
		if !ok || len(rows) > 100 {
			return nil, errors.New("VPN排除每类最多100项")
		}
		clean := []any{}
		seen := map[string]bool{}
		for _, v := range rows {
			s, ok := v.(string)
			if !ok {
				return nil, errors.New("VPN排除内容必须是文本")
			}
			s = strings.TrimSpace(s)
			switch key {
			case "interfaces":
				if iface(s) != nil {
					return nil, errors.New("VPN排除接口名称无效")
				}
			case "cidrs":
				_, subnet, err := net.ParseCIDR(s)
				if err != nil {
					return nil, errors.New("VPN排除须填写有效CIDR")
				}
				s = subnet.String()
			case "domains":
				prefix := "domain:"
				if strings.HasPrefix(s, "full:") {
					prefix = "full:"
					s = strings.TrimPrefix(s, "full:")
				} else {
					s = strings.TrimPrefix(strings.TrimPrefix(s, "domain:"), "*.")
				}
				if !safeHostname(s) || net.ParseIP(s) != nil {
					return nil, errors.New("VPN排除域名无效；支持example.com、*.example.com或full:example.com")
				}
				s = prefix + strings.ToLower(s)
			}
			if !seen[s] {
				clean = append(clean, s)
				seen[s] = true
			}
		}
		p[key] = clean
	}
	return p, nil
}
func customVPNExclusions(c M) M {
	p, err := normalizeVPNExclusions(c["vpn_exclusions"])
	if err != nil || p["mode"] != "custom" {
		return defaultVPNExclusions()
	}
	return p
}
func vpnExcludedTarget(c M, host, address string) bool {
	p := customVPNExclusions(c)
	ip := net.ParseIP(address)
	for _, v := range arr(p["cidrs"]) {
		_, subnet, err := net.ParseCIDR(str(v))
		if err == nil && subnet.Contains(ip) {
			return true
		}
	}
	for _, v := range arr(p["domains"]) {
		s := str(v)
		name := strings.TrimPrefix(strings.TrimPrefix(s, "domain:"), "full:")
		if strings.EqualFold(host, name) || !strings.HasPrefix(s, "full:") && strings.HasSuffix(strings.ToLower(host), "."+name) {
			return true
		}
	}
	return false
}
