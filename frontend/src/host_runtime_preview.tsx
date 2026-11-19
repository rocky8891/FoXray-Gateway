import {
  Activity,
  Cpu,
  MemoryStick,
  HardDrive,
  ChevronDown,
} from "lucide-react";
import { useLayoutEffect, useRef } from "react";
import type { Dict } from "./api";

export function HostRuntimePreview({
  items,
  order,
  onChange,
}: {
  items: Dict;
  order: string[];
  onChange: (value: string[]) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    root.current?.querySelectorAll<HTMLElement>(".gauge").forEach((e) => {
      e.draggable = true;
      e.tabIndex = 0;
      e.setAttribute(
        "aria-label",
        "拖动运行指标 " +
          ({
            load: "平均负载",
            cpu: "CPU",
            memory: "内存",
            root: "磁盘使用率",
          }[e.dataset.monitor || ""] || e.dataset.monitor),
      );
    });
  }, [items]);
  const swap = (from: string, to: string) => {
    const a = order.indexOf(from),
      b = order.indexOf(to);
    if (a < 0 || b < 0 || a === b) return;
    const next = [...order];
    [next[a], next[b]] = [next[b], next[a]];
    onChange(next);
  };
  return (
    <div
      className="host-runtime-layout-preview"
      ref={root}
      onDragStart={(e) => {
        const card = (e.target as Element).closest<HTMLElement>(".gauge");
        if (card) {
          e.dataTransfer.setData(
            "text/x-foxray-runtime",
            card.dataset.monitor || "",
          );
          e.dataTransfer.effectAllowed = "move";
        }
      }}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes("text/x-foxray-runtime")) {
          e.preventDefault();
          e.dataTransfer.dropEffect = "move";
        }
      }}
      onDrop={(e) => {
        const card = (e.target as Element).closest<HTMLElement>(".gauge");
        if (card) {
          e.preventDefault();
          swap(
            e.dataTransfer.getData("text/x-foxray-runtime"),
            card.dataset.monitor || "",
          );
        }
      }}
      onKeyDown={(e) => {
        if (
          !(e.target instanceof HTMLElement) ||
          !e.target.matches(".gauge") ||
          !e.key.startsWith("Arrow")
        )
          return;
        const cards = [
          ...root.current!.querySelectorAll<HTMLElement>(".gauge"),
        ].sort(
          (a, b) =>
            order.indexOf(a.dataset.monitor || "") -
            order.indexOf(b.dataset.monitor || ""),
        );
        const index = cards.indexOf(e.target),
          cols = getComputedStyle(
            root.current!.querySelector(".gauge-grid")!,
          ).gridTemplateColumns.split(" ").length;
        const step =
          e.key === "ArrowLeft"
            ? -1
            : e.key === "ArrowRight"
              ? 1
              : e.key === "ArrowUp"
                ? -cols
                : cols;
        const next = cards[index + step];
        if (next) {
          e.preventDefault();
          swap(e.target.dataset.monitor || "", next.dataset.monitor || "");
        }
      }}
    >
      <div className="mock-runtime-grid gauge-grid">
        {order
          .filter((id) => items[id] !== false)
          .map((id) => {
            const tone =
              {
                load: "#b39aff",
                cpu: "#57b6ff",
                memory: "#00c99a",
                root: "#ffb45b",
              }[id] || "#00c99a";
            const label =
              {
                load: "平均负载",
                cpu: "CPU",
                memory: "内存",
                root: "磁盘使用率",
              }[id] || id;
            const Icon =
              {
                load: Activity,
                cpu: Cpu,
                memory: MemoryStick,
                root: HardDrive,
              }[id] || Activity;
            const detail =
              {
                load: "1 / 5 / 15 分钟：0.68 / 0.52 / 0.47",
                cpu: "0.76 / 2 核",
                memory: "1.52 GiB / 4.00 GiB",
                root: "38.00 GiB / 100.00 GiB",
              }[id] || "";
            return (
              <div
                key={id}
                className="gauge mock-runtime-gauge"
                data-monitor={id}
                style={{ order: order.indexOf(id), color: tone }}
              >
                <svg viewBox="0 0 120 120" aria-hidden="true">
                  <circle
                    className="mock-runtime-track"
                    cx="60"
                    cy="60"
                    r="48"
                  />
                  <circle
                    cx="60"
                    cy="60"
                    r="48"
                    pathLength="100"
                    strokeDasharray={id === "load" ? "34 100" : "38 100"}
                    transform="rotate(-90 60 60)"
                  />
                  <text x="60" y="52" textAnchor="middle">
                    {id === "load" ? "34.0" : "38.0"}
                    <tspan fontSize="8">%</tspan>
                  </text>
                  <g transform="translate(48 74)">
                    <Icon size={13} />
                    <ChevronDown x={16} y={2} size={9} />
                  </g>
                  <title>{label}</title>
                </svg>
                <div className="mock-runtime-detail">{detail}</div>
              </div>
            );
          })}
      </div>
    </div>
  );
}
