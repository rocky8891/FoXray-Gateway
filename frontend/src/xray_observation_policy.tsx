import { useEffect, useRef, useState } from "react";
import { Field, SettingsSection, ErrorLine, SettingInfo } from "./ui";
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
 return <SettingsSection className="xray-observation-policy" title="DNS 分流与域名识别" info={<>
  <p>FoXray 按下列优先级处理已接管的 TCP / UDP 53 查询。</p>
  <ol>
   <li><strong>自定义规则：</strong>显式匹配的域名规则优先。IP 规则须指定包含 53 的端口。</li>
   <li><strong>DNS 强制代理：</strong>开启后，其余查询统一走当前 Xray 出口。</li>
   <li><strong>直连清单与普通分流：</strong>关闭强制代理时按配置选择上游。
    <ul><li>匹配代理域名的查询走代理组。</li><li>其余查询优先走直连组。直连组全部失败时，可尝试代理组。</li></ul>
   </li>
  </ol>
  <p>LAN 智能接管可让客户端继续使用原 DNS 设置。系统 DNS 设置、VPN 排除和 DoH / DoT 保持原有行为。保存并应用可能短暂重启 Xray 核心。</p>
 </>}>
  <ErrorLine error={String(runtime?.error || "")} />
  <div className="xray-dns-switches">
  <label className="check"><input type="checkbox" aria-label="LAN DNS智能接管" checked={value.lan_dns_intercept!==false} disabled={disabled} onChange={e=>onChange("lan_dns_intercept",e.target.checked)} />LAN DNS智能接管</label>
  <div className="monitor-item-option">
   <label className="check"><input type="checkbox" aria-label="DNS强制代理" checked={value.dns_force_proxy===true} disabled={disabled} onChange={e=>onChange("dns_force_proxy",e.target.checked)} />DNS 强制代理</label>
   <SettingInfo label="DNS 强制代理" text={<ul>
    <li><strong>开启后：</strong>已接管的传统 DNS 查询优先遵循显式的自定义 DNS 规则，其余查询走当前 Xray 出口。</li>
    <li><strong>LAN 智能接管已开启时：</strong>其余查询使用代理 DNS 上游。同组上游失败后会尝试备用地址。</li>
    <li><strong>使用条件：</strong>要让查询实际经代理发出，需选择可用代理节点。此开关仅影响 TCP / UDP 53。</li>
   </ul>} />
  </div>
  </div>
  <div className="form-grid">
   <Field label="直连DNS上游" info="预置常用国内DNS的明确IP清单，可修改或清空；每行一个IP，最多32项。不修改系统DNS，不影响该IP的其他端口。开启DNS强制代理后，普通查询暂不使用此清单；明确指定直连的自定义规则仍可使用。">
    <textarea aria-label="直连DNS上游" rows={5} disabled={disabled} value={text} onFocus={()=>{editing.current=true}} onBlur={()=>{editing.current=false;setText(servers.join("\n"))}} onChange={e=>{setText(e.target.value);onChange("dns_direct_servers",e.target.value.split(/[\n,]+/).map(v=>v.trim()).filter(Boolean))}} />
   </Field>
   <Field label="代理DNS上游" info="智能接管下，命中代理域名规则的查询使用此组上游，直连组故障时也作为备用，通过当前Xray节点发送真实DNS查询；首选失败后切换备用。每行一个IP，最多32项。">
    <textarea aria-label="代理DNS上游" rows={5} disabled={disabled || value.lan_dns_intercept===false} value={proxyText} onFocus={()=>{proxyEditing.current=true}} onBlur={()=>{proxyEditing.current=false;setProxyText(proxyServers.join("\n"))}} onChange={e=>{setProxyText(e.target.value);onChange("dns_proxy_servers",e.target.value.split(/[\n,]+/).map(v=>v.trim()).filter(Boolean))}} />
   </Field>
  </div>
   <Field label="域名识别" info={<ul>
    <li><strong>识别来源：</strong>HTTP Host、TLS / QUIC SNI 和 Xray 核心提供的目标域名。ECH 连接可能只暴露外层域名。</li>
    <li><strong>显示结果：</strong>将域名关联到对应连接。无法识别时显示「未观测」。</li>
    <li><strong>关闭后：</strong>停止新增识别并清除内存缓存，已有历史与分流所需的嗅探继续保留。</li>
   </ul>}>
    <label className="check"><input type="checkbox" aria-label="启用域名识别" disabled={disabled} checked={value.domain_identification===true} onChange={e=>onChange("domain_identification",e.target.checked)} />启用HTTP／TLS／QUIC域名识别</label>
   </Field>
 </SettingsSection>;
}
