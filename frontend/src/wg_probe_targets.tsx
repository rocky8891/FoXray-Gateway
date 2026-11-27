import { useState } from "react";
import { Search } from "lucide-react";
import { AdaptiveSearch } from "./adaptive_search";
import { ActionMenu } from "./action_menu";
import { CompactFilters, ListFilter, ResetFilters, SortHeading } from "./list_controls";
import { ExportButton, useRowSelection } from "./resource_controls";
import { Button, Field, Modal, SettingInfo, Table, type Column } from "./ui";
import { api, errorMessage, record, type Dict } from "./api";

type TargetRow = { name: string; remark: string; active: boolean; candidates: string[]; auto: boolean; target: string };
function targetRow(item: Dict, policy: Dict): TargetRow {
  const name = String(item.name);
  const candidates = Array.isArray(item.probe_candidates) ? item.probe_candidates.map(String) : [];
  const saved = String(record(policy.targets)[name] || "");
  const auto = typeof record(policy.auto_targets)[name] === "boolean" ? record(policy.auto_targets)[name] === true : !saved;
  return { name, remark: String(item.remark || ""), active: item.active === true, candidates, auto, target: auto ? candidates[0] || "" : saved };
}
export function WireGuardProbeTargets({ items, policy, onChange, onRefresh }: {
  items: Dict[]; policy: Dict; onChange: (value: Dict) => void; onRefresh: () => void;
}) {
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<Record<string,string>>({});
  const [sort, setSort] = useState({key: "name", direction: 1});
  const [editing, setEditing] = useState<{name: string; auto: boolean; target: string} | null>(null);
  const all = items.map(item => targetRow(item, policy));
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null);
  const editedRow = all.find(row => row.name === editing?.name);
  const testTarget = (editing?.auto ? editedRow?.candidates[0] : editing?.target)?.trim() || "";

  function changeEditing(next: typeof editing) {
    setTestResult(null);
    setEditing(next);
  }

  async function testProbe() {
    if (!editing || !editedRow?.active || !testTarget || testing) return;
    setTesting(true);
    setTestResult(null);
    try {
      const result = await api<Dict>("/api/network/wireguard/test-interface-probe", { name: editing.name, target: testTarget });
      const ok = result.ok === true;
      const latency = Number(result.latency);
      const message = ok
        ? Number.isFinite(latency) && latency >= 0 ? `目标可达 · ${latency.toFixed(1)} ms` : "目标可达"
        : "未收到 ICMP 回复";
      setTestResult({ ok, message });
    } catch (err) {
      setTestResult({ ok: false, message: errorMessage(err) });
    } finally {
      setTesting(false);
    }
  }

  const fields = [
    {key:"name",label:"接口",value:(r:TargetRow)=>[r.name,r.remark].filter(Boolean).join(" ")},
    {key:"status",label:"状态",value:(r:TargetRow)=>r.active?"运行中":"已停止"},
    {key:"mode",label:"识别方式",value:(r:TargetRow)=>r.auto?"自动识别":"手动指定"},
    {key:"target",label:"对端隧道 IP",value:(r:TargetRow)=>r.target||"未配置"},
    {key:"candidates",label:"候选地址",value:(r:TargetRow)=>r.candidates.join("、")||"无候选地址"},
  ];
  const rows = all.filter(row => fields.every(field=>!filters[field.key] || field.value(row).toLowerCase().includes(filters[field.key].trim().toLowerCase())) &&
    [row.name, row.remark, row.target, ...row.candidates].join(" ").toLowerCase().includes(query.trim().toLowerCase()))
    .sort((a,b) => {
      const field=fields.find(field=>field.key===sort.key)!;
      return (field.value(a).localeCompare(field.value(b), undefined, {numeric:true}) || a.name.localeCompare(b.name)) * sort.direction;
    });
  const setFilter=(key:string,value:string)=>setFilters(old=>({...old,[key]:value}));
  const filterField=(key:string)=>{
    const field=fields.find(field=>field.key===key)!;
    return <Field label={field.label}>{["mode","status"].includes(key)
      ? <select aria-label={field.label+"筛选条件"} value={filters[key]||""} onChange={e=>setFilter(key,e.target.value)}><option value="">全部</option>{[...new Set(all.map(field.value))].map(value=><option key={value}>{value}</option>)}</select>
      : <input aria-label={field.label+"筛选条件"} placeholder="输入关键字" value={filters[key]||""} onChange={e=>setFilter(key,e.target.value)}/>}</Field>;
  };
  const column=(value:Column<TargetRow>):Column<TargetRow>=>({...value,
    sortDirection:sort.key===value.key?(sort.direction===1?"ascending":"descending"):undefined,
    heading:<span className="table-heading-tools"><SortHeading label={value.label} direction={sort.key===value.key?sort.direction:undefined}
      onClick={()=>setSort({key:value.key,direction:sort.key===value.key?-sort.direction:1})}/>
      <ListFilter label={value.label+"筛选"} column active={!!filters[value.key]}>{filterField(value.key)}<Button onClick={()=>setFilter(value.key,"")}>重置</Button></ListFilter></span>,
  });
  const selection = useRowSelection(rows, row => row.name);
  function apply(changes: { name: string; auto: boolean; target?: string }[]) {
    const targets = {...record(policy.targets)}, autoTargets = {...record(policy.auto_targets)};
    for (const change of changes) {
      autoTargets[change.name] = change.auto;
      if (change.target !== undefined) targets[change.name] = change.target.trim();
    }
    onChange({...policy, targets, auto_targets: autoTargets});
  }
  function batch(operation: string) {
    apply(selection.chosen.map(row => ({ name: row.name, auto: operation === "auto", target: operation === "clear" ? "" : operation === "manual" ? row.target : undefined })));
    selection.setSelected(new Set());
  }
  return <section className="wg-probe-target-table">
    <div className="wg-probe-target-heading"><h3 className="settings-section-title">对端隧道 IP<SettingInfo label="对端隧道 IP" text={<ul>
      <li><strong>自动识别：</strong>使用该接口首个候选主机地址；没有候选时，请手动填写。</li>
      <li><strong>手动指定：</strong>填写对端隧道的单个 IPv4 或 IPv6 地址。清空地址会同时关闭自动识别。</li>
      <li><strong>批量操作：</strong>只作用于已勾选的筛选结果；改为手动时保留每个接口各自的地址。</li>
      <li><strong>保存生效：</strong>表格修改先进入待保存配置，点击本卡片「保存并应用」后生效；不会启停 VPN 接口。</li>
    </ul>}/></h3></div>
    <CompactFilters label="探测目标筛选"
      before={<><Button onClick={onRefresh}>刷新</Button>
        <ActionMenu label="探测目标批量操作" triggerLabel="批量操作" showLabel items={[
          {label:"自动识别", disabled: !selection.chosen.length, onClick:()=>batch("auto")},
          {label:"改为手动（保留当前地址）", disabled: !selection.chosen.length, onClick:()=>batch("manual")},
          {label:"清空地址", disabled: !selection.chosen.length, onClick:()=>batch("clear")},
        ]}/></>}
      reset={<ResetFilters active={!!query || Object.values(filters).some(Boolean) || sort.key !== "name" || sort.direction !== 1} selectedCount={selection.chosen.length}
        onClick={()=>{setQuery("");setFilters({});setSort({key:"name",direction:1});selection.setSelected(new Set());}}/>}
      actions={<ExportButton name="WireGuard探测目标" rows={selection.chosen.length?selection.chosen:rows} columns={[["接口",r=>r.name],["状态",r=>r.active?"运行中":"已停止"],["识别方式",r=>r.auto?"自动识别":"手动指定"],["对端隧道 IP",r=>r.target]]}/>}
    >
      <AdaptiveSearch tableWidthKey="wg-probe-targets"><Search size={15}/><input aria-label="搜索探测目标" placeholder="查询接口、备注或 IP" value={query} onChange={e=>setQuery(e.target.value)}/></AdaptiveSearch>

    </CompactFilters>
    <Table columnSizingKey="wg-probe-targets" keepHeaders rows={rows} rowKey={row=>row.name} total={all.length} selectedCount={selection.chosen.length}
      columns={[selection.column, ...([
        {key:"name",label:"接口",render:r=><><strong>{r.name}</strong>{r.remark&&<small>{r.remark}</small>}</>},
        {key:"status",label:"状态",render:r=><span className={r.active?"accent":"muted"}>{r.active?"运行中":"已停止"}</span>},
        {key:"mode",label:"识别方式",render:r=>r.auto?"自动识别":"手动指定"},
        {key:"target",label:"对端隧道 IP",render:r=>r.target||<span className="muted">{r.auto?"无候选地址，请手动指定":"未配置"}</span>},
        {key:"candidates",label:"候选地址",render:r=>r.candidates.join("、")||"—"},
      ] as Column<TargetRow>[]).map(column),
        {key:"actions",label:"操作",render:r=><><Button size="compact" onClick={()=>changeEditing({name:r.name,auto:r.auto,target:r.target})}>编辑</Button>
          <Button size="compact" onClick={()=>apply([{name:r.name,auto:!r.auto,target:r.auto?r.target:undefined}])}>{r.auto?"改为手动":"自动识别"}</Button>
          <Button size="compact" onClick={()=>apply([{name:r.name,auto:false,target:""}])}>清空地址</Button></>},
      ]}/>
    {editing && <Modal title={"编辑探测目标 · "+editing.name} onClose={()=>{ if (!testing) changeEditing(null); }}>
      <Field label="识别方式"><select aria-label="探测目标识别方式" disabled={testing} value={editing.auto?"auto":"manual"} onChange={e=>changeEditing({...editing,auto:e.target.value==="auto"})}><option value="auto">自动识别</option><option value="manual">手动指定</option></select></Field>
      <Field label="对端隧道 IP" hint={editing.auto?"使用首个明确的候选主机地址":"单个 IPv4 或 IPv6 地址"}>
        <input aria-label="对端隧道 IP" disabled={editing.auto || testing} value={editing.auto ? testTarget : editing.target}
          onChange={e=>changeEditing({...editing,target:e.target.value})}/>
        {!editing.auto && !!all.find(r=>r.name===editing.name)?.candidates.length && <div className="actions wg-probe-candidate-actions">
          {all.find(r=>r.name===editing.name)?.candidates.map(ip=><Button key={ip} size="compact" disabled={testing} onClick={()=>changeEditing({...editing,target:ip})}>{ip}</Button>)}
        </div>}
      </Field>
      {testResult && <p role="status" className={testResult.ok ? "accent" : "poor"}>{testResult.message}</p>}
      <div className="actions">
        <Button disabled={testing || !editedRow?.active || !testTarget} title={editedRow?.active ? "测试当前探测目标" : "接口运行后可测试"} onClick={() => void testProbe()}>{testing ? "测试中…" : "测试"}</Button>
        <Button disabled={testing} onClick={() => changeEditing(null)}>取消</Button>
        <Button variant="primary" disabled={testing || !editedRow} onClick={() => { apply([editing]); changeEditing(null); }}>应用到待保存配置</Button>
      </div>
    </Modal>}
  </section>;
}
