import { RefreshCw, Network, SlidersHorizontal } from "lucide-react";
import { FirewallIcon } from "./firewall_icons";
import { Button, Field, SettingInfo } from "./ui";
import { list, record, type Dict } from "./api";
import { firewallBindings, firewallTargetLabel, firewallViewState } from "./firewall_view_state";
import "./firewall_summary.css";

export function FirewallWorkspace({ data, error, driver, zone, refresh, changeDriver, changeZone, manageInterfaces, serviceAction, lastRead, refreshing, selectedFilter, filterRules, viewRules, addService,manageSources,editPolicy,configScope,changeScope }: {
  data: Dict | null; error: string; driver: string; zone: string;
  refresh: () => void; changeDriver: (value: string) => void; changeZone: (value: string) => void;
  manageInterfaces: () => void; serviceAction: (action: string) => void;
  lastRead: number; refreshing: boolean;
  selectedFilter: string; filterRules: (value:string)=>void; viewRules:(kind:string)=>void; addService:()=>void;
  manageSources:()=>void;editPolicy:()=>void;configScope:string;changeScope:(value:string)=>void;
}) {
  const view = firewallViewState(data, driver, zone, error), info = view.current && !error ? record(data?.zone_info) : {};
  const permanentTarget=String(record(record(data?.zone_policy).permanent).target||"");
  const types = list(data?.types), zones = Array.isArray(data?.zones) ? data.zones.map(String) : [];
  const interfaces = firewallBindings(info.interfaces), sources = firewallBindings(info.sources), target = String(info.target || "");
  const entries = view.current && !error ? list(data?.entries) : [], managed = entries.filter(r => r.managed).length;
  const blocked = entries.filter(r=>["drop","reject"].includes(String(r.action).toLowerCase())).length;
  const forwards = entries.filter(r=>r.kind === "forward").length;
  const icmpBlocks = firewallBindings(info["icmp-blocks"]);
  const stats = [{id:"all",label:"全部规则",value:entries.length,hint:"当前查看范围"},{id:"managed",label:"本应用管理",value:managed,hint:"明确纳入管理"},{id:"external",label:"系统／外部",value:entries.length-managed,hint:"需先纳管再编辑"},{id:"blocked",label:"显式阻断",value:blocked,hint:"drop／reject条目"}];
  const count = (number: number) => view.complete ? String(number) : "—";
  return <section className="firewall-workspace" aria-label="防火墙状态与管理范围">
    <div className="firewall-service-bar">
      <div className="firewall-service-identity"><span className="firewall-service-symbol"><FirewallIcon size={24} /></span><div>
        <div className="firewall-service-title"><h2>{view.native?"防火墙工具":"防火墙服务"}</h2><SettingInfo label="防火墙" text="选择管理工具和查看区域，再管理规则。查看范围不改变网络配置；接口归属、服务启停和规则修改均需先生成变更预览。" /><span className={"firewall-status " + view.tone}><i />{view.label}</span></div>
        <div className="firewall-service-meta"><span>管理工具：{driver}</span><span>{view.native ? "不提供独立服务状态" : view.autostart == null ? "开机启动：未确认" : "开机启动：" + (view.autostart ? "已开启" : "已关闭")}</span><span className={view.complete ? "muted" : "fair"}>{view.inventoryLabel}</span>{lastRead>0 && view.current && <span>{error ? "上次读取：" : "最近读取："}{new Date(lastRead).toLocaleTimeString('zh-CN',{hour12:false})}</span>}</div>
      </div></div>
      <div className="firewall-service-buttons"><Button disabled={refreshing} onClick={refresh}><RefreshCw size={15} />{refreshing ? "刷新中" : "刷新"}</Button>{!view.native && <><Button disabled={!view.canServiceControl} onClick={()=>serviceAction(view.running ? "stop" : "start")}>{view.running ? "停止服务" : "启动服务"}</Button><Button disabled={!view.canServiceControl||view.autostart==null} onClick={()=>serviceAction(view.autostart ? "disable" : "enable")}>{view.autostart ? "关闭自启" : "开启自启"}</Button></>}</div>
    </div>

    <div className="firewall-scope-bar">
      <div className="firewall-scope-fields">
        <Field label="管理工具" info="仅切换查看和编辑规则的工具，不会安装、启停或迁移防火墙。nftables、iptables已安装不代表独立服务正在运行。"><select aria-label="防火墙管理工具" value={driver} onChange={e=>changeDriver(e.target.value)}>
          {(types.length ? types : [{id:driver,installed:true}]).map(t=><option key={String(t.id)} value={String(t.id)} disabled={t.installed===false}>{String(t.id)}{t.installed===false ? " · 未安装" : ""}{t.id==="ufw" ? " · 只读" : ""}</option>)}
        </select></Field>
        {!view.native && <Field label="查看区域" info="仅决定下方显示和管理哪个区域的规则，不会改变系统默认区域，也不会移动网卡或来源地址。区域是否作用于某个连接，取决于接口、来源地址归属及其他规则。"><select aria-label="查看防火墙区域" value={zone} onChange={e=>changeZone(e.target.value)}>{[...new Set([...zones,zone])].map(z=><option key={z}>{z}</option>)}</select></Field>}
      </div>
      <div className="firewall-view-mode"><Field label="配置范围" info="这里只筛选显示运行时、永久或存在差异的条目，不决定写入范围。规则与区域配置的变更仍分别核对运行时和永久配置，在变更预览中明确展示。"><select aria-label="防火墙配置范围" disabled={view.native} title={view.native?"当前工具仅提供系统规则快照":undefined} value={configScope} onChange={e=>changeScope(e.target.value)}><option value="all">全部配置</option><option value="runtime">运行时</option><option value="permanent">永久配置</option><option value="difference">存在差异</option></select></Field></div>
    </div>

    <div className={"firewall-overview-grid"+(view.native ? " is-native" : "")}>
      {!view.native && <section className="firewall-binding-block firewall-layout-section">
        <div className="firewall-detail-heading"><h3><Network size={17} />区域与流量归属<SettingInfo label="区域与流量归属" text="接口和来源地址是区域匹配条件；这不是访问白名单。当前显示的是所选区域返回的真实绑定信息。" /></h3><div className="firewall-binding-actions"><Button size="compact" disabled={!view.canManage} onClick={manageInterfaces}>管理归属</Button><Button size="compact" disabled={!view.canManage} onClick={manageSources}>管理来源</Button></div></div>
        <div className="firewall-zone-name"><strong>{zone}</strong><span>查看区域</span></div>
        <dl className="firewall-binding-list">
          <div><dt>绑定接口<SettingInfo label="接口归属" text="经过这些网络接口的流量按此区域处理；来源地址绑定等更具体的归属条件可能影响最终区域。调整归属需先生成变更预览并确认。" /></dt><dd>{!view.current || error ? "未读取" : interfaces.length ? interfaces.map(v=><span className="firewall-address-chip" key={v}>{v}</span>) : "未绑定接口"}</dd></div>
          <div><dt>来源地址<SettingInfo label="来源地址归属" text="按来源IP、网段、MAC地址或IP集合把流量归入此区域。这是区域匹配条件，不是允许访问的来源白名单。未设置仅表示没有来源地址绑定，不代表允许所有来源。" /></dt><dd>{!view.current || error ? "未读取" : sources.length ? sources.map(v=><span className="firewall-address-chip" key={v}>{v}</span>) : "未设置"}</dd></div>
        </dl>
      </section>}
      <section className="firewall-rule-overview firewall-layout-section" aria-label="规则概览">
        <div className="firewall-detail-heading"><h3>规则概览<SettingInfo label="规则概览" text="按当前工具和区域返回的规则条目统计，合并运行时与永久配置，不是底层展开的内核规则数量。显式阻断只统计动作明确为drop或reject的条目，不含默认策略推断。读取不完整时不显示为零。点击统计项筛选下方规则。" /></h3><Button size="compact" onClick={()=>filterRules("all")}>查看全部规则</Button></div>
        <div className="firewall-stats-grid">{stats.map(stat=><button type="button" key={stat.id} className={"button secondary firewall-stat-button"+(stat.id!=="all"&&selectedFilter===stat.id ? " is-selected" : "")} aria-label={"按"+stat.label+"筛选规则"} aria-pressed={stat.id!=="all"&&selectedFilter===stat.id} disabled={!view.complete} onClick={()=>filterRules(stat.id)}><strong>{count(stat.value)}</strong><span>{stat.label}</span><small>{stat.hint}</small></button>)}</div>
        {view.complete && entries.length>0 ? <div className="firewall-ownership"><div className="firewall-ownership-meter" role="img" aria-label={"本应用管理"+managed+"条，系统或外部"+(entries.length-managed)+"条"}><span style={{width:(100*managed/entries.length)+"%"}} /></div><div className="firewall-ownership-legend"><span>本应用管理 {managed}</span><span>系统／外部 {entries.length-managed}</span></div></div> : <div className="firewall-ownership-legend">{view.complete ? "当前范围暂无规则条目" : "规则读取未完成，统计暂不可用"}</div>}
      </section>
    </div>
    {!view.native ? <section className="firewall-policy-strip firewall-layout-section" aria-label="区域策略">
      <div className="firewall-detail-heading"><h3><SlidersHorizontal size={17} />区域策略<SettingInfo label="默认处理策略" text="对应firewalld区域target配置，必须结合具体规则和流量方向判断。系统默认（default）不等于全部允许。编辑区域策略可修改默认行为、IP地址伪装和ICMP。默认行为可仅保存永久配置，或完整核对后重新加载生效；所有修改先预览、确认并保留回退。" /></h3><Button size="compact" disabled={!view.canManage||record(data?.zone_policy).complete!==true} onClick={editPolicy}>编辑区域策略</Button></div>
      <div className="firewall-policy-grid">
        <div><span>默认行为</span><div className="firewall-target-inline"><strong>{firewallTargetLabel(target)}</strong>{target && <code>运行时 · {target}</code>}</div>{permanentTarget&&permanentTarget!==target.replace("%%REJECT%%","REJECT")&&<small className="fair">永久：{firewallTargetLabel(permanentTarget)} · 待重新加载</small>}</div>
        <div><span>IP地址伪装</span><strong>{!view.current || error || typeof data?.masquerade!=="boolean" ? "未确认" : data.masquerade ? "已开启" : "已关闭"}</strong></div>
        <div><span>ICMP阻断配置<SettingInfo label="ICMP阻断配置" text="仅展示所选区域返回的ICMP阻断项和反向匹配开关，不据此推断某个ICMP报文是否可达；最终还需结合默认策略和其他规则。" /></span><strong title={icmpBlocks.join("、")}>{!("icmp-blocks" in info) ? "未读取" : icmpBlocks.length ? icmpBlocks.length+"项阻断配置" : "未配置阻断项"}</strong>{info["icmp-block-inversion"]==="yes" && <small>反向匹配开启</small>}</div>
        <div><span>转发规则</span><button type="button" className="firewall-rule-jump" disabled={!view.complete} onClick={()=>viewRules("forward")}>{count(forwards)} 条 · 查看</button></div>
      </div>
      <div className="firewall-allowed-services"><span>允许服务</span><div>{!view.current || error ? "未读取" : Array.isArray(data?.allowed_services) && data.allowed_services.length ? data.allowed_services.map(String).map(name=><span className="firewall-address-chip" key={name}>{name}</span>) : "未配置"}</div><Button size="compact" disabled={!view.canManage} onClick={addService}>添加服务</Button></div>
    </section> : <div className="firewall-native-note">当前显示 {driver} 的系统规则；不使用 firewalld 的区域与接口绑定。{driver==="ufw" ? "此视图只读。" : "编辑仅针对明确纳入本应用管理的规则。"}</div>}
  </section>;
}
