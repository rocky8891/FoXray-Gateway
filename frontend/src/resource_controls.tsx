import { Children, useEffect, useState, type ReactNode } from "react";
import { ChevronLeft, ChevronRight, Copy, Download } from "lucide-react";

export function SortIcon({ size = 16 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 1024 1024"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M358.265 273.813l-111.561 94.165c-19.508 16.466-48.67 14-65.136-5.508-16.466-19.507-14-48.67 5.508-65.135l187.597-158.346c30.052-25.365 76.036-4.003 76.036 35.322v682.667c0 25.528-20.694 46.222-46.222 46.222-25.528 0-46.222-20.694-46.222-46.222V273.813z m410.837 389.409c19.507-16.466 48.67-14 65.135 5.508 16.466 19.507 14 48.67-5.507 65.135L641.132 892.211c-30.05 25.365-76.036 4.003-76.036-35.322V174.222c0-25.528 20.695-46.222 46.223-46.222 25.527 0 46.222 20.694 46.222 46.222v583.165l111.56-94.165z" />
    </svg>
  );
}

export function usePagination<T>(rows: T[], resetKey = "", maxPageSize = 100, autoPageSize?: number) {
  const [page, setPage] = useState(1),
    [requestedSize, setSize] = useState<number | null>(null);
  const pageSize = Math.max(1, Math.min(requestedSize ?? autoPageSize ?? 10, maxPageSize));
  useEffect(() => setPage(1), [resetKey]);
  const pageCount = Math.max(1, Math.ceil(rows.length / pageSize));
  const current = Math.min(page, pageCount),
    start = (current - 1) * pageSize;
  return {
    automatic: requestedSize === null && autoPageSize != null,
    autoPageSize,
    items: rows.slice(start, start + pageSize),
    page: current,
    pageSize,
    pageCount,
    start,
    end: Math.min(start + pageSize, rows.length),
    count: rows.length,
    setPage: (value: number) =>
      setPage(Math.max(1, Math.min(value, pageCount))),
    setPageSize: (value: number | null) => {
      setSize(value);
      setPage(1);
    },
  };
}
export type PaginationState = ReturnType<typeof usePagination>;
export function Pagination({
  value,
  total,
  selected,
  summary,
  actions,
}: {
  value: PaginationState;
  total?: number;
  selected?: number;
  summary?: ReactNode;
  actions?: ReactNode;
}) {
  const [jump, setJump] = useState("");
  const { page, pageCount, count, start, end } = value;
  const pages = [
    ...new Set(
      [
        1,
        ...Array.from({ length: 5 }, (_, i) => page - 2 + i),
        pageCount,
      ].filter((p) => p >= 1 && p <= pageCount),
    ),
  ].sort((a, b) => a - b);
  return (
    <div className="resource-pagination" aria-label="分页">
      <span className="resource-count">
        第 {count ? start + 1 : 0}-{end} 项，共 {count} 项
        {total !== undefined && total !== count ? ` · 总数 ${total}` : ""}
        {selected !== undefined ? ` · 已选 ${selected}` : ""}
        {summary}
      </span>
      <nav aria-label="页码">
        <button
          type="button"
          className="button ghost page-arrow"
          aria-label="上一页"
          disabled={page === 1}
          onClick={() => value.setPage(page - 1)}
        >
          <ChevronLeft size={16} />
        </button>
        {pages.map((p, i) => (
          <span key={p}>
            {i > 0 && p - pages[i - 1] > 1 && (
              <span className="page-gap">…</span>
            )}
            <button
              type="button"
              className={"page-number" + (p === page ? " active" : "")}
              aria-label={`第 ${p} 页`}
              aria-current={p === page ? "page" : undefined}
              onClick={() => value.setPage(p)}
            >
              {p}
            </button>
          </span>
        ))}
        <button
          type="button"
          className="button ghost page-arrow"
          aria-label="下一页"
          disabled={page === pageCount}
          onClick={() => value.setPage(page + 1)}
        >
          <ChevronRight size={16} />
        </button>
        <select
          aria-label="每页条数"
          value={value.automatic ? "auto" : value.pageSize}
          onChange={(e) => value.setPageSize(e.target.value === "auto" ? null : Number(e.target.value))}
        >
          {value.autoPageSize != null && <option value="auto">自动（{value.autoPageSize} 项/页）</option>}
          {[...new Set([value.pageSize, 10, 20, 50, 100])].sort((a,b)=>a-b).map((n) => (
            <option key={n} value={n}>
              {n} 项/页
            </option>
          ))}
        </select>
        <div className="page-jump">
          <label>
            跳至{" "}
            <input
              aria-label="跳转页码"
              inputMode="numeric"
              value={jump}
              onChange={(e) => setJump(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  e.stopPropagation();
                  if (jump.trim() && Number.isInteger(Number(jump))) {
                    value.setPage(Number(jump));
                    setJump("");
                  }
                }
              }}
            />
          </label>
          <button
            type="button"
            className="button ghost"
            onClick={() => {
              if (jump.trim() && Number.isInteger(Number(jump))) {
                value.setPage(Number(jump));
                setJump("");
              }
            }}
          >
            页
          </button>
        </div>
      </nav>
      {actions}
    </div>
  );
}
export function PaginatedCards({
  children,
  total,
  selected,
  resetKey = "",
  className = "cards two",
  pageSizeLimit,
  autoPageSize,
}: {
  children: ReactNode;
  total?: number;
  selected?: number;
  resetKey?: string;
  className?: string;
  pageSizeLimit?: number;
  autoPageSize?: number;
}) {
  const items = Children.toArray(children),
    value = usePagination(items, resetKey, pageSizeLimit, autoPageSize);
  return (
    <>
      <div className={className}>{value.items}</div>
      <Pagination value={value} total={total} selected={selected} />
    </>
  );
}

export function downloadFile(
  name: string,
  value: string,
  type = "text/plain;charset=utf-8",
) {
  const url = URL.createObjectURL(new Blob([value], { type })),
    anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function ExportButton<T>({
  rows,
  name,
  columns,
}: {
  rows: T[];
  name: string;
  columns?: [string, (row: T) => unknown][];
}) {
  const csv = () => {
    const headers =
      columns ||
      [...new Set(rows.flatMap((row) => Object.keys(row as object)))].map(
        (key) =>
          [key, (row: T) => (row as Record<string, unknown>)[key]] as [
            string,
            (row: T) => unknown,
          ],
      );
    const cell = (value: unknown) => {
      let text =
        value == null
          ? ""
          : typeof value === "object"
            ? JSON.stringify(value)
            : String(value);
      if (typeof value === "string" && /^(?:\s*[=+\-@]|[\t\r\n])/.test(text))
        text = "'" + text;
      return '"' + text.replace(/"/g, '""') + '"';
    };
    downloadFile(
      name + ".csv",
      "\ufeff" +
        [
          headers.map(([label]) => cell(label)).join(","),
          ...rows.map((row) =>
            headers.map(([, get]) => cell(get(row))).join(","),
          ),
        ].join("\r\n"),
      "text/csv;charset=utf-8",
    );
  };
  return (
    <button
      type="button"
      className="button secondary resource-export"
      disabled={!rows.length}
      onClick={csv}
      title="导出当前筛选范围；已选择时导出所选对象"
    >
      <Download size={15} />
      导出
    </button>
  );
}
export function CopyButton({
  text,
  label = "复制详情",
}: {
  text: string;
  label?: string;
}) {
  const [status, setStatus] = useState("");
  useEffect(() => {
    if (!status) return;
    const timer = setTimeout(() => setStatus(""), 2500);
    return () => clearTimeout(timer);
  }, [status]);
  const copy = async () => {
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        setStatus("已复制");
        return;
      } catch {}
    }
    const previous = document.activeElement as HTMLElement | null,
      area = document.createElement("textarea");
    try {
      area.value = text;
      area.style.position = "fixed";
      area.style.opacity = "0";
      (document.querySelector("dialog[open]") || document.body).append(area);
      area.select();
      if (!document.execCommand("copy")) throw Error();
      setStatus("已复制");
    } catch {
      setStatus("复制失败，请手动选择");
    } finally {
      area.remove();
      previous?.focus();
    }
  };
  return (
    <button
      type="button"
      className="button secondary copy-details"
      aria-label={status || label}
      disabled={!text}
      onClick={() => void copy()}
    >
      <Copy size={14} />
      <span role="status">{status || label}</span>
    </button>
  );
}

export function useRowSelection<T>(rows: T[], key: (row: T) => string) {
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const chosen = rows.filter((row) => selected.has(key(row)));
  const toggle = (row: T, on: boolean) =>
    setSelected((old) => {
      const next = new Set(old);
      on ? next.add(key(row)) : next.delete(key(row));
      return next;
    });
  const column = {
    key: "selected",
    label: "选择",
    heading: (
      <input
        type="checkbox"
        aria-label="全选"
        title="选择全部筛选结果，包含其他分页"
        checked={rows.length > 0 && chosen.length === rows.length}
        ref={(el) => {
          if (el)
            el.indeterminate = chosen.length > 0 && chosen.length < rows.length;
        }}
        onChange={(e) =>
          setSelected(e.target.checked ? new Set(rows.map(key)) : new Set())
        }
      />
    ),
    render: (row: T) => (
      <input
        type="checkbox"
        aria-label={"选择 " + key(row)}
        checked={selected.has(key(row))}
        onChange={(e) => toggle(row, e.target.checked)}
      />
    ),
  };
  return { chosen, selected, setSelected, toggle, column };
}
