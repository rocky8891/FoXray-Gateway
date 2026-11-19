package gateway

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"time"
)

func (a *App) wgMonitorPolicy() M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p := M{"enabled": false, "interval": 60, "count": 3, "size": 56, "timeout": 2, "targets": M{}}
	for k, v := range obj(a.settings["wireguard_monitor"]) {
		p[k] = v
	}
	return p
}
func (a *App) saveWGMonitor(p M) error {
	for k := range p {
		if k != "enabled" && k != "interval" && k != "count" && k != "size" && k != "timeout" && k != "targets" && k != "auto_targets" {
			return errors.New("探测参数包含未知字段")
		}
	}

	if _, ok := p["enabled"].(bool); !ok {
		return errors.New("启用参数无效")
	}
	for _, f := range []struct {
		k        string
		min, max int
	}{{"interval", 10, 3600}, {"count", 1, 10}, {"size", 0, 1400}, {"timeout", 1, 5}} {
		if _, err := integer(p[f.k], f.min, f.max); err != nil {
			return err
		}
	}
	for name, v := range obj(p["targets"]) {
		if err := iface(name); err != nil {
			return err
		}
		if str(v) == "" {
			continue
		}
		ip := net.ParseIP(str(v))
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() {
			return errors.New("探测目标须为对端隧道 IP")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.settings["wireguard_monitor"]
	a.settings["wireguard_monitor"] = clone(p)
	if err := writeJSON(filepath.Join(a.base, "manager-settings.json"), a.settings); err != nil {
		a.settings["wireguard_monitor"] = old
		return err
	}
	return nil
}
func (a *App) wgMonitorLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	next := time.Time{}
	lastInterval := -1.
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if !a.moduleEnabled("wireguard") {
				continue
			}
			p := a.wgMonitorPolicy()
			interval := num(p["interval"])
			if lastInterval >= 0 && lastInterval != interval {
				next = time.Now().Add(time.Duration(max(1., interval)) * time.Second)
			}
			lastInterval = interval
			if !yes(p["enabled"]) || time.Now().Before(next) {
				continue
			}
			next = time.Now().Add(time.Duration(num(p["interval"])) * time.Second)
			for _, r := range a.network.wireguard() {
				name := str(r["name"])
				target := str(obj(p["targets"])[name])
				auto, hasAuto := obj(p["auto_targets"])[name].(bool)
				if !hasAuto {
					auto = target == ""
				}
				if auto {
					candidates := arr(r["probe_candidates"])
					if len(candidates) > 0 {
						target = str(candidates[0])
					} else {
						target = ""
					}
				}
				if !yes(r["active"]) || net.ParseIP(target) == nil {
					continue
				}
				ctx, cancel := context.WithTimeout(a.ctx, time.Duration(num(p["count"])*num(p["timeout"])+5)*time.Second)
				args := []string{"-n", "-I", name, "-c", strOrNumber(p["count"]), "-W", strOrNumber(p["timeout"]), "-s", strOrNumber(p["size"]), "--", target}
				raw, _ := a.runner.Run(ctx, append([]string{"ping"}, args...), "")
				cancel()
				sent, received := num(p["count"]), 0.
				if m := pingCount.FindStringSubmatch(raw); len(m) == 3 {
					sent, _ = strconv.ParseFloat(m[1], 64)
					received, _ = strconv.ParseFloat(m[2], 64)
				}
				latency := -1.
				if m := pingAverage.FindStringSubmatch(raw); len(m) == 2 {
					latency, _ = strconv.ParseFloat(m[1], 64)
				}
				if sent > 0 {
					_ = a.storeMeasurement("wg:"+name, true, M{"packet_loss_at": now(), "icmp_average_ms": latency, "icmp_success_pct": received / sent * 100, "icmp_sent": sent, "icmp_received": received, "packet_loss_status": "measured"})
				}
			}
		}
	}
}

// Only peer host routes identify a specific tunnel address. Network prefixes are never guessed.
func wgProbeCandidates(allowed, local []string) []string {
	own := map[string]bool{}
	for _, v := range local {
		if ip, _, e := net.ParseCIDR(v); e == nil {
			own[ip.String()] = true
		} else if ip := net.ParseIP(v); ip != nil {
			own[ip.String()] = true
		}
	}
	out := []string{}
	for _, v := range allowed {
		v = strings.Trim(v, ", ")
		ip, network, e := net.ParseCIDR(v)
		if e != nil {
			ip = net.ParseIP(v)
			if ip == nil {
				continue
			}
		} else {
			bits, total := network.Mask.Size()
			if bits != total {
				continue
			}
		}
		if !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLoopback() && !own[ip.String()] {
			out = append(out, ip.String())
		}
	}
	return uniq(out)
}
