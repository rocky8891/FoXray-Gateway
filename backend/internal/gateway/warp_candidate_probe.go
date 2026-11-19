package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

const scoutMASQUESNI = "consumer-masque-proxy.cloudflareclient.com"

type scoutProbeEnvironment struct{ binary, account, uid, gid string }

func scoutMASQUEArgs(protocol string) []string {
	if protocol == "masque-h2" {
		return []string{"--masque-sni", scoutMASQUESNI}
	}
	return nil
}
func (a *App) prepareScoutProbe() (scoutProbeEnvironment, func(), error) {
	env := scoutProbeEnvironment{binary: filepath.Join(a.base, "probe-tools", "warpscout"), account: "/var/lib/foxray-scout/account.json"}
	if !regular(env.binary) {
		return env, nil, errors.New("WARPSCOUT 未安装，无法进行独立隧道预检")
	}
	u, err := user.Lookup("foxray-scout")
	if err != nil {
		return env, nil, errors.New("独立扫描用户未配置")
	}
	env.uid, env.gid = u.Uid, u.Gid
	if !regular(env.account) {
		return env, nil, errors.New("独立扫描账号未初始化，保留官方 WARP 注册不变")
	}
	table := "foxray_scout_probe"
	if _, err = a.command(3, "", "nft", "list", "table", "inet", table); err == nil {
		return env, nil, errors.New("已有独立候选检测，请等待完成")
	}
	rules := "table inet " + table + " { chain output { type route hook output priority -170; policy accept; meta skuid " + u.Uid + " meta mark set 0x100cf ct mark set 0x100cf; }\n}\n"
	if _, err = a.command(5, rules, "nft", "-f", "-"); err != nil {
		return env, nil, errors.New("无法准备独立候选检测规则")
	}
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.runner.Run(cleanupCtx, []string{"nft", "delete", "table", "inet", table}, "")
	}
	return env, cleanup, nil
}
func scoutScanArgs(env scoutProbeEnvironment, protocol, target, report string, latency bool) []string {
	host, port, _ := net.SplitHostPort(target)
	args := []string{"setpriv", "--reuid", env.uid, "--regid", env.gid, "--clear-groups", "--inh-caps=+net_raw", "--ambient-caps=+net_raw", env.binary, "scan", "--proto", protocol, "--target", host, "--port", port, "--interface", "ens16", "--account", env.account, "--tunnel-jobs", "1", "--timeout", "2", "--plain", "--output", report}
	if latency {
		args = append(args, "--tun-ping-count", "10")
	}
	return append(args, scoutMASQUEArgs(protocol)...)
}
func (a *App) probeScoutCandidate(ctx context.Context, env scoutProbeEnvironment, protocol, target string, latency bool) M {
	report := filepath.Join("/var/lib/foxray-scout", "go-report-"+id()+".txt")
	defer os.Remove(report)
	child, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	_, runErr := a.runner.Run(child, scoutScanArgs(env, protocol, target, report, latency), "")
	raw, readErr := os.ReadFile(report)
	result := parseScoutReport(string(raw), protocol, target)
	if runErr != nil || readErr != nil || child.Err() != nil {
		result["durable"] = false
		result["probe_error"] = "独立候选隧道检测失败或超时"
	} else if !yes(result["durable"]) {
		result["probe_error"] = "候选隧道未通过认证或连通性检测"
	}
	return result
}
func (a *App) preflightWarpCandidate(ctx context.Context, endpoint string) error {
	if !a.scoutOperate.TryLock() {
		return errors.New("已有候选检测进行中，原隧道未更改")
	}
	defer a.scoutOperate.Unlock()
	env, cleanup, err := a.prepareScoutProbe()
	if err != nil {
		return err
	}
	defer cleanup()
	r := a.probeScoutCandidate(ctx, env, "masque-h2", endpoint, true)
	if !yes(r["durable"]) {
		return errors.New("候选认证隧道预检未通过，原隧道未更改")
	}
	return nil
}
func mergeScoutMetrics(old, measured M, metrics []string) M {
	result := clone(old)
	if result == nil {
		result = M{}
	}
	for _, key := range []string{"endpoint", "protocol", "tested", "tested_at", "handshake", "durable"} {
		result[key] = measured[key]
	}
	delete(result, "probe_error")
	if measured["probe_error"] != nil {
		result["probe_error"] = measured["probe_error"]
	}
	for _, metric := range metrics {
		if metric == "speed" || metric == "true_latency" {
			continue
		}
		value, stamp := "rtt_ms", "rtt_at"
		if metric == "loss" {
			value, stamp = "loss_pct", "loss_at"
		}
		result[metric+"_attempt_at"] = now()
		ok := yes(measured["durable"]) && measured[value] != nil
		result[metric+"_attempt_ok"] = ok
		if ok {
			result[value], result[stamp] = measured[value], measured["tested_at"]
		}
	}
	return result
}
func applyScoutBandwidth(result, measured M) {
	result["speed_attempt_at"], result["speed_attempt_ok"] = now(), yes(measured["ok"])
	if yes(measured["ok"]) {
		result["speed_kib_s"], result["speed_at"], result["speed_source"] = measured["speed"], measured["time"], measured["source"]
		delete(result, "speed_error")
	} else {
		result["speed_error"] = firstNonempty(str(measured["error"]), "下载测速未通过，保留上次成功带宽")
	}
}
func scoutResultSummary(target string, r M, metrics []string) string {
	parts := []string{target}
	for _, m := range metrics {
		switch m {
		case "latency":
			if yes(r["latency_attempt_ok"]) {
				parts = append(parts, fmt.Sprintf("RTT %v ms", r["rtt_ms"]))
			} else {
				parts = append(parts, "RTT检测未通过")
			}
		case "loss":
			if yes(r["loss_attempt_ok"]) {
				parts = append(parts, fmt.Sprintf("丢包 %v%%", r["loss_pct"]))
			} else {
				parts = append(parts, "丢包检测未通过")
			}
		case "true_latency":
			if yes(r["true_latency_attempt_ok"]) {
				parts = append(parts, fmt.Sprintf("真延迟 %v ms", r["true_latency_ms"]))
			} else {
				parts = append(parts, "真延迟检测失败："+str(r["true_latency_error"]))
			}
		case "speed":
			if yes(r["speed_attempt_ok"]) {
				parts = append(parts, fmt.Sprintf("下载 %.1f KB/s", num(r["speed_kib_s"])*1024/1000))
			} else {
				parts = append(parts, "下载测速失败："+str(r["speed_error"]))
			}
		}
	}
	return strings.Join(parts, " · ")
}
