import { useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { HomeHeaderContext, HomeHeaderToggleIcon, reduceHomeMotion } from "./home_header";

export function HomeTools({
  dock = false,
  view,
  children,
}: {
  dock?: boolean;
  view: string;
  children?: ReactNode;
}) {
  const header = useContext(HomeHeaderContext);
  const edge = useRef<HTMLDivElement>(null);
  const showHeaderButton = header.hidden && !header.collapseInNav;
  const previousView = useRef(view);
  const previousHeaderButton = useRef(showHeaderButton);
  const wasClosing = useRef(header.closing);
  const toolsRef = useRef<HTMLElement>(null);
  const animations = useRef<Animation[]>([]);
  const [handoff, setHandoff] = useState(false);
  const [autoHide, setAutoHide] = useState(true);
  const [pointerOpen, setPointerOpen] = useState(false);
  const [switchingView, setSwitchingView] = useState(false);
  const [awaitingPointer, setAwaitingPointer] = useState(false);
  const hideTimer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(hideTimer.current), []);

  useLayoutEffect(() => {
    const finishedClosing = wasClosing.current && !header.closing;
    wasClosing.current = header.closing;
    if (!dock || !showHeaderButton) {
      setHandoff(false);
      return;
    }
    if (finishedClosing && !reduceHomeMotion()) setHandoff(true);
  }, [dock, header.closing, showHeaderButton]);

  useEffect(() => {
    if (!handoff) return;
    const timer = window.setTimeout(() => setHandoff(false), 1000);
    return () => window.clearTimeout(timer);
  }, [handoff]);

  const animateTools = () => {
    const tools = toolsRef.current;
    if (!tools || reduceHomeMotion() || getComputedStyle(tools).pointerEvents === "none") return;
    animations.current.forEach(animation => animation.cancel());
    const buttons = Array.from(tools.querySelectorAll<HTMLElement>("button"))
      .filter(button => button.getClientRects().length);
    const first = buttons[0]?.getBoundingClientRect();
    if (!first) return;
    animations.current = buttons.slice(1).map((button, index) => {
      const position = button.getBoundingClientRect();
      return button.animate([
        { translate: `${first.left - position.left}px ${first.top - position.top}px`, scale: 0.9, opacity: 0 },
        { translate: "0 0", scale: 1, opacity: 1 },
      ], {
        duration: 560,
        delay: index * 55,
        easing: "cubic-bezier(.22,.8,.24,1)",
        fill: "backwards",
      });
    });
  };

  useLayoutEffect(() => {
    const changed = previousView.current !== view;
    previousView.current = view;
    if (!dock || !changed) return;
    window.clearTimeout(hideTimer.current);
    setPointerOpen(false);
    setAwaitingPointer(true);
    setSwitchingView(!reduceHomeMotion());
  }, [dock, view]);

  useEffect(() => {
    if (!switchingView) return;
    const tools = toolsRef.current;
    const count = tools ? Array.from(tools.querySelectorAll("button")).filter(button => button.getClientRects().length).length : 0;
    const timer = window.setTimeout(() => setSwitchingView(false), 660 + Math.max(0, count - 2) * 55);
    return () => window.clearTimeout(timer);
  }, [switchingView, view]);

  useLayoutEffect(() => {
    const changed = previousHeaderButton.current !== showHeaderButton;
    previousHeaderButton.current = showHeaderButton;
    const tools = toolsRef.current;
    if (!dock || !tools) return;
    if (switchingView || changed || !autoHide) animateTools();
    const onReveal = (event: TransitionEvent) => {
      if (event.target === tools && event.propertyName === "transform") animateTools();
    };
    tools.addEventListener("transitionrun", onReveal);
    return () => tools.removeEventListener("transitionrun", onReveal);
  }, [dock, view, switchingView, showHeaderButton, autoHide]);

  useEffect(() => () => animations.current.forEach(animation => animation.cancel()), []);

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
    <aside ref={toolsRef} className="overview-side-tools" data-overview-view={view} aria-label="主页快捷工具">
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
      data-view-switch={switchingView || undefined}
      data-awaiting-pointer={awaitingPointer || undefined}
      onPointerMove={(event) => {
        if (!awaitingPointer || event.pointerType === "touch") return;
        setAwaitingPointer(false);
        setPointerOpen(true);
      }}
      onPointerEnter={(event) => {
        if (event.pointerType === "touch" || awaitingPointer) return;
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
