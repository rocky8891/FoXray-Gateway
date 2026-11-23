import { RandomThemeIcon } from "./requirement_icons";
import { MenuScrollArea } from "./menu_scroll_area";
import { createPortal } from "react-dom";
import { overviewPolicy } from "./overview_policy";
import {
  useId,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { ChevronDown, ChevronRight, Check, Search } from "lucide-react";
import {
  SERVICE_THEMES,
  currentServiceTheme,
  customServiceTheme,
  listThemes,
  randomServiceTheme,
} from "./service_themes";
import type { Dict } from "./api";

function ThemeBranch({
  id,
  label,
  level,
  open,
  toggle,
  children,
}: {
  id: string;
  label: string;
  level: number;
  open: boolean;
  toggle: () => void;
  children: ReactNode;
}) {
  return (
    <div
      role="treeitem"
      tabIndex={-1}
      aria-level={level}
      aria-expanded={open}
      aria-labelledby={id}
      className="theme-branch"
      data-branch="true"
      onClick={(event) => {
        if (
          (event.target as Element).closest('[role="treeitem"]') ===
          event.currentTarget
        ) {
          event.preventDefault();
          event.stopPropagation();
          toggle();
        }
      }}
    >
      <div className="theme-branch-title" id={id}>
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span>{label}</span>
      </div>
      {open && <div role="group">{children}</div>}
    </div>
  );
}
export function ServiceThemePicker({
  presentation,
  onChange,
}: {
  presentation: Dict;
  onChange: (values: Dict) => void;
}) {
  const custom = listThemes(presentation),
    selectedCustom = customServiceTheme(presentation);
  const selected = selectedCustom?.id || currentServiceTheme(presentation);
  const label =
    selectedCustom?.name ||
    SERVICE_THEMES.find((theme) => theme.id === selected)?.label ||
    "自定义";
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [expanded, setExpanded] = useState<Record<string, boolean>>({}),
    [lastRandom, setLastRandom] = useState(""),
    [position, setPosition] = useState({
      top: 0,
      left: 0,
      width: 480,
      maxHeight: 640,
    });
  const trigger = useRef<HTMLButtonElement>(null),
    menu = useRef<HTMLDivElement>(null),
    tree = useRef<HTMLDivElement>(null),
    id = useId();
  const close = (focus = false) => {
    setOpen(false);
    setQuery("");
    if (focus) trigger.current?.focus();
  };
  const matches = (text: string) =>
    text.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());
  const branchOpen = (key: string) => !!query || expanded[key] === true;
  const toggle = (key: string) => {
    if (!query)
      setExpanded((previous) => ({
        ...previous,
        [key]: !previous[key],
      }));
  };
  const openMenu = () => {
    const theme = SERVICE_THEMES.find((theme) => theme.id === selected);
    setExpanded(
      selectedCustom
        ? { custom: true }
        : theme
          ? { [theme.group]: true, [theme.group + theme.shape]: true }
          : {},
    );
    setQuery("");
    setOpen(true);
  };
  const focusNode = (node?: HTMLElement | null) => {
    if (!node) return;
    node.focus({ preventScroll: true });
    const row =
      node.querySelector<HTMLElement>(":scope>.theme-branch-title") || node;
    const bounds = tree.current?.getBoundingClientRect(),
      r = row.getBoundingClientRect();
    if (bounds && tree.current) {
      if (r.top < bounds.top) tree.current.scrollTop -= bounds.top - r.top;
      else if (r.bottom > bounds.bottom)
        tree.current.scrollTop += r.bottom - bounds.bottom;
    }
  };
  useEffect(() => {
    if (tree.current) tree.current.scrollTop = 0;
  }, [query]);
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const t = trigger.current?.getBoundingClientRect(),
        panel = menu.current;
      if (!t || !panel) return;
      if (t.bottom < 8 || t.top > innerHeight - 8) {
        close();
        return;
      }
      const width = Math.min(
        Math.max(t.width, 480),
        760,
        document.documentElement.clientWidth - 24,
      );
      const below = Math.max(0, innerHeight - t.bottom - 18),
        above = Math.max(0, t.top - 18);
      const down = below >= Math.min(600, innerHeight * 0.55) || below >= above;
      const maxHeight = Math.min(640, down ? below : above);
      panel.style.width = width + "px";
      panel.style.maxHeight = maxHeight + "px";
      setPosition({
        width,
        maxHeight,
        left: Math.max(
          12,
          Math.min(t.left, document.documentElement.clientWidth - width - 12),
        ),
        top: down ? t.bottom + 6 : Math.max(12, t.top - panel.offsetHeight - 6),
      });
    };
    const outside = (event: PointerEvent) => {
      if (
        !trigger.current?.contains(event.target as Node) &&
        !menu.current?.contains(event.target as Node)
      )
        close();
    };
    const wheel = (event: WheelEvent) => {
      if (event.ctrlKey || !event.deltaY || !tree.current) return;
      event.preventDefault();
      tree.current.scrollTop +=
        event.deltaY *
        (event.deltaMode === 1
          ? 16
          : event.deltaMode === 2
            ? tree.current.clientHeight
            : 1);
    };
    const panel = menu.current;
    panel?.addEventListener("wheel", wheel, { passive: false });
    place();
    focusNode(
      menu.current?.querySelector<HTMLElement>('[aria-selected="true"]') ||
        menu.current?.querySelector<HTMLElement>('[role="treeitem"]'),
    );
    const observer = new ResizeObserver(place);
    if (trigger.current) observer.observe(trigger.current);
    if (menu.current) observer.observe(menu.current);
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    document.addEventListener("pointerdown", outside, true);
    return () => {
      observer.disconnect();
      panel?.removeEventListener("wheel", wheel);
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
      document.removeEventListener("pointerdown", outside, true);
    };
  }, [open]);
  const leaf = (
    theme: { id: string; label: string; values: Dict },
    text: string,
    level: number,
  ) => (
    <button
      key={theme.id}
      type="button"
      role="treeitem"
      aria-level={level}
      tabIndex={-1}
      aria-label={theme.label}
      aria-selected={selected === theme.id}
      data-theme-id={theme.id}
      disabled={
        theme.id.includes("radial-vertical") &&
        overviewPolicy(presentation, "services").cards[0] ===
          "service-wireguard"
      }
      title={
        theme.id.includes("radial-vertical")
          ? "竖排用于左右布局左侧的服务器、Xray或WARP卡片"
          : undefined
      }
      onClick={(event) => {
        event.stopPropagation();
        onChange(theme.values);
        close(true);
      }}
    >
      <span className="theme-choice-check">
        {selected === theme.id && <Check size={14} />}
      </span>
      {text}
    </button>
  );
  const groups = [...new Set(SERVICE_THEMES.map((theme) => theme.group))];
  const count =
    SERVICE_THEMES.filter((theme) => matches(theme.label + " " + theme.id))
      .length + custom.filter((theme) => matches(theme.name)).length;
  return (
    <div className="service-theme-picker">
      <button
        type="button"
        role="combobox"
        aria-label="服务总览主题"
        aria-haspopup="tree"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        className="service-theme-trigger"
        ref={trigger}
        onClick={() => {
          open ? close() : openMenu();
        }}
        onKeyDown={(event) => {
          if (["ArrowDown", "ArrowUp"].includes(event.key)) {
            event.preventDefault();
            openMenu();
          }
          if (event.key === "Escape") close(true);
        }}
      >
        <span>{label}</span>
        <ChevronDown size={16} />
      </button>
      <button
        type="button"
        className="button service-theme-random"
        aria-label="随机主题"
        title={
          "随机选择内置主题、自定义主题或生成四卡片混搭；保存后应用" +
          (lastRandom ? "。上次：" + lastRandom : "")
        }
        onClick={(event) => {
          event.preventDefault();
          const theme = randomServiceTheme(presentation);
          setLastRandom(theme.label);
          onChange(theme.values);
          close();
        }}
      >
        <RandomThemeIcon size={16} />
        随机
      </button>
      {open &&
        createPortal(
          <div
            className="service-theme-menu"
            style={position}
            ref={menu}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                event.stopPropagation();
                close(true);
                return;
              }
              if (event.key === "Tab") {
                close();
                return;
              }
              const nodes = [
                  ...menu.current!.querySelectorAll<HTMLElement>(
                    '[role="treeitem"]:not(:disabled)',
                  ),
                ],
                target = document.activeElement as HTMLElement,
                index = nodes.indexOf(target);
              if (target.tagName === "INPUT") {
                if (event.key === "ArrowDown") {
                  event.preventDefault();
                  focusNode(nodes[0]);
                }
                return;
              }
              if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
                event.preventDefault();
                const next =
                  event.key === "Home"
                    ? 0
                    : event.key === "End"
                      ? nodes.length - 1
                      : (index +
                          (event.key === "ArrowDown" ? 1 : -1) +
                          nodes.length) %
                        nodes.length;
                focusNode(nodes[next]);
                return;
              }
              if (event.key === "ArrowRight") {
                event.preventDefault();
                if (target.getAttribute("aria-expanded") === "false")
                  target.click();
                else
                  focusNode(
                    target.querySelector<HTMLElement>('[role="treeitem"]'),
                  );
              } else if (event.key === "ArrowLeft") {
                event.preventDefault();
                if (target.getAttribute("aria-expanded") === "true")
                  target.click();
                else
                  focusNode(
                    target.parentElement?.closest<HTMLElement>(
                      '[role="treeitem"]',
                    ),
                  );
              } else if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                target.click();
              }
            }}
          >
            <label className="theme-search">
              <Search size={15} />
              <input
                aria-label="查询主题"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="查询布局、圆形样式或主题名称"
              />
            </label>
            <MenuScrollArea role="tree" aria-label="主题分级列表" id={id} ref={tree}>
              {!!custom.length && (
                <ThemeBranch
                  id={id + "-custom"}
                  label="自定义主题"
                  level={1}
                  open={branchOpen("custom")}
                  toggle={() => toggle("custom")}
                >
                  {custom
                    .filter((theme) => matches(theme.name))
                    .map((theme) =>
                      leaf({ ...theme, label: theme.name }, theme.name, 2),
                    )}
                </ThemeBranch>
              )}
              {groups.map((group, groupIndex) => {
                const themes = SERVICE_THEMES.filter(
                  (theme) =>
                    theme.group === group &&
                    matches(theme.label + " " + theme.id),
                );
                if (!themes.length) return null;
                return (
                  <ThemeBranch
                    key={group}
                    id={id + "-g" + groupIndex}
                    label={group}
                    level={1}
                    open={branchOpen(group)}
                    toggle={() => toggle(group)}
                  >
                    {group === "默认"
                      ? themes.map((theme) => leaf(theme, theme.leaf, 2))
                      : [...new Set(themes.map((theme) => theme.shape))].map(
                          (shape, shapeIndex) => {
                            const key = group + shape;
                            return (
                              <ThemeBranch
                                key={shape}
                                id={id + "-g" + groupIndex + "-s" + shapeIndex}
                                label={shape}
                                level={2}
                                open={branchOpen(key)}
                                toggle={() => toggle(key)}
                              >
                                {themes
                                  .filter((theme) => theme.shape === shape)
                                  .map((theme) => leaf(theme, theme.leaf, 3))}
                              </ThemeBranch>
                            );
                          },
                        )}
                  </ThemeBranch>
                );
              })}
              {!count && (
                <div className="small muted theme-no-results">
                  没有匹配的主题
                </div>
              )}
            </MenuScrollArea>
          </div>,
          trigger.current?.closest("dialog[open]") || document.body,
        )}
    </div>
  );
}
