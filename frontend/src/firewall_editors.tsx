import { record,list,type Dict,type Plan } from "./api";
import { Field,NumberInput,SettingInfo } from "./ui";
import { firewallTargetLabel } from "./firewall_view_state";
import { FirewallServiceSelect } from "./firewall_service_select";

export function ZoneSourceEditor({value,onChange,zones,sources}:{value:Dict;onChange:(k:string,v:unknown)=>void;zones:string[];sources:string[]}){
 const release=value.binding_action==="unbind";
 return <><p className="hint">来源地址决定适用哪个区域的整套规则，不是访问白名单。支持IPv4／IPv6地址或网段；本次同时处理运行时与永久配置。</p><div className="form-grid">
  <Field label="操作"><select aria-label="来源归属操作" value={String(value.binding_action||"bind")} onChange={e=>onChange("binding_action",e.target.value)}><option value="bind">绑定／移入区域</option><option value="unbind">解除区域绑定</option></select></Field>
  <Field label={release?"解除绑定的区域":"目标区域"}><select aria-label="来源归属目标区域" value={String(value.zone)} onChange={e=>onChange("zone",e.target.value)}>{zones.map(z=><option key={z}>{z}</option>)}</select></Field>
  <Field label="来源IP／网段" info={<ul><li><strong>支持格式。</strong>填写单个 IPv4、IPv6 地址或 CIDR，例如 192.0.2.10、192.0.2.0/24 或 2001:db8::/64。</li><li><strong>变更检查。</strong>分别核对运行时和永久归属；发现冲突或重叠网段时，不生成变更计划。</li><li><strong>编辑限制。</strong>MAC 与 IP 集合目前仅展示，不能在此编辑。</li></ul>}><input aria-label="来源归属IP或网段" value={String(value.source||"")} onChange={e=>onChange("source",e.target.value)} placeholder="192.168.1.0/24" /></Field>
  {sources.length>0&&<Field label="当前查看区域的运行时来源"><select aria-label="选择已有来源" value="" onChange={e=>onChange("source",e.target.value)}><option value="">选择来源以填入</option>{sources.map(source=><option key={source} value={source} disabled={source.startsWith("ipset:")||/^(?:[\da-f]{2}:){5}[\da-f]{2}$/i.test(source)}>{source}</option>)}</select></Field>}
 </div></>;
}

export function ZonePolicyEditor({value,onChange,policy}:{value:Dict;onChange:(k:string,v:unknown)=>void;policy:Dict}){
 const runtime=record(policy.runtime),permanent=record(policy.permanent),types=Array.isArray(policy.icmp_types)?policy.icmp_types.map(String):[],selected=Array.isArray(value.icmp_blocks)?value.icmp_blocks.map(String):[];
 const target=String(value.target||permanent.target||"default"),apply=String(value.target_apply||"permanent"),targetChanged=target!==String(permanent.target)||(apply==="immediate"&&target!==String(runtime.target));
 return <><div className="firewall-editor-baseline"><span>区域 <strong>{String(value.zone)}</strong></span><span>运行时默认行为 <strong>{firewallTargetLabel(runtime.target)}</strong></span><span>永久默认行为 <strong>{firewallTargetLabel(permanent.target)}</strong></span></div>
 <p className="hint">默认行为可修改。默认仅写永久配置；立即生效需先核对全局运行时差异并重新加载。应用后120秒内确认，失败或未确认按原值回退。</p>
 {policy.different===true&&<p className="hint">运行时与永久策略存在差异。地址伪装、ICMP草稿从运行时初始化；默认行为从永久配置初始化，生效范围以本次选择和预览为准。</p>}
 <div className="form-grid"><Field label="默认行为" info={<ul><li><strong>default。</strong>使用系统默认处理，不等于全部允许。</li><li><strong>ACCEPT。</strong>允许未被其他适用规则终止的流量。</li><li><strong>DROP / REJECT。</strong>DROP 静默丢弃；REJECT 拒绝并回应。</li><li><strong>判断范围。</strong>结合流量方向和具体规则判断实际结果。</li></ul>}><select aria-label="区域默认行为" value={target} onChange={e=>{onChange("target",e.target.value);onChange("target_ack",false);onChange("reload_ack",false)}}><option value="default">系统默认：ICMP允许，其余拒绝（default）</option><option value="ACCEPT">允许（ACCEPT）</option><option value="DROP">静默丢弃（DROP）</option><option value="REJECT">拒绝并回应（REJECT）</option></select></Field><Field label="默认行为生效方式" info={<ul><li><strong>仅保存永久配置。</strong>当前运行时行为保持不变；下次加载永久配置时生效。</li><li><strong>立即生效。</strong>会重新加载 firewalld。先核对运行时与永久配置的差异，避免丢失仅运行时的条目。</li></ul>}><select aria-label="默认行为生效方式" value={apply} onChange={e=>{onChange("target_apply",e.target.value);onChange("reload_ack",false)}}><option value="permanent">仅保存永久配置</option><option value="immediate">检查后立即生效</option></select></Field><Field label="IP地址伪装"><select aria-label="区域IP地址伪装" value={value.masquerade?"on":"off"} onChange={e=>onChange("masquerade",e.target.value==="on")}><option value="on">开启</option><option value="off">关闭</option></select></Field></div>
 {typeof runtime.forward === "boolean" && <label className="check"><input type="checkbox" checked={value.forward===true} onChange={event=>onChange("forward",event.target.checked)}/>允许同一区域内转发</label>}
 {targetChanged&&<div className="firewall-target-impact"><p className="hint">{apply==="immediate"?"将重新加载整个firewalld。存在运行时专有规则或配置差异时拒绝重载，请改为仅保存永久配置。":"本次默认行为只写永久配置；当前运行时不变，下次重新加载后生效。"} 默认行为变化可能放宽访问，或中断管理连接与转发，请先核对放行规则。</p><label className="check risk"><input type="checkbox" aria-label="确认默认行为影响" checked={value.target_ack===true} onChange={e=>onChange("target_ack",e.target.checked)}/>我已核对默认行为对访问、管理连接和转发的影响</label>{apply==="immediate"&&<label className="check risk"><input type="checkbox" aria-label="确认全局重新加载影响" checked={value.reload_ack===true} onChange={e=>onChange("reload_ack",e.target.checked)}/>允许检查通过后重新加载整个firewalld，期间不并发修改防火墙</label>}</div>}
 <section className="settings-section"><h3 className="settings-section-title">ICMP阻断类型<SettingInfo label="ICMP阻断类型" text="勾选类型会加入当前区域ICMP阻断列表。ICMP承担诊断、路径MTU与错误通知等功能，应只按明确需求阻断。列表来自当前系统支持的类型，不提供禁止所有ICMP的快捷开关。" /></h3>
 <div className="firewall-icmp-choices">{types.map(name=><label className="check" key={name}><input type="checkbox" aria-label={"阻断ICMP "+name} checked={selected.includes(name)} onChange={e=>onChange("icmp_blocks",e.target.checked?[...selected,name]:selected.filter(v=>v!==name))}/>{name}</label>)}</div></section>
 <details className="advanced-form"><summary>高级选项</summary><label className="check"><input type="checkbox" aria-label="ICMP反向匹配" checked={!!value.icmp_inversion} onChange={e=>onChange("icmp_inversion",e.target.checked)}/>ICMP反向匹配</label><p className={value.icmp_inversion?"hint":"small muted"}>{value.icmp_inversion?"已反转语义：列表中的类型成为例外，其余ICMP类型会被阻断。空列表可能阻断所有ICMP。":"默认关闭；开启后列表含义反转，必须核对变更预览。"}</p></details>
 </>;
}

export function FirewallDestinationField({value,onChange}:{value:Dict;onChange:(value:string)=>void}) {
  return <Field label="目标 IP" info={<ul><li><strong>支持格式。</strong>填写单个 IPv4、IPv6 地址或 CIDR。</li><li><strong>留空含义。</strong>不增加目标地址限制；仍须满足该规则的其他条件。</li></ul>}>
    <input aria-label="目标 IP" value={String(value.destination || "")} onChange={event => onChange(event.target.value)} />
  </Field>;
}

export function FirewallAuditFields({value,onChange}:{value:Dict;onChange:(key:string,value:unknown)=>void}) {
  return <>
    <label className="check"><input type="checkbox" checked={value.audit===true} onChange={event=>onChange("audit",event.target.checked)}/>记录审计</label>
    <Field label="动作速率限制" hint="留空不限速，例如 5/m"><input value={String(value.action_rate||"")} onChange={event=>onChange("action_rate",event.target.value)}/></Field>
  </>;
}

export function RichRuleEditor({value,onChange,services}:{value:Dict;onChange:(k:string,v:unknown)=>void;services:string[]}){
 const match=String(value.rich_match||"port");
 return <><p className="small muted">高级规则使用firewalld Rich Rules；按表单生成，支持端口、服务或所选来源的全部流量。无法无损解析的现有规则继续只读。</p><div className="form-grid">
 <Field label="备注名称"><input aria-label="高级规则名称" value={String(value.name||"")} onChange={e=>onChange("name",e.target.value)}/></Field>
 <Field label="匹配类型"><select aria-label="高级规则匹配类型" value={match} onChange={e=>onChange("rich_match",e.target.value)}><option value="port">协议／端口</option><option value="service">预定义服务</option><option value="all">全部流量</option></select></Field>
 <Field label="源 IP" info="留空表示当前区域内所有来源，并非整个系统所有流量。"><input aria-label="高级规则来源" value={String(value.source||"")} placeholder="192.168.1.0/24" onChange={e=>onChange("source",e.target.value)}/></Field>
 <FirewallDestinationField value={value} onChange={destination => onChange("destination", destination)} />
 {match==="port"&&<><Field label="协议"><select aria-label="高级规则协议" value={String(value.protocol||"tcp")} onChange={e=>onChange("protocol",e.target.value)}><option value="tcp">TCP</option><option value="udp">UDP</option><option value="tcpudp">TCP＋UDP</option></select></Field><Field label="端口／范围"><input aria-label="高级规则端口" value={String(value.port||"")} placeholder="443 或 8000-8100" onChange={e=>onChange("port",e.target.value)}/></Field></>}
 {match==="service"&&<Field label="预定义服务"><FirewallServiceSelect label="高级规则服务" value={String(value.service||"")} services={services} onChange={name=>onChange("service",name)}/></Field>}
 <Field label="动作"><select aria-label="高级规则动作" value={String(value.action||"accept")} onChange={e=>onChange("action",e.target.value)}><option value="accept">允许</option><option value="drop">丢弃</option><option value="reject">拒绝并回应</option></select></Field>
 </div><details className="advanced-form"><summary>高级选项{(value.priority || value.logging || value.audit || value.action_rate) ? " · 已设置" : ""}</summary><div className="form-grid"><Field label="地址族"><select aria-label="高级规则地址族" value={String(value.family||"ipv4")} onChange={e=>onChange("family",e.target.value)}><option value="ipv4">IPv4</option><option value="ipv6">IPv6</option></select></Field><Field label="优先级" hint="-32768至32767；0使用firewalld默认顺序"><NumberInput min={-32768} max={32767} value={Number(value.priority||0)} onChange={e=>onChange("priority",Number(e.target.value))}/></Field></div><label className="check"><input type="checkbox" checked={!!value.logging} onChange={e=>onChange("logging",e.target.checked)}/>记录匹配日志</label>{value.logging===true&&<Field label="日志速率限制"><input aria-label="高级规则日志速率" value={String(value.log_rate||"5/m")} onChange={e=>onChange("log_rate",e.target.value)}/></Field>}<FirewallAuditFields value={value} onChange={onChange}/></details>
 <div className="firewall-human-summary">{value.source?String(value.source):"当前区域全部来源"} → {String(value.destination || "任意目标")} → {match==="port"?String(value.protocol||"tcp").toUpperCase()+" / "+String(value.port||"未填写端口"):match==="service"?"服务 "+String(value.service||"未选择"):"全部流量"} → {({accept:"允许",drop:"丢弃",reject:"拒绝并回应"} as Record<string,string>)[String(value.action||"accept")]}</div></>;
}

export function FirewallChangeDetails({plan}:{plan:Plan}){
 const details=record(plan.details),sources=list(details.source_changes),policies=list(details.policy_changes);
 return <>{sources.length>0&&<section className="settings-section"><h3>来源归属差异</h3>{sources.map((row,i)=><p key={i}><strong>{String(row.scope)}</strong> · {String(row.source)}：{String(row.before)} → {String(row.after)}</p>)}</section>}
 {policies.length>0&&<section className="settings-section"><h3>区域策略差异</h3>{policies.map((row,i)=>{const before=record(row.before),after=record(row.after),icmp=(v:Dict)=>Array.isArray(v.icmp_blocks)?v.icmp_blocks.map(String).join("、")||"未配置":"未读取";return <div className="firewall-policy-diff" key={i}><strong>{String(row.scope)}</strong><p>地址伪装：{before.masquerade?"开启":"关闭"} → {after.masquerade?"开启":"关闭"}</p><p>ICMP列表：{icmp(before)} → {icmp(after)}</p><p>反向匹配：{before.icmp_inversion?"开启":"关闭"} → {after.icmp_inversion?"开启":"关闭"}</p><p>默认行为：{firewallTargetLabel(before.target)} → {firewallTargetLabel(after.target)}{before.target===after.target?"（保持）":""}</p></div>})}</section>}
 {!!plan.rollback?.length&&<details className="advanced-form"><summary>回退命令</summary><pre className="command-preview">{plan.rollback.join("\n")}</pre></details>}</>;
}

export function FirewallRuleSummary({value}:{value:Dict}){
 const action=({accept:"允许",drop:"丢弃",reject:"拒绝",masquerade:"地址伪装",snat:"源地址转换"} as Record<string,string>)[String(value.action)]||String(value.action||"未识别");
 return <><div className="firewall-human-summary">{String(value.source||"当前区域全部来源")} → {String(value.destination || "任意目标")} → {value.service?"服务 "+String(value.service):value.port?String(value.protocol||"").toUpperCase()+" / "+String(value.port):"匹配条件见原始规则"} → {action}</div><dl className="firewall-rule-facts"><div><dt>区域</dt><dd>{String(value.zone||"—")}</dd></div><div><dt>管理归属</dt><dd>{value.managed?"本应用管理":"系统／外部"}</dd></div><div><dt>运行时</dt><dd>{value.partial_runtime?"部分生效":value.runtime?"存在":"不存在"}</dd></div><div><dt>永久配置</dt><dd>{value.partial_permanent?"部分存在":value.permanent?"存在":"不存在"}</dd></div><div><dt>编辑能力</dt><dd>{value.editable===false?"只读：当前表单不能无损重建全部条件":value.managed?"可通过预览修改":"需先纳入管理"}</dd></div></dl><details className="advanced-form"><summary>原始规则与完整字段</summary><pre className="command-preview">{String(value.raw||"未提供原始规则")}</pre><pre className="json-details">{JSON.stringify(value,null,2)}</pre></details></>;
}
