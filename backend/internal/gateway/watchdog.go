package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileRestore struct {
	InPlace      bool   `json:"in_place,omitempty"`
	ExpectedHash string `json:"expected_hash"`
	OldHash      string `json:"old_hash"`
	Target       string `json:"target"`
	Backup       string `json:"backup"`
	Existed      bool   `json:"existed"`
}
type rollbackSpec struct {
	ID      string       `json:"id"`
	Kind    string       `json:"kind"`
	Summary string       `json:"summary"`
	Created float64      `json:"created"`
	Expires float64      `json:"expires"`
	Status  string       `json:"status"`
	Undo    []Step       `json:"undo"`
	File    *FileRestore `json:"file,omitempty"`
	Persist M            `json:"persist,omitempty"`
}

func (n *Network) specPath(p *NetworkPlan) string {
	return filepath.Join(n.app.base, "private", "network-plans", p.ID+".json")
}
func (n *Network) writeSpec(p *NetworkPlan, done int) error {
	end := min(done, len(p.undo))
	if len(p.undoEnds) == len(p.steps) && len(p.undoEnds) > 0 {
		end = 0
		if done > 0 {
			end = p.undoEnds[min(done, len(p.undoEnds))-1]
		}
	}
	return writeJSON(n.specPath(p), rollbackSpec{ID: p.ID, Kind: p.Kind, Summary: p.Summary, Created: p.Created, Expires: p.Expires, Status: p.Status, Undo: p.undo[:end], File: p.restoreFile, Persist: p.persist})
}
func (n *Network) arm(p *NetworkPlan) error {
	p.Expires = now() + 120
	p.Status = "armed"
	if e := n.writeSpec(p, 0); e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	_, e = n.app.command(8, "", "systemd-run", "--quiet", "--unit=foxray-net-rollback-"+p.ID, "--on-active=120s", "--timer-property=AccuracySec=1s", "--property=UMask=0077", exe, "--rollback-plan", n.specPath(p))
	if e != nil {
		p.Status = "failed"
		_ = n.writeSpec(p, 0)
		return errors.New("无法创建独立回退定时器，未修改网络")
	}
	return nil
}
func (n *Network) refreshPlan(p *NetworkPlan) {
	var spec rollbackSpec
	if readJSON(n.specPath(p), &spec) == nil {
		p.Status = spec.Status
		p.Expires = spec.Expires
	}
}
func (n *Network) recoverPlans() {
	paths, _ := filepath.Glob(filepath.Join(n.app.base, "private", "network-plans", "*.json"))
	for _, path := range paths {
		var s rollbackSpec
		if readJSON(path, &s) != nil || !regexpID(s.ID) {
			continue
		}
		n.plans[s.ID] = &NetworkPlan{ID: s.ID, Kind: s.Kind, Summary: s.Summary, Created: s.Created, Expires: s.Expires, Status: s.Status, Warnings: []string{}, Preview: []string{}, undo: s.Undo, restoreFile: s.File, persist: s.Persist}
	}
}
func RollbackFile(path string) error { return rollbackFile(path, SystemRunner{}) }
func rollbackFile(path string, runner Runner) error {
	if !strings.HasSuffix(path, ".json") || filepath.Base(filepath.Dir(path)) != "network-plans" {
		return errors.New("回退计划路径无效")
	}
	stat, e := os.Lstat(path)
	if e != nil || !stat.Mode().IsRegular() || !securePlan(stat) {
		return errors.New("回退计划权限无效")
	}
	unlock, e := lockPlan(path + ".lock")
	if e != nil {
		return e
	}
	defer unlock()
	var spec rollbackSpec
	if e = readJSON(path, &spec); e != nil {
		return e
	}
	if spec.Status == "confirmed" || spec.Status == "rolled_back" || spec.Status == "failed" {
		return nil
	}
	if spec.File != nil {
		f := spec.File
		if filepath.Dir(f.Target) != "/etc/wireguard" && !((f.Target == "/etc/netns/foxray-warp/resolv.conf" || f.Target == "/etc/foxray/warp-netns-resolv.conf") && f.InPlace) {
			return errors.New("回退文件不在受管接口目录")
		}
		if e := restoreManagedFile(f); e != nil {
			spec.Status = "rollback_error"
			_ = writeJSON(path, spec)
			return e
		}

	}
	failed := false
	for i := len(spec.Undo) - 1; i >= 0; i-- {
		step := spec.Undo[i]
		if len(step.Args) == 0 {
			return errors.New("回退命令无效")
		}
		switch step.Args[0] {
		case "ip", "systemctl", "firewall-cmd", "nft", "iptables", "ip6tables", "nmcli":
		default:
			return errors.New("回退命令不在允许列表")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, e := runner.Run(ctx, step.Args, step.Input)
		cancel()
		if e != nil {
			failed = true
		}
	}
	if failed {
		spec.Status = "rollback_error"
	} else {
		spec.Status = "rolled_back"
	}
	if e = writeJSON(path, spec); e != nil {
		return e
	}
	if failed {
		return errors.New("部分回退命令未完成，请查看当前接口与规则")
	}
	return nil
}
func (n *Network) confirmSpec(p *NetworkPlan) error {
	path := n.specPath(p)
	unlock, e := lockPlan(path + ".lock")
	if e != nil {
		return e
	}
	defer unlock()
	var spec rollbackSpec
	if readJSON(path, &spec) != nil {
		return errors.New("回退计划无法读取")
	}
	if spec.Status != "awaiting_confirmation" || spec.Expires-now() < 5 {
		return errors.New("确认期限已到或即将到，请等待回退后重新应用")
	}
	spec.Status = "confirmed"
	if e = writeJSON(path, spec); e != nil {
		return e
	}
	p.Status = "confirmed"
	return nil
}

var _ json.RawMessage

func restoreManagedFile(f *FileRestore) error {
	if f.ExpectedHash == "" {
		return nil
	}
	current, e := os.ReadFile(f.Target)
	missing := os.IsNotExist(e)
	if e != nil && !missing {
		return errors.New("回退无法检查接口配置")
	}
	if !missing && fingerprint(string(current)) == f.OldHash {
		return nil
	}
	if f.ExpectedHash == "delete" {
		if !missing {
			return errors.New("配置在应用后被其他操作修改，未覆盖")
		}
	} else if missing || fingerprint(string(current)) != f.ExpectedHash {
		return errors.New("接口配置与本次写入不匹配，未覆盖其他修改")
	}
	if f.Existed {
		old, e := os.ReadFile(f.Backup)
		if e != nil || fingerprint(string(old)) != f.OldHash {
			return errors.New("原配置备份不可用")
		}
		if f.InPlace {
			return os.WriteFile(f.Target, old, 0644)
		}
		return atomicFile(f.Target, old, 0600)
	}
	return os.Remove(f.Target)
}
