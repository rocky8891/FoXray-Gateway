import {
  Children,
  isValidElement,
  type ReactElement,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { Funnel } from "lucide-react";

export function SortHeading({
  label,
  direction,
  onClick,
}: {
  label: string;
  direction?: number;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className="sort-heading"
      aria-label={"按" + label + "排序"}
      onClick={onClick}
    >
      <span>{label}</span>
      <span className="sort-caret" aria-hidden="true">
        <svg viewBox="0 0 12 7" className={direction === 1 ? "active" : ""}>
          <path d="M6 0 12 7H0Z" />
        </svg>
        <svg viewBox="0 0 12 7" className={direction === -1 ? "active" : ""}>
          <path d="M0 0H12L6 7Z" />
        </svg>
      </span>
    </button>
  );
}
export function ListFilter({
  label,
  children,
  active = false,
  icon,
  column = false,
}: {
  label: string;
  children: ReactNode;
  active?: boolean;
  icon?: ReactNode;
  column?: boolean;
}) {
  const [open, setOpen] = useState(false),
    [position, setPosition] = useState({ top: 0, left: 0 });
  const anchor = useRef<HTMLButtonElement>(null),
    popup = useRef<HTMLDivElement>(null),
    id = useId();
  const parts = Children.toArray(children);
  const isAction = (child: ReactNode) =>
    isValidElement(child) &&
    typeof (child as ReactElement<{ onClick?: unknown }>).props.onClick ===
      "function";
  const close = (focus = false) => {
    setOpen(false);
    if (focus) anchor.current?.focus();
  };
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const a = anchor.current?.getBoundingClientRect(),
        p = popup.current;
      if (!a || !p) return;
      if (a.bottom < 0 || a.top > innerHeight) {
        close();
        return;
      }
      setPosition({
        left: Math.max(
          8,
          Math.min(a.right - p.offsetWidth, innerWidth - p.offsetWidth - 8),
        ),
        top: Math.max(
          8,
          Math.min(a.bottom + 6, innerHeight - p.offsetHeight - 8),
        ),
      });
    };
    place();
    popup.current?.querySelector<HTMLElement>("input,select,button")?.focus();
    const outside = (e: PointerEvent) => {
      if (
        !popup.current?.contains(e.target as Node) &&
        !anchor.current?.contains(e.target as Node)
      )
        close();
    };
    addEventListener("resize", place);
    addEventListener("scroll", place, true);
    document.addEventListener("pointerdown", outside);
    return () => {
      removeEventListener("resize", place);
      removeEventListener("scroll", place, true);
      document.removeEventListener("pointerdown", outside);
    };
  }, [open]);
  return (
    <span className={"list-filter-control" + (column ? " column-filter" : "")}>
      <button
        ref={anchor}
        type="button"
        className={"button ghost list-tool-button" + (active ? " active" : "")}
        aria-label={label}
        title={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen(!open)}
      >
        {icon || <Funnel size={column ? 13 : 16} />}
      </button>
      {open &&
        createPortal(
          <div
            ref={popup}
            id={id}
            role="dialog"
            aria-label={label}
            className="list-filter-popup"
            style={position}
            onBlur={(e) => {
              if (
                !e.currentTarget.contains(e.relatedTarget as Node) &&
                e.relatedTarget !== anchor.current
              )
                close();
            }}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                close(true);
              }
            }}
          >
            <div className="filter-fields">
              {parts.filter((child) => !isAction(child))}
            </div>
            <div className="filter-actions">
              {parts.filter(isAction)}
              <button
                type="button"
                className="button secondary filter-done"
                onClick={() => close(true)}
              >
                完成
              </button>
            </div>
          </div>,
          anchor.current?.closest("dialog") || document.body,
        )}
    </span>
  );
}

export function CompactFilters({
  children,
  label,
  active = false,
  actions,
}: {
  children: ReactNode;
  label: string;
  active?: boolean;
  actions?: ReactNode;
}) {
  const parts = Children.toArray(children).filter(
    (child) => typeof child !== "string" || child.trim(),
  );
  return (
    <div className="management-toolbar">
      <div className="management-query-tools">
        {parts[0]}
        {parts.length > 1 && (
          <ListFilter label={label} active={active}>
            {parts.slice(1)}
          </ListFilter>
        )}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  );
}
