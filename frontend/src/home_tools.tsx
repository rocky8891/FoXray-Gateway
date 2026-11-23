import { useContext, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { HomeHeaderContext } from "./home_header";

export function HomeTools({
  dock = false,
  view,
  children,
}: {
  dock?: boolean;
  view: string;
  children: ReactNode;
}) {
  const header = useContext(HomeHeaderContext);
  const edge = useRef<HTMLDivElement>(null);
  const [autoHide, setAutoHide] = useState(true);

  useLayoutEffect(() => {
    const rail = edge.current;
    const card = document.querySelector("main > .overview-page .overview-home > :is(.dashboard-current, .dashboard-nodes)");
    const buttons = rail?.querySelector(".overview-side-tools");
    if (!rail || !card || !buttons) return;
    const measure = () => {
      const bounds = card.getBoundingClientRect();
      const gap = Number.parseFloat(getComputedStyle(rail).getPropertyValue("--dock-card-gap"));
      const left = bounds.right + gap;
      rail.style.setProperty("--dock-left", `${left}px`);
      rail.style.setProperty("--dock-top", `${bounds.top}px`);
      // The slide only changes the transform, so it cannot affect the fit calculation.
      setAutoHide(left + buttons.getBoundingClientRect().width > document.documentElement.clientWidth);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(card);
    observer.observe(buttons);
    window.addEventListener("resize", measure);
    measure();
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [dock, view, header.expanded, header.hidden]);

  const tools = (
    <aside className="overview-side-tools" data-overview-view={view} aria-label="主页快捷工具">
      {header.hidden && (
        <button
          type="button"
          className="button home-header-toggle"
          aria-label={header.expanded ? "收起首页标题栏" : "展开首页标题栏"}
          title={header.expanded ? "收起首页标题栏" : "展开首页标题栏"}
          aria-expanded={header.expanded}
          onClick={(event) => header.toggle(event.currentTarget)}
        >
          <svg viewBox="105 83 292 349" aria-hidden="true">
            <image href="/foxray-icon.svg?v=0.9.4" width="512" height="512" />
          </svg>
        </button>
      )}
      {children}
    </aside>
  );
  if (!dock) return tools;

  return createPortal(
    <div
      ref={edge}
      className="home-tools-dock"
      data-auto-hide={autoHide || undefined}
      onPointerLeave={(event) => {
        const focused = document.activeElement;
        if (
          event.pointerType === "mouse" &&
          focused instanceof HTMLButtonElement &&
          focused.classList.contains("home-tools-handle") &&
          !focused.matches(":focus-visible")
        ) focused.blur();
      }}
    >
      {autoHide && (
        <button
          type="button"
          className="home-tools-handle"
          aria-label="展开首页工具栏"
          onKeyDown={(event) => {
            if (event.key === "Escape") event.currentTarget.blur();
          }}
        />
      )}
      {tools}
    </div>,
    document.body,
  );
}
