import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { ChevronDown } from "lucide-react";

export type IconOption = {
  value: string;
  label: string;
  icon: ReactNode;
  tone: string;
  disabled?: boolean;
};
export function IconSelect({
  label,
  value,
  options,
  onChange,
  triggerText,
  showIcon = true,
  triggerTitle,
}: {
  label: string;
  value: string;
  options: IconOption[];
  onChange: (value: string) => void;
  triggerText?: string;
  showIcon?: boolean;
  triggerTitle?: string;
}) {
  const [open, setOpen] = useState(false),
    [position, setPosition] = useState({
      top: 0,
      left: 0,
      width: 160,
      maxHeight: 280,
    });
  const trigger = useRef<HTMLButtonElement>(null),
    menu = useRef<HTMLDivElement>(null),
    id = useId();
  const selected = options.find((row) => row.value === value) || options[0];
  const close = (focus = false) => {
    setOpen(false);
    if (focus) trigger.current?.focus();
  };
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const r = trigger.current?.getBoundingClientRect(),
        m = menu.current;
      if (!r || !m) return;
      if (r.bottom < 0 || r.top > innerHeight) {
        close();
        return;
      }
      const context = document.createElement("canvas").getContext("2d"),
        style = getComputedStyle(m);
      if (context)
        context.font = `${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
      const width = Math.min(
          Math.max(
            160,
            ...options.map(
              (row) =>
                (context?.measureText(row.label).width ||
                  row.label.length * 13) + (showIcon ? 72 : 40),
            ),
          ),
          document.documentElement.clientWidth - 24,
        ),
        height = Math.min(m.scrollHeight, 280, innerHeight - 24);
      const top =
        r.bottom + 6 + height <= innerHeight - 12
          ? r.bottom + 6
          : Math.max(12, r.top - height - 6);
      setPosition({
        top,
        left: Math.max(
          12,
          Math.min(r.left, document.documentElement.clientWidth - width - 12),
        ),
        width,
        maxHeight: Math.min(280, innerHeight - top - 12),
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
    const buttons = [
      ...menu.current!.querySelectorAll<HTMLButtonElement>(
        "button:not(:disabled)",
      ),
    ];
    (
      buttons.find((b) => b.getAttribute("aria-selected") === "true") ||
      buttons[0]
    )?.focus();
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
    <div className="icon-select">
      <button
        title={triggerTitle || selected?.label}
        ref={trigger}
        type="button"
        role="combobox"
        aria-label={label}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={open ? id : undefined}
        className="icon-select-trigger"
        onClick={() => setOpen(!open)}
        onKeyDown={(e) => {
          if (["ArrowDown", "ArrowUp", "Enter", " "].includes(e.key)) {
            e.preventDefault();
            setOpen(true);
          }
          if (e.key === "Escape") {
            e.preventDefault();
            close();
          }
        }}
      >
        {showIcon && (
          <span className={"icon-select-symbol metric-" + selected?.tone}>
            {selected?.icon}
          </span>
        )}
        <span>{triggerText || selected?.label}</span>
        <ChevronDown size={16} />
      </button>
      {open &&
        createPortal(
          <div
            ref={menu}
            id={id}
            role="listbox"
            aria-label={label}
            className={
              "icon-select-menu" +
              (label === "主页切换" ? " overview-view-menu" : "")
            }
            style={position}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                close(true);
                return;
              }
              if (e.key === "Tab") {
                close();
                return;
              }
              if (["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) {
                e.preventDefault();
                const buttons = [
                    ...menu.current!.querySelectorAll<HTMLButtonElement>(
                      "button:not(:disabled)",
                    ),
                  ],
                  i = buttons.indexOf(
                    document.activeElement as HTMLButtonElement,
                  );
                const next =
                  e.key === "Home"
                    ? 0
                    : e.key === "End"
                      ? buttons.length - 1
                      : (i +
                          (e.key === "ArrowDown" ? 1 : -1) +
                          buttons.length) %
                        buttons.length;
                buttons[next]?.focus();
              }
            }}
          >
            {options.map((row) => (
              <button
                key={row.value}
                type="button"
                role="option"
                aria-selected={row.value === value}
                disabled={row.disabled}
                tabIndex={-1}
                onClick={() => {
                  onChange(row.value);
                  close(true);
                }}
              >
                {showIcon && (
                  <span className={"icon-select-symbol metric-" + row.tone}>
                    {row.icon}
                  </span>
                )}
                <span>{row.label}</span>
              </button>
            ))}
          </div>,
          trigger.current?.closest("dialog[open]") || document.body,
        )}
    </div>
  );
}
