import { useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ChevronDown, Search } from "lucide-react";
import { MenuScrollArea } from "./menu_scroll_area";

export function MultiSelect({
  label,
  options,
  value,
  onChange,
  limit = 50,
  itemName = "接口",
}: {
  label: string;
  options: string[];
  value: string[];
  onChange: (value: string[]) => void;
  limit?: number;
  itemName?: string;
}) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [position, setPosition] = useState({ top: 0, left: 0 });
  const trigger = useRef<HTMLButtonElement>(null),
    menu = useRef<HTMLDivElement>(null),
    id = useId();
  const rows = options.filter((v) =>
      v.toLowerCase().includes(query.toLowerCase()),
    ),
    all = rows.length > 0 && rows.every((v) => value.includes(v));
  const close = (focus = false) => {
    setOpen(false);
    if (focus) trigger.current?.focus();
  };
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const a = trigger.current?.getBoundingClientRect(),
        m = menu.current;
      if (!a || !m) return;
      setPosition({
        top: Math.max(
          8,
          Math.min(a.bottom + 6, innerHeight - m.offsetHeight - 8),
        ),
        left: Math.max(8, Math.min(a.left, innerWidth - m.offsetWidth - 8)),
      });
    };
    const outside = (e: PointerEvent) => {
      if (
        !trigger.current?.contains(e.target as Node) &&
        !menu.current?.contains(e.target as Node)
      )
        close();
    };
    place();
    menu.current?.querySelector<HTMLInputElement>("input")?.focus();
    const observer = new ResizeObserver(place);
    if (menu.current) observer.observe(menu.current);
    addEventListener("resize", place);
    addEventListener("scroll", place, true);
    document.addEventListener("pointerdown", outside);
    return () => {
      observer.disconnect();
      removeEventListener("resize", place);
      removeEventListener("scroll", place, true);
      document.removeEventListener("pointerdown", outside);
    };
  }, [open]);
  return (
    <div className="multi-select">
      <button
        type="button"
        ref={trigger}
        className="multi-select-trigger"
        role="combobox"
        aria-label={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen(!open)}
      >
        <span>{value.length ? `已选 ${value.length} 个${itemName}` : `选择${itemName}`}</span>
        <ChevronDown size={18} />
      </button>
      {open &&
        createPortal(
          <div
            id={id}
            ref={menu}
            className="multi-select-popup"
            role="dialog"
            aria-label={label}
            style={position}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                close(true);
              }
            }}
          >
            <strong>
              选择{itemName} · {value.length}/{limit}
            </strong>
            <label className="multi-select-search">
              <Search size={16} />
              <input
                aria-label={`查询可选${itemName}`}
                placeholder={`查询${itemName}`}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
            <label className="check">
              <input
                type="checkbox"
                aria-label={`全选筛选${itemName}`}
                checked={all}
                disabled={!rows.length || !all && new Set([...value, ...rows]).size > limit}
                title={
                  new Set([...value, ...rows]).size > limit
                    ? `最多选择${limit}个${itemName}，请缩小筛选范围或逐项选择`
                    : undefined
                }
                ref={(el) => {
                  if (el)
                    el.indeterminate =
                      !all && rows.some((v) => value.includes(v));
                }}
                onChange={(e) =>
                  onChange(
                    e.target.checked
                      ? [...new Set([...value, ...rows])].slice(0, limit)
                      : value.filter((v) => !rows.includes(v)),
                  )
                }
              />
              全选当前筛选
            </label>
            <MenuScrollArea className="multi-select-options">
              {rows.map((name) => (
                <label className="check" key={name}>
                  <input
                    type="checkbox"
                    aria-label={`选择${itemName} ${name}`}
                    checked={value.includes(name)}
                    disabled={!value.includes(name) && value.length >= limit}
                    onChange={(e) =>
                      onChange(
                        e.target.checked
                          ? [...value, name]
                          : value.filter((v) => v !== name),
                      )
                    }
                  />
                  <span>{name}</span>
                </label>
              ))}
              {!rows.length && (
                <span className="small muted">{options.length ? `没有匹配${itemName}` : `没有可选${itemName}`}</span>
              )}
            </MenuScrollArea>
          </div>,
          trigger.current?.closest("dialog[open]") || document.body,
        )}
    </div>
  );
}
