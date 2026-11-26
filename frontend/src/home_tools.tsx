import { useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { HomeHeaderContext, HomeHeaderToggleIcon } from "./home_header";

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
  const showHeaderButton = header.hidden && !header.collapseInNav;
  const hadHeaderButton = useRef(false);
  const [handoff, setHandoff] = useState(false);
  const [autoHide, setAutoHide] = useState(true);
  const [pointerOpen, setPointerOpen] = useState(false);
  const hideTimer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(hideTimer.current), []);

  useEffect(() => {
    if (!dock || !header.closing || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    setHandoff(true);
  }, [dock, header.closing]);

  useEffect(() => {
    if (!handoff || header.closing) return;
    const timer = window.setTimeout(() => setHandoff(false), 1000);
    return () => window.clearTimeout(timer);
  }, [handoff, header.closing]);

  useLayoutEffect(() => {
    const opening = hadHeaderButton.current && !showHeaderButton && header.expanded;
    hadHeaderButton.current = showHeaderButton;
    const tools = edge.current?.querySelector(".overview-side-tools");
    if (!dock || !opening || !tools || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const distance = Number.parseFloat(getComputedStyle(tools).getPropertyValue("--button-height-regular")) + 6;
    if (!Number.isFinite(distance)) return;
    const animations = Array.from(tools.children, (child, index) => {
      const delay = Math.min(index, 3) * 55;
      return child.animate([{ translate: `0 ${distance}px` }, { translate: "0 0" }], {
        duration: 530 - delay,
        delay,
        easing: "cubic-bezier(.22,.8,.24,1)",
        fill: "backwards",
      });
    });
    return () => animations.forEach(animation => animation.cancel());
  }, [dock, header.expanded, showHeaderButton]);

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
      {showHeaderButton && (
        <button
          type="button"
          className="button home-header-toggle"
          aria-label={header.expanded ? "收起首页标题栏" : "展开首页标题栏"}
          title={header.expanded ? "收起首页标题栏" : "展开首页标题栏"}
          aria-expanded={header.expanded}
          onClick={(event) => header.toggle(event.currentTarget, event.detail === 0)}
        >
          <HomeHeaderToggleIcon />
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
      data-pointer-open={pointerOpen || undefined}
      data-header-handoff={handoff || undefined}
      onPointerEnter={(event) => {
        if (event.pointerType === "touch") return;
        window.clearTimeout(hideTimer.current);
        setPointerOpen(true);
      }}
      onPointerLeave={(event) => {
        if (event.pointerType !== "touch") {
          window.clearTimeout(hideTimer.current);
          hideTimer.current = window.setTimeout(() => setPointerOpen(false), 1000);
        }
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
