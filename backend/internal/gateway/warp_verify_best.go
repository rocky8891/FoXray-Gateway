package gateway

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

func (a *App) measureOfficialWarp(ctx context.Context, j *Job, target string) M {
	r := M{"endpoint": target, "probe_profile": scoutProbeProfile, "measured_at": now()}
	a.stage(j, "实际隧道质量验证 · "+target)
	child, cancel := context.WithTimeout(ctx, 12*time.Second)
	raw, err := a.runner.Run(child, []string{"ip", "netns", "exec", "foxray-warp", "ping", "-n", "-I", "CloudflareWARP", "-c", "5", "-W", "2", "-w", "10", "1.1.1.1"}, "")
	cancel()
	if err == nil {
		for key, value := range parseWarpPing(raw) {
			r[key] = value
		}
	}
	latency := a.warpLatency(ctx)
	applyScoutTrueLatency(r, latency)
	if yes(a.warpMonitorPolicy()["speed_enabled"]) {
		applyScoutBandwidth(r, a.warpBandwidth(ctx))
	}
	current := a.warp()
	a.invalidateWarp()
	current = a.warp()
	r["durable"] = warpEndpointConfirmed(current, target) && r["rtt_ms"] != nil && r["loss_pct"] != nil && yes(latency["ok"])
	return r
}

func (a *App) verifyBestWarp(ctx context.Context, j *Job, protocol string) (result error) {
	if !a.warpOperate.TryLock() {
		return errors.New("已有 WARP 连接操作进行中，请稍后再验证")
	}
	defer a.warpOperate.Unlock()
	if err := a.verifyWarp(); err != nil {
		return err
	}
	a.invalidateWarp()
	original := a.warp()
	if !yes(original["connected"]) || scoutProtocol(original) != protocol {
		return errors.New("请先连接当前协议的隧道，再验证优选")
	}
	originalTarget := firstNonempty(str(original["endpoint_address"]), str(original["override"]))
	if !endpointValid(originalTarget) {
		return errors.New("无法确认原端点，未进行切换")
	}
	baseline := a.measureOfficialWarp(ctx, j, originalTarget)
	if !yes(baseline["durable"]) {
		return errors.New("原隧道质量基线不完整，未开始切换；请先检查当前隧道")
	}
	a.recordWarpOfficial(protocol, originalTarget, true)
	bestTarget, bestScore := originalTarget, scoutQuality(baseline)
	initialScore := bestScore
	a.jobDetail(j, fmt.Sprintf("原端点 %s；实际质量评分 %.1f", originalTarget, bestScore))
	fresh := a.scoutStatus()
	candidates := []M{}
	for _, value := range arr(obj(fresh["collections"])[protocol]) {
		r := obj(value)
		if str(r["endpoint"]) != originalTarget && !officialWarpCoolingDown(r) && scoutQuality(r) >= 0 && now()-num(r["tested_at"]) < 1800 {
			candidates = append(candidates, r)
		}
	}
	sort.SliceStable(candidates, func(i, k int) bool { return scoutQuality(candidates[i]) > scoutQuality(candidates[k]) })
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	if len(candidates) == 0 {
		a.stage(j, "暂无新鲜且通过预检的候选，原端点保持；请先重新发现")
		return nil
	}
	keepBest := false
	defer func() {
		if keepBest {
			return
		}
		restore, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		a.jobs.mu.RLock()
		message := j.Message
		a.jobs.mu.RUnlock()
		if err := a.restoreWarpConnection(restore, j, original); err != nil {
			a.jobDetail(j, "原隧道回退未确认："+err.Error())
			result = errors.New("优选已停止，原隧道恢复尚未确认，请检查实际 WARP 状态")
		} else {
			a.stage(j, message)
		}
	}()
	verified, attempted := 0, 0
	for i, candidate := range candidates {
		if ctx.Err() != nil {
			return errors.New("优选已取消，正在恢复原隧道")
		}
		target := str(candidate["endpoint"])
		attempted++
		a.stage(j, fmt.Sprintf("验证官方端点 %d / %d · %s", i+1, len(candidates), target))
		if err := a.activateWarpLocked(ctx, j, protocol, target); err != nil {
			a.jobDetail(j, "候选未通过官方连接："+target)
		} else {
			measured := a.measureOfficialWarp(ctx, j, target)
			if yes(measured["durable"]) {
				verified++
				score := scoutQuality(measured)
				a.jobDetail(j, fmt.Sprintf("候选 %s；实际评分 %.1f", target, score))
				if score > bestScore {
					bestTarget, bestScore = target, score
				}
			} else {
				a.recordWarpOfficial(protocol, target, false)
				a.jobDetail(j, "候选连接后质量校验未通过："+target)
			}
		}
		restore, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		err := a.restoreWarpConnection(restore, j, original)
		cancel()
		if err != nil {
			return errors.New("原隧道回退未确认，已停止后续候选测试")
		}
		a.jobDetail(j, "本轮验证后已确认原端点连接恢复")
	}
	if bestTarget != originalTarget && bestScore-initialScore >= 5 {
		if err := a.activateWarpLocked(ctx, j, protocol, bestTarget); err != nil {
			return err
		}
		confirmed := a.measureOfficialWarp(ctx, j, bestTarget)
		if yes(confirmed["durable"]) && scoutQuality(confirmed)-initialScore >= 5 {
			keepBest = true
			a.stage(j, fmt.Sprintf("已保留更优端点 %s；原评分 %.1f，新评分 %.1f", bestTarget, initialScore, scoutQuality(confirmed)))
			return nil
		}
		a.jobDetail(j, "最终复测改善不足，恢复原端点")
	}
	a.stage(j, fmt.Sprintf("实际验证%d个候选，连接质量通过%d个；无稳定改善超过5分的端点，原端点保持", attempted, verified))
	return nil
}
