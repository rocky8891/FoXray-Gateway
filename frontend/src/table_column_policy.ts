import type { Column } from "./ui";

// Widths describe the field's display budget, never the currently visible rows.
// Explicit pixel widths/fixedWidth let a table reserve more room for its controls.
export function tableToolWidth<T>(column: Column<T>): number | undefined {
  if (column.label === "选择" || ["selected", "select"].includes(column.key)) return 36;
  if (column.label === "序号" || column.key === "serial") return 42;
  if (column.label === "操作" || column.key === "actions") return 48;
  if (column.label === "详情" || column.key === "details") return 96;
}

export function fixedTableColumnWidth<T>(column: Column<T>, index: number, count: number): number | undefined {
  const tool = tableToolWidth(column);
  if (tool == null && index !== 0 && index !== count - 1 && column.fixedWidth == null) return;
  if (column.fixedWidth != null && Number.isFinite(column.fixedWidth)) return Math.max(tool || 32, column.fixedWidth);
  if (column.width && /^\d+(?:\.\d+)?px$/.test(column.width)) return Math.max(tool || 32, parseFloat(column.width));
  if (tool != null) return tool;
  if (/时间|日期/.test(column.label) || /^(time|timestamp|created|updated)$/.test(column.key)) return 192;
  if (/IP|地址|网段|Endpoint/i.test(column.label) || /^(source|destination|endpoint)$/.test(column.key)) return 280;
  if (/端口/.test(column.label)) return 112;
  if (/状态|结果/.test(column.label)) return 160;
  return 224;
}
