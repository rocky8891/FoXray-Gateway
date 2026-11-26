import { useRef } from "react";
import { ZoneSettingsIcon } from "./firewall_feature_icons";
import { FirewallManagerIcon, ZoneOwnershipIcon, FirewallOverviewIcon } from "./workspace_icons";
import { RefreshCw } from "lucide-react";
import { FirewallIcon } from "./firewall_icons";
import { Button, Field, SettingInfo } from "./ui";
import { list, record, type Dict } from "./api";
import { ALL_FIREWALL_ZONES, firewallBindings, firewallOrigins, firewallTargetLabel, firewallViewState } from "./firewall_view_state";
import "./firewall_summary.css";

export function FirewallWorkspace({ crossZone = false, serviceData,serviceError,rulesLoading,data, error, driver, zone, refresh, changeDriver, changeZone, manageInterfaces, serviceAction, lastRead, refreshing, selectedFilter, filterRules, viewRules, addService,manageSources,editPolicy,editDefault,configScope,changeScope,origin,changeOrigin }: {
  crossZone?: boolean;
  serviceData:Dict|null;serviceError:string;rulesLoading:boolean;
  data: Dict | null; error: string; driver: string; zone: string;
  refresh: () => void; changeDriver: (value: string) => void; changeZone: (value: string) => void;
  manageInterfaces: () => void; serviceAction: (action: string) => void;
  lastRead: number; refreshing: boolean;
  selectedFilter: string; filterRules: (value:string)=>void; viewRules:(kind:string)=>void; addService:()=>void;
  manageSources:()=>void;editPolicy:()=>void;editDefault:()=>void;configScope:string;changeScope:(value:string)=>void;origin:string;changeOrigin:(value:string)=>void;
}) {
  const view = firewallViewState(data, driver, zone, error), info = view.current && !error ? record(data?.zone_info) : {};
  const serviceView=firewallViewState(serviceData ? {...serviceData,zone} : data,driver,zone,serviceError || (serviceData ? "" : error));
  const allZones = driver === "firewalld" && zone === ALL_FIREWALL_ZONES;
  const titleStatus = view.native ? view : serviceView;
  const permanentTarget=String(record(record(data?.zone_policy).permanent).target||"");
  const rememberedTypes=useRef<Dict[]>([]);
  const loadedTypes=list(serviceData?.types).length?list(serviceData?.types):list(data?.types);
  if(loadedTypes.length)rememberedTypes.current=loadedTypes;
  const types = loadedTypes.length?loadedTypes:rememberedTypes.current, zones = Array.isArray(data?.zones) ? data.zones.map(String) : [];
  const interfaces = firewallBindings(info.interfaces), sources = firewallBindings(info.sources), target = String(info.target || "");
  const entries = view.current && !error ? list(data?.entries) : [], managed = entries.filter(r => r.managed).length;
  const blocked = entries.filter(r=>["drop","reject"].includes(String(r.action).toLowerCase())).length;
  const forwards = entries.filter(r=>r.kind === "forward").length;
  const icmpBlocks = firewallBindings(info["icmp-blocks"]);
  const stats = [{id:"all",label:"全部规则",value:entries.length,hint:"当前查看范围"},{id:"managed",label:"本应用管理",value:managed,hint:"明确纳入管理"},{id:"external",label:"系统／外部",value:entries.length-managed,hint:"需先纳管再编辑"},{id:"blocked",label:"显式阻断",value:blocked,hint:"drop／reject条目"}];
  const count = (number: number) => view.complete ? String(number) : "—";
  return <section className="firewall-workspace" aria-label="防火墙状态与管理范围">
    <div className="firewall-service-bar">
      <div className="firewall-service-identity"><div>
        <div className="firewall-service-title"><h2><FirewallIcon size={20}/><span>{view.native?"防火墙工具":"服务管理"}</span></h2><SettingInfo label="防火墙" text={<ul><li><strong>操作顺序。</strong>先选择管理工具，再选择区域和规则分类。</li><li><strong>查看范围。</strong>切换视图只改变显示内容，不会迁移规则或启停服务。</li><li><strong>修改配置。</strong>接口归属、服务状态和规则修改均先生成预览，再确认执行。</li></ul>} />{(view.native || !serviceView.running) && <span className={"firewall-status " + titleStatus.tone}><i />{titleStatus.label}</span>}</div>
        <div className="firewall-service-meta"><span>管理工具：{driver}</span><span>{view.native ? "不提供独立服务状态" : serviceView.autostart == null ? "开机启动：未确认" : "开机启动：" + (serviceView.autostart ? "已开启" : "已关闭")}</span>{!view.native && <span className={view.complete ? "muted" : "fair"}>{rulesLoading&&!data ? "规则读取中" : view.inventoryLabel}</span>}{lastRead>0 && view.current && <span>{error ? "上次读取：" : "最近读取："}{new Date(lastRead).toLocaleTimeString('zh-CN',{hour12:false})}</span>}</div>
      </div></div>
      <div className="firewall-service-buttons"><Button disabled={refreshing} onClick={refresh}><RefreshCw size={15} />{refreshing ? "刷新中" : "刷新"}</Button>{!view.native && <><Button disabled={!serviceView.canServiceControl} onClick={()=>serviceAction(serviceView.running ? "stop" : "start")}>{serviceView.running ? "停止服务" : "启动服务"}</Button><Button disabled={!serviceView.canServiceControl||serviceView.autostart==null} onClick={()=>serviceAction(serviceView.autostart ? "disable" : "enable")}>{serviceView.autostart ? "关闭自启" : "开启自启"}</Button></>}</div>
    </div>

    <div className="firewall-scope-bar">
      <div className="firewall-scope-fields">
        <Field label="管理工具" icon={<FirewallManagerIcon size={20} className="firewall-heading-icon"/>} info={view.native ? `当前显示 ${driver} 的系统规则。此视图不使用 firewalld 的区域与接口绑定，${driver === "ufw" ? "仅供查看" : "可编辑 FoXray 管理的规则"}。` : "选择查看和编辑规则的工具。切换工具不会安装、启停或迁移防火墙。"}><select className="firewall-driver-select" aria-label="防火墙管理工具" value={driver} onChange={e=>changeDriver(e.target.value)}>
          {(types.length ? types : [{id:driver,installed:true}]).map(t=><option key={String(t.id)} value={String(t.id)} disabled={t.installed===false}>{String(t.id)}{t.installed===false ? " · 未安装" : ""}{t.id==="ufw" ? " · 只读" : ""}</option>)}
        </select></Field>
        {!crossZone && !view.native && <Field label="查看区域" info={<ul><li><strong>区域筛选。</strong>选择具体区域或所有区域，决定下方展示哪些规则。</li><li><strong>保持配置。</strong>筛选不会改变默认区域、接口或来源地址归属。</li><li><strong>匹配依据。</strong>某条流量是否进入该区域，要结合接口、来源绑定和实际规则判断。</li></ul>}><select aria-label="查看防火墙区域" value={zone} onChange={e=>changeZone(e.target.value)}><option value={ALL_FIREWALL_ZONES}>所有区域</option>{[...new Set([...zones,zone])].filter(z => z !== ALL_FIREWALL_ZONES).map(z=><option key={z}>{z}</option>)}</select></Field>}
        {!crossZone && <Field label="配置范围" info={<ul><li><strong>显示范围。</strong>按运行时、永久配置或差异条目筛选列表。</li><li><strong>写入范围。</strong>此筛选不决定修改哪份配置。实际变更范围在预览中分别列出。</li></ul>}><select aria-label="防火墙配置范围" disabled={view.native} title={view.native?"当前工具仅提供系统规则快照":undefined} value={configScope} onChange={e=>changeScope(e.target.value)}><option value="all">全部配置</option><option value="runtime">运行时</option><option value="permanent">永久配置</option><option value="difference">存在差异</option></select></Field>}
        {!view.native && <Field label="规则来源"><select aria-label="防火墙规则来源" value={origin} onChange={e => changeOrigin(e.target.value)}><option value="all">全部来源</option>{firewallOrigins.map(([key, label]) => <option value={key} key={key}>{label}</option>)}</select></Field>}
      </div>
      {!crossZone && !view.native && <div className="firewall-scope-actions">
        <Button size="compact" disabled={!view.canManage || !record(data?.zone_policy).default_zone} onClick={editDefault}>设置默认区域</Button>
      </div>}
    </div>

    <div className={"firewall-overview-grid"+(view.native || crossZone || allZones ? " is-native" : "")}>
      {!view.native && !crossZone && !allZones && <section className="firewall-binding-block firewall-layout-section">
        <div className="firewall-detail-heading"><h3><ZoneOwnershipIcon size={20} className="firewall-heading-icon"/>区域与流量归属<SettingInfo label="区域与流量归属" text="接口和来源地址是区域匹配条件；这不是访问白名单。当前显示的是所选区域返回的真实绑定信息。" /></h3><div className="firewall-binding-actions"><Button size="compact" disabled={!view.canManage} onClick={manageInterfaces}>管理归属</Button><Button size="compact" disabled={!view.canManage} onClick={manageSources}>管理来源</Button></div></div>
        <div className="firewall-zone-name"><strong>{zone}</strong><span>{String(record(data?.zone_policy).default_zone || "") === zone ? "默认区域" : "查看区域"}</span></div>
        <dl className="firewall-binding-list">
          <div><dt>绑定接口<SettingInfo label="接口归属" text="经过这些网络接口的流量按此区域处理；来源地址绑定等更具体的归属条件可能影响最终区域。调整归属需先生成变更预览并确认。" /></dt><dd>{!("interfaces" in info) ? "未读取" : interfaces.length ? interfaces.map(v=><span className="firewall-address-chip" key={v}>{v}</span>) : "未绑定接口"}</dd></div>
          <div><dt>来源地址<SettingInfo label="来源地址归属" text={<ul><li><strong>区域归属。</strong>根据来源 IP、网段、MAC 或 IP 集合确定区域。</li><li><strong>访问控制。</strong>来源归属本身不表示放行；是否允许仍由相应规则决定。</li><li><strong>未设置。</strong>表示没有来源绑定，不等于允许全部来源。</li></ul>} /></dt><dd>{!("sources" in info) ? "未读取" : sources.length ? sources.map(v=><span className="firewall-address-chip" key={v}>{v}</span>) : "未设置"}</dd></div>
        </dl>
      </section>}
      <section className="firewall-rule-overview firewall-layout-section" aria-label="规则概览">
        <div className="firewall-detail-heading"><h3><FirewallOverviewIcon size={20} className="firewall-heading-icon"/>规则概览<SettingInfo label="规则概览" text={<ul><li><strong>统计范围。</strong>按当前视图统计条目，合并同一规则的运行时与永久配置。</li><li><strong>显式阻断。</strong>只统计动作明确为 drop 或 reject 的条目。</li><li><strong>快速查看。</strong>点击统计项筛选下方规则；计数不等于实际流量命中数。</li></ul>} /></h3><Button size="compact" onClick={()=>filterRules("all")}>查看全部规则</Button></div>
        <div className="firewall-stats-grid">{stats.map(stat=><button type="button" key={stat.id} data-stat={stat.id} className={"button secondary firewall-stat-button"+(stat.id!=="all"&&selectedFilter===stat.id ? " is-selected" : "")} aria-label={"按"+stat.label+"筛选规则"} aria-pressed={stat.id!=="all"&&selectedFilter===stat.id} disabled={!view.complete} onClick={()=>filterRules(stat.id)}><strong>{count(stat.value)}</strong><span>{stat.label}</span><small>{stat.hint}</small></button>)}</div>
        {view.complete && entries.length>0 ? <div className="firewall-ownership"><div className="firewall-ownership-meter" role="img" aria-label={"本应用管理"+managed+"条，系统或外部"+(entries.length-managed)+"条"}><span style={{width:(100*managed/entries.length)+"%"}} /></div><div className="firewall-ownership-legend"><span>本应用管理 {managed}</span><span>系统／外部 {entries.length-managed}</span></div></div> : <div className="firewall-ownership-legend">{view.complete ? "当前范围暂无规则条目" : "规则读取未完成，统计暂不可用"}</div>}
      </section>
    </div>
    {crossZone || allZones ? null : !view.native ? <section className="firewall-policy-strip firewall-layout-section" aria-label="区域策略">
      <div className="firewall-detail-heading"><h3><ZoneSettingsIcon size={20} className="firewall-heading-icon"/>区域设置<SettingInfo label="默认处理策略" text={<ul><li><strong>判断有效行为。</strong>结合区域 target、具体规则和流量方向判断。系统默认（default）不等于全部允许。</li><li><strong>编辑范围。</strong>可修改区域默认行为、IP 地址伪装和 ICMP 设置。</li><li><strong>生效方式。</strong>仅保存永久配置，或在核对完整配置后重新加载。所有修改均须预览和确认，并保留回退选项。</li></ul>} /></h3><div className="firewall-binding-actions"><Button size="compact" disabled={!view.canManage||record(data?.zone_policy).complete!==true} onClick={editPolicy}>编辑区域策略</Button></div></div>
      <div className="firewall-policy-grid">
        <div><span>默认行为</span><div className="firewall-target-inline"><span>运行时 · </span><strong>{firewallTargetLabel(target)}</strong>{target === "default" && <SettingInfo label="系统默认（default）" text="未匹配放行等规则的入站流量，ICMP 默认允许，其他默认拒绝并回应；ICMP 阻断配置可改变 ICMP 处理。转发流量还需结合区域转发与策略判断。"/>}</div>{permanentTarget&&permanentTarget!==target.replace("%%REJECT%%","REJECT")&&<small className="fair">永久：{firewallTargetLabel(permanentTarget)} · 待重新加载</small>}</div>
        <div><span>IP地址伪装</span><strong>{!view.current || error || typeof data?.masquerade!=="boolean" ? "未确认" : data.masquerade ? "已开启" : "已关闭"}</strong></div>
        <div><span>ICMP阻断配置<SettingInfo label="ICMP阻断配置" text="仅展示所选区域返回的ICMP阻断项和反向匹配开关，不据此推断某个ICMP报文是否可达；最终还需结合默认策略和其他规则。" /></span><strong title={icmpBlocks.join("、")}>{!("icmp-blocks" in info) ? "未读取" : icmpBlocks.length ? icmpBlocks.length+"项阻断配置" : "未配置阻断项"}</strong>{info["icmp-block-inversion"]==="yes" && <small>反向匹配开启</small>}</div>
        <div><span>转发规则</span><button type="button" className="firewall-rule-jump" disabled={!view.complete} onClick={()=>viewRules("forward")}>{count(forwards)} 条 · 查看</button></div>
      </div>
      <div className="firewall-allowed-services"><span>允许服务</span><div>{!view.current || error ? "未读取" : Array.isArray(data?.allowed_services) && data.allowed_services.length ? data.allowed_services.map(String).map(name=><span className="firewall-address-chip" key={name}>{name}</span>) : "未配置"}</div><Button size="compact" disabled={!view.canManage} onClick={addService}>添加服务</Button></div>
    </section> : null}
  </section>;
}
