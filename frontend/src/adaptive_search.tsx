import { createPortal } from "react-dom";
import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { Search } from "lucide-react";
export function AdaptiveSearch({ children, tableWidthKey }: { children: ReactNode; tableWidthKey?: string }) {
  const fieldRef = useRef<HTMLLabelElement>(null);
  const ref = useRef<HTMLSpanElement>(null),
    [collapsed, setCollapsed] = useState(false),
    [open, setOpen] = useState(false),
    [position, setPosition] = useState({ left: 8, top: 8 });
  useLayoutEffect(() => {
    const root = ref.current,
      bar = root?.closest<HTMLElement>(
        ".management-toolbar,.monitor-search-row,.filter-bar,.log-table-panel > .page-heading > .actions",
      );
    if (!root || !bar) return;
    const measure = () => {
      const tools = Array.from(
        bar.querySelectorAll<HTMLElement>(
          ".button,.detection-split,.warp-protocol-controls,.icon-select-trigger,.filter-summary",
        ),
      );
      const top = tools.filter(
        (e) =>
          !root.contains(e) && !tools.some((p) => p !== e && p.contains(e)),
      );
      const occupied = top.reduce(
        (n, e) => n + e.getBoundingClientRect().width + 8,
        0,
      );
      setCollapsed(bar.clientWidth - occupied < 220);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(bar);
    const widthSlot = root.previousElementSibling;
    if (widthSlot?.classList.contains("table-width-slot")) ro.observe(widthSlot);
    return () => ro.disconnect();
  }, [tableWidthKey]);
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const a = ref.current?.getBoundingClientRect();
      if (a)
        setPosition({
          left: Math.max(
            8,
            Math.min(a.left, innerWidth - Math.min(300, innerWidth - 32) - 8),
          ),
          top: Math.max(8, Math.min(a.bottom + 6, innerHeight - 50)),
        });
    };
    place();
    addEventListener("resize", place);
    addEventListener("scroll", place, true);
    fieldRef.current?.querySelector("input")?.focus();
    const close = (e: PointerEvent) => {
      if (
        !ref.current?.contains(e.target as Node) &&
        !fieldRef.current?.contains(e.target as Node)
      )
        setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => {
      document.removeEventListener("pointerdown", close);
      removeEventListener("resize", place);
      removeEventListener("scroll", place, true);
    };
  }, [open]);
  const field = (
    <label
      ref={fieldRef}
      className={
        "search adaptive-search-field" + (collapsed ? " search-popover" : "")
      }
      style={collapsed ? position : undefined}
      hidden={collapsed && !open}
    >
      {children}
    </label>
  );
  return (
    <>
    {tableWidthKey && <span className="table-width-slot" data-table-width-slot={tableWidthKey} />}
    <span
      ref={ref}
      className={"adaptive-search" + (collapsed ? " is-collapsed" : "")}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          setOpen(false);
          ref.current?.querySelector<HTMLButtonElement>("button")?.focus();
        }
      }}
    >
      {collapsed && (
        <button
          type="button"
          className="button secondary"
          data-control-icon="true"
          aria-label="展开搜索"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
        >
          <Search size={18} />
        </button>
      )}
      {collapsed
        ? createPortal(field, ref.current?.closest("dialog") || document.body)
        : field}
    </span>
    </>
  );
}
