package gateway

import (
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var ruleDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func validateCustomRules(c M) error {
	rows := arr(c["custom_rules"])
	if len(rows) > 100 {
		return errors.New("自定义规则最多100条")
	}
	for _, v := range rows {
		r := obj(v)
		if _, ok := r["enabled"].(bool); !ok {
			return errors.New("规则启用状态无效")
		}
		action := str(r["action"])
		if action != "direct" && action != "proxy" && action != "block" {
			return errors.New("规则动作无效")
		}
		kind := str(r["type"])
		if kind != "domain" && kind != "ip" {
			return errors.New("规则目标类型无效")
		}
		targets := arr(r["targets"])
		if len(targets) == 0 || len(targets) > 500 {
			return errors.New("每条规则须包含1～500个目标")
		}
		for _, v := range targets {
			s := str(v)
			if kind == "ip" {
				if net.ParseIP(s) == nil {
					if _, _, e := net.ParseCIDR(s); e != nil {
						return errors.New("IP/CIDR 目标无效")
					}
				}
			} else {
				if strings.HasPrefix(s, "regexp:") {
					pattern := strings.TrimPrefix(s, "regexp:")
					if len(pattern) > 1024 {
						return errors.New("域名正则过长")
					}
					if _, err := regexp.Compile(pattern); err != nil {
						return errors.New("域名正则无效")
					}
					continue
				}
				for _, prefix := range []string{"domain:", "full:", "keyword:"} {
					s = strings.TrimPrefix(s, prefix)
				}
				s = strings.TrimPrefix(s, "*.")
				if len(s) > 253 || !ruleDomainPattern.MatchString(s) {
					return errors.New("域名目标无效，仅支持域名、domain/full/keyword 前缀")
				}
			}
		}
		network := str(r["network"])
		if network != "tcp" && network != "udp" && network != "tcp,udp" {
			return errors.New("传输协议无效")
		}
		if s := str(r["port"]); s != "" {
			for _, part := range strings.Split(s, ",") {
				bounds := strings.Split(part, "-")
				if len(bounds) > 2 {
					return errors.New("端口格式无效")
				}
				last := 0
				for _, v := range bounds {
					n, e := strconv.Atoi(v)
					if e != nil || n < 1 || n > 65535 || n < last {
						return errors.New("端口范围无效")
					}
					last = n
				}
			}
		}
	}
	return nil
}
func customEngineRules(c M, selected string) []any {
	out := []any{}
	for _, v := range activeRules(c) {
		r := obj(v)
		if !yes(r["enabled"]) {
			continue
		}
		tag := "out-" + selected
		if str(r["action"]) == "direct" {
			tag = "out-direct"
		} else if str(r["action"]) == "block" {
			tag = "out-block"
		}
		rule := M{"type": "field", "inboundTag": []string{"tun"}, "network": r["network"], "outboundTag": tag}
		if str(r["port"]) != "" {
			rule["port"] = r["port"]
		}
		values := []string{}
		for _, v := range arr(r["targets"]) {
			s := str(v)
			if str(r["type"]) == "domain" && !strings.Contains(s, ":") {
				s = "domain:" + strings.TrimPrefix(s, "*.")
			}
			values = append(values, s)
		}
		if str(r["type"]) == "ip" {
			rule["ip"] = values
		} else {
			rule["domain"] = values
		}
		out = append(out, rule)
	}
	return out
}
