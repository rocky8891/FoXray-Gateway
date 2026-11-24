package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

func wgCovers(prefix netip.Prefix, owner []string) bool {
	for _, value := range owner {
		_, candidate, err := canonicalSource(value)
		if err == nil && candidate.Bits() <= prefix.Bits() && candidate.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}

// Only reconcile exact routes installed by the interface's configured routing mode.
func syncWGPeerRoutes(ctx context.Context, runner Runner, profile *WGProfile, peer WGPeer) error {
	if profile.Table == "off" && (profile.Network == nil || profile.Network.RouteMode != "manual") {
		return nil
	}
	if profile.Network != nil && profile.Network.RouteMode == "off" {
		return nil
	}
	prefixes, err := wgAllowedInput(peer.AllowedIPs)
	if err != nil {
		return err
	}
	if len(prefixes) == 0 {
		return nil
	}
	raw, err := runner.Run(ctx, []string{"wg", "show", profile.Name, "allowed-ips"}, "")
	if err != nil {
		return errors.New("无法读取其他 Peer 的 AllowedIPs")
	}
	var others []string
	for _, line := range textLines(raw) {
		fields := strings.Fields(strings.ReplaceAll(line, ",", " "))
		if len(fields) > 1 && fields[0] != peer.PublicKey {
			others = append(others, fields[1:]...)
		}
	}
	manual := profile.Network != nil && profile.Network.RouteMode == "manual"
	table := profile.Table
	protocol := "boot"
	configuredTable := table != "" && table != "auto"
	if manual {
		network, err := validateWGNetwork(*profile.Network)
		if err != nil {
			return err
		}
		table, protocol, prefixes = network.RouteTable, "187", network.Routes
	} else if !configuredTable {
		table = "254"
	} else {
		number, err := routeTable(table)
		if err != nil {
			return err
		}
		table = strconv.Itoa(number)
	}
	for _, destination := range prefixes {
		_, prefix, err := canonicalSource(destination)
		if err != nil {
			return err
		}
		if manual && (!wgCovers(prefix, peer.AllowedIPs) || wgCovers(prefix, others)) {
			continue
		}
		if !manual {
			shared := false
			for _, value := range others {
				_, existing, err := canonicalSource(value)
				shared = shared || err == nil && existing == prefix
			}
			if shared {
				continue
			}
		}
		family := "-4"
		if prefix.Addr().Is6() {
			family = "-6"
		}
		routeTableID := table
		if !manual && !configuredTable && prefix.Bits() == 0 {
			mark, err := runner.Run(ctx, []string{"wg", "show", profile.Name, "fwmark"}, "")
			if err != nil {
				return err
			}
			number, err := strconv.ParseUint(strings.TrimSpace(mark), 0, 32)
			if err != nil || number == 0 {
				if peer.Disabled {
					continue
				}
				return errors.New("默认路由需要已有的 wg-quick 策略路由，请通过接口配置应用")
			}
			routeTableID = strconv.FormatUint(number, 10)
			if !peer.Disabled {
				raw, err := runner.Run(ctx, []string{"ip", "-j", family, "rule", "show"}, "")
				var rules []M
				if err != nil || json.Unmarshal([]byte(raw), &rules) != nil {
					return errors.New("无法核对默认路由策略")
				}
				found := false
				for _, rule := range rules {
					ruleMark, err := strconv.ParseUint(strOrNumber(rule["fwmark"]), 0, 32)
					inverted, hasInversion := rule["not"]
					found = found || (err == nil && ruleMark == number && hasInversion && (inverted == nil || yes(inverted)) && strOrNumber(rule["table"]) == routeTableID)
				}
				if !found {
					return errors.New("默认路由需要已有的 wg-quick 策略路由，请通过接口配置应用")
				}
			}
		}
		if err := syncWGRoute(ctx, runner, family, profile.Name, prefix, routeTableID, protocol, peer.Disabled); err != nil {
			return err
		}
	}
	return nil
}

func syncWGRoute(ctx context.Context, runner Runner, family, name string, prefix netip.Prefix, table, protocol string, remove bool) error {
	raw, err := runner.Run(ctx, []string{"ip", "-j", family, "route", "show", "table", "all", "exact", prefix.String(), "dev", name}, "")
	var rows []M
	if err != nil || json.Unmarshal([]byte(raw), &rows) != nil {
		return errors.New("无法核对 Peer 路由")
	}
	for _, row := range rows {
		reported := strOrNumber(first(row["table"], "main"))
		tableNumber, err := routeTable(reported)
		if err != nil || strconv.Itoa(tableNumber) != table || (str(row["dev"]) != "" && str(row["dev"]) != name) {
			continue
		}
		if !remove {
			return nil
		}
		proto := strOrNumber(first(row["protocol"], "boot"))
		if protocol == "boot" && proto == "3" {
			proto = "boot"
		}
		expectedMetric := num(row["metric"]) == 0 || (family == "-6" && num(row["metric"]) == 1024)
		if proto != protocol || !expectedMetric || row["gateway"] != nil || row["via"] != nil || row["multipath"] != nil || row["prefsrc"] != nil || row["nhid"] != nil || row["encap"] != nil || firstNonempty(str(row["type"]), "unicast") != "unicast" {
			continue
		}
		args := []string{"ip", family, "route", "del", prefix.String(), "dev", name, "table", table, "proto", protocol}
		if row["metric"] != nil {
			args = append(args, "metric", strOrNumber(row["metric"]))
		}
		_, err = runner.Run(ctx, args, "")
		return err
	}
	if remove {
		return nil
	}
	_, err = runner.Run(ctx, []string{"ip", family, "route", "add", prefix.String(), "dev", name, "table", table, "proto", protocol}, "")
	return err
}
