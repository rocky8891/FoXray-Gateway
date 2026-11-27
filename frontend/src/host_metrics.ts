import { list, record, type Dict } from "./api";

export const LOAD_OPTIONS = [["load1", "1 分钟"], ["load5", "5 分钟"], ["load15", "15 分钟"]];
export const CPU_OPTIONS = [["cpu_percent", "使用率"], ["cpu_hi", "hi · 硬中断"], ["cpu_si", "si · 软中断"], ["cpu_st", "st · 等待宿主机"]];
export const MEMORY_OPTIONS = [["memory_percent", "使用率"], ["memory_used", "used"], ["memory_free", "free"], ["memory_shared", "shared"], ["memory_buff_cache", "buff/cache"], ["memory_available", "available"]];

export function formatCapacity(value: unknown) {
  if (value == null) return "—";
  let amount = Number(value);
  if (!Number.isFinite(amount)) return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let index = 0;
  while (amount >= 1024 && index < units.length - 1) { amount /= 1024; index++; }
  return `${amount.toFixed(index ? 2 : 0)} ${units[index]}`;
}

type Metric = { key: string; label: string; percent: number | null; value: string };
const cached = new WeakMap<Dict, Record<"load" | "cpu" | "memory" | "root", Metric>>();

export function hostMetrics(snapshot: Dict) {
  const existing = cached.get(snapshot);
  if (existing) return existing;
  const policy = record(snapshot.policy);
  const sample = record(snapshot.current);
  const current = policy.enabled === false || sample.supported === false ? {} : sample;
  const number = (value: unknown) => value != null && Number.isFinite(Number(value)) ? Number(value) : null;
  const percent = (value: number | null) => value == null ? "—" : `${value.toFixed(1)}%`;
  const load = String(policy.default_load || "load1");
  const cpu = String(policy.default_cpu || "cpu_percent");
  const memory = String(policy.default_memory || "memory_percent");
  const partition = String(policy.default_partition || "/");
  const loadValue = number(current[load]);
  const cpuValue = number(current[cpu]);
  const memoryValue = number(current[memory]);
  const memoryTotal = number(current.memory_total);
  const memoryPercent = memory === "memory_percent" ? memoryValue :
    memoryValue != null && memoryTotal && memoryTotal > 0 ? memoryValue / memoryTotal * 100 : null;
  const root = list(current.mounts).find(mount => mount.path === partition);
  const rootPercent = number(root?.percent);
  const result = {
    load: { key: load, label: `平均负载 · ${LOAD_OPTIONS.find(([key]) => key === load)?.[1] || ""}`,
      percent: loadValue == null ? null : loadValue / Math.max(1, number(current.cores) || 1) * 100, value: loadValue?.toFixed(2) ?? "—" },
    cpu: { key: cpu, label: `CPU · ${CPU_OPTIONS.find(([key]) => key === cpu)?.[1] || ""}`, percent: cpuValue, value: percent(cpuValue) },
    memory: { key: memory, label: `内存 · ${MEMORY_OPTIONS.find(([key]) => key === memory)?.[1] || ""}`,
      percent: memoryPercent, value: memory === "memory_percent" ? percent(memoryPercent) : formatCapacity(memoryValue) },
    root: { key: partition, label: partition === "/" ? "根分区使用率" : `${partition} 使用率`, percent: rootPercent, value: percent(rootPercent) },
  };
  cached.set(snapshot, result);
  return result;
}
