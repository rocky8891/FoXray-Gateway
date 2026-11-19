import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Pipette } from "lucide-react";

const rgb = (hex: string) =>
  [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) || 0);
const hex = (values: number[]) =>
  "#" +
  values
    .map((v) =>
      Math.round(Math.max(0, Math.min(255, v)))
        .toString(16)
        .padStart(2, "0"),
    )
    .join("");
function hsv(value: string) {
  const [r, g, b] = rgb(value).map((v) => v / 255),
    max = Math.max(r, g, b),
    min = Math.min(r, g, b),
    d = max - min;
  const h = !d
    ? 0
    : max === r
      ? ((g - b) / d + 6) % 6
      : max === g
        ? (b - r) / d + 2
        : (r - g) / d + 4;
  return { h: h * 60, s: max ? d / max : 0, v: max };
}
function fromHSV(h: number, s: number, v: number) {
  const c = v * s,
    x = c * (1 - Math.abs(((h / 60) % 2) - 1)),
    m = v - c;
  const base =
    h < 60
      ? [c, x, 0]
      : h < 120
        ? [x, c, 0]
        : h < 180
          ? [0, c, x]
          : h < 240
            ? [0, x, c]
            : h < 300
              ? [x, 0, c]
              : [c, 0, x];
  return hex(base.map((n) => (n + m) * 255));
}

export function ColorInput({
  value,
  onChange,
  disabled = false,
  "aria-label": label,
}: {
  value: string;
  onChange: (color: string) => void;
  disabled?: boolean;
  "aria-label": string;
}) {
  const trigger = useRef<HTMLButtonElement>(null),
    panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false),
    [mode, setMode] = useState("RGB");
  const [channels, setChannels] = useState(rgb(value).map(String)),
    [hexText, setHexText] = useState(value);
  const [position, setPosition] = useState({ top: 0, left: 0, width: 0 });
  const [color, setColor] = useState(() => hsv(value));
  const hue = useRef(color.h);
  useEffect(() => {
    const next = hsv(value);
    if (next.s && next.v) hue.current = next.h;
    setColor({ ...next, h: hue.current });
    setChannels(rgb(value).map(String));
    setHexText(value);
  }, [value]);
  const close = (focus = false) => {
    setOpen(false);
    if (focus) trigger.current?.focus();
  };
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const t = trigger.current?.getBoundingClientRect(),
        p = panel.current;
      if (!t || !p) return;
      const w = Math.min(t.width, document.documentElement.clientWidth - 24);
      p.style.width = `${w}px`;
      const h = p.offsetHeight;
      setPosition({
        width: w,
        left: Math.max(
          12,
          Math.min(t.left, document.documentElement.clientWidth - w - 12),
        ),
        top: Math.max(12, Math.min(t.bottom + 6, innerHeight - h - 12)),
      });
    };
    const outside = (e: PointerEvent) => {
      if (
        !panel.current?.contains(e.target as Node) &&
        !trigger.current?.contains(e.target as Node)
      )
        close();
    };
    place();
    const observer = new ResizeObserver(place);
    if (trigger.current) observer.observe(trigger.current);
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    document.addEventListener("pointerdown", outside, true);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
      document.removeEventListener("pointerdown", outside, true);
    };
  }, [open, mode]);
  useEffect(() => {
    if (disabled) close();
  }, [disabled]);
  const setHSV = (next: typeof color) => {
    hue.current = next.h;
    setColor(next);
    onChange(fromHSV(next.h, next.s, next.v));
  };
  return (
    <>
      <button
        ref={trigger}
        type="button"
        className="color-input"
        aria-label={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen(!open)}
      >
        <span style={{ background: value }} />
      </button>
      {open &&
        createPortal(
          <div
            ref={(el) => {
              panel.current = el;
              el?.showPopover();
            }}
            popover="manual"
            role="dialog"
            aria-label={label + "颜色选择器"}
            className="color-picker"
            style={position}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                close(true);
              }
            }}
          >
            <div
              className="color-picker-spectrum"
              role="group"
              aria-label="饱和度与明度"
              style={{
                background: `linear-gradient(to top,#000,transparent),linear-gradient(to right,#fff,hsl(${color.h} 100% 50%))`,
              }}
              onPointerDown={(e) => {
                e.currentTarget.setPointerCapture(e.pointerId);
                const r = e.currentTarget.getBoundingClientRect();
                setHSV({
                  ...color,
                  s: Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)),
                  v:
                    1 -
                    Math.max(0, Math.min(1, (e.clientY - r.top) / r.height)),
                });
              }}
              onPointerMove={(e) => {
                if (!e.currentTarget.hasPointerCapture(e.pointerId)) return;
                const r = e.currentTarget.getBoundingClientRect();
                setHSV({
                  ...color,
                  s: Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)),
                  v:
                    1 -
                    Math.max(0, Math.min(1, (e.clientY - r.top) / r.height)),
                });
              }}
            >
              <span
                style={{
                  left: `${color.s * 100}%`,
                  top: `${(1 - color.v) * 100}%`,
                }}
              />
            </div>
            <div className="color-picker-tools">
              {"EyeDropper" in window && (
                <button
                  type="button"
                  aria-label="屏幕取色"
                  onClick={async () => {
                    const Picker = (
                      window as unknown as {
                        EyeDropper: new () => {
                          open: () => Promise<{ sRGBHex: string }>;
                        };
                      }
                    ).EyeDropper;
                    try {
                      const picked = await new Picker().open();
                      onChange(picked.sRGBHex);
                    } catch {
                      /* User cancelled the picker. */
                    }
                  }}
                >
                  <Pipette size={18} />
                </button>
              )}
              <span
                className="color-picker-current"
                style={{ background: value }}
              />
              <input
                type="range"
                aria-label="色相"
                min={0}
                max={359}
                value={color.h}
                onChange={(e) =>
                  setHSV({ ...color, h: Number(e.target.value) })
                }
              />
            </div>
            <div className="color-picker-values">
              {mode === "RGB" ? (
                <div className="color-picker-rgb">
                  {channels.map((channel, i) => (
                    <input
                      key={i}
                      type="number"
                      aria-label={["R", "G", "B"][i]}
                      min={0}
                      max={255}
                      value={channel}
                      onChange={(e) => {
                        const next = [...channels];
                        next[i] = e.target.value;
                        setChannels(next);
                        if (
                          next.every(
                            (v) =>
                              v !== "" &&
                              Number.isInteger(Number(v)) &&
                              Number(v) >= 0 &&
                              Number(v) <= 255,
                          )
                        )
                          onChange(hex(next.map(Number)));
                      }}
                      onBlur={() => {
                        const next = channels.map((v) =>
                          Math.max(
                            0,
                            Math.min(255, Math.round(Number(v) || 0)),
                          ),
                        );
                        setChannels(next.map(String));
                        onChange(hex(next));
                      }}
                    />
                  ))}
                </div>
              ) : (
                <input
                  className="color-picker-hex"
                  aria-label="HEX"
                  value={hexText}
                  onChange={(e) => {
                    setHexText(e.target.value);
                    if (/^#[0-9a-f]{6}$/i.test(e.target.value))
                      onChange(e.target.value);
                  }}
                  onBlur={() => {
                    if (!/^#[0-9a-f]{6}$/i.test(hexText)) setHexText(value);
                  }}
                />
              )}
              <div
                className="color-picker-modes"
                role="group"
                aria-label="颜色格式"
              >
                {["RGB", "HEX"].map((m) => (
                  <button
                    type="button"
                    aria-pressed={mode === m}
                    key={m}
                    onClick={() => {
                      setMode(m);
                    }}
                  >
                    {m}
                  </button>
                ))}
              </div>
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
