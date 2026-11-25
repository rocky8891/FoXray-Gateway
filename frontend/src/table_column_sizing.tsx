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
function measurementClone(element: HTMLElement) {
  const clone = element.cloneNode(true) as HTMLElement;
  clone.removeAttribute("id");
  clone.querySelectorAll("[id]").forEach(e => e.removeAttribute("id"));
  clone.setAttribute("aria-hidden", "true");
  Object.assign(clone.style, { position: "fixed", left: "-100000px", top: "0", width: "max-content", minWidth: "0", maxWidth: "none", height: "auto", visibility: "hidden", display: "inline-block", whiteSpace: "nowrap", overflow: "visible" });
  return clone;
}

export function useTableColumnSizing<T>(table: RefObject<HTMLTableElement | null>, columns: Column<T>[], storageKey?: string) {
  const enabled = !!storageKey, key = "foxray.column-widths.v1." + storageKey;
  const [saved, setSaved] = useState(() => ({ key, widths: load(key) }));
  const [sizes, setSizes] = useState<Record<string, Size>>({});
  const [available, setAvailable] = useState(0);
  const drag = useRef<{ key: string; x: number; width: number; pointer: number } | null>(null);
  const manual = saved.key === key ? saved.widths : {};
  const fixedEdge = (column: Column<T>) => column === columns[0] || column === columns.at(-1);
  useEffect(() => { if (enabled) setSaved({ key, widths: load(key) }); drag.current = null; }, [key, enabled]);
  useEffect(() => {
    if (!enabled || saved.key !== key) return;
    try {
      if (Object.keys(saved.widths).length) localStorage.setItem(key, JSON.stringify(saved.widths));
      else localStorage.removeItem(key);
    } catch { /* Resizing still works when browser storage is unavailable. */ }
  }, [saved, key, enabled]);
  const measured = useRef<{ table: HTMLTableElement; signature: string } | null>(null);
  const [fontRevision, setFontRevision] = useState(0);

  useLayoutEffect(() => {
    const wrap = table.current?.parentElement;
    if (!enabled || !wrap) return;
    const updateWidth = () => {
      const css = getComputedStyle(wrap);
      const width = Math.floor(wrap.clientWidth - parseFloat(css.paddingLeft) - parseFloat(css.paddingRight));
      setAvailable(old => old === width ? old : width);
    };
    const fontsChanged = () => setFontRevision(value => value + 1);
    const observer = new ResizeObserver(updateWidth);
    updateWidth();
    observer.observe(wrap);
    document.fonts?.addEventListener("loadingdone", fontsChanged);
    return () => {
      observer.disconnect();
      document.fonts?.removeEventListener("loadingdone", fontsChanged);
    };
  }, [enabled, key, table]);

  useLayoutEffect(() => {
    const current = table.current;
    if (!enabled || !current) {
      measured.current = null;
      return;
    }
    const frame = requestAnimationFrame(() => {
      const bodyRows = [...current.querySelectorAll<HTMLTableRowElement>("tbody tr")];
      const groups = [...current.querySelectorAll<HTMLElement>("thead th[data-column]")].flatMap((th, index) => {
        const column = columns[index];
        const heading = th.querySelector<HTMLElement>(".adaptive-column-heading");
        if (!column || !heading) return [];
        const bodies = bodyRows.flatMap(row => {
          const cell = row.cells[index];
          const body = cell?.dataset.column === column.key && cell.querySelector<HTMLElement>(".adaptive-cell-content");
          return body ? [body] : [];
        });
        return [{ column, heading, bodies }];
      });
      const style = getComputedStyle(current);
      const signature = JSON.stringify([key, fontRevision, available, style.font, style.letterSpacing,
        groups.map(({ column, heading, bodies }) => [column.key, column.maxWidth, column.fitContent,
          heading.innerHTML, bodies.map(body => [body.parentElement?.className, body.innerHTML])]),
      ]);
      if (measured.current?.table === current && measured.current.signature === signature) return;

      const next: Record<string, Size> = {};
      const padding = (cell: HTMLElement) => {
        const css = getComputedStyle(cell);
        return parseFloat(css.paddingLeft) + parseFloat(css.paddingRight);
      };
      const samples = groups.map(({ column, heading, bodies }) => ({
        column,
        items: [heading, ...bodies].map(element => ({
          parent: element.parentElement!,
          padding: padding(element.parentElement!),
          clone: measurementClone(element),
        })),
      }));
      // Keep the table's CSS context and batch DOM writes before layout reads.
      for (const { items } of samples) for (const item of items) item.parent.appendChild(item.clone);
      try {
        for (const { column, items } of samples) {
          const widths = items.map(item => item.clone.getBoundingClientRect().width + item.padding);
          const min = Math.ceil(widths[0] + (fixedEdge(column) ? 0 : 14));
          const content = Math.ceil(Math.max(min, ...widths.slice(1)));
          const max = Math.max(min, column.maxWidth || 480, fixedEdge(column) || column.fitContent ? content : 0);
          const size = { min, max, auto: 0 };
          size.auto = clamp(content, size);
          next[column.key] = size;
        }
      } finally {
        for (const { items } of samples) for (const item of items) item.clone.remove();
      }
      measured.current = { table: current, signature };
      setSizes(old => JSON.stringify(old) === JSON.stringify(next) ? old : next);
    });
    return () => cancelAnimationFrame(frame);
  });
  const size = (column: Column<T>): Size => sizes[column.key] || { min: 64, max: column.maxWidth || 480, auto: 140 };
  const fitted = fitColumnWidths(columns.map(c => ({key:c.key,...size(c),manual:fixedEdge(c) ? undefined : manual[c.key],fitContent:fixedEdge(c) || c.fitContent})), available);
  const width = (column: Column<T>) => fitted[column.key];
  const setWidth = (column: Column<T>, value?: number) => setSaved(old => {
    const widths = { ...(old.key === key ? old.widths : {}) };
    if (value == null) delete widths[column.key]; else widths[column.key] = clamp(value, size(column));
    return { key, widths };
  });
  const handle = (column: Column<T>) => {
    if (fixedEdge(column)) return null;
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
  return { enabled, width, handle, reset: () => setSaved({ key, widths: {} }), manual: Object.keys(manual).some(k => columns.some(c => c.key === k && !fixedEdge(c))), total: columns.reduce((sum, c) => sum + width(c), 0) };
}
