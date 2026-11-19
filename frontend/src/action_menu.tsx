import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { MoreHorizontal, ChevronRight, CircleDot } from "lucide-react";
import { labelIcon } from "./ui_icons";

export type MenuAction = {
  label: string;
  onClick?: () => void;
  items?: MenuAction[];
  disabled?: boolean;
  title?: string;
  icon?: ReactNode;
};

function MenuLayer({
  actions,
  anchor,
  rootId,
  label,
  submenu = false,
  close,
  back,
}: {
  actions: MenuAction[];
  anchor: HTMLElement;
  rootId: string;
  label: string;
  submenu?: boolean;
  close: (focus?: boolean) => void;
  back?: () => void;
}) {
  const menu = useRef<HTMLDivElement>(null),
    buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const [position, setPosition] = useState({ top: 0, left: 0 });
  const [child, setChild] = useState<number | null>(null);
  const hoverBlocked = useRef<number | null>(null);
  useLayoutEffect(() => {
    const place = () => {
      const a = anchor.getBoundingClientRect(),
        m = menu.current;
      if (!m) return;
      if (a.bottom < 0 || a.top > innerHeight || !anchor.isConnected) {
        close();
        return;
      }
      const height = m.offsetHeight,
        width = m.offsetWidth;
      const left = submenu
        ? a.right + width + 6 <= innerWidth
          ? a.right + 4
          : a.left - width - 4
        : a.right - width;
      setPosition({
        left: Math.max(8, Math.min(left, innerWidth - width - 8)),
        top: Math.max(
          8,
          Math.min(
            submenu
              ? a.top
              : a.bottom + height + 8 <= innerHeight
                ? a.bottom + 6
                : a.top - height - 6,
            innerHeight - height - 8,
          ),
        ),
      });
    };
    place();
    menu.current
      ?.querySelector<HTMLButtonElement>("button:not(:disabled)")
      ?.focus();
    addEventListener("resize", place);
    addEventListener("scroll", place, true);
    return () => {
      removeEventListener("resize", place);
      removeEventListener("scroll", place, true);
    };
  }, [anchor, submenu]);
  const item = child == null ? undefined : actions[child],
    childAnchor = child == null ? null : buttons.current[child];
  return (
    <>
      {createPortal(
        <div
          ref={menu}
          role="menu"
          id={submenu ? undefined : rootId}
          aria-label={label}
          data-menu-root={rootId}
          className={"action-menu-popup" + (submenu ? " action-submenu" : "")}
          style={position}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              close(true);
            }
            if (e.key === "ArrowLeft" && back) {
              e.preventDefault();
              e.stopPropagation();
              back();
            }
            if (e.key === "Tab") close();
            if (["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) {
              e.preventDefault();
              e.stopPropagation();
              const list = buttons.current.filter(
                (b): b is HTMLButtonElement => !!b && !b.disabled,
              );
              const index = list.indexOf(
                document.activeElement as HTMLButtonElement,
              );
              const next =
                e.key === "Home"
                  ? 0
                  : e.key === "End"
                    ? list.length - 1
                    : (index + (e.key === "ArrowDown" ? 1 : -1) + list.length) %
                      list.length;
              list[next]?.focus();
            }
          }}
        >
          {actions.map((action, index) => (
            <button
              key={action.label + index}
              ref={(node) => {
                buttons.current[index] = node;
              }}
              role="menuitem"
              type="button"
              className={
                "action-menu-item" +
                (/删除/.test(action.label) ? " danger" : "")
              }
              title={action.title}
              disabled={action.disabled}
              aria-haspopup={action.items?.length ? "menu" : undefined}
              aria-expanded={action.items?.length ? child === index : undefined}
              onMouseEnter={() => {
                if (hoverBlocked.current !== index)
                  setChild(
                    !action.disabled && action.items?.length ? index : null,
                  );
              }}
              onMouseLeave={() => {
                if (hoverBlocked.current === index) hoverBlocked.current = null;
              }}
              onFocus={() => {
                if (!action.items?.length) setChild(null);
              }}
              onKeyDown={(e) => {
                if (e.key === "ArrowRight" && action.items?.length) {
                  e.preventDefault();
                  e.stopPropagation();
                  hoverBlocked.current = null;
                  if (child === index) {
                    const layer = [
                      ...document.querySelectorAll<HTMLElement>(
                        "[data-menu-root]",
                      ),
                    ].find(
                      (n) =>
                        n.dataset.menuRoot === rootId &&
                        n.getAttribute("aria-label") ===
                          label + " · " + action.label,
                    );
                    layer
                      ?.querySelector<HTMLButtonElement>(
                        "button:not(:disabled)",
                      )
                      ?.focus();
                  } else setChild(index);
                }
              }}
              onClick={() => {
                if (action.items?.length) {
                  hoverBlocked.current = null;
                  setChild(index);
                } else {
                  close(true);
                  action.onClick?.();
                }
              }}
            >
              {action.icon || labelIcon(action.label, 15) || (
                <CircleDot size={15} aria-hidden="true" />
              )}
              <span>{action.label}</span>
              {action.items?.length && (
                <ChevronRight
                  size={14}
                  className="submenu-arrow"
                  aria-hidden="true"
                />
              )}
            </button>
          ))}
        </div>,
        anchor.closest("dialog") || document.body,
      )}
      {item?.items?.length && !item.disabled && childAnchor && (
        <MenuLayer
          actions={item.items}
          anchor={childAnchor}
          rootId={rootId}
          label={label + " · " + item.label}
          submenu
          close={close}
          back={() => {
            hoverBlocked.current = child;
            setChild(null);
            childAnchor.focus();
          }}
        />
      )}
    </>
  );
}
export function ActionMenu({
  label,
  primary,
  items,
  triggerLabel = "操作",
}: {
  label: string;
  primary?: MenuAction;
  items: MenuAction[];
  triggerLabel?: string;
}) {
  const actions = primary ? [primary, ...items] : items;
  const [open, setOpen] = useState(false),
    anchor = useRef<HTMLButtonElement>(null),
    id = useId();
  const close = (focus = false) => {
    setOpen(false);
    if (focus) anchor.current?.focus();
  };
  useLayoutEffect(() => {
    if (!open) return;
    const outside = (e: PointerEvent) => {
      if (
        !anchor.current?.contains(e.target as Node) &&
        !(e.target as Element).closest?.(`[data-menu-root="${id}"]`)
      )
        close();
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [open, id]);
  if (!actions.length) return null;
  return (
    <div className="row-action-menu">
      <button
        ref={anchor}
        type="button"
        className="button secondary more-actions-trigger"
        data-control-icon="true"
        title={triggerLabel}
        aria-label={
          label + (triggerLabel === "更多" ? "更多操作" : triggerLabel)
        }
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen(!open)}
        onKeyDown={(e) => {
          if (["ArrowDown", "ArrowUp"].includes(e.key)) {
            e.preventDefault();
            setOpen(true);
          }
        }}
      >
        <MoreHorizontal size={18} aria-hidden="true" />
      </button>
      {open && anchor.current && (
        <MenuLayer
          actions={actions}
          anchor={anchor.current}
          rootId={id}
          label={label + "操作"}
          close={close}
        />
      )}
    </div>
  );
}
