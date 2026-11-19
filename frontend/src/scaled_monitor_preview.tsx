import { useLayoutEffect, useRef, useState, type ReactNode } from "react";

export function ScaledMonitorPreview({ children }: { children: ReactNode }) {
  const frame = useRef<HTMLDivElement>(null),
    canvas = useRef<HTMLDivElement>(null);
  const [zoom, setZoom] = useState(1),
    [size, setSize] = useState({ scale: 0.5, height: 300 });
  useLayoutEffect(() => {
    const measure = () => {
      if (!frame.current || !canvas.current) return;
      const scale = Math.min(1, frame.current.clientWidth / 1200) * zoom;
      setSize((old) => {
        const height = canvas.current!.offsetHeight * scale;
        return Math.abs(old.scale - scale) < 0.0001 &&
          Math.abs(old.height - height) < 0.5
          ? old
          : { scale, height };
      });
    };
    const observer = new ResizeObserver(measure);
    if (frame.current) observer.observe(frame.current);
    if (canvas.current) observer.observe(canvas.current);
    measure();
    return () => observer.disconnect();
  }, [zoom]);
  return (
    <div className="scaled-monitor-preview">
      <div className="monitor-preview-tools">
        <span className="small muted">示意预览 · 拖动卡片调整顺序</span>
        <label>
          预览缩放{" "}
          <select
            aria-label="预览缩放"
            value={zoom}
            onChange={(e) => setZoom(Number(e.target.value))}
          >
            <option value={1}>自动适配</option>
            <option value={0.75}>75%</option>
            <option value={0.5}>50%</option>
          </select>
        </label>
      </div>
      <div
        ref={frame}
        className="monitor-preview-frame"
        style={{ height: size.height }}
      >
        <div
          ref={canvas}
          className="monitor-preview-canvas"
          style={{ width: 1200, transform: `scale(${size.scale})` }}
        >
          {children}
        </div>
      </div>
    </div>
  );
}

export type MonitorPreviewSpec = {
  kind: "area" | "line" | "table" | "message";
  title?: string;
  series?: string[];
  colors?: string[];
  message?: string;
};
export function MockMonitorChart({
  title,
  kind,
  series = ["指标"],
  colors = ["var(--accent)", "var(--metric-orange)"],
  message,
}: Omit<MonitorPreviewSpec, "title"> & { title: string }) {
  const lines = [
    "M24 92L50 84L80 88L110 49L140 60L170 43L200 65L230 32L260 46L290 61L320 37L350 40L380 20L410 35L430 26",
    "M24 101L50 94L80 80L110 90L140 71L170 80L200 56L230 70L260 65L290 42L320 57L350 30L380 48L410 24L430 40",
  ];
  return (
    <div
      className="mock-monitor-chart"
      data-preview-kind={kind}
      data-preview-series={series.length}
      role="img"
      aria-label={
        title +
        (kind === "table"
          ? "表格示意"
          : kind === "message"
            ? "说明示意"
            : "曲线图示意")
      }
    >
      <div className="mock-chart-title">⠿ {title}</div>
      {kind === "table" ? (
        <div className="mock-chart-rows" aria-hidden="true">
          {[0, 1, 2, 3].map((i) => (
            <div key={i}>
              {[0, 1, 2].map((j) => (i === 0 ? <b key={j} /> : <i key={j} />))}
            </div>
          ))}
        </div>
      ) : kind === "message" ? (
        <p className="small muted mock-chart-message">{message}</p>
      ) : (
        <>
          <svg
            viewBox="0 0 440 130"
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            <path
              className="mock-chart-grid"
              d="M24 12V110H430M24 36H430M24 62H430M24 88H430"
            />
            {series.map((label, index) => (
              <g
                key={label + index}
                style={{ color: colors[index % colors.length] }}
              >
                {kind === "area" && (
                  <path
                    className="mock-chart-fill"
                    d={lines[index % lines.length] + "V110H24Z"}
                  />
                )}
                <path
                  className="mock-chart-line"
                  d={lines[index % lines.length]}
                />
              </g>
            ))}
          </svg>
          <div className="mock-chart-legend" aria-hidden="true">
            {series.map((label, index) => (
              <span key={label + index}>
                <i style={{ background: colors[index % colors.length] }} />
                {label}
              </span>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
