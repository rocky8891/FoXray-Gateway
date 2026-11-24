export interface WidthFitColumn { key: string; min: number; max: number; auto: number; manual?: number; fitContent?: boolean }

// Distribute spare space proportionally among automatic columns, respecting bounds.
// Manual widths stay pinned, and oversized content retains horizontal scrolling.
export function fitColumnWidths(columns: WidthFitColumn[], available: number): Record<string, number> {
  const widths: Record<string, number> = {};
  for (const c of columns) widths[c.key] = Math.round(Math.max(c.min, Math.min(c.max, c.manual ?? c.auto)));
  let spare = Math.max(0, Math.floor(available) - Object.values(widths).reduce((a, b) => a + b, 0));
  let active = columns.filter(c => !c.fitContent && c.manual == null && widths[c.key] < c.max);
  while (spare > 0 && active.length) {
    const weight = active.reduce((sum,c) => sum + Math.max(1,c.auto), 0), budget = spare;
    let used = 0;
    for (const c of active) {
      const add = Math.min(c.max - widths[c.key], Math.floor(budget * Math.max(1,c.auto) / weight));
      widths[c.key] += add; used += add;
    }
    spare -= used;
    if (!used) {
      for (const c of active) { if (!spare) break; if (widths[c.key] < c.max) { widths[c.key]++; spare--; } }
    }
    active = active.filter(c => widths[c.key] < c.max);
  }
  return widths;
}
