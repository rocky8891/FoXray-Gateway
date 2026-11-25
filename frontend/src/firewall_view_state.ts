import type { Dict } from "./api";

export const ALL_FIREWALL_ZONES = "*";

export const firewallOrigins = [
  ["firewalld", "firewalld"],
  ["wireguard", "WireGuard"],
  ["warp", "WARP"],
  ["docker", "Docker"],
];

export function firewallRuleOrigin(row: Dict, driver = "firewalld") {
  return String(row.rule_origin || driver).toLowerCase();
}

export function firewallRuleOriginLabel(row: Dict, driver = "firewalld") {
  const key = firewallRuleOrigin(row, driver);
  return firewallOrigins.find(([origin]) => origin === key)?.[1] || key;
}

export function firewallViewState(data: Dict | null, driver: string, zone: string, error = "") {
  const native = driver !== "firewalld";
  const service = (data?.firewalld || {}) as Dict;
  const current = !!data && data.driver === driver;
  const serviceState = String(service.state || "");
  const scopeCurrent = current && (native || data?.zone === zone || !!data?.error || serviceState !== "active");
  const installed = data?.installed === true;
  const complete = scopeCurrent && !error && installed && data?.complete === true && !data?.error;
  const inventoryLabel = error || (current && data?.error)
    ? "规则读取失败"
    : !scopeCurrent
      ? "规则读取中"
      : !installed
        ? "规则不可用"
        : complete ? "规则读取完整" : "规则读取不完整";
  let label = "读取中", tone = "muted";
  if (native) {
    label = current && data?.installed === false && !error ? "工具未安装" : inventoryLabel;
    tone = complete ? "accent" : label === "规则读取中" ? "muted" : "fair";
  }
  else if (error) { label = "状态未确认"; tone = "fair"; }
  else if (current && data?.installed === false) { label = "工具未安装"; tone = "fair"; }
  else if (scopeCurrent && serviceState === "active") { label = "运行中"; tone = "accent"; }
  else if (scopeCurrent && serviceState === "inactive") { label = "已停止"; tone = "fair"; }
  else if (scopeCurrent && serviceState === "failed") { label = "服务异常"; tone = "poor"; }
  else if (scopeCurrent) { label = "状态未确认"; tone = "fair"; }
  const canManage = complete && driver !== "ufw" && (native || serviceState === "active");
  const canServiceControl = current && !error && installed && !native && ["active", "inactive", "failed"].includes(serviceState);
  return {
    native, label, tone, current: scopeCurrent, complete, canManage, canServiceControl,
    running: !native && scopeCurrent && !error && serviceState === "active",
    autostart: current && !native && !error && typeof service.autostart === "boolean" ? service.autostart : null,
    inventoryLabel,
  };
}

export function firewallTargetLabel(value: unknown) {
  const raw = String(value || "");
  return ({ default: "系统默认（default）", ACCEPT: "允许", DROP: "静默丢弃", REJECT: "拒绝并回应", "%%REJECT%%": "拒绝并回应" } as Record<string, string>)[raw] || raw || "未读取";
}

export function firewallBindings(value: unknown) {
  if (Array.isArray(value)) return value.map(String).map(v => v.trim()).filter(v => v && v !== "无");
  const text = String(value || "").trim();
  return !text || text === "无" || text === "—" ? [] : text.split(/\s+/);
}
