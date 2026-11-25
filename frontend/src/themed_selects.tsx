import { useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Search } from "lucide-react";
import { MenuScrollArea } from "./menu_scroll_area";

type SelectTarget = HTMLSelectElement | HTMLInputElement;
type Choice = { index: number; value: string; label: string; group: string; disabled: boolean };
const isSelect = (element: Element): element is HTMLSelectElement => element instanceof HTMLSelectElement;
const isDropdown = (element: Element | null): element is SelectTarget =>
  !!element && ((isSelect(element) && !element.multiple && element.size <= 1) ||
    (element instanceof HTMLInputElement && !!element.dataset.themedDatalist));

function choices(target: SelectTarget): Choice[] {
  const source = isSelect(target) ? target : document.getElementById(target.dataset.themedDatalist || "");
  if (!(source instanceof HTMLSelectElement || source instanceof HTMLDataListElement)) return [];
  return [...source.options].filter(option => !option.hidden && !(option.parentElement instanceof HTMLOptGroupElement && option.parentElement.hidden)).map((option, index) => ({
    index: isSelect(target) ? option.index : index,
    value: option.value,
    label: option.label || option.textContent || option.value,
    group: option.parentElement instanceof HTMLOptGroupElement ? option.parentElement.label : "",
    disabled: option.disabled || (option.parentElement instanceof HTMLOptGroupElement && option.parentElement.disabled),
  }));
}

// Keep the native form control, refs and React change handlers intact. Only the
// browser-owned popup is replaced, including controls mounted later in dialogs.
export function ThemedSelects() {
  const [target, setTarget] = useState<SelectTarget | null>(null);
  const [query, setQuery] = useState("");
  const [typedSearch, setTypedSearch] = useState(false);
  const [, refresh] = useState(0);
  const [position, setPosition] = useState({ top: 0, left: 0, width: 200, maxHeight: 320 });
  const active = useRef<SelectTarget | null>(null), menu = useRef<HTMLDivElement>(null);
  const selecting = useRef(false), arrowFocus = useRef(false);
  const id = useId();
  const close = (focus = false) => {
    const element = active.current;
    active.current = null;
    setTarget(null);
    if (focus && element?.isConnected) element.focus({ preventScroll: true });
  };
  const open = (element: SelectTarget, initialQuery = "", arrow = false) => {
    if (element.disabled || (element instanceof HTMLInputElement && element.readOnly)) return;
    arrowFocus.current = arrow;
    active.current = element;
    setQuery(initialQuery);
    setTypedSearch(!!initialQuery && isSelect(element));
    setTarget(element);
  };

  useLayoutEffect(() => {
    const converted = new Map<HTMLInputElement, { list: string; attrs: Record<string, string | null> }>();
    let pending = 0;
    const scan = () => {
      pending = 0;
      document.querySelectorAll<HTMLInputElement>("input[list]").forEach(element => {
        const list = element.getAttribute("list");
        if (!list) return;
        const attrs = converted.get(element)?.attrs || Object.fromEntries(["role", "aria-autocomplete", "aria-haspopup", "aria-expanded"].map(name => [name, element.getAttribute(name)]));
        converted.set(element, { list, attrs });
        element.dataset.themedDatalist = list;
        element.setAttribute("role", attrs.role || "combobox");
        element.setAttribute("aria-autocomplete", "list");
        element.setAttribute("aria-haspopup", "dialog");
        element.setAttribute("aria-expanded", "false");
        element.removeAttribute("list");
      });
      if (active.current && (!active.current.isConnected || active.current.disabled)) close();
    };
    const observer = new MutationObserver(() => {
      if (!pending) pending = requestAnimationFrame(scan);
    });
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["list"] });
    scan();
    let pointerHandled = false;
    const pointer = (event: PointerEvent) => {
      if (event.button !== 0) return;
      const element = event.target instanceof Element ? event.target.closest("select,input") : null;
      if (!isDropdown(element) || element.disabled) return;
      if (isSelect(element)) {
        event.preventDefault();
        pointerHandled = true;
        element.focus({ preventScroll: true });
        if (active.current === element) close(); else open(element);
      } else open(element, element.value);
    };
    const mouse = (event: MouseEvent) => {
      const element = event.target instanceof Element ? event.target.closest("select") : null;
      if (isDropdown(element) && !element.disabled) event.preventDefault();
    };
    const click = (event: MouseEvent) => {
      const element = event.target instanceof Element ? event.target.closest("select") : null;
      if (isDropdown(element) && !element.disabled) {
        event.preventDefault();
        if (!pointerHandled) open(element);
      }
      pointerHandled = false;
    };
    const outside = (event: PointerEvent) => {
      if (event.target !== active.current && !menu.current?.contains(event.target as Node)) close();
    };
    const input = (event: Event) => {
      if (!selecting.current && event.target instanceof HTMLInputElement && isDropdown(event.target)) open(event.target, event.target.value);
    };
    const key = (event: KeyboardEvent) => {
      const element = event.target instanceof Element ? event.target : null;
      if (!isDropdown(element) || element.disabled) return;
      if (event.key === "Escape" && active.current === element) {
        event.preventDefault(); event.stopPropagation(); close(); return;
      }
      if (event.key === "Tab") { close(); return; }
      if (["ArrowDown", "ArrowUp", "F4"].includes(event.key) || (isSelect(element) && [" ", "Enter"].includes(event.key))) {
        event.preventDefault(); event.stopPropagation();
        if (active.current === element && menu.current && ["ArrowDown", "ArrowUp"].includes(event.key)) {
          const buttons = [...menu.current.querySelectorAll<HTMLButtonElement>('[role="option"]:not(:disabled)')];
          (event.key === "ArrowDown" ? buttons[0] : buttons[buttons.length - 1])?.focus();
          return;
        }
        open(element, isSelect(element) ? "" : element.value, true);
      } else if (isSelect(element) && event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
        event.preventDefault(); open(element, event.key);
      }
    };
    document.addEventListener("pointerdown", outside, true);
    document.addEventListener("pointerdown", pointer, true);
    document.addEventListener("mousedown", mouse, true);
    document.addEventListener("click", click, true);
    document.addEventListener("keydown", key, true);
    document.addEventListener("input", input, true);
    return () => {
      observer.disconnect(); cancelAnimationFrame(pending);
      document.removeEventListener("pointerdown", outside, true);
      document.removeEventListener("pointerdown", pointer, true);
      document.removeEventListener("mousedown", mouse, true);
      document.removeEventListener("click", click, true);
      document.removeEventListener("keydown", key, true);
      document.removeEventListener("input", input, true);
      converted.forEach(({ list, attrs }, element) => {
        element.setAttribute("list", list); delete element.dataset.themedDatalist;
        Object.entries(attrs).forEach(([name, value]) => value == null ? element.removeAttribute(name) : element.setAttribute(name, value));
      });
    };
  }, []);

  const options = target ? choices(target) : [];
  const search = !!target && isSelect(target) && (options.length > 8 || typedSearch);
  const rows = options.filter(option => (option.label + " " + option.value + " " + option.group).toLowerCase().includes(query.toLowerCase()));
  const label = target?.getAttribute("aria-label") || target?.labels?.[0]?.querySelector(".field-title")?.textContent?.trim() || target?.title || "选择选项";
  const choose = (option: Choice) => {
    if (!target || !target.isConnected || target.disabled || option.disabled) return;
    if ((isSelect(target) && target.selectedIndex === option.index) || (!isSelect(target) && target.value === option.value)) { close(true); return; }
    selecting.current = true;
    if (isSelect(target)) target.selectedIndex = option.index;
    else {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(target, option.value);
      target.dispatchEvent(new Event("input", { bubbles: true }));
    }
    target.dispatchEvent(new Event("change", { bubbles: true }));
    selecting.current = false;
    close(true);
  };

  useLayoutEffect(() => {
    if (!target) return;
    const oldControls = target.getAttribute("aria-controls"), oldExpanded = target.getAttribute("aria-expanded");
    target.setAttribute("aria-controls", id); target.setAttribute("aria-expanded", "true");
    const place = () => {
      if (!target.isConnected || target.disabled) { close(); return; }
      const r = target.getBoundingClientRect(), m = menu.current;
      if (!m) return;
      if (r.bottom < 0 || r.top > innerHeight) { close(); return; }
      const width = Math.min(Math.max(r.width, search ? 240 : 160), document.documentElement.clientWidth - 24);
      const height = Math.min(m.scrollHeight, 320, innerHeight - 24);
      const top = r.bottom + height + 6 <= innerHeight - 12 ? r.bottom + 6 : Math.max(12, r.top - height - 6);
      const left = Math.max(12, Math.min(r.left, document.documentElement.clientWidth - width - 12));
      const box = m.getBoundingClientRect();
      const css = getComputedStyle(m);
      const measuredX = box.width / (parseFloat(css.width) || box.width || 1);
      const measuredY = box.height / (parseFloat(css.height) || box.height || 1);
      const scaleX = Math.abs(measuredX - 1) < 0.01 ? 1 : measuredX;
      const scaleY = Math.abs(measuredY - 1) < 0.01 ? 1 : measuredY;
      const originX = box.left - (parseFloat(m.style.left) || 0) * scaleX;
      const originY = box.top - (parseFloat(m.style.top) || 0) * scaleY;
      setPosition({ top: (top - originY) / scaleY, left: (left - originX) / scaleX, width: width / scaleX, maxHeight: Math.min(320, innerHeight - top - 12) / scaleY });
    };
    place();
    if (isSelect(target)) {
      const selected = menu.current?.querySelector<HTMLButtonElement>('[aria-selected="true"]:not(:disabled)');
      const first = menu.current?.querySelector<HTMLButtonElement>('[role="option"]:not(:disabled)');
      if (search && !arrowFocus.current) menu.current?.querySelector<HTMLInputElement>("input")?.focus();
      else (selected || first)?.focus();
      selected?.scrollIntoView({ block: "nearest" });
    } else if (arrowFocus.current) menu.current?.querySelector<HTMLButtonElement>('[role="option"]:not(:disabled)')?.focus();
    const observer = new ResizeObserver(place);
    if (menu.current) observer.observe(menu.current);
    observer.observe(target);
    const mutation = new MutationObserver(() => { refresh(value => value + 1); place(); });
    mutation.observe(target, { attributes: true, childList: true, characterData: true, subtree: true });
    const dataList = !isSelect(target) && document.getElementById(target.dataset.themedDatalist || "");
    if (dataList) mutation.observe(dataList, { childList: true, subtree: true, attributes: true });
    addEventListener("resize", place); addEventListener("scroll", place, true);
    return () => {
      observer.disconnect(); mutation.disconnect();
      removeEventListener("resize", place); removeEventListener("scroll", place, true);
      if (oldControls == null) target.removeAttribute("aria-controls"); else target.setAttribute("aria-controls", oldControls);
      if (oldExpanded == null) target.removeAttribute("aria-expanded"); else target.setAttribute("aria-expanded", oldExpanded);
    };
  }, [target, search]);

  if (!target) return null;
  return createPortal(<div ref={menu} id={id} role="dialog" aria-label={label + "选项"}
    className="icon-select-menu app-select-menu" style={position}
    onKeyDown={event => {
      if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); close(true); return; }
      if (event.key === "Tab") { close(true); return; }
      const input = event.target instanceof HTMLInputElement;
      const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]:not(:disabled)')];
      if (input && event.key === "Enter") { event.preventDefault(); const first = rows.find(option => !option.disabled); if (first) choose(first); return; }
      if (["ArrowDown", "ArrowUp"].includes(event.key) || (!input && ["Home", "End"].includes(event.key))) {
        event.preventDefault(); event.stopPropagation();
        const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : current < 0 ? (event.key === "ArrowDown" ? 0 : buttons.length - 1) : (current + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
        buttons[next]?.focus();
      }
    }}>
    {search && <label className="multi-select-search"><Search size={16}/><input type="search" aria-label={"搜索" + label}
      placeholder="搜索选项" value={query} onChange={event => setQuery(event.target.value)}/></label>}
    <MenuScrollArea className="app-select-options" role="listbox" aria-label={label}>
      {rows.map(option => <button type="button" key={option.index} role="option" disabled={option.disabled} tabIndex={-1}
        aria-selected={isSelect(target) ? target.selectedIndex === option.index : target.value === option.value}
        title={option.label} onClick={() => choose(option)}>
        {option.group && <small>{option.group} · </small>}{option.label || "（空）"}
      </button>)}
    </MenuScrollArea>
    {!rows.length && <p className="small muted" role="status">{options.length ? "没有匹配的选项" : "暂无可选项"}</p>}
  </div>, target.closest(".list-filter-popup,.detection-options-popup,.multi-select-popup,.service-theme-menu,.metric-picker-menu,.action-menu-popup,details[open]:not(.firewall-diagnosis-advanced),dialog[open]") || document.body);
}
