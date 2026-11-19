export type Dict = Record<string, unknown>;
export interface Metrics {
  active_connections?: number;
  active_connections_tcp?: number;
  active_connections_udp?: number;
  connections_at?: number;
  connections_note?: string;
  connections_label?: string;
  latency_ms?: number | null;
  latency_average_ms?: number | null;
  latency_average_samples?: number;
  latency_average_window_minutes?: number;
  latency_average_min_samples?: number;
  bandwidth_kib_s?: number | null;
  bandwidth_at?: number | null;
  request_success_pct?: number | null;
  checked_at?: number;
  icmp_success_pct?: number | null;
  icmp_average_ms?: number | null;
  packet_loss_status?: string;
  packet_loss_at?: number;
  packet_loss_ttl?: number;
}
export interface Score {
  id: string;
  score: number | null;
  eligible: boolean;
  complete: boolean;
  basis: string[];
  selected: string[];
  caps: Record<string, number>;
  missing: string[];
  reason: string;
  latency_average_ms: number;
  latency_average_samples: number;
}
export interface Node {
  id: string;
  name: string;
  protocol: string;
  source: string;
  native: boolean;
  enabled: boolean;
  priority: number;
  subscription?: string | null;
  active: boolean;
  status: string;
  metrics: Metrics;
  score: Score;
  geo?: { country?: string; ip?: string; checked_at?: number };
}
export interface Subscription {
  enabled?: boolean;
  created?: number | null;
  id: string;
  name: string;
  auto_update: boolean;
  interval: number;
  updated: number;
  count: number;
  error?: string;
}
export interface Config extends Dict {
  score_latency_window_minutes?: number;
  score_latency_min_samples?: number;
  latency_enabled?: boolean;
  enabled: boolean;
  mode: string;
  selected: string;
  interval: number;
  timeout: number;
  failures: number;
  hold: number;
  improvement: number;
  confirmations: number;
  route_profile: string;
  capture_host: boolean;
  capture_lan: boolean;
  score_metrics: string[];
  score_weights: Record<string, number>;
  latency_url: string;
  bypass_ips: string[];
  endpoints: Dict[];
}
export interface State {
  monitoring_items?: Dict;
  connection_monitor?: Dict;
  version: string;
  backend: string;
  frontend: string;
  read_only: boolean;
  config: Config;
  nodes: Node[];
  native: { nodes: Dict[]; subscriptions: Subscription[] };
  scores: Score[];
  state: {
    active: string;
    applied: boolean;
    last_switch: number;
    active_since?: number;
    recovery_issue?: string;
    history: Record<string, Dict[]>;
  };
  components: {
    xray: boolean;
    warp: boolean;
    wireguard: boolean;
    komari: boolean;
    komari_url?: string;
  };
  manager_settings: Dict;
  icmp_policy: Dict;
  proxy: Dict;
}
export interface Task {
  source?: "manual" | "automatic" | "unknown";
  log_id?: string;
  id: string;
  title: string;
  group: string;
  status: string;
  message: string;
  started: number;
  finished: number;
  details: string[];
}
export interface Operation {
  id: string;
  time: number;
  level: string;
  action: string;
  status: string;
  message: string;
  details: Dict;
}
export interface Plan {
  zone_changes?: {
    interface: string;
    target_zone: string;
    runtime_zone: string;
    permanent_zone: string;
    commands: string[];
    rollback: string[];
  }[];
  preview_only?: boolean;
  id: string;
  kind: string;
  summary: string;
  status: string;
  created: number;
  expires: number;
  warnings: string[];
  preview: string[];
}
export interface NetSnapshot extends Dict {
  items?: Dict[];
  ipv4?: Dict[];
  ipv6?: Dict[];
  managed?: Dict[];
  plans: Plan[];
  complete?: boolean;
}
export type Page =
  | "overview"
  | "nodes"
  | "subscriptions"
  | "monitoring"
  | "logs"
  | "history"
  | "switches"
  | "events"
  | "xray"
  | "warp"
  | "wireguard"
  | "traffic"
  | "network"
  | "routes"
  | "rules"
  | "firewall"
  | "diagnostics"
  | "monitor"
  | "settings";
export async function api<T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener("abort", abort, { once: true });
  if (signal?.aborted) abort();
  const timer = setTimeout(abort, body === undefined ? 12000 : 45000);
  try {
    const response = await fetch(path, {
      method: body === undefined ? "GET" : "POST",
      headers: body === undefined ? {} : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal,
    });
    const data = await response.json();
    if (!response.ok)
      throw new Error(
        typeof data.error === "string" ? data.error : "请求未完成",
      );
    return data as T;
  } catch (error) {
    if (controller.signal.aborted)
      throw new Error(
        body === undefined
          ? "数据读取超时，请刷新重试；持续发生时查看管理服务状态"
          : "操作请求等待超时，请先查看当前任务状态",
      );
    throw error;
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", abort);
  }
}
export function record(value: unknown): Dict {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Dict)
    : {};
}
export function list(value: unknown): Dict[] {
  return Array.isArray(value) ? value.map(record) : [];
}
export function text(value: unknown): string {
  return value == null
    ? "—"
    : typeof value === "object"
      ? JSON.stringify(value)
      : String(value);
}
export function timestamp(value: unknown): string {
  const n = Number(value);
  return n > 0
    ? new Date(n * 1000).toLocaleString("zh-CN", { hour12: false })
    : "未记录";
}
export function bandwidth(kib: unknown): string {
  const n = Number(kib);
  if (kib == null || !Number.isFinite(n) || n < 0) return "未测";
  return n * 1024 > 1000000
    ? ((n * 1024) / 1000000).toFixed(2) + " MB/s"
    : ((n * 1024) / 1000).toFixed(1) + " KB/s";
}
export function errorMessage(e: unknown) {
  return e instanceof Error ? e.message : "操作未完成";
}

export function scopedState(state: State, module: "xray" | "warp"): State {
  const nodes = state.nodes.filter(
    (n) => (n.id === "warp") === (module === "warp"),
  );
  return {
    ...state,
    nodes,
    scores: state.scores.filter(
      (n) => (n.id === "warp") === (module === "warp"),
    ),
    config: {
      ...state.config,
      endpoints: state.config.endpoints.filter(
        (n) => (n.id === "warp") === (module === "warp"),
      ),
    },
  };
}
