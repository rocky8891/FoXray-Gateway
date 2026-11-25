import { CrossZoneIcon } from "./cross_zone_icon";
import { useState } from "react";
import { Plus, Search } from "lucide-react";
import { api, errorMessage, list, text, type Dict, type Plan } from "./api";
import { Button, ErrorLine, Field, Modal, NumberInput, Panel, Table, useResource, SettingInfo, type Column } from "./ui";
import { AdaptiveSearch } from "./adaptive_search";
import { CompactFilters, ListFilter, ResetFilters, SortHeading } from "./list_controls";
import { ActionMenu } from "./action_menu";
import { ExportButton, useRowSelection } from "./resource_controls";
import type { Act } from "./nodes";

const completeRows = (data: Dict | null, error: string) => data?.complete === true && !error ? list(data.items) : [];
const policyTargets: Record<string, string> = { ACCEPT: "允许", DROP: "丢弃", REJECT: "拒绝", CONTINUE: "继续匹配" };
const actions: Record<string,string> = {accept:"允许",drop:"丢弃",reject:"拒绝并回应"};
export function CrossZoneRules({act, resource, filter, resetFilter, linkedRule = ""}: {
  linkedRule?: string;
  act: Act;
  resource: ReturnType<typeof useResource<Dict>>;
  filter: string;
  resetFilter: () => void;
}) {
  const [query, setQuery] = useState(linkedRule);
  const [priorityOrder,setPriorityOrder] = useState(1);
  const [filters, setFilters] = useState<Record<string, string>>({});
  const actionValue = (row: Dict) => String(row.action || row.policy_target || "").toLowerCase();
  const facets: Record<string, (row: Dict) => string> = {
    source: row => String(row.ingress_zone || ""),
    destination: row => String(row.egress_zone || ""),
    protocol: row => String(row.protocol || ""),
    action: actionValue,
    state: row => row.enabled ? "enabled" : "disabled",
  };
  const items = completeRows(resource.data, resource.error);
  const facetLabel = (key: string, value: string) => key === "state" ? (value === "enabled" ? "启用" : "停用")
    : key === "action" ? actions[value] || policyTargets[value.toUpperCase()] || value
    : key === "protocol" ? (value === "all" ? "全部协议" : value.toUpperCase()) : value;
  const filterSummary = [
    query.trim() && `搜索：${query.trim()}`,
    filter !== "all" && `范围：${filter === "managed" ? "本应用管理" : filter === "external" ? "系统 / 外部" : "显式阻断"}`,
    ...Object.entries(filters).filter(([, value]) => value).map(([key, value]) => {
      const labels: Record<string, string> = {source: "入口区域", destination: "出口区域", protocol: "协议", action: "动作", state: "状态"};
      return `${labels[key]}：${facetLabel(key, value)}`;
    }),
    priorityOrder !== 1 && "优先级：降序",
  ].filter(Boolean).join(" · ");
  const [form,setForm]=useState<Dict|null>(null),[operation,setOperation]=useState("add"),[ack,setAck]=useState(false);
  const [plan,setPlan]=useState<Plan|null>(null),[failure,setFailure]=useState(""),[busy,setBusy]=useState(false),[detail,setDetail]=useState<Dict|null>(null);
  const zones=Array.isArray(resource.data?.zones)?resource.data.zones.map(String):[];
  const complete=resource.data?.complete===true&&!resource.error;
  const update=(key:string,value:unknown)=>setForm(current=>current?{...current,[key]:value}:null);
  const start=(op:string,item?:Dict)=>{setFailure("");setAck(false);setOperation(op);setForm(item?{...item,apply_mode:"permanent",reload_ack:false}:{kind:"cross-zone",driver:"firewalld",name:"",ingress_zone:zones.includes("public")?"public":zones[0]||"",egress_zone:zones.includes("home-vpn")?"home-vpn":zones[1]||"",source:"",destination:"",protocol:"all",port:"",action:"accept",enabled:true,priority:-1,apply_mode:"permanent",reload_ack:false});};
  const [batch, setBatch] = useState<{operation:string; ids:string[]; mode:string} | null>(null);
  async function preview(){
    setFailure("");setBusy(true);
    try {const result=await api<Plan>("/api/network/plan",batch ? {kind:"firewall",scope:"cross-zone",operation:batch.operation,apply_mode:batch.mode,items:batch.ids.map(id=>({id})),allow_protected:ack} : {kind:"firewall",operation,item:form,allow_protected:ack});setPlan(result);setForm(null);setBatch(null);if(batch) selection.setSelected(new Set())}
    catch(error){setFailure(errorMessage(error))}finally{setBusy(false)}
  }
  const columns:Column<Dict>[]=[
    {key:"priority",label:"优先级",heading:<SortHeading label="优先级" direction={priorityOrder} onClick={()=>setPriorityOrder(v=>-v)} />,render:r=>text(r.priority)},
    {key:"name",label:"规则名称",render:r=><><strong>{text(r.name||r.policy_name||r.chain)}</strong><small>{r.managed?"本应用管理":"系统 / 外部 · 只读"}</small></>},
    {key:"source",label:"入口区域 / IP",render:r=><><strong>{text(r.ingress_zone)}</strong><small>{[r.in_interface, r.source].filter(Boolean).join(" · ") || "未指定源 IP"}</small></>},
    {key:"destination",label:"出口区域 / IP",render:r=><><strong>{text(r.egress_zone)}</strong><small>{[r.out_interface, r.destination].filter(Boolean).join(" · ") || "未指定目标 IP"}</small></>},
    {key:"protocol",label:"协议 / 端口",render:r=>[r.protocol === "all" ? "全部协议" : r.protocol && String(r.protocol).toUpperCase(), r.port].filter(Boolean).join(" / ") || "—"},
    {key:"action",label:"动作",render:r=><>
      <strong>{r.action ? actions[text(r.action)] || text(r.action) : r.policy_target ? policyTargets[text(r.policy_target)] || text(r.policy_target) : "动作未解析"}</strong>
      {r.masquerade === true && <small>已启用地址伪装</small>}
      {Array.isArray(r.policy_rules) && r.policy_rules.length > 0 && <small>{r.policy_rules.length} 条富规则</small>}
    </>},
    {key:"state",label:"状态 / 配置",render:r=><><span className={!r.enabled?"muted":r.managed&&!r.runtime?"fair":"accent"}>{r.managed?(r.enabled?(r.runtime?"已生效":"待生效"):"配置停用"):(r.enabled?"启用":"停用")}</span><small>{r.runtime?"运行时已载入":"运行时未同步"} · {r.permanent?"永久已保存":"永久未保存"}{r.drift?" · 存在差异":""}</small></>},
    {key:"actions",label:"操作",render:r=><div className="row-actions"><Button size="compact" onClick={()=>setDetail(r)}>详情</Button>{!!r.managed&&<><Button size="compact" disabled={!complete} onClick={()=>start("replace",r)}>编辑</Button><Button size="compact" disabled={!complete} onClick={()=>start("replace",{...r,enabled:!r.enabled})}>{r.enabled?"停用":"启用"}</Button><Button size="compact" variant="danger" disabled={!complete} onClick={()=>start("delete",r)}>删除</Button></>}</div>},
  ];
  const rows = items.filter(row => {
    const matchesFilter = filter === "all" ||
      (filter === "managed" && row.managed) ||
      (filter === "external" && !row.managed) ||
      (filter === "blocked" && ["drop", "reject"].includes(actionValue(row)));
    return matchesFilter && Object.entries(filters).every(([key, value]) => !value || facets[key](row) === value) && (linkedRule && query === linkedRule ? text(row.id) === linkedRule : JSON.stringify(row).toLowerCase().includes(query.trim().toLowerCase()));
  }).sort((a,b)=>(Number(a.priority ?? -1)-Number(b.priority ?? -1))*priorityOrder);
  const selection = useRowSelection(rows, row => text(row.id), row => !!row.managed);
  const batchLabels: Record<string,string> = {enable:"启用",disable:"停用",delete:"删除"};
  const canBatch = complete && selection.chosen.length > 0 && selection.chosen.length <= 50;
  const beginBatch = (operation:string) => {
    setAck(false);
    setFailure("");
    setBatch({operation, ids:selection.chosen.map(row=>text(row.id)), mode:"permanent"});
  };
  return <Panel className="firewall-main-panel management-panel">
    <div className="firewall-rules-heading" id="cross-zone-rules">
      <h3><CrossZoneIcon size={20} className="firewall-heading-icon"/>域间规则<SettingInfo label="域间规则" text="按入口区域、出口区域、来源、目标和协议匹配转发流量。" /></h3>
      <span className="muted">运行时与永久配置 · 合并展示</span>
    </div>
    <CompactFilters label="域间规则筛选"
      before={<>
        <Button disabled={!complete} onClick={() => start("add")}><Plus size={15} />添加</Button>
        <Button disabled={!canBatch} onClick={() => beginBatch("enable")}>启用</Button>
        <Button disabled={!canBatch} onClick={() => beginBatch("disable")}>停用</Button>
        <ActionMenu label="批量" triggerLabel="批量操作" showLabel items={Object.entries(batchLabels).map(([operation,label])=>({label,disabled:!canBatch,onClick:()=>beginBatch(operation)}))} />
      </>}
      actions={<span className="cross-zone-toolbar-actions"><ResetFilters active={!!filterSummary} summary={filterSummary} selectedCount={selection.chosen.length} onClick={() => {selection.setSelected(new Set()); setQuery(""); setFilters({}); setPriorityOrder(1); resetFilter()}} />
        <ExportButton rows={selection.chosen.length ? selection.chosen : rows} name="域间规则" columns={[["优先级", r => r.priority], ["名称", r => r.name || r.policy_name], ["入口区域", r => r.ingress_zone], ["出口区域", r => r.egress_zone], ["来源", r => r.source], ["目标", r => r.destination], ["协议", r => r.protocol], ["端口", r => r.port], ["动作", r => facetLabel("action", actionValue(r))]]} /></span>}
    >
      <AdaptiveSearch tableWidthKey="cross-zone-rules"><Search size={15} /><input aria-label="搜索域间规则" placeholder="查询区域、IP、网段或协议" value={query} onChange={event => setQuery(event.target.value)} /></AdaptiveSearch>
    </CompactFilters>
    <ErrorLine error={resource.error || String(resource.data?.error || "") || (!form && !plan && !batch ? failure : "")} />
    {resource.loading && !resource.data && <p className="hint">正在读取域间规则…</p>}
    <Table columnSizingKey="cross-zone-rules" keepHeaders rows={rows} total={items.length} selectedCount={selection.chosen.length} columns={[selection.column, ...columns.map(column => {
      const facet = facets[column.key];
      if (!facet) return column;
      const options = [...new Set(items.map(facet).filter(Boolean))].sort();
      return {...column, heading: <span className="table-heading-tools">{column.label}
        <ListFilter label={column.label + "筛选"} column active={!!filters[column.key]}>
          <Field label={column.label}><select value={filters[column.key] || ""} onChange={event => {
            setFilters(previous => ({...previous, [column.key]: event.target.value}));
            selection.setSelected(new Set());
          }}>
            <option value="">全部</option>
            {options.map(value => <option key={value} value={value}>{facetLabel(column.key, value)}</option>)}
          </select></Field>
          <Button disabled={!filters[column.key]} onClick={() => {
            setFilters(previous => ({...previous, [column.key]: ""}));
            selection.setSelected(new Set());
          }}>重置</Button>
        </ListFilter>
      </span>};
    })]} rowKey={r => text(r.id)} />
    {batch && <Modal className="firewall-dialog" title={"批量" + batchLabels[batch.operation] + "域间规则"} onClose={() => !busy && setBatch(null)}>
      <p>已选 {batch.ids.length} 条 FoXray 纳管规则。</p>
      <Field label="生效方式"><select value={batch.mode} onChange={event=>setBatch({...batch,mode:event.target.value})}>
        <option value="permanent">仅保存永久配置</option>
        <option value="immediate">立即生效，同时保存永久配置</option>
      </select></Field>
      <p className="hint">批量立即生效只处理已加载且无需重载的策略。</p>
      <label className="check risk"><input type="checkbox" checked={ack} onChange={event=>setAck(event.target.checked)} />我已确认对转发和管理连接的影响</label>
      <ErrorLine error={failure}/>
      <div className="modal-actions"><Button disabled={busy} onClick={()=>setBatch(null)}>取消</Button><Button variant="primary" disabled={busy||!ack} onClick={()=>void preview()}>生成变更预览</Button></div>
    </Modal>}
    {form&&<Modal className="firewall-dialog" title={operation==="delete"?"删除域间规则":operation==="add"?"添加域间规则":"编辑域间规则"} onClose={()=>!busy&&setForm(null)} wide>
      {operation==="delete"?<p>删除“{text(form.name||form.policy_name)}”：{text(form.ingress_zone)} → {text(form.egress_zone)}，{text(form.source)} → {text(form.destination)}。</p>:<div className="form-grid">
        <Field label="备注名称"><input value={text(form.name)} onChange={e=>update("name",e.target.value)}/></Field>
        <Field label="规则状态"><select value={form.enabled?"yes":"no"} onChange={e=>update("enabled",e.target.value==="yes")}><option value="yes">启用</option><option value="no">停用</option></select></Field>
        {(["ingress_zone","egress_zone"] as const).map((key,i)=><Field key={key} label={i?"出口区域":"入口区域"}><select aria-label={i?"出口区域":"入口区域"} value={text(form[key])} onChange={e=>update(key,e.target.value)}>{zones.map(z=><option key={z}>{z}</option>)}</select></Field>)}
        <Field label="源 IP"><input aria-label="源 IP" placeholder="例如 192.168.1.10 或 192.168.1.0/24" value={text(form.source)} onChange={e=>{update("source",e.target.value);update("family","")}}/></Field>
        <Field label="目标 IP"><input aria-label="目标 IP" placeholder="例如 10.24.255.3" value={text(form.destination)} onChange={e=>{update("destination",e.target.value);update("family","")}}/></Field>
        <Field label="协议"><select value={text(form.protocol)} onChange={e=>update("protocol",e.target.value)}>{["all","tcp","udp","tcpudp","icmp"].map(p=><option key={p} value={p}>{p==="all"?"全部协议":p==="tcpudp"?"TCP + UDP":p.toUpperCase()}</option>)}</select></Field>
        <Field label="目标端口（可选）"><input disabled={!["tcp","udp","tcpudp"].includes(text(form.protocol))} placeholder="留空匹配此协议所有端口，例如 443 或 8000-8010" value={text(form.port)} onChange={e=>update("port",e.target.value)}/></Field>
        <Field label="优先级" hint="数值越小越先执行。0 为保留值。"><NumberInput type="number" min={-32768} max={32767} value={Number(form.priority ?? -1)} onChange={e=>update("priority",Number(e.target.value))} /></Field>
        <Field label="动作"><select value={text(form.action)} onChange={e=>update("action",e.target.value)}>{Object.entries(actions).map(([value,label])=><option key={value} value={value}>{label}</option>)}</select></Field>
      </div>}
      <Field label="生效方式"><select aria-label="生效方式" value={text(form.apply_mode)} onChange={e=>update("apply_mode",e.target.value)}><option value="permanent">仅保存永久配置</option><option value="immediate">检查后立即生效，同时保存永久配置</option></select></Field>
      <p className="hint">永久配置在下次 firewalld 重载或启动后生效。新增策略或修改优先级后立即生效需要全局重载；会保留已核对的运行时富规则；其他配置差异仍阻止重载。</p>
      <div className="cross-zone-acknowledgements">{form.apply_mode==="immediate"&&operation!=="delete"&&<label className="check risk"><input type="checkbox" checked={form.reload_ack===true} onChange={e=>update("reload_ack",e.target.checked)}/>允许在新增策略或修改优先级时核对配置并重载</label>}
      <label className="check risk"><input type="checkbox" checked={ack} onChange={e=>setAck(e.target.checked)}/>我已确认转发与管理连接影响；应用后 120 秒内确认，否则自动回退</label>
      </div><ErrorLine error={failure}/><div className="modal-actions"><Button disabled={busy} onClick={()=>setForm(null)}>取消</Button><Button variant="primary" disabled={busy||!ack||(operation!=="delete"&&(!text(form.source).trim()||!text(form.destination).trim()||form.ingress_zone===form.egress_zone))} onClick={()=>void preview()}>{busy?"正在核对…":"生成变更预览"}</Button></div>
    </Modal>}
    {plan&&<Modal className="firewall-dialog" title="域间规则变更预览" onClose={()=>!busy&&setPlan(null)} wide>
      <h3>{plan.summary}</h3>{plan.warnings.map((warning,i)=><p className="hint" key={i}>{warning}</p>)}
      <pre className="command-preview">{plan.preview.join("\n")}</pre><details><summary>回退命令</summary><pre className="command-preview">{plan.rollback?.join("\n")}</pre></details>
      <ErrorLine error={failure}/><div className="modal-actions"><Button disabled={busy} onClick={()=>setPlan(null)}>取消</Button><Button variant="primary" disabled={busy} onClick={()=>{setBusy(true);void act("/api/network/apply",{id:plan.id}).then(ok=>{if(ok){setPlan(null);void resource.refresh()}}).finally(()=>setBusy(false))}}>应用变更</Button></div>
    </Modal>}
    {detail&&<Modal title="域间规则详情" onClose={()=>setDetail(null)} wide><p className="hint">{detail.managed?"本应用管理的独立域间规则。":"系统或外部策略，只读展示，保留原有配置。"}</p><pre className="command-preview">{JSON.stringify(detail,null,2)}</pre></Modal>}
  </Panel>;
}
