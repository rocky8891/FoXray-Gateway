import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

export function MetricHover({
  label,
  detail,
  className,
  children,
}: {
  label: string;
  detail: string;
  className: string;
  children: ReactNode;
}) {
  const anchor = useRef<HTMLDivElement>(null),
    tooltip = useRef<HTMLDivElement>(null),
    id = useId();
  const [open, setOpen] = useState(false),
    [position, setPosition] = useState({ left: 0, top: 0 });
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const a = anchor.current?.getBoundingClientRect(),
        t = tooltip.current;
      if (!a || !t) return;
      const card = anchor.current
        ?.closest(".host-runtime-panel")
        ?.getBoundingClientRect();
      const minTop = Math.max(12, (card?.top ?? 0) + 8);
      const maxBottom = Math.min(
        innerHeight - 12,
        card ? card.bottom - 8 : innerHeight - 12,
      );
      const preferredTop =
        a.bottom - 4 + t.offsetHeight <= maxBottom
          ? a.bottom - 4
          : Math.min(a.bottom - 4, maxBottom - t.offsetHeight);
      setPosition({
        left: Math.max(
          12,
          Math.min(
            a.left + a.width / 2 - t.offsetWidth / 2,
            document.documentElement.clientWidth - t.offsetWidth - 12,
          ),
        ),
        top: Math.max(
          minTop,
          Math.min(preferredTop, maxBottom - t.offsetHeight),
        ),
      });
    };
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, label, detail]);
  return (
    <div
      ref={anchor}
      className={className}
      role="group"
      aria-label={label}
      tabIndex={0}
      aria-describedby={open ? id : undefined}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setOpen(false);
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") setOpen(false);
      }}
    >
      {children}
      {open &&
        createPortal(
          <div
            ref={tooltip}
            id={id}
            role="tooltip"
            className="metric-hover-info"
            style={position}
          >
            <strong>{label}</strong>
            <p>{detail}</p>
          </div>,
          document.body,
        )}
    </div>
  );
}
