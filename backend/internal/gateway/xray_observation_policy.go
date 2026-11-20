package gateway

import (
	"errors"
	"net"
)

// These are explicit resolver addresses, never a GeoIP inference.
func defaultDirectDNSServers() []any {
	return []any{"114.114.114.114", "114.114.115.115", "223.5.5.5", "223.6.6.6", "119.29.29.29"}
}
func defaultProxyDNSServers() []any { return []any{"1.1.1.1", "8.8.8.8"} }
func directDNSServers(c M) ([]string, error) {
	return resolverServers(c, "dns_direct_servers", defaultDirectDNSServers())
}
func resolverServers(c M, key string, defaults []any) ([]string, error) {
	raw, exists := c[key]
	if !exists {
		raw = defaults
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, errors.New("直连DNS服务器必须为IP地址列表")
	}
	if len(values) > 32 {
		return nil, errors.New("直连DNS服务器最多32项")
	}
	result := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		s, ok := v.(string)
		ip := net.ParseIP(s)
		if !ok || ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, errors.New("直连DNS服务器须为有效单播IP，不支持域名、端口或网段")
		}
		if !seen[ip.String()] {
			result = append(result, ip.String())
			seen[ip.String()] = true
		}
	}
	return result, nil
}
func validateObservationPolicy(c M) error {
	if _, err := directDNSServers(c); err != nil {
		return err
	}
	for _, key := range []string{"domain_identification", "dns_force_proxy", "lan_dns_intercept"} {
		if v, exists := c[key]; exists {
			if _, ok := v.(bool); !ok {
				return errors.New("域名识别和DNS强制代理开关必须为布尔值")
			}
		}
	}
	if _, err := resolverServers(c, "dns_proxy_servers", defaultProxyDNSServers()); err != nil {
		return err
	}
	if yes(c["lan_dns_intercept"]) {
		proxy, _ := resolverServers(c, "dns_proxy_servers", defaultProxyDNSServers())
		if len(proxy) == 0 {
			return errors.New("智能DNS至少配置一个代理上游")
		}
	}
	return nil
}
