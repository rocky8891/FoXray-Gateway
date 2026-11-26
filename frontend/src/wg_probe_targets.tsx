import { useState } from "react";
import { Search } from "lucide-react";
import { AdaptiveSearch } from "./adaptive_search";
import { ActionMenu } from "./action_menu";
import { CompactFilters, ResetFilters, SortHeading } from "./list_controls";
import { ExportButton, useRowSelection } from "./resource_controls";
import { Button, Field, Modal, Table } from "./ui";
import { record, type Dict } from "./api";

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
  const [mode, setMode] = useState("all");
  const [status, setStatus] = useState("all");
  const [direction, setDirection] = useState(1);
  const [editing, setEditing] = useState<{name: string; auto: boolean; target: string} | null>(null);
  const all = items.map(item => targetRow(item, policy));
  const rows = all.filter(row => (mode === "all" || row.auto === (mode === "auto")) &&
    (status === "all" || row.active === (status === "active")) &&
    [row.name, row.remark, row.target, ...row.candidates].join(" ").toLowerCase().includes(query.trim().toLowerCase()))
    .sort((a,b) => a.name.localeCompare(b.name, undefined, {numeric: true}) * direction);
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
    <h3 className="settings-section-title">对端隧道 IP</h3>
    <p className="hint">自动识别使用首个候选主机地址；修改进入待保存配置，点击本卡片的保存按钮后生效。</p>
    <CompactFilters label="探测目标筛选" active={mode !== "all" || status !== "all"}
      before={<><Button onClick={onRefresh}>刷新</Button>
        <ActionMenu label="探测目标批量操作" triggerLabel="批量操作" showLabel items={[
          {label:"自动识别", disabled: !selection.chosen.length, onClick:()=>batch("auto")},
          {label:"改为手动（保留当前地址）", disabled: !selection.chosen.length, onClick:()=>batch("manual")},
          {label:"清空地址", disabled: !selection.chosen.length, onClick:()=>batch("clear")},
        ]}/></>}
      reset={<ResetFilters active={!!query || mode !== "all" || status !== "all" || direction !== 1} selectedCount={selection.chosen.length}
        onClick={()=>{setQuery("");setMode("all");setStatus("all");setDirection(1);selection.setSelected(new Set());}}/>}
      actions={<ExportButton name="WireGuard探测目标" rows={rows} columns={[["接口",r=>r.name],["状态",r=>r.active?"运行中":"已停止"],["识别方式",r=>r.auto?"自动识别":"手动指定"],["对端隧道 IP",r=>r.target]]}/>}
    >
      <AdaptiveSearch tableWidthKey="wg-probe-targets"><Search size={15}/><input aria-label="搜索探测目标" placeholder="查询接口、备注或 IP" value={query} onChange={e=>setQuery(e.target.value)}/></AdaptiveSearch>
      <Field label="识别方式"><select aria-label="探测目标识别方式筛选" value={mode} onChange={e=>setMode(e.target.value)}><option value="all">全部方式</option><option value="auto">自动识别</option><option value="manual">手动指定</option></select></Field>
      <Field label="接口状态"><select aria-label="探测目标接口状态筛选" value={status} onChange={e=>setStatus(e.target.value)}><option value="all">全部状态</option><option value="active">运行中</option><option value="inactive">已停止</option></select></Field>
    </CompactFilters>
    <Table columnSizingKey="wg-probe-targets" keepHeaders rows={rows} rowKey={row=>row.name} total={all.length} selectedCount={selection.chosen.length}
      columns={[selection.column,
        {key:"name",label:"接口",heading:<SortHeading label="接口" direction={direction} onClick={()=>setDirection(-direction)}/>,render:r=><><strong>{r.name}</strong>{r.remark&&<small>{r.remark}</small>}</>},
        {key:"status",label:"状态",render:r=><span className={r.active?"accent":"muted"}>{r.active?"运行中":"已停止"}</span>},
        {key:"mode",label:"识别方式",render:r=>r.auto?"自动识别":"手动指定"},
        {key:"target",label:"对端隧道 IP",render:r=>r.target||<span className="muted">{r.auto?"无候选地址，请手动指定":"未配置"}</span>},
        {key:"candidates",label:"候选地址",render:r=>r.candidates.join("、")||"—"},
        {key:"actions",label:"操作",render:r=><><Button size="compact" onClick={()=>setEditing({name:r.name,auto:r.auto,target:r.target})}>编辑</Button>
          <Button size="compact" onClick={()=>apply([{name:r.name,auto:!r.auto,target:r.auto?r.target:undefined}])}>{r.auto?"改为手动":"自动识别"}</Button>
          <Button size="compact" onClick={()=>apply([{name:r.name,auto:false,target:""}])}>清空地址</Button></>},
      ]}/>
    {editing && <Modal title={"编辑探测目标 · "+editing.name} onClose={()=>setEditing(null)}>
      <Field label="识别方式"><select aria-label="探测目标识别方式" value={editing.auto?"auto":"manual"} onChange={e=>setEditing({...editing,auto:e.target.value==="auto"})}><option value="auto">自动识别</option><option value="manual">手动指定</option></select></Field>
      <Field label="对端隧道 IP" hint={editing.auto?"使用首个明确的候选主机地址":"单个 IPv4 或 IPv6 地址"}>
        <input aria-label="对端隧道 IP" disabled={editing.auto} value={editing.auto?all.find(r=>r.name===editing.name)?.candidates[0]||"":editing.target}
          onChange={e=>setEditing({...editing,target:e.target.value})}/>
        {!editing.auto && !!all.find(r=>r.name===editing.name)?.candidates.length && <div className="actions wg-probe-candidate-actions">
          {all.find(r=>r.name===editing.name)?.candidates.map(ip=><Button key={ip} size="compact" onClick={()=>setEditing({...editing,target:ip})}>{ip}</Button>)}
        </div>}
      </Field>
      <div className="actions"><Button onClick={()=>setEditing(null)}>取消</Button><Button variant="primary" disabled={!all.some(r=>r.name===editing.name)} onClick={()=>{apply([editing]);setEditing(null);}}>应用到待保存配置</Button></div>
    </Modal>}
  </section>;
}
