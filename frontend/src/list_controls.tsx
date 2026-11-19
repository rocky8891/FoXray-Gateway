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
import { DoneIcon } from "./action_icons";
import { Button } from "./ui";

export function ResetFilters({
  onClick,
  summary = "",
  active = false,
  selectedCount = 0,
}: {
  onClick: () => void;
  summary?: string;
  active?: boolean;
  selectedCount?: number;
}) {
  const display = [
    selectedCount > 0 && `已选 ${selectedCount} 项`,
    active && summary,
  ]
    .filter(Boolean)
    .join(" · ")
    .replace(
      /(^| · )(状态|归属|分组|协议|接口)：(all|enabled|disabled|active|inactive|managed|external|standalone|subscription)(?= ·|$)/g,
      (_: string, prefix: string, label: string, value: string) =>
        prefix +
        label +
        "：" +
        ({
          all: "全部",
          enabled: "已启用",
          disabled: "已停用",
          active: "活动",
          inactive: "非活动",
          managed: "受管理",
          external: "外部",
          standalone: "独立节点",
          subscription: "订阅节点",
        }[value] || value),
    );
  return (
    <span className="filter-reset">
      <Button
        iconOnly
        ariaLabel="重置所有筛选"
        title={display ? `重置所有筛选\n${display}` : "重置所有筛选"}
        variant="primary"
        highlighted={!!display}
        onClick={onClick}
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          width="18"
          height="18"
          aria-hidden="true"
        >
          <path d="M3 11a9 9 0 1 1 2.4 7.2M3 4v7h7" />
        </svg>
      </Button>
    </span>
  );
}

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
    const resizeObserver = new ResizeObserver(place);
    if (popup.current) resizeObserver.observe(popup.current);
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
      resizeObserver.disconnect();
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
                <DoneIcon size={18} /> 完成
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
  before,
}: {
  children: ReactNode;
  label: string;
  active?: boolean;
  actions?: ReactNode;
  before?: ReactNode;
}) {
  const parts = Children.toArray(children).filter(
    (child) => typeof child !== "string" || child.trim(),
  );
  return (
    <div className="management-toolbar">
      {before}
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
