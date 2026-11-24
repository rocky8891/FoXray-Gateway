import { useEffect, useState } from "react";
import { Search } from "lucide-react";
import { type Dict } from "./api";
import { AdaptiveSearch } from "./adaptive_search";
import { ListFilter, ResetFilters, SortHeading } from "./list_controls";
import { Button, Field, type Column } from "./ui";

type Filter = { text?: string; min?: string; max?: string; rate?: boolean };
const numeric = new Set(["upload", "download", "age"]);
const exact = new Set(["protocol", "state", "routing"]);
const collator = new Intl.Collator("zh-CN", { numeric: true, sensitivity: "base" });

export function useConnectionTableControls(rows: Dict[], resetKey: string, tableWidthKey: string) {
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<Record<string, Filter>>({});
  const [sort, setSort] = useState<{ key: string; direction: number } | null>(null);
  const reset = () => { setQuery(""); setFilters({}); setSort(null); };
  useEffect(reset, [resetKey]);
  const value = (row: Dict, key: string): string | number | null => {
    if (key === "interface") return String(row.interface || "未识别");
    if (key === "routing") return row.routing_classification === "proxy" ? "代理" : "未确认";
    if (key === "state") return ["近期有流量", "Active"].includes(String(row.state)) ? "Active" : String(row.state || "");
    if (numeric.has(key)) {
      const v = row[key === "age" ? "observed_seconds" : filters[key]?.rate ? key + "_bps" : key];
      return v == null || !Number.isFinite(Number(v)) ? null : Number(v);
    }
    return String(row[key] || (key === "domain" ? "未观测" : ""));
  };
  const filtered = rows.filter(row => {
    const haystack = [...["source", "destination", "domain", "protocol", "state", "node_name", "node_id", "interface", "upload", "download", "observed_seconds"].map(key => row[key]), value(row,"interface"), value(row,"routing"), value(row,"state")].join(" ").toLowerCase();
    if (!query.trim().toLowerCase().split(/\s+/).every(term => haystack.includes(term))) return false;
    return Object.entries(filters).every(([key, filter]) => {
      const v = value(row, key);
      if (numeric.has(key)) return (filter.min == null || filter.min === "" || v != null && Number(v) >= Number(filter.min)) && (filter.max == null || filter.max === "" || v != null && Number(v) <= Number(filter.max));
      const text = (filter.text || "").trim().toLowerCase();
      return !text || (exact.has(key) ? String(v).toLowerCase() === text : String(v).toLowerCase().includes(text));
    });
  });
  if (sort) filtered.sort((a, b) => {
    const av = value(a, sort.key), bv = value(b, sort.key);
    if (av == null) return bv == null ? 0 : 1;
    if (bv == null) return -1;
    const compared = typeof av === "number" && typeof bv === "number" ? av - bv : collator.compare(String(av), String(bv));
    return compared * sort.direction || collator.compare(String(a.id), String(b.id));
  });
  const active = !!query || !!sort || Object.values(filters).some(f => f.text || f.min || f.max || f.rate);
  const toolbar = <div className="connections-detail-tools">
    <AdaptiveSearch tableWidthKey={tableWidthKey}><Search size={18} /><input aria-label="搜索连接详情" placeholder="搜索源、目标、域名、接口或协议" value={query} onChange={e => setQuery(e.target.value)} /></AdaptiveSearch>
    <ResetFilters active={active} summary="连接详情排序、搜索和列筛选" onClick={reset} />
  </div>;
  const column = (c: Column<Dict>): Column<Dict> => {
    if (["serial", "actions"].includes(c.key)) return c;
    const f = filters[c.key] || {};
    const set = (patch: Filter) => setFilters(old => ({ ...old, [c.key]: { ...old[c.key], ...patch } }));
    return { ...c, sortDirection: sort?.key === c.key ? sort.direction === 1 ? "ascending" : "descending" : "none", heading: <span className="table-heading-tools">
      <SortHeading label={c.label} direction={sort?.key === c.key ? sort.direction : undefined} onClick={() => setSort(old => ({key:c.key,direction:old?.key === c.key ? -old.direction : 1}))} />
      <ListFilter label={c.label + "筛选"} column active={!!(f.text || f.min || f.max || f.rate)}>
        {numeric.has(c.key) ? <>
          {c.key !== "age" && <Field label="数值依据"><select aria-label={c.label + "数值依据"} value={f.rate ? "rate" : "bytes"} onChange={e => set({rate:e.target.value === "rate"})}><option value="bytes">累计字节</option><option value="rate">每秒速率（B/s）</option></select></Field>}
          <Field label={c.key === "age" ? "最小时长（秒）" : f.rate ? "最小速率（B/s）" : "最小字节数"}><input type="number" min="0" aria-label={c.label + "最小值"} value={f.min || ""} onChange={e => set({min:e.target.value})} /></Field>
          <Field label="最大值（相同单位）"><input type="number" min="0" aria-label={c.label + "最大值"} value={f.max || ""} onChange={e => set({max:e.target.value})} /></Field>
        </> : <Field label={c.label}>{exact.has(c.key) ? <select aria-label={c.label + "筛选值"} value={f.text || ""} onChange={e => set({text:e.target.value})}><option value="">全部</option>{[...new Set(rows.map(r => String(value(r,c.key))))].sort(collator.compare).map(v => <option key={v} value={v}>{v}</option>)}</select> : <input aria-label={c.label + "筛选值"} placeholder="包含关键词" value={f.text || ""} onChange={e => set({text:e.target.value})} />}</Field>}
        <Button onClick={() => setFilters(old => {const next={...old};delete next[c.key];return next})}>清除此列筛选</Button>
      </ListFilter>
    </span> };
  };
  return { rows: filtered, toolbar, columns: (columns: Column<Dict>[]) => columns.map(column), paginationKey: JSON.stringify({resetKey,query,filters,sort}) };
}
