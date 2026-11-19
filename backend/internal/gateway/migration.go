package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

func diffPaths(a, b any, path string, out *[]string) {
	if len(*out) >= 64 {
		return
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		keys := map[string]bool{}
		for k := range am {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		sorted := []string{}
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			diffPaths(am[k], bm[k], path+"."+k, out)
		}
		return
	}
	aa, aok := a.([]any)
	bb, bok := b.([]any)
	if aok && bok {
		if len(aa) != len(bb) {
			*out = append(*out, path+".length")
		}
		for i := 0; i < min(len(aa), len(bb)); i++ {
			diffPaths(aa[i], bb[i], path+"["+strconv.Itoa(i)+"]", out)
		}
		return
	}
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		*out = append(*out, path)
	}
}
func (a *App) migrationCheck() M {
	a.mu.RLock()
	c, native, active := clone(a.cfg), clone(a.native), a.active
	a.mu.RUnlock()
	desired, e := a.buildEngine(c, native, active)
	if e != nil {
		return M{"compatible": false, "error": e.Error()}
	}
	existing := M{}
	if readJSON(filepath.Join(a.base, "engine.json"), &existing) != nil {
		return M{"compatible": false, "error": "未找到现有核心配置快照"}
	}
	paths := []string{}
	var normalized any
	b, _ := json.Marshal(desired)
	_ = json.Unmarshal(b, &normalized)
	diffPaths(existing, normalized, "engine", &paths)
	path := filepath.Join(a.base, "private", "migration-engine-check.json")
	if e = atomicFile(path, b, 0600); e != nil {
		return M{"compatible": false, "error": "无法创建受限校验文件"}
	}
	defer os.Remove(path)
	_, valid := a.command(15, "", filepath.Join(a.base, "xray"), "run", "-test", "-c", path)
	return M{"compatible": len(paths) == 0 && valid == nil, "core_validation": valid == nil, "semantic_diff_paths": paths, "nodes": len(arr(native["nodes"])), "active": active}
}

// MigrationReport compares generated core configuration without changing forwarding.
func (a *App) MigrationReport() M { return a.migrationCheck() }
