package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Version = "0.9.43"

type M = map[string]any

func obj(v any) M {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return M{}
}
func arr(v any) []any {
	if a, ok := v.([]any); ok && a != nil {
		return a
	}
	return []any{}
}
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case uint:
		return float64(n)
	case uint32:
		return float64(n)
	case float32:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func yes(v any) bool     { b, _ := v.(bool); return b }
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func now() float64       { return float64(time.Now().UnixMilli()) / 1000 }
func id() string         { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func readJSON(path string, into any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, into)
}
func writeJSON(path string, data any) error {
	b, e := json.MarshalIndent(data, "", "  ")
	if e != nil {
		return e
	}
	return atomicFile(path, b, 0600)
}
func atomicNewFile(path string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	file, e := os.CreateTemp(filepath.Dir(path), ".foxray-new-")
	if e != nil {
		return e
	}
	defer os.Remove(file.Name())
	if e = file.Chmod(mode); e != nil {
		file.Close()
		return e
	}
	if _, e = file.Write(b); e != nil {
		file.Close()
		return e
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	return os.Link(file.Name(), path)
}
func atomicFile(path string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".foxray-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	_ = f.Close()
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
func regular(path string) bool { s, e := os.Lstat(path); return e == nil && s.Mode().IsRegular() }

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$`)

func iface(s string) error {
	if !safeName.MatchString(s) {
		return errors.New("接口名称无效")
	}
	return nil
}
func cidr(s string) error {
	if s == "default" || s == "all" {
		return nil
	}
	if _, _, e := net.ParseCIDR(s); e == nil {
		return nil
	}
	if net.ParseIP(s) != nil {
		return nil
	}
	return errors.New("IP 或 CIDR 无效")
}
func label(s string) string {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	if strings.Contains(s, "://") || strings.Contains(low, "token=") || strings.Contains(low, "password=") {
		return "未命名"
	}
	r := []rune(s)
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}
func integer(v any, min, max int) (int, error) {
	n := num(v)
	if n != float64(int(n)) || n < float64(min) || n > float64(max) {
		return 0, fmt.Errorf("数值须为 %d～%d 之间的整数", min, max)
	}
	return int(n), nil
}
func port(v any) (int, error) { return integer(v, 1, 65535) }
func uniq(s []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range s {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func textLines(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, "\n") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

type Runner interface {
	Run(context.Context, []string, string) (string, error)
}
type SystemRunner struct{}

func (SystemRunner) Run(ctx context.Context, args []string, input string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("空命令")
	}
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Env = append(os.Environ(), "LC_ALL=C")
	if input != "" {
		c.Stdin = strings.NewReader(input)
	}
	out, e := c.Output()
	if e != nil {
		if ctx.Err() != nil {
			return "", errors.New("操作超时或已取消")
		}
		return string(out), fmt.Errorf("%s 执行失败", filepath.Base(args[0]))
	}
	return string(out), nil
}
func (a *App) command(seconds int, input string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	return a.runner.Run(ctx, args, input)
}
func (a *App) commandJSON(args ...string) ([]any, error) {
	s, e := a.command(6, "", args...)
	if e != nil {
		return nil, e
	}
	var rows []any
	e = json.Unmarshal([]byte(s), &rows)
	return rows, e
}
func (a *App) service(unit string) M {
	s, e := a.command(5, "", "systemctl", "show", unit, "-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "-p", "UnitFileState")
	m := M{"name": unit, "state": "unknown", "autostart": false}
	if e != nil {
		return m
	}
	for _, line := range textLines(s) {
		p := strings.SplitN(line, "=", 2)
		if len(p) != 2 {
			continue
		}
		switch p[0] {
		case "ActiveState":
			m["state"] = p[1]
		case "SubState":
			m["substate"] = p[1]
		case "MainPID":
			m["pid"] = p[1]
		case "UnitFileState":
			m["unit_file_state"] = p[1]
			m["autostart"] = p[1] == "enabled" || p[1] == "enabled-runtime"
		}
	}
	return m
}
func parseKV(s string) M {
	m := M{}
	for _, line := range textLines(s) {
		p := strings.SplitN(line, "=", 2)
		if len(p) == 2 {
			m[p[0]] = p[1]
		}
	}
	return m
}
func intstr(n any) string { return strconv.Itoa(int(num(n))) }
func limitedRead(r io.Reader, limit int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, limit+1))
	if e == nil && int64(len(b)) > limit {
		return nil, errors.New("内容超过大小限制")
	}
	return b, e
}

type Job struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Group    string   `json:"group"`
	Source   string   `json:"source"`
	UIAction string   `json:"ui_action,omitempty"`
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Started  float64  `json:"started"`
	Finished float64  `json:"finished"`
	Details  []string `json:"details"`
	cancel   context.CancelFunc
}
type jobSet struct {
	mu    sync.RWMutex
	items []*Job
	gate  chan struct{}
}

func (a *App) submit(title, group string, fn func(context.Context, *Job) error) (string, error) {
	return a.submitTask(title, group, "manual", fn)
}
func (a *App) submitAutomatic(title, group string, fn func(context.Context, *Job) error) (string, error) {
	return a.submitTask(title, group, "automatic", fn)
}
func (a *App) submitTask(title, group, source string, fn func(context.Context, *Job) error, uiAction ...string) (string, error) {
	select {
	case a.jobs.gate <- struct{}{}:
	default:
		return "", errors.New("任务队列已满，请等待现有任务完成")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	actionID := uiActionID(uiAction...)
	ctx = context.WithValue(ctx, uiActionContextKey{}, actionID)
	j := &Job{ID: id(), Title: title, Group: group, Source: source, Status: "running", Started: now(), Details: []string{}, cancel: cancel}
	j.UIAction = actionID
	a.jobs.mu.Lock()
	a.jobs.items = append([]*Job{j}, a.jobs.items...)
	if len(a.jobs.items) > 100 {
		kept := make([]*Job, 0, 108)
		completed := 0
		for _, item := range a.jobs.items {
			if item.Status == "running" || item.Status == "queued" {
				kept = append(kept, item)
			} else if completed < 100 {
				kept = append(kept, item)
				completed++
			}
		}
		a.jobs.items = kept
	}
	a.jobs.mu.Unlock()
	go func() {
		defer func() { <-a.jobs.gate; cancel() }()
		e := fn(ctx, j)
		a.jobs.mu.Lock()
		j.Finished = now()
		j.Status = "success"
		if e != nil {
			j.Status = "error"
			var partial *partialTaskError
			if errors.As(e, &partial) {
				j.Status = "partial"
			}
			j.Message = e.Error()
		} else if j.Message == "" {
			j.Message = "操作完成"
		}
		a.jobs.mu.Unlock()
		a.jobs.mu.RLock()
		details := append([]string{}, j.Details...)
		a.jobs.mu.RUnlock()
		if len(details) > 200 {
			details = details[len(details)-200:]
		}
		a.maintainControlState()
		if !isPeriodicNodeTask(j.Source, j.Group) {
			a.audit(group, j.Title+"："+j.Message, e == nil, M{"job_id": j.ID, "result_status": j.Status, "results": details, "source": j.Source, "title": j.Title, "started": j.Started, "finished": j.Finished}, j.UIAction)
		}
	}()
	return j.ID, nil
}
func (a *App) stage(j *Job, s string) { a.jobs.mu.Lock(); j.Message = s; a.jobs.mu.Unlock() }
func (a *App) jobDetail(j *Job, s string) {
	a.jobs.mu.Lock()
	j.Details = append(j.Details, s)
	a.jobs.mu.Unlock()
}
