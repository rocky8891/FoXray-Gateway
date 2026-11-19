import { useEffect, useRef } from "react";
import { cardNeighbor } from "./layout_editor";

type Target = { scope: string; id: string; element: HTMLElement };
export function useOverviewDrag(
  enabled: boolean,
  layout: string,
  move: (scope: string, from: string, to: string, ids: string[]) => void,
) {
  const root = useRef<HTMLDivElement>(null);
  const moveRef = useRef(move);
  moveRef.current = move;
  const moving = useRef<Target | null>(null);
  useEffect(() => {
    const element = root.current;
    if (!enabled || !element) return;
    const targets = () => {
      const result: Target[] = [];
      element
        .querySelectorAll<HTMLElement>(".dashboard-exit-grid>section")
        .forEach((el) => {
          const id =
            el.dataset.monitor ||
            [
              "service-host",
              "service-xray",
              "service-warp",
              "service-wireguard",
            ].find((c) => el.classList.contains(c));
          if (id) result.push({ scope: "cards", id, element: el });
        });
      element
        .querySelectorAll<HTMLElement>(".overview-node-grid>.node-card")
        .forEach((el) =>
          result.push({ scope: "nodes", id: el.dataset.nodeId!, element: el }),
        );
      for (const module of ["host", "xray", "warp"])
        element
          .querySelectorAll<HTMLElement>(
            ".service-" + module + " [data-summary-metric]",
          )
          .forEach((el) =>
            result.push({
              scope: module,
              id: el.dataset.summaryMetric!,
              element: el,
            }),
          );
      element
        .querySelectorAll<HTMLElement>(".service-wireguard .service-tunnel-row")
        .forEach((el) =>
          result.push({
            scope: "wireguard",
            id: el.dataset.interfaceName!,
            element: el,
          }),
        );
      return result;
    };
    const prepare = () =>
      targets().forEach((t) => {
        t.element.draggable = false;
        t.element.dataset.overviewEditable = t.scope;
        t.element.tabIndex = 0;
      });
    const find = (node: EventTarget | null) => {
      const el =
        node instanceof Element
          ? node.closest<HTMLElement>("[data-overview-editable]")
          : null;
      return el ? targets().find((t) => t.element === el) : undefined;
    };
    const swap = (a: Target, b: Target) => {
      if (a.scope !== b.scope || a.id === b.id) return;
      const siblings = targets()
        .filter((t) => t.scope === a.scope)
        .sort(
          (x, y) =>
            Number(getComputedStyle(x.element).order) -
            Number(getComputedStyle(y.element).order),
        );
      moveRef.current(
        a.scope,
        a.id,
        b.id,
        siblings.map((t) => t.id),
      );
    };
    let origin: {
      target: Target;
      x: number;
      y: number;
      pointer: number;
    } | null = null;
    let ghost: HTMLDivElement | null = null;
    let destination: Target | undefined;
    let point = { x: 0, y: 0 };
    let frame = 0,
      dragged = false;
    const closest = (x: number, y: number) => {
      const source = moving.current;
      if (!source) return;
      const candidates = targets().filter((t) => t.scope === source.scope);
      // Resolve using the source scope, even over nested metrics or empty gaps.
      return candidates.sort((a, b) => {
        const distance = (t: Target) => {
          const r = t.element.getBoundingClientRect();
          const dx = Math.max(r.left - x, 0, x - r.right),
            dy = Math.max(r.top - y, 0, y - r.bottom);
          return Math.hypot(dx, dy);
        };
        return distance(a) - distance(b);
      })[0];
    };
    const mark = () => {
      const candidate = closest(point.x, point.y);
      const next = candidate?.id === moving.current?.id ? undefined : candidate;
      if (next?.element !== destination?.element) {
        destination?.element.classList.remove("overview-drop-target");
        destination = next;
        destination?.element.classList.add("overview-drop-target");
      }
    };
    const scroll = () => {
      if (!moving.current) return;
      const edge = (value: number, min: number, max: number) =>
        value < min + 64
          ? -Math.min(20, (min + 64 - value) / 3)
          : value > max - 64
            ? Math.min(20, (value - max + 64) / 3)
            : 0;
      let parent: HTMLElement | null = moving.current.element.parentElement;
      let consumed = false;
      while (parent) {
        const style = getComputedStyle(parent);
        if (
          /(auto|scroll)/.test(style.overflowY) &&
          parent.scrollHeight > parent.clientHeight
        ) {
          const r = parent.getBoundingClientRect();
          const dy = edge(
            point.y,
            Math.max(0, r.top),
            Math.min(innerHeight, r.bottom),
          );
          const before = parent.scrollTop;
          parent.scrollTop += dy;
          if (parent.scrollTop !== before) {
            consumed = true;
            break;
          }
        }
        parent = parent.parentElement;
      }
      if (!consumed) window.scrollBy(0, edge(point.y, 0, innerHeight));
      mark();
      frame = requestAnimationFrame(scroll);
    };
    const end = () => {
      cancelAnimationFrame(frame);
      moving.current?.element.classList.remove("overview-dragging");
      destination?.element.classList.remove("overview-drop-target");
      ghost?.remove();
      ghost = null;
      destination = undefined;
      moving.current = null;
      origin = null;
      document.body.classList.remove("overview-pointer-drag");
    };
    const down = (e: PointerEvent) => {
      if (e.button !== 0) return;
      const target = find(e.target);
      if (!target) return;
      origin = { target, x: e.clientX, y: e.clientY, pointer: e.pointerId };
      point = { x: e.clientX, y: e.clientY };
      target.element.setPointerCapture(e.pointerId);
    };
    const pointerMove = (e: PointerEvent) => {
      if (!origin || e.pointerId !== origin.pointer) return;
      point = { x: e.clientX, y: e.clientY };
      if (
        !moving.current &&
        Math.hypot(point.x - origin.x, point.y - origin.y) < 5
      )
        return;
      e.preventDefault();
      if (!moving.current) {
        moving.current = origin.target;
        dragged = true;
        moving.current.element.classList.add("overview-dragging");
        ghost = document.createElement("div");
        ghost.className = "overview-pointer-ghost";
        ghost.setAttribute("aria-hidden", "true");
        ghost.textContent =
          moving.current.element.querySelector(
            "h3,.summary-metric-label,.service-interface-name,.node-title",
          )?.textContent || "移动卡片";
        (element.closest("dialog[open]") || document.body).append(ghost);
        document.body.classList.add("overview-pointer-drag");
        frame = requestAnimationFrame(scroll);
      }
      if (ghost) {
        ghost.style.left = `${point.x + 16}px`;
        ghost.style.top = `${point.y + 16}px`;
      }
      mark();
    };
    const up = (e: PointerEvent) => {
      if (!origin || origin.pointer !== e.pointerId) return;
      if (moving.current) {
        e.preventDefault();
        mark();
        if (destination) swap(moving.current, destination);
      }
      end();
    };
    const nativeStart = (e: DragEvent) => e.preventDefault();
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape" && origin) {
        e.preventDefault();
        e.stopPropagation();
        end();
        return;
      }
      if (
        !(e.target instanceof HTMLElement) ||
        !e.target.hasAttribute("data-overview-editable")
      )
        return;
      const a = find(e.target),
        direction = (
          {
            ArrowUp: "up",
            ArrowDown: "down",
            ArrowLeft: "left",
            ArrowRight: "right",
          } as Record<string, string>
        )[e.key];
      if (!a || !direction) return;
      e.preventDefault();
      e.stopPropagation();
      const siblings = targets()
        .filter((t) => t.scope === a.scope)
        .sort(
          (x, y) =>
            Number(getComputedStyle(x.element).order) -
            Number(getComputedStyle(y.element).order),
        );
      const card = a.element.closest<HTMLElement>(".service-overview-card");
      const local = ["host", "xray", "warp"].includes(a.scope)
        ? card?.dataset.circleLayout ||
          (card?.classList.contains("service-style-radial-diamond")
            ? "diamond"
            : card?.classList.contains("service-style-radial") &&
                card.dataset.ringColumns !== "2"
              ? "ring-row"
              : "grid")
        : a.scope === "wireguard"
          ? card?.dataset.adaptiveCircles === "true"
            ? card.dataset.tunnelAxis === "column"
              ? "single"
              : "ring-row"
            : matchMedia("(max-width:999px)").matches ||
                !card?.classList.contains("service-style-radial")
              ? "single"
              : "ring-row"
          : layout;
      const index = cardNeighbor(
        siblings.findIndex((t) => t.element === a.element),
        direction,
        siblings.length,
        local,
      );
      if (index >= 0) swap(a, siblings[index]);
    };
    const click = (e: MouseEvent) => {
      if (dragged || find(e.target)) {
        dragged = false;
        e.preventDefault();
        e.stopPropagation();
      }
    };
    prepare();
    const observer = new MutationObserver(prepare);
    observer.observe(element, { childList: true, subtree: true });
    element.addEventListener("click", click, true);
    element.addEventListener("dragstart", nativeStart);
    element.addEventListener("pointerdown", down);
    document.addEventListener("pointermove", pointerMove, { passive: false });
    document.addEventListener("pointerup", up);
    document.addEventListener("pointercancel", end);
    element.addEventListener("keydown", key);
    return () => {
      observer.disconnect();
      end();
      targets().forEach((t) => {
        t.element.removeAttribute("draggable");
        t.element.removeAttribute("data-overview-editable");
        t.element.removeAttribute("tabindex");
      });
      element.removeEventListener("click", click, true);
      element.removeEventListener("dragstart", nativeStart);
      element.removeEventListener("pointerdown", down);
      document.removeEventListener("pointermove", pointerMove);
      document.removeEventListener("pointerup", up);
      document.removeEventListener("pointercancel", end);
      element.removeEventListener("keydown", key);
    };
  }, [enabled, layout]);
  return root;
}
