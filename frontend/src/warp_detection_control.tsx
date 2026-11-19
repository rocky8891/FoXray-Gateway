import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ChevronDown, LoaderCircle } from "lucide-react";
import { ProbeIcon } from "./action_icons";
import { Button } from "./ui";
import type { Task } from "./api";

export const warpMetricChoices = [
  ["latency", "隧道 RTT"],
  ["loss", "隧道丢包"],
  ["true_latency", "真延迟"],
  ["speed", "下载速度"],
] as const;
export function useWarpMetricSelection(key: string) {
  const [metrics, setMetrics] = useState<string[]>(() => {
    try {
      const saved = JSON.parse(localStorage.getItem(key) || "null");
      if (Array.isArray(saved))
        return warpMetricChoices
          .map(([k]) => k)
          .filter((k) => saved.includes(k));
    } catch {
      /* Invalid storage falls back to the initial full selection. */
    }
    return warpMetricChoices.map(([k]) => k);
  });
  useEffect(() => {
    try {
      localStorage.setItem(key, JSON.stringify(metrics));
    } catch {
      /* Selection remains usable when browser storage is unavailable. */
    }
  }, [key, metrics]);
  return [metrics, setMetrics] as const;
}
export function warpTaskProgress(task: Task | null, metrics: string[]) {
  if (!task) return "";
  const endpoint = task.message.match(
    /(?:检测端点|下载测速)\s+(\d+)\s*\/\s*(\d+)/,
  );
  if (endpoint) return `${endpoint[1]}/${endpoint[2]}`;
  const stage = task.message.match(/检测当前隧道 · (RTT|丢包|真延迟|速度)/);
  if (!stage) return "";
  const metric = (
    {
      RTT: "latency",
      丢包: "loss",
      真延迟: "true_latency",
      速度: "speed",
    } as Record<string, string>
  )[stage[1]];
  const index = metrics.indexOf(metric);
  return index < 0 ? "" : `${index + 1}/${metrics.length}`;
}
export function DetectionControl({
  label,
  metrics,
  onChange,
  onRun,
  disabled,
  disabledReason,
  running,
  locked,
  progress,
  onCancel,
  cancelling,
}: {
  label: string;
  metrics: string[];
  onChange: (metrics: string[]) => void;
  onRun: () => void;
  disabled: boolean;
  disabledReason?: string;
  running: boolean;
  locked: boolean;
  progress: string;
  onCancel?: () => void;
  cancelling: boolean;
}) {
  const [open, setOpen] = useState(false),
    [position, setPosition] = useState({ top: 0, left: 0 });
  const anchor = useRef<HTMLButtonElement>(null),
    popup = useRef<HTMLDivElement>(null),
    id = useId();
  const close = (focus = false) => {
    setOpen(false);
    if (focus) anchor.current?.focus();
  };
  useEffect(() => {
    if (locked) setOpen(false);
  }, [locked]);
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
    const outside = (e: PointerEvent) => {
      if (
        !popup.current?.contains(e.target as Node) &&
        !anchor.current?.contains(e.target as Node)
      )
        close();
    };
    place();
    const resizeObserver = new ResizeObserver(place);
    if (popup.current) resizeObserver.observe(popup.current);
    popup.current?.querySelector<HTMLInputElement>("input")?.focus();
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
    <div className="detection-control detection-split">
      <Button
        disabled={
          running ? !onCancel || cancelling : disabled || !metrics.length
        }
        onClick={running ? onCancel : onRun}
        title={
          running
            ? "取消本次检测"
            : disabledReason ||
              (!metrics.length
                ? "请至少选择一个检测项目"
                : "按当前选择立即检测")
        }
        ariaLabel={
          running
            ? cancelling
              ? "正在取消检测"
              : "取消检测"
            : label + "开始检测"
        }
      >
        {running ? (
          <LoaderCircle size={18} className="spin" />
        ) : (
          <ProbeIcon size={18} />
        )}
        <span aria-live="polite">
          {running
            ? cancelling
              ? "取消中…"
              : `检测中${progress ? " " + progress : "…"}`
            : `检测 · ${metrics.length} 项`}
        </span>
      </Button>
      <button
        ref={anchor}
        type="button"
        className="button secondary detection-options-trigger"
        data-control-icon="true"
        aria-label={label + "检测项"}
        title="调整检测项目"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        disabled={locked}
        onClick={() => setOpen(!open)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.stopPropagation();
            close(true);
          }
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setOpen(true);
          }
        }}
      >
        <ChevronDown size={18} />
      </button>
      {open &&
        createPortal(
          <div
            ref={popup}
            role="dialog"
            aria-label={label + "检测项"}
            id={id}
            className="detection-options-popup"
            style={position}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                close(true);
              }
            }}
          >
            <strong>检测项目</strong>
            <label className="check detection-select-all">
              <input
                type="checkbox"
                aria-label="全选检测项目"
                checked={metrics.length === warpMetricChoices.length}
                ref={(el) => {
                  if (el)
                    el.indeterminate =
                      metrics.length > 0 &&
                      metrics.length < warpMetricChoices.length;
                }}
                onChange={(e) =>
                  onChange(
                    e.target.checked ? warpMetricChoices.map(([k]) => k) : [],
                  )
                }
              />
              <span>全选</span>
            </label>
            {warpMetricChoices.map(([key, name]) => (
              <label className="check" key={key}>
                <input
                  type="checkbox"
                  checked={metrics.includes(key)}
                  onChange={(e) =>
                    onChange(
                      warpMetricChoices
                        .map(([k]) => k)
                        .filter((k) =>
                          k === key ? e.target.checked : metrics.includes(k),
                        ),
                    )
                  }
                />
                <span>{name}</span>
              </label>
            ))}
          </div>,
          anchor.current?.closest("dialog") || document.body,
        )}
    </div>
  );
}
