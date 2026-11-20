package gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

func discoveryTargets(protocol string, port int, current string, previous []any, start int) []string {
	seen := map[string]bool{}
	rows := []string{}
	add := func(target string) {
		if !endpointValid(target) || seen[target] {
			return
		}
		_, p, _ := net.SplitHostPort(target)
		if p != strconv.Itoa(port) {
			return
		}
		seen[target] = true
		rows = append(rows, target)
	}
	add(current)
	for index, row := range previous {
		if index >= 3 {
			break
		}
		add(str(obj(row)["endpoint"]))
	}
	pool := "162.159.198."
	if protocol == "wg" {
		pool = "162.159.192."
	}
	// 73 and 254 are coprime, so each run spreads samples over the entire pool.
	for i := 0; i < 254 && len(rows) < 64; i++ {
		add(pool + strconv.Itoa((start+i*73)%254+1) + ":" + strconv.Itoa(port))
	}
	if len(rows) > 64 {
		rows = rows[:64]
	}
	return rows
}

func scoutQuality(r M) float64 {
	if officialWarpCoolingDown(r) {
		return -2
	}
	if !yes(r["durable"]) || str(r["probe_profile"]) != scoutProbeProfile {
		return -1
	}
	if r["rtt_ms"] == nil || r["loss_pct"] == nil {
		return -1
	}
	score := (100-num(r["loss_pct"]))*0.5 + latencyQuality(num(r["rtt_ms"]))*0.3
	if yes(r["true_latency_attempt_ok"]) {
		score += latencyQuality(num(r["true_latency_ms"])) * 0.1
	}
	if yes(r["speed_attempt_ok"]) && num(r["speed_kib_s"]) > 0 {
		speed := num(r["speed_kib_s"])
		score += speed / (speed + 1024) * 10
	}
	return score
}

func (a *App) discoverWarp(ctx context.Context, j *Job, protocol string, port int) error {
	if !a.scoutOperate.TryLock() {
		return errors.New("已有候选发现或检测进行中")
	}
	defer a.scoutOperate.Unlock()
	env, cleanup, err := a.prepareScoutProbe()
	if err != nil {
		return err
	}
	defer cleanup()
	state := a.scoutStatus()
	collections := obj(state["collections"])
	previous := arr(collections[protocol])
	current := obj(state["current"])
	pinned := ""
	if scoutProtocol(current) == protocol {
		pinned = firstNonempty(str(current["endpoint_address"]), str(current["override"]))
	}
	seed, err := rand.Int(rand.Reader, big.NewInt(254))
	if err != nil {
		return err
	}
	targets := discoveryTargets(protocol, port, pinned, previous, int(seed.Int64()))
	a.jobDetail(j, fmt.Sprintf("扩大采样 %d 个端点；使用官方同域名口径，原隧道保持连接", len(targets)))
	type reachable struct {
		target string
		ms     float64
	}
	ready := []reachable{}
	if protocol == "masque-h2" {
		a.stage(j, "初筛候选 TCP 连通性；随后验证真实隧道")
		var mu sync.Mutex
		var wg sync.WaitGroup
		limit := make(chan struct{}, 8)
		for _, target := range targets {
			if ctx.Err() != nil {
				break
			}
			target := target
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return ctx.Err()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-limit }()
				child, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				defer cancel()
				start := time.Now()
				conn, err := directDialer().DialContext(child, "tcp", target)
				if err == nil {
					conn.Close()
					mu.Lock()
					ready = append(ready, reachable{target, float64(time.Since(start).Microseconds()) / 1000})
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		sort.Slice(ready, func(i, k int) bool { return ready[i].ms < ready[k].ms })
	} else {
		for _, target := range targets {
			ready = append(ready, reachable{target, 0})
		}
	}
	// Keep a control sample of the currently connected endpoint in the shortlist.
	short := []reachable{}
	if endpointValid(pinned) {
		short = append(short, reachable{pinned, 0})
	}
	for _, r := range ready {
		if r.target != pinned && len(short) < 10 {
			short = append(short, r)
		}
	}
	rows := []any{}
	passed := 0
	for i, candidate := range short {
		if ctx.Err() != nil {
			return errors.New("端点发现已取消，原候选列表保留")
		}
		a.stage(j, fmt.Sprintf("检测端点 %d / %d · 官方同域名隧道验证", i+1, len(short)))
		child, cancel := context.WithTimeout(ctx, 20*time.Second)
		r := a.probeScoutCandidate(child, env, protocol, candidate.target, true)
		cancel()
		r["tcp_connect_ms"] = candidate.ms
		for _, oldValue := range previous {
			old := obj(oldValue)
			if str(old["endpoint"]) == candidate.target {
				for _, key := range []string{"official_ok", "official_at"} {
					if value, ok := old[key]; ok {
						r[key] = value
					}
				}
			}
		}
		if yes(r["durable"]) {
			passed++
			child, cancel = context.WithTimeout(ctx, 15*time.Second)
			applyScoutTrueLatency(r, a.scoutTrueLatency(child, protocol, candidate.target, env))
			cancel()
			rows = append(rows, r)
		} else if candidate.target == pinned {
			r["pinned"], r["current"], r["official_connected"] = true, true, yes(current["connected"])
			rows = append(rows, r)
		}
		a.jobDetail(j, scoutResultSummary(candidate.target, mergeScoutMetrics(nil, r, []string{"latency", "loss"}), []string{"latency", "loss"}))
	}
	if ctx.Err() != nil {
		return errors.New("端点发现已取消，原候选列表保留")
	}
	sort.SliceStable(rows, func(i, k int) bool { return scoutQuality(obj(rows[i])) > scoutQuality(obj(rows[k])) })
	// Bound automatic bandwidth checks to the three best verified tunnels, 5 MiB each.
	policy := a.warpMonitorPolicy()
	if yes(policy["speed_enabled"]) {
		policy = clone(policy)
		policy["speed_timeout"], policy["speed_size_mb"] = 5, 5
		for i := 0; i < len(rows) && i < 3; i++ {
			r := obj(rows[i])
			if !yes(r["durable"]) {
				continue
			}
			a.stage(j, fmt.Sprintf("下载测速 %d / %d · 优选候选抽样", i+1, min(3, len(rows))))
			measured := a.scoutSOCKS(ctx, protocol, str(r["endpoint"]), env.binary, env.account, env.uid, env.gid, 5, func(child context.Context, p int) M { return bandwidthWithPolicy(child, p, "socks5", policy) })
			applyScoutBandwidth(r, measured)
		}
	}
	if ctx.Err() != nil {
		return errors.New("端点发现已取消，原候选列表保留")
	}
	sort.SliceStable(rows, func(i, k int) bool { return scoutQuality(obj(rows[i])) > scoutQuality(obj(rows[k])) })
	if len(rows) == 0 {
		return errors.New("本轮未发现通过官方同域名隧道验证的端点；原候选与原隧道保留")
	}
	backup := obj(state["previous"])
	backup[protocol] = clone(previous)
	collections[protocol] = rows
	state["collections"], state["previous"] = collections, backup
	state["discovery"] = M{"sampled": len(targets), "reachable": len(ready), "verified": passed, "finished": now(), "profile": scoutProbeProfile}
	if err := writeJSON(filepath.Join(a.base, "warp-scout-results.json"), state); err != nil {
		return err
	}
	a.stage(j, fmt.Sprintf("采样%d个，连通%d个，真实隧道通过%d个；候选已按质量排序，当前端点未切换", len(targets), len(ready), passed))
	return nil
}
