package gateway

import "testing"

func TestHostRatesUseCounterDeltasAndDoNotInventFirstSample(t *testing.T) {
	prior := M{"_cpu_total": uint64(1000), "_cpu_idle": uint64(600), "interfaces": []any{M{"name": "test0", "receive_total": uint64(1000), "send_total": uint64(500)}}}
	current := M{"_cpu_total": uint64(1200), "_cpu_idle": uint64(750), "cpu_percent": nil, "interfaces": []any{M{"name": "test0", "receive_total": uint64(1400), "send_total": uint64(700)}}}
	hostRates(nil, current, 2)
	if current["cpu_percent"] != nil {
		t.Fatal("first sample invented CPU usage")
	}
	hostRates(prior, current, 2)
	if num(current["cpu_percent"]) != 25 || num(obj(arr(current["interfaces"])[0])["download_bps"]) != 200 {
		t.Fatal("incorrect utilization or rate")
	}
}
func TestHostMonitorPolicyRejectsUnboundedSamplingAndInvalidInterfaces(t *testing.T) {
	a, _ := fixture(t)
	for _, input := range []M{{"interval": 0}, {"storage_interval": 1}, {"retention_days": 99999}, {"default_interface": "eth0;reboot"}} {
		if a.host.configure(input) == nil {
			t.Fatal("invalid monitoring configuration accepted")
		}
	}
}

func TestDiskAwaitAndIOPSUseDeltasAndWeightedHistory(t *testing.T) {
	before := M{"disks": []any{M{"name": "sda", "read_ops": 100, "write_ops": 50, "io_time_ms": 500}}}
	current := M{"disks": []any{M{"name": "sda", "read_ops": 106, "write_ops": 54, "io_time_ms": 550}}}
	hostRates(before, current, 2)
	disk := obj(arr(current["disks"])[0])
	if num(disk["total_iops"]) != 5 || num(disk["await_ms"]) != 5 {
		t.Fatal("disk delta rates incorrect", disk)
	}
	next := M{"disks": []any{M{"name": "sda", "total_iops": 15, "await_ms": 1}}}
	summary := hostAggregate([]M{current, next})
	if num(obj(arr(summary["disks"])[0])["await_ms"]) != 2 {
		t.Fatal("disk await must be weighted by operation rate")
	}
	idle := M{"disks": []any{M{"name": "sda", "read_ops": 106, "write_ops": 54, "io_time_ms": 550}}}
	hostRates(current, idle, 2)
	if obj(arr(idle["disks"])[0])["await_ms"] != nil {
		t.Fatal("idle disk must not invent zero latency")
	}
	reset := M{"disks": []any{M{"name": "sda", "read_ops": 1, "write_ops": 1, "io_time_ms": 2}}}
	hostRates(current, reset, 2)
	if obj(arr(reset["disks"])[0])["total_iops"] != nil {
		t.Fatal("reset counters must not become negative IOPS")
	}
}
