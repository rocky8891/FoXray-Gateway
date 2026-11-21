import type { Dict } from "./api";
export type InterfaceLink = "ready" | "down" | "unknown" | "disabled";
export function interfaceLinkState(row: Dict): InterfaceLink {
  if (!row.admin_up) return "disabled";
  const flags=Array.isArray(row.flags)?row.flags.map(String):[];
  const state=String(row.operstate||"");
  if(state==="UP" || (state==="UNKNOWN"&&(flags.includes("LOWER_UP")||flags.includes("RUNNING"))))return "ready";
  if(state==="DOWN"||flags.includes("NO-CARRIER"))return "down";
  return "unknown";
}
export const interfaceLinkLabels:Record<InterfaceLink,string>={ready:"链路就绪",down:"链路未就绪",unknown:"链路未确认",disabled:"接口已停用"};
export function interfaceOwner(row:Dict):string {return row.managed_by_vpn?"module":row.managed?"app":"system"}
export const interfaceOwnerLabels:Record<string,string>={module:"VPN／代理维护",app:"FoXray创建",system:"系统接口"};
export function interfaceEditReason(row:Dict):string {
  return row.ifname==="lo"?"回环接口不在此修改":row.managed_by_vpn?"由VPN／代理模块维护，请在对应模块管理":row.protected?"受保护接口；修改需确认影响，并保留独立回退":"修改前生成变更预览";
}
