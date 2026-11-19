//go:build linux

package gateway

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func hostRaw(interfaces []any) (M, error) {
	cpu, e := os.ReadFile("/proc/stat")
	if e != nil {
		return nil, e
	}
	fields := strings.Fields(strings.SplitN(string(cpu), "\n", 2)[0])
	total, idle := uint64(0), uint64(0)
	for i := 1; i < len(fields) && i <= 8; i++ {
		v, _ := strconv.ParseUint(fields[i], 10, 64)
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	r := M{"supported": true, "time": now(), "cores": runtime.NumCPU(), "_cpu_total": total, "_cpu_idle": idle, "cpu_percent": nil}
	for index, key := range map[int]string{6: "hi", 7: "si", 8: "st"} {
		if len(fields) > index {
			value, _ := strconv.ParseUint(fields[index], 10, 64)
			r["_cpu_"+key] = value
		}
		r["cpu_"+key] = nil
	}
	raw, _ := os.ReadFile("/proc/loadavg")
	load := strings.Fields(string(raw))
	for i, key := range []string{"load1", "load5", "load15"} {
		if len(load) > i {
			r[key], _ = strconv.ParseFloat(load[i], 64)
		}
	}
	raw, _ = os.ReadFile("/proc/uptime")
	if values := strings.Fields(string(raw)); len(values) > 0 {
		r["uptime_seconds"], _ = strconv.ParseFloat(values[0], 64)
	}
	raw, _ = os.ReadFile("/proc/meminfo")
	mem := map[string]uint64{}
	for _, line := range textLines(string(raw)) {
		f := strings.Fields(line)
		if len(f) > 1 {
			v, _ := strconv.ParseUint(f[1], 10, 64)
			mem[strings.TrimSuffix(f[0], ":")] = v * 1024
		}
	}
	available := mem["MemAvailable"]
	if available == 0 {
		available = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
	}
	used := uint64(0)
	if mem["MemTotal"] > available {
		used = mem["MemTotal"] - available
	}
	r["memory_total"], r["memory_used"], r["memory_available"] = mem["MemTotal"], used, available
	r["memory_free"], r["memory_shared"], r["memory_buff_cache"] = mem["MemFree"], mem["Shmem"], mem["Buffers"]+mem["Cached"]+mem["SReclaimable"]
	r["memory_percent"] = 100 * float64(used) / float64(max(mem["MemTotal"], 1))
	r["swap_total"], r["swap_used"] = mem["SwapTotal"], mem["SwapTotal"]-min(mem["SwapFree"], mem["SwapTotal"])
	raw, _ = os.ReadFile("/proc/sys/kernel/osrelease")
	r["kernel"] = strings.TrimSpace(string(raw))
	r["architecture"] = runtime.GOARCH
	raw, _ = os.ReadFile("/etc/os-release")
	for _, line := range textLines(string(raw)) {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			r["os"] = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	raw, _ = os.ReadFile("/proc/cpuinfo")
	for _, line := range textLines(string(raw)) {
		p := strings.SplitN(line, ":", 2)
		if len(p) == 2 && (strings.TrimSpace(p[0]) == "model name" || strings.TrimSpace(p[0]) == "Hardware") {
			r["cpu_model"] = strings.TrimSpace(p[1])
			break
		}
	}
	for _, value := range interfaces {
		row := obj(value)
		_, err := os.Stat(filepath.Join("/sys/class/net", str(row["name"]), "device"))
		row["physical"] = err == nil
		row["upload_bps"], row["download_bps"] = nil, nil
	}
	r["interfaces"] = interfaces
	raw, _ = os.ReadFile("/proc/diskstats")
	disks := []any{}
	for _, line := range textLines(string(raw)) {
		f := strings.Fields(line)
		if len(f) < 14 {
			continue
		}
		name := f[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		if _, e := os.Stat(filepath.Join("/sys/block", name)); e != nil {
			continue
		}
		n := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
		disks = append(disks, M{"name": name, "physical": !strings.HasPrefix(name, "dm-") && !strings.HasPrefix(name, "md"), "read_total": n(5) * 512, "write_total": n(9) * 512, "read_ops": n(3), "write_ops": n(7), "busy_ms": n(12), "io_time_ms": n(6) + n(10), "total_iops": nil, "await_ms": nil, "read_bps": nil, "write_bps": nil, "read_iops": nil, "write_iops": nil, "busy_percent": nil})
	}
	r["disks"] = disks
	raw, _ = os.ReadFile("/proc/mounts")
	mounts := []any{}
	seen := map[string]bool{}
	for _, line := range textLines(string(raw)) {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		mount := strings.ReplaceAll(f[1], `\040`, " ")
		if mount != "/" && !strings.HasPrefix(f[0], "/dev/") {
			continue
		}
		if f[2] == "tmpfs" || f[2] == "devtmpfs" || seen[f[0]] {
			continue
		}
		var stat unix.Statfs_t
		if unix.Statfs(mount, &stat) != nil {
			continue
		}
		all := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		if all == 0 {
			continue
		}
		seen[f[0]] = true
		mounts = append(mounts, M{"path": mount, "device": f[0], "filesystem": f[2], "total": all, "used": all - free, "available": stat.Bavail * uint64(stat.Bsize), "percent": 100 * float64(all-free) / float64(all)})
		if len(mounts) >= 32 {
			break
		}
	}
	r["mounts"] = mounts
	return r, nil
}
