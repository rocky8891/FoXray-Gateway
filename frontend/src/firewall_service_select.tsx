import { useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ChevronDown, Search } from "lucide-react";
import { MenuScrollArea } from "./menu_scroll_area";

export function FirewallServiceSelect({ value, services, onChange, label = "预定义服务" }: {
  value: string;
  services: string[];
  onChange: (value: string) => void;
  label?: string;
}) {
  const [open, setOpen] = useState(false), [query, setQuery] = useState("");
  const [position, setPosition] = useState({ top: 0, left: 0, width: 260, maxHeight: 320 });
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const id = useId();
  const options = useMemo(() => [...new Set(services.map(name => name.trim()).filter(Boolean))]
    .sort((a, b) => a.localeCompare(b)), [services]);
  const rows = options.filter(name => name.toLowerCase().includes(query.trim().toLowerCase()));
  const close = (focus = false) => {
    setOpen(false);
    if (focus) trigger.current?.focus();
  };
  const show = () => { setQuery(""); setOpen(true); };
  const choose = (name: string) => { onChange(name); close(true); };

  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const r = trigger.current?.getBoundingClientRect(), m = menu.current;
      if (!r || !m) return;
      if (r.bottom < 0 || r.top > innerHeight) { close(); return; }
      const width = Math.min(Math.max(r.width, 260), document.documentElement.clientWidth - 24);
      const height = Math.min(m.scrollHeight, 320, innerHeight - 24);
      const top = r.bottom + height + 6 <= innerHeight - 12 ? r.bottom + 6 : Math.max(12, r.top - height - 6);
      setPosition({ top, left: Math.max(12, Math.min(r.left, document.documentElement.clientWidth - width - 12)), width, maxHeight: Math.min(320, innerHeight - top - 12) });
    };
    const outside = (event: PointerEvent) => {
      if (!trigger.current?.contains(event.target as Node) && !menu.current?.contains(event.target as Node)) close();
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

  return <div className="firewall-service-select">
    <button ref={trigger} type="button" role="combobox" aria-label={label}
      aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? id : undefined}
      className="icon-select-trigger" title={value || "选择预定义服务"}
      onClick={() => open ? close() : show()}
      onKeyDown={event => {
        if (["ArrowDown", "ArrowUp"].includes(event.key)) { event.preventDefault(); show(); }
      }}>
      <span>{value || "选择预定义服务"}</span><ChevronDown size={16}/>
    </button>
    {!options.length && <p className="small muted" role="status">预定义服务清单为空或尚未读取，请刷新防火墙页面后重试。</p>}
    {value && options.length > 0 && !options.includes(value) && <p className="small muted" role="status">当前服务不在已读取清单中，请刷新后重新选择。</p>}
    {open && createPortal(<div ref={menu} id={id} role="dialog" aria-label={"选择" + label}
      className="icon-select-menu firewall-service-menu" style={position}
      onKeyDown={event => {
        if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); close(true); return; }
        if (event.key === "Tab") { close(); return; }
        const input = event.target instanceof HTMLInputElement;
        const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]')];
        if (input && event.key === "Enter") { event.preventDefault(); if (rows[0]) choose(rows[0]); return; }
        if (["ArrowDown", "ArrowUp"].includes(event.key) || (!input && ["Home", "End"].includes(event.key))) {
          event.preventDefault();
          const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
          const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1
            : current < 0 ? (event.key === "ArrowDown" ? 0 : buttons.length - 1)
              : (current + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
          buttons[next]?.focus();
        }
      }}>
      <label className="multi-select-search"><Search size={16}/><input type="search" aria-label={"搜索" + label}
        placeholder="搜索服务，如 ssh、http、dns" value={query} onChange={event => setQuery(event.target.value)}/></label>
      <MenuScrollArea className="firewall-service-options" role="listbox" aria-label={label + "列表"}>
        {rows.map(name => <button type="button" role="option" aria-selected={name === value} tabIndex={-1}
          key={name} onClick={() => choose(name)}>{name}</button>)}
      </MenuScrollArea>
      {!rows.length && <p className="small muted" role="status">{options.length ? "没有匹配的预定义服务" : "没有读取到预定义服务，请刷新后重试"}</p>}
    </div>, trigger.current?.closest("dialog[open]") || document.body)}
  </div>;
}
