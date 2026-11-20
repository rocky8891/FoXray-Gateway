import { useEffect, useRef, useState } from "react";
import { Field, SettingsSection, ErrorLine } from "./ui";
import { type Dict } from "./api";

export const DEFAULT_PROXY_DNS = ["1.1.1.1", "8.8.8.8"];
export const DEFAULT_DIRECT_DNS = ["114.114.114.114", "114.114.115.115", "223.5.5.5", "223.6.6.6", "119.29.29.29"];
export function XrayObservationPolicy({value,onChange,disabled,runtime}:{runtime?:Dict;value:Dict;onChange:(key:string,value:unknown)=>void;disabled:boolean}) {
 const servers=Array.isArray(value.dns_direct_servers)?value.dns_direct_servers:DEFAULT_DIRECT_DNS;
 const proxyServers=Array.isArray(value.dns_proxy_servers)?value.dns_proxy_servers:DEFAULT_PROXY_DNS;
 const [proxyText,setProxyText]=useState(proxyServers.join("\n"));
 const [text,setText]=useState(servers.join("\n"));
 const editing=useRef(false),proxyEditing=useRef(false);
 useEffect(()=>{if(!proxyEditing.current)setProxyText(proxyServers.join("\n"))},[JSON.stringify(proxyServers)]);
 useEffect(()=>{if(!editing.current)setText(servers.join("\n"))},[JSON.stringify(servers)]);
 return <SettingsSection className="xray-observation-policy" title="DNS分流与域名识别" info="优先级：自定义规则、DNS强制代理、直连DNS清单、普通分流。智能接管启用时，将已接管LAN的传统DNS统一转发；PC无需改DNS，失败后自动尝试同组备用上游。命中代理域名规则的查询经代理，其他查询优先直连上游；直连组全部失败后再尝试代理组，明确自定义DNS规则优先（IP规则需指定含53的端口，通用IP分流不决定查询上游）。同时覆盖已启用的本机接管范围，避免服务器自身DNS旁路；仅影响TCP／UDP 53，不修改系统DNS、VPN排除及DoH／DoT；代理出口和至少一个对应上游需要可用。保存并应用可能短暂重启Xray核心。">
  <ErrorLine error={String(runtime?.error || "")} />
  <div className="xray-dns-switches">
  <label className="check"><input type="checkbox" aria-label="LAN DNS智能接管" checked={value.lan_dns_intercept!==false} disabled={disabled} onChange={e=>onChange("lan_dns_intercept",e.target.checked)} />LAN DNS智能接管</label>
  <label className="check"><input type="checkbox" aria-label="DNS强制代理" checked={value.dns_force_proxy===true} disabled={disabled} onChange={e=>onChange("dns_force_proxy",e.target.checked)} />DNS强制代理</label>
  </div>
  <div className="form-grid">
   <Field label="直连DNS上游" info="预置常用国内DNS的明确IP清单，可修改或清空；每行一个IP，最多32项。不修改系统DNS，不影响该IP的其他端口。开启DNS强制代理后，普通查询暂不使用此清单；明确指定直连的自定义规则仍可使用。">
    <textarea aria-label="直连DNS上游" rows={5} disabled={disabled} value={text} onFocus={()=>{editing.current=true}} onBlur={()=>{editing.current=false;setText(servers.join("\n"))}} onChange={e=>{setText(e.target.value);onChange("dns_direct_servers",e.target.value.split(/[\n,]+/).map(v=>v.trim()).filter(Boolean))}} />
   </Field>
   <Field label="代理DNS上游" info="智能接管下，命中代理域名规则的查询使用此组上游，直连组故障时也作为备用，通过当前Xray节点发送真实DNS查询；首选失败后切换备用。每行一个IP，最多32项。">
    <textarea aria-label="代理DNS上游" rows={5} disabled={disabled || value.lan_dns_intercept===false} value={proxyText} onFocus={()=>{proxyEditing.current=true}} onBlur={()=>{proxyEditing.current=false;setProxyText(proxyServers.join("\n"))}} onChange={e=>{setProxyText(e.target.value);onChange("dns_proxy_servers",e.target.value.split(/[\n,]+/).map(v=>v.trim()).filter(Boolean))}} />
   </Field>
  </div>
   <Field label="域名识别" info="开启后使用HTTP Host、TLS／QUIC SNI及核心目标域名，通过Webhook关联完整连接元组。不解密HTTPS正文，不识别客户端应用。ECH可能只提供外层名称；无法识别时显示未观测。关闭后停止新增域名识别并清除内存域名缓存，已有历史不删除；分流规则需要的嗅探仍保留。">
    <label className="check"><input type="checkbox" aria-label="启用域名识别" disabled={disabled} checked={value.domain_identification===true} onChange={e=>onChange("domain_identification",e.target.checked)} />启用HTTP／TLS／QUIC域名识别</label>
   </Field>
 </SettingsSection>;
}
