package gateway

import (
	"context"
	"errors"
	"net"
	"time"
)

// Restore only the official WARP client, and report completion after verification.
func (a *App) restoreWarpConnection(ctx context.Context, j *Job, old M) error {
	a.invalidateWarp()
	if warpSettingsRestored(a.warp(), old) {
		return nil
	}
	commands := [][]string{{"disconnect"}}
	if proto := str(old["protocol"]); proto == "MASQUE" || proto == "WireGuard" {
		commands = append(commands, []string{"tunnel", "protocol", "set", proto})
		if proto == "MASQUE" && str(old["masque_options"]) != "" {
			commands = append(commands, []string{"tunnel", "masque-options", "set", str(old["masque_options"])})
		}
	}
	if target := str(old["override"]); target != "" {
		commands = append(commands, []string{"tunnel", "endpoint", "set", target})
	} else {
		commands = append(commands, []string{"tunnel", "endpoint", "reset"})
	}
	failed := false
	for _, args := range commands {
		if _, err := a.warpCommand(ctx, args...); err != nil {
			failed = true
		}
	}
	if yes(old["connected"]) {
		if _, err := a.warpCommand(ctx, "connect"); err != nil {
			return errors.New("原隧道重连命令失败")
		}
		waitSeconds := 30
		if deadline, ok := ctx.Deadline(); ok {
			waitSeconds = min(75, max(1, int(time.Until(deadline).Seconds())-5))
		}
		if err := a.waitWarpState(ctx, j, waitSeconds, false); err != nil {
			return errors.New("已尝试回退原设置，但原隧道连接尚未确认")
		}
	}
	if failed {
		return errors.New("原隧道回退命令未全部成功，需核对实际设置")
	}
	a.invalidateWarp()
	actual := a.warp()
	for _, key := range []string{"protocol", "masque_options", "override"} {
		if str(actual[key]) != str(old[key]) {
			return errors.New("回退后的 WARP 设置与原设置不一致")
		}
	}
	if yes(actual["connected"]) != yes(old["connected"]) {
		return errors.New("回退后的 WARP 连接状态与原状态不一致")
	}
	if yes(old["connected"]) {
		target := firstNonempty(str(old["endpoint_address"]), str(old["override"]))
		if target != "" && !warpEndpointConfirmed(actual, target) {
			return errors.New("原隧道已连接，但实际端点尚未确认")
		}
	}
	return nil
}

func warpSettingsRestored(actual, old M) bool {
	for _, key := range []string{"protocol", "masque_options", "override"} {
		if str(actual[key]) != str(old[key]) {
			return false
		}
	}
	if yes(actual["connected"]) != yes(old["connected"]) {
		return false
	}
	if yes(old["connected"]) {
		target := firstNonempty(str(old["endpoint_address"]), str(old["override"]))
		return target != "" && warpEndpointConfirmed(actual, target)
	}
	return true
}

func warpEndpointConfirmed(actual M, target string) bool {
	if !yes(actual["connected"]) {
		return false
	}
	if address := str(actual["endpoint_address"]); address != "" {
		return address == target
	}
	host, _, err := net.SplitHostPort(target)
	return err == nil && str(actual["endpoint"]) == host
}
