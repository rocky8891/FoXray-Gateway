import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import type { Column } from "./ui";
import "./table_column_sizing.css";
import { fitColumnWidths } from "./table_width_fit";

type Size = { min: number; max: number; auto: number };
type Widths = Record<string, number>;
const clamp = (width: number, size: Size) => Math.round(Math.min(size.max, Math.max(size.min, width)));
function load(key: string): Widths {
  try {
    const value = JSON.parse(localStorage.getItem(key) || "{}");
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    return Object.fromEntries(Object.entries(value).filter(([, width]) => typeof width === "number" && Number.isFinite(width)) as [string, number][]);
  } catch { return {}; }
}
function naturalWidth(element: HTMLElement) {
  const clone = element.cloneNode(true) as HTMLElement;
  clone.querySelectorAll("[id]").forEach(e => e.removeAttribute("id"));
  clone.setAttribute("aria-hidden", "true");
  Object.assign(clone.style, { position: "fixed", left: "-100000px", top: "0", width: "max-content", minWidth: "0", maxWidth: "none", height: "auto", visibility: "hidden", display: "inline-block", whiteSpace: "nowrap", overflow: "visible" });
  element.parentElement!.appendChild(clone);
  const width = clone.getBoundingClientRect().width;
  clone.remove();
  return width;
}

export function useTableColumnSizing<T>(table: RefObject<HTMLTableElement | null>, columns: Column<T>[], rows: unknown, pageKey: string, storageKey?: string) {
  const enabled = !!storageKey, key = "foxray.column-widths.v1." + storageKey;
  const [saved, setSaved] = useState(() => ({ key, widths: load(key) }));
  const [sizes, setSizes] = useState<Record<string, Size>>({});
  const [available, setAvailable] = useState(0);
  const drag = useRef<{ key: string; x: number; width: number; pointer: number } | null>(null);
  const manual = saved.key === key ? saved.widths : {};
  useEffect(() => { if (enabled) setSaved({ key, widths: load(key) }); drag.current = null; }, [key, enabled]);
  useEffect(() => {
    if (!enabled || saved.key !== key) return;
    try {
      if (Object.keys(saved.widths).length) localStorage.setItem(key, JSON.stringify(saved.widths));
      else localStorage.removeItem(key);
    } catch { /* Resizing still works when browser storage is unavailable. */ }
  }, [saved, key, enabled]);
  // Measure the rendered page, including sort/filter controls, in the active font.
  useLayoutEffect(() => {
    if (!enabled || !table.current) return;
    let disposed = false;
    const measure = () => {
      const wrap = table.current?.parentElement;
      if (wrap) {
        const css = getComputedStyle(wrap);
        const width = Math.floor(wrap.clientWidth - parseFloat(css.paddingLeft) - parseFloat(css.paddingRight));
        setAvailable(old => old === width ? old : width);
      }
      const next: Record<string, Size> = {};
      table.current?.querySelectorAll<HTMLElement>("thead th[data-column]").forEach((th, index) => {
        const column = columns[index], heading = th.querySelector<HTMLElement>(".adaptive-column-heading");
        if (!column || !heading) return;
        const css = getComputedStyle(th), padding = parseFloat(css.paddingLeft) + parseFloat(css.paddingRight);
        const min = Math.ceil(naturalWidth(heading) + padding + 14);
        const max = Math.max(min, column.maxWidth || 480);
        let content = min;
        table.current?.querySelectorAll<HTMLTableRowElement>("tbody tr").forEach(row => {
          const cell = row.cells[index];
          if (cell?.dataset.column !== column.key) return;
          const body = cell.querySelector<HTMLElement>(".adaptive-cell-content");
          if (body) content = Math.max(content, naturalWidth(body) + padding);
        });
        const size = { min, max, auto: 0 };
        size.auto = clamp(content, size); next[column.key] = size;
      });
      setSizes(old => JSON.stringify(old) === JSON.stringify(next) ? old : next);
    };
    measure();
    document.fonts?.ready.then(() => { if (!disposed && table.current) measure(); });
    const observer = new ResizeObserver(measure);
    if (table.current.parentElement) observer.observe(table.current.parentElement);
    return () => { disposed = true; observer.disconnect(); };
  }, [enabled, columns, rows, pageKey, key, table]);
  const size = (column: Column<T>): Size => sizes[column.key] || { min: 64, max: column.maxWidth || 480, auto: 140 };
  const fitted = fitColumnWidths(columns.map(c => ({key:c.key,...size(c),manual:manual[c.key]})), available);
  const width = (column: Column<T>) => fitted[column.key];
  const setWidth = (column: Column<T>, value?: number) => setSaved(old => {
    const widths = { ...(old.key === key ? old.widths : {}) };
    if (value == null) delete widths[column.key]; else widths[column.key] = clamp(value, size(column));
    return { key, widths };
  });
  const handle = (column: Column<T>) => {
    const bounds = size(column);
    return <span className="column-resize-handle" role="separator" aria-orientation="vertical" tabIndex={0}
      aria-label={column.label + "列宽"} aria-valuemin={bounds.min} aria-valuemax={bounds.max} aria-valuenow={width(column)}
      title="拖动调整列宽；双击恢复此列自动宽度；方向键微调"
      onClick={e => e.stopPropagation()}
      onDoubleClick={e => { e.stopPropagation(); setWidth(column); }}
      onPointerDown={e => { if (e.button !== 0) return; e.preventDefault(); e.stopPropagation(); drag.current = { key: column.key, x: e.clientX, width: width(column), pointer: e.pointerId }; e.currentTarget.setPointerCapture(e.pointerId); }}
      onPointerMove={e => { const d = drag.current; if (d?.key === column.key && d.pointer === e.pointerId) setWidth(column, d.width + e.clientX - d.x); }}
      onPointerUp={e => { drag.current = null; if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId); }}
      onPointerCancel={() => { drag.current = null; }} onLostPointerCapture={() => { drag.current = null; }}
      onKeyDown={e => {
        const step = e.shiftKey ? 32 : 8;
        if (e.key === "ArrowLeft" || e.key === "ArrowRight") { e.preventDefault(); setWidth(column, width(column) + (e.key === "ArrowLeft" ? -step : step)); }
        else if (e.key === "Home" || e.key === "End") { e.preventDefault(); setWidth(column, e.key === "Home" ? bounds.min : bounds.max); }
        else if (e.key === "Delete" || e.key === "Backspace") { e.preventDefault(); setWidth(column); }
      }} />;
  };
  return { enabled, width, handle, reset: () => setSaved({ key, widths: {} }), manual: Object.keys(manual).some(k => columns.some(c => c.key === k)), total: columns.reduce((sum, c) => sum + width(c), 0) };
}
