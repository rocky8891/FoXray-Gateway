import { ConnectedTime } from "./connected_time";
import { ServerIcon, TrueLatencyIcon } from "./requirement_icons";
import { circleLayout } from "./circle_layout";
import {
  serviceStyle,
  wireguardNameStyle,
  SERVICE_METRICS,
  SERVICE_CIRCLE_STYLES,
} from "./overview_policy";
import { ConnectionIcon } from "./monitor_icons";
import { useRef, useLayoutEffect, type ReactNode } from "react";
import {
  Activity,
  Cpu,
  MemoryStick,
  HardDrive,
  ArrowUp,
  ArrowDown,
} from "lucide-react";
import { list, record, type Dict, type State } from "./api";
import { formatRate } from "./host";
import { ErrorLine, Panel, SettingInfo, useResource } from "./ui";
import { ProductIcon } from "./product_icons";
const pct = (v: unknown) => (v == null ? "—" : Number(v).toFixed(1) + "%");
const delay = (v: unknown) =>
  v == null || Number(v) < 0 ? "未测" : Number(v).toFixed(1) + " ms";
function uptime(v: unknown) {
  if (v == null || !Number.isFinite(Number(v))) return "运行时间待读取";
  const s = Math.max(0, Math.floor(Number(v)));
  return `已运行 ${Math.floor(s / 86400)}天 ${Math.floor(s / 3600) % 24}小时 ${Math.floor(s / 60) % 60}分钟`;
}
function SummaryHeader({
  title,
  icon,
  status,
  tone = "muted",
  identity,
  help,
  controls,
  titleExtra,
}: {
  title: string;
  icon: ReactNode;
  status: string;
  tone?: string;
  identity?: ReactNode;
  help?: string;
  controls?: ReactNode;
  titleExtra?: ReactNode;
}) {
  const heading = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const card = heading.current?.closest<HTMLElement>(
      ".service-overview-card",
    );
    if (!card) return;
    const measure = () => {
      if (card.classList.contains("service-wireguard"))
        card.style.setProperty(
          "--wg-interface-count",
          String(
            Math.max(1, card.querySelectorAll(".service-tunnel-row").length),
          ),
        );
      const scale = Number(
        card.closest<HTMLElement>("[data-preview-scale]")?.dataset
          .previewScale || 1,
      );
      const logical = (r: DOMRect) => ({
        width: r.width / scale,
        height: r.height / scale,
      });
      const identity = heading.current?.querySelector<HTMLElement>(
        ".service-overview-identity-row",
      );
      if (identity && titleExtra)
        heading.current?.style.setProperty(
          "--summary-identity-width",
          `${identity.getBoundingClientRect().width / scale}px`,
        );
      const box = logical(card.getBoundingClientRect()),
        surfaceElement = card.querySelector(".service-overview-surface"),
        surface = surfaceElement
          ? logical(surfaceElement.getBoundingClientRect())
          : undefined;
      const width = surface?.width || box.width,
        height = surface?.height || box.height;
      const circleStyle = [...card.classList]
        .filter((value) => value.startsWith("service-style-"))
        .map((value) => value.slice("service-style-".length))
        .find((value) => SERVICE_CIRCLE_STYLES.includes(value));
      const requestedLabel =
        parseFloat(
          getComputedStyle(card).getPropertyValue(
            card.classList.contains("service-wireguard")
              ? "--wg-label-size"
              : "--overview-label-size",
          ),
        ) || 14;
      const requestedValue =
        parseFloat(
          getComputedStyle(card).getPropertyValue(
            card.classList.contains("service-wireguard")
              ? "--wg-value-size"
              : "--overview-value-size",
          ),
        ) || 24;
      const context = document.createElement("canvas").getContext("2d");
      const textSize = (text: string, element: HTMLElement, size: number) => {
        if (!context) return text.length * size;
        const font = getComputedStyle(element);
        context.font = `${font.fontWeight} ${size}px ${font.fontFamily}`;
        return context.measureText(text).width;
      };
      const fitText = (
        element: HTMLElement,
        area: number,
        diameter: number,
      ) => {
        const label = element.querySelector<HTMLElement>(
            ".summary-metric-label",
          ),
          value = element.querySelector<HTMLElement>("strong");
        let labelSize = Math.min(requestedLabel, Math.max(7, diameter * 0.12)),
          valueSize = Math.min(requestedValue, Math.max(10, diameter * 0.18));
        const icon = Math.min(14, Math.max(9, labelSize + 1)),
          extras =
            icon + 5 + (label?.querySelector(".setting-info") ? icon + 5 : 0);
        if (label)
          labelSize = Math.min(
            labelSize,
            Math.max(
              5,
              (area - extras) /
                Math.max(1, textSize(label.textContent || "", label, 1)),
            ),
          );
        if (value)
          valueSize = Math.min(
            valueSize,
            area / Math.max(1, textSize(value.textContent || "", value, 1)),
          );
        element.style.setProperty("--circle-label-size", `${labelSize}px`);
        element.style.setProperty("--circle-value-size", `${valueSize}px`);
        element.style.setProperty(
          "--circle-icon-size",
          `${Math.min(icon, labelSize + 2)}px`,
        );
      };
      const alignInterfaceNames = () => {
        if (!context) return;
        card
          .querySelectorAll<HTMLElement>(".service-interface-name")
          .forEach((name) => {
            const font = getComputedStyle(name);
            context.font = `${font.fontWeight} ${font.fontSize} ${font.fontFamily}`;
            const ink = context.measureText(name.textContent || "");
            const offset =
              ((ink.fontBoundingBoxAscent || 0) -
                (ink.fontBoundingBoxDescent || 0) +
                ink.actualBoundingBoxDescent -
                ink.actualBoundingBoxAscent) /
              2;
            name.parentElement?.style.setProperty(
              "--status-ink-offset",
              `${Number.isFinite(offset) ? offset : 0}px`,
            );
          });
      };
      if (!circleStyle) {
        delete card.dataset.adaptiveCircles;
        delete card.dataset.circleLayout;
        delete card.dataset.tunnelAxis;
        delete card.dataset.wgNamePosition;
        if (card.classList.contains("service-wireguard")) {
          let inset = 12;
          if (card.classList.contains("service-style-metrics-left")) {
            const ref = card
              .closest(".dashboard-exit-grid")
              ?.querySelector<HTMLElement>(".service-xray");
            const label = ref?.querySelector<HTMLElement>(
              '.summary-metric[data-summary-slot="0"] .summary-metric-label',
            );
            const first = card.querySelector<HTMLElement>(".summary-metric");
            if (ref && label && first) {
              const col = logical(first.getBoundingClientRect()).width;
              inset = Math.max(
                12,
                Math.min(
                  col * 0.45,
                  (label.getBoundingClientRect().left -
                    ref.getBoundingClientRect().left) /
                    scale -
                    (first.getBoundingClientRect().left -
                      card.getBoundingClientRect().left) /
                      scale,
                ),
              );
            }
          }
          card.style.setProperty("--wg-rect-inset", `${inset}px`);
        }
        let valueSize = requestedValue,
          labelSize = requestedLabel;
        card
          .querySelectorAll<HTMLElement>(".summary-metric")
          .forEach((metric) => {
            const padding = getComputedStyle(metric),
              area =
                logical(metric.getBoundingClientRect()).width -
                parseFloat(padding.paddingLeft) -
                parseFloat(padding.paddingRight) -
                4;
            const value = metric.querySelector<HTMLElement>("strong"),
              label = metric.querySelector<HTMLElement>(
                ".summary-metric-label",
              );
            if (value)
              valueSize = Math.min(
                valueSize,
                area / Math.max(1, textSize(value.textContent || "", value, 1)),
              );
            if (label)
              labelSize = Math.min(
                labelSize,
                Math.max(
                  7,
                  (area - 20) /
                    Math.max(1, textSize(label.textContent || "", label, 1)),
                ),
              );
          });
        card.style.setProperty("--ordinary-value-size", `${valueSize}px`);
        card.style.setProperty("--ordinary-label-size", `${labelSize}px`);
        alignInterfaceNames();
        return;
      }
      card.dataset.adaptiveCircles = "true";
      if (!card.classList.contains("service-wireguard")) {
        const fit = circleLayout(width, height, circleStyle);
        card.style.setProperty("--summary-ring-diameter", `${fit.diameter}px`);
        card.style.setProperty(
          "--summary-ring-stroke",
          String(400 / fit.diameter),
        );
        card.dataset.ringColumns = String(fit.columns);
        card.dataset.circleLayout = fit.layout;
        card
          .querySelectorAll<HTMLElement>(".service-metric-grid>.summary-metric")
          .forEach((metric, index) => {
            const slot = Number(metric.dataset.summarySlot ?? index),
              point = fit.centers[slot] || fit.centers[index];
            metric.style.setProperty("--circle-x", `${point[0]}px`);
            metric.style.setProperty("--circle-y", `${point[1]}px`);
            const diameter = fit.diameters[slot] || fit.diameter;
            metric.style.setProperty("--circle-diameter", `${diameter}px`);
            metric.style.setProperty(
              "--summary-ring-stroke",
              String(400 / diameter),
            );
            fitText(metric, diameter * 0.8, diameter);
          });
      } else {
        const rows = [
            ...card.querySelectorAll<HTMLElement>(".service-tunnel-row"),
          ],
          count = Math.max(1, rows.length);
        const reserved = 0,
          gap = 12;
        card.style.setProperty("--wg-name-reserved", `${reserved}px`);
        const horizontal = Math.min(
          (width - gap * (count + 1)) / count,
          height - reserved - 12,
        );
        const vertical = Math.min(
          width - 12,
          (height - gap * (count + 1)) / count - reserved,
        );
        const preferred = getComputedStyle(card)
          .getPropertyValue("--wg-circle-axis")
          .trim();
        const axis =
          preferred === "row" || preferred === "column"
            ? preferred
            : horizontal >= vertical
              ? "row"
              : "column";
        card.dataset.tunnelAxis = axis;
        const available = Math.max(1, axis === "row" ? horizontal : vertical);
        const requested =
          parseFloat(
            getComputedStyle(card).getPropertyValue("--wg-circle-size"),
          ) || 0;
        const grid = card.closest<HTMLElement>(".dashboard-exit-grid");
        const leftAnchor =
          grid?.classList.contains("overview-layout-sidebar") &&
          grid.dataset.overviewAnchor === "service-wireguard";
        const diameter =
          requested > 0
            ? Math.min(requested, available)
            : leftAnchor
              ? Math.min(available, 280)
              : grid?.classList.contains("overview-layout-grid")
                ? available * 0.9
                : available;
        card.style.setProperty("--summary-tunnel-diameter", `${diameter}px`);
        const center =
          card.classList.contains("wg-circle-center") && diameter >= 180;
        card.dataset.wgNamePosition = center ? "center" : "top";
        rows.forEach((row) => {
          const name = row.querySelector<HTMLElement>(
            ".service-interface-name",
          );
          if (name) {
            const available = diameter * (center ? 0.27 : 0.82) - 12;
            row.style.setProperty(
              "--circle-name-size",
              `${Math.min(14, diameter * 0.08, available / Math.max(1, textSize(name.textContent || "", name, 1)))}px`,
            );
          }
          row
            .querySelectorAll<HTMLElement>(".summary-metric")
            .forEach((metric) => {
              const middle = ["latency", "connections"].includes(
                metric.dataset.summaryMetric || "",
              );
              fitText(
                metric,
                diameter * (middle ? (center ? 0.27 : 0.4) : 0.62),
                diameter * (middle ? 0.74 : 0.64),
              );
            });
        });
      }
      alignInterfaceNames();
      const metrics = [
        ...card.querySelectorAll<HTMLElement>(".summary-metric"),
      ];
      const commonValue = Math.min(
        requestedValue,
        ...metrics.map(
          (metric) =>
            parseFloat(metric.style.getPropertyValue("--circle-value-size")) ||
            requestedValue,
        ),
      );
      const commonLabel = Math.min(
        requestedLabel,
        ...metrics.map(
          (metric) =>
            parseFloat(metric.style.getPropertyValue("--circle-label-size")) ||
            requestedLabel,
        ),
      );
      metrics.forEach((metric) => {
        metric.style.setProperty("--circle-value-size", `${commonValue}px`);
        metric.style.setProperty("--circle-label-size", `${commonLabel}px`);
        metric.style.setProperty(
          "--circle-icon-size",
          `${Math.min(14, commonLabel + 1)}px`,
        );
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(card);
    const identity = heading.current?.querySelector(
      ".service-overview-identity-row",
    );
    if (identity && titleExtra) observer.observe(identity);
    const surface = card.querySelector(".service-overview-surface");
    if (surface) observer.observe(surface);
    const classes = new MutationObserver(measure);
    classes.observe(card, {
      attributes: true,
      attributeFilter: ["class"],
      childList: true,
      subtree: true,
      characterData: true,
    });
    const appearance = new MutationObserver(measure);
    const page = card.closest(".overview-page");
    if (page)
      appearance.observe(page, {
        attributes: true,
        attributeFilter: ["style"],
      });
    return () => {
      observer.disconnect();
      classes.disconnect();
      appearance.disconnect();
    };
  }, [identity]);
  return (
    <div className="service-overview-heading" ref={heading}>
      <div className="service-overview-title-row">
        <h2 aria-label={title}>
          {icon}
          {title}
          {help && <SettingInfo label={title} text={help} />}
          {titleExtra && (
            <span className="service-title-extra">{titleExtra}</span>
          )}
        </h2>
        {status && (
          <span
            className={
              "service-status-dot " +
              (tone === "accent" ? "accent" : tone === "fair" ? "fair" : "poor")
            }
            role="img"
            aria-label={title + "：" + status}
            title={status}
          />
        )}
      </div>
      <div className="service-overview-identity-row">
        <div className="service-overview-identity">
          {identity || <span className="muted">—</span>}
        </div>
        {controls && (
          <div className="service-overview-pagination">{controls}</div>
        )}
      </div>
    </div>
  );
}
function SummaryMetric({
  label,
  icon: Icon,
  tone,
  value,
  percent,
  metricKey,
  help,
  slot,
}: {
  slot?: number;
  label: string;
  icon: React.ComponentType<{ size?: number; className?: string }>;
  tone: string;
  value: ReactNode;
  percent?: number | null;
  metricKey?: string;
  help?: string;
}) {
  return (
    <div
      className={"summary-metric metric-" + tone}
      data-summary-metric={metricKey}
      data-summary-slot={slot}
      style={slot == null ? undefined : { order: slot }}
    >
      {percent !== undefined && (
        <svg
          className="summary-percent-ring"
          viewBox="0 0 100 100"
          aria-hidden="true"
        >
          <circle className="summary-ring-track" cx="50" cy="50" r="48" />
          {percent != null && (
            <circle
              cx="50"
              cy="50"
              r="48"
              pathLength="100"
              strokeDasharray={`${Math.min(100, Math.max(0, percent))} 100`}
              transform="rotate(-90 50 50)"
            />
          )}
        </svg>
      )}
      <span className="summary-metric-label">
        <Icon size={16} className={"metric-" + tone} />
        {label}
        {help && <SettingInfo label={label} text={help} />}
      </span>
      <strong className={"metric-" + tone}>{value}</strong>
    </div>
  );
}
function SummaryMetrics({
  rows,
  count,
  latency,
  paused = false,
  compact = false,
  order = SERVICE_METRICS,
}: {
  order?: string[];
  rows: Dict[];
  count: unknown;
  latency: unknown;
  paused?: boolean;
  compact?: boolean;
}) {
  const valid = rows.filter((r) => r.available);
  const sum = (key: string) =>
    valid.length ? valid.reduce((n, r) => n + Number(r[key] || 0), 0) : null;
  return (
    <div className={"service-metric-grid" + (paused ? " summary-muted" : "")}>
      <SummaryMetric
        metricKey="upload"
        slot={order.indexOf("upload")}
        label="上传"
        icon={ArrowUp}
        tone="orange"
        value={formatRate(sum("upload_bps"))}
      />
      <SummaryMetric
        metricKey="download"
        slot={order.indexOf("download")}
        label="下载"
        icon={ArrowDown}
        tone="green"
        value={formatRate(sum("download_bps"))}
      />
      <SummaryMetric
        metricKey="latency"
        slot={order.indexOf("latency")}
        label="延迟"
        icon={TrueLatencyIcon}
        tone="blue"
        value={paused ? "—" : delay(latency)}
      />
      <SummaryMetric
        metricKey="connections"
        slot={order.indexOf("connections")}
        label={compact ? "连接数" : "活动连接数"}
        icon={ConnectionIcon}
        tone="purple"
        value={count == null ? "—" : String(count)}
      />
    </div>
  );
}
export function ServiceOverviewCards({
  state,
  cards,
}: {
  state: State;
  cards: string[];
}) {
  const system = useResource<Dict>("/api/system", 2000),
    traffic = useResource<Dict>("/api/traffic", 2000),
    warp = useResource<Dict>("/api/warp", 15000),
    wg = useResource<Dict>("/api/network/wireguard", 15000),
    services = useResource<Dict>("/api/services", 15000);
  const wgDelay = useResource<Dict>(
      "/api/latency-history?module=wireguard&kind=icmp&hours=1",
      15000,
    ),
    interfaceCount = Number(
      record(state.manager_settings.presentation).services_wireguard_count || 2,
    );
  const moduleOn = (m: string) =>
    record(record(state.monitoring_items)._modules)[m] !== false;
  const current = record(system.data?.current),
    hostOn = moduleOn("host") && record(system.data?.policy).enabled !== false,
    host = hostOn ? current : {},
    partition = String(record(system.data?.policy).default_partition || "/"),
    root = list(host.mounts).find((r) => r.path === partition);
  const connections = record(state.connection_monitor),
    rows = list(traffic.data?.items),
    xrayNode = state.nodes.find((n) => n.active),
    warpNode = state.nodes.find((n) => n.id === "warp");
  const allInterfaces = list(wg.data?.items),
    activeInterfaceCount = allInterfaces.filter((row) => row.active).length,
    preferredInterfaces = record(state.manager_settings.presentation)
      .services_wireguard_interfaces as string[] | undefined,
    interfaces = preferredInterfaces?.length
      ? preferredInterfaces
          .concat(
            allInterfaces
              .map((r) => String(r.name))
              .filter((n) => !preferredInterfaces.includes(n)),
          )
          .flatMap((name) => allInterfaces.filter((row) => row.name === name))
          .slice(0, interfaceCount)
      : allInterfaces.slice(0, interfaceCount),
    wgOn = moduleOn("wireguard") && !!state.components.wireguard;
  const hostStatus = !moduleOn("host")
    ? "监控已关闭"
    : !hostOn
      ? "采集已暂停"
      : system.error || current.supported === false
        ? "数据不可用"
        : !system.data
          ? "读取中"
          : "采集中";
  const hostOrder = (record(state.manager_settings.presentation)
    .services_host_order as string[]) || ["cpu", "load", "memory", "root"];
  const presentation = record(state.manager_settings.presentation);
  const cardStyle = (module: string) =>
    " service-style-" +
    serviceStyle(presentation, module) +
    (serviceStyle(presentation, module).startsWith("radial-") &&
    serviceStyle(presentation, module) !== "radial-diamond"
      ? " service-style-radial-diamond"
      : "");
  return (
    <>
      {cards.includes("service-host") && (
        <Panel
          className={
            "overview-summary-card service-host service-overview-card summary-v417" +
            cardStyle("host")
          }
        >
          <SummaryHeader
            title="服务器"
            icon={<ServerIcon size={20} />}
            status={hostStatus}
            tone={hostStatus === "采集中" ? "accent" : "muted"}
            identity={uptime(current.uptime_seconds)}
          />
          <div
            className={
              "service-overview-surface" + (!hostOn ? " summary-muted" : "")
            }
          >
            <div className="service-metric-grid">
              <SummaryMetric
                metricKey="cpu"
                slot={hostOrder.indexOf("cpu")}
                percent={
                  host.cpu_percent == null ? null : Number(host.cpu_percent)
                }
                label="CPU使用率"
                icon={Cpu}
                tone="blue"
                value={pct(host.cpu_percent)}
              />
              <SummaryMetric
                metricKey="load"
                slot={hostOrder.indexOf("load")}
                percent={
                  host.load1 == null
                    ? null
                    : (Number(host.load1) /
                        Math.max(1, Number(host.cores || 1))) *
                      100
                }
                help="数值为1分钟平均负载；圆环按CPU核数归一化，达到或超过核数时进度为满环。"
                label="平均负载"
                icon={Activity}
                tone="purple"
                value={host.load1 == null ? "—" : Number(host.load1).toFixed(2)}
              />
              <SummaryMetric
                metricKey="memory"
                slot={hostOrder.indexOf("memory")}
                percent={
                  host.memory_percent == null
                    ? null
                    : Number(host.memory_percent)
                }
                label="内存使用率"
                icon={MemoryStick}
                tone="green"
                value={pct(host.memory_percent)}
              />
              {record(record(state.monitoring_items).host).root !== false && (
                <SummaryMetric
                  metricKey="root"
                  slot={hostOrder.indexOf("root")}
                  percent={root?.percent == null ? null : Number(root.percent)}
                  label={
                    partition === "/" ? "根分区使用率" : `${partition}使用率`
                  }
                  icon={HardDrive}
                  tone="orange"
                  value={pct(root?.percent)}
                />
              )}
            </div>
          </div>
          <ErrorLine error={system.error} />
        </Panel>
      )}
      {(
        [
          ["xray", "Xray"],
          ["warp", "WARP"],
        ] as const
      )
        .filter(([m]) => cards.includes("service-" + m))
        .map(([module, title]) => {
          const service = record(record(services.data?.services)[module]),
            serviceState = String(service.state || ""),
            node = module === "xray" ? xrayNode : warpNode,
            summary = record(connections[module]);
          let status = "",
            name = "",
            suffix = "";
          if (!state.components[module]) status = "未启用";
          else if (!moduleOn(module)) status = "监控已关闭";
          else if (services.error) status = "服务状态不可用";
          else if (!services.data) status = "读取中";
          else if (
            ["inactive", "failed", "deactivating", "dead"].includes(
              serviceState,
            )
          )
            status = "服务已停止";
          else if (serviceState !== "active")
            status = serviceState === "activating" ? "启动中" : "服务状态未知";
          else if (module === "warp" && !warp.data)
            status = warp.error ? "隧道状态不可用" : "读取中";
          else if (module === "warp" && !warp.data?.connected)
            status = "隧道未连接";
          else if (module === "xray" && !state.proxy.enabled)
            status = "代理未启用";
          else {
            status = module === "xray" ? "运行中" : "已连接";
            name =
              module === "xray"
                ? node?.name || "系统直连"
                : String(
                    warp.data?.endpoint ||
                      warp.data?.endpoint_address ||
                      warp.data?.override ||
                      "",
                  );
            if (name) suffix = module === "xray" ? "当前出口" : "当前端点";
          }
          const paused = !suffix;
          const serviceRows = !paused
            ? rows.filter((r) =>
                module === "warp"
                  ? r.id === "warp"
                  : r.id !== "warp" && !String(r.id).startsWith("wg:"),
              )
            : [];
          return (
            <Panel
              key={module}
              className={
                "overview-summary-card service-overview-card summary-v417 service-" +
                module +
                cardStyle(module)
              }
            >
              <SummaryHeader
                title={title}
                icon={<ProductIcon product={module} size={20} />}
                status={status}
                tone={paused ? "muted" : "accent"}
                titleExtra={
                  suffix ? (
                    <span
                      className="xray-exit-name"
                      title={`${name} · ${suffix}`}
                    >
                      {name} · {suffix}
                    </span>
                  ) : undefined
                }
                identity={
                  <ConnectedTime
                    since={
                      module === "xray"
                        ? state.state.active_since
                        : Number(warp.data?.connected_since || 0)
                    }
                    connected={
                      module === "xray" ? !!suffix : !!warp.data?.connected
                    }
                    observed={
                      module === "warp" &&
                      warp.data?.connection_time_source === "observed"
                    }
                  />
                }
              />
              <div className="service-overview-surface">
                <SummaryMetrics
                  order={
                    (presentation[
                      "services_" + module + "_order"
                    ] as string[]) || SERVICE_METRICS
                  }
                  rows={serviceRows}
                  count={!paused && summary.available ? summary.count : null}
                  latency={!paused ? node?.metrics.latency_ms : undefined}
                  paused={paused}
                />
              </div>
              <ErrorLine
                error={module === "warp" ? warp.error : traffic.error}
              />
            </Panel>
          );
        })}
      {cards.includes("service-wireguard") && (
        <Panel
          className={
            "overview-summary-card service-overview-card summary-v417 service-wireguard" +
            cardStyle("wireguard") +
            " wg-circle-" +
            wireguardNameStyle(state.manager_settings.presentation)
          }
        >
          <SummaryHeader
            title="WireGuard"
            icon={<ProductIcon product="wireguard" size={20} />}
            status={
              !state.components.wireguard
                ? "未启用"
                : wg.error
                  ? "接口状态不可用"
                  : !wg.data
                    ? "读取中"
                    : !allInterfaces.length
                      ? "暂无接口"
                      : activeInterfaceCount === allInterfaces.length
                        ? "全部正常"
                        : activeInterfaceCount > 0
                          ? "部分正常"
                          : "全部故障"
            }
            tone={
              state.components.wireguard &&
              !wg.error &&
              activeInterfaceCount > 0
                ? activeInterfaceCount === allInterfaces.length
                  ? "accent"
                  : "fair"
                : "muted"
            }
            identity={`总数 / 已连接：${allInterfaces.length} / ${activeInterfaceCount} · 显示 ${interfaces.length}个`}
            help="按接口运行状态显示已连接，不代表每个Peer均可达。默认显示两个接口，可在主页布局选择1～4个并指定名称；模块监控关闭后数值暂停，接口状态仍独立展示。"
          />
          <div className="service-overview-surface service-tunnel-list">
            {interfaces.length ? (
              interfaces.map((row) => {
                const id = "wg:" + row.name,
                  samples = list(record(wgDelay.data?.series)[id]),
                  latest = samples[samples.length - 1],
                  node = record(
                    record(record(connections.wireguard).nodes)[id],
                  ),
                  ready = wgOn && !!row.active;
                return (
                  <div
                    className={
                      "service-tunnel-row" +
                      (!row.active ? " disconnected" : "")
                    }
                    key={id}
                    data-interface-name={String(row.name)}
                    style={
                      {
                        "--tunnel-tone":
                          row.name === "wg-home"
                            ? "var(--accent)"
                            : row.name === "wg-zstack"
                              ? "var(--metric-orange)"
                              : interfaces.some(
                                    (item) => item.name === "wg-home",
                                  )
                                ? "var(--metric-orange)"
                                : interfaces.some(
                                      (item) => item.name === "wg-zstack",
                                    )
                                  ? "var(--accent)"
                                  : interfaces.indexOf(row) === 0
                                    ? "var(--accent)"
                                    : "var(--metric-orange)",
                      } as React.CSSProperties
                    }
                  >
                    <div className="service-tunnel-heading">
                      <button
                        type="button"
                        className="service-interface-link"
                        onClick={() =>
                          window.dispatchEvent(
                            new CustomEvent("foxray:navigate", {
                              detail: "wireguard",
                            }),
                          )
                        }
                        title={String(row.name) + " · 打开 WireGuard 接口管理"}
                      >
                        <span className="service-interface-name">
                          {String(row.name)}
                        </span>
                        <span
                          className={
                            "interface-status-dot " +
                            (row.active ? "connected" : "disconnected")
                          }
                          role="img"
                          aria-label={row.active ? "接口已启动" : "接口已停止"}
                        />
                      </button>
                    </div>
                    <SummaryMetrics
                      rows={ready ? rows.filter((r) => r.id === id) : []}
                      count={
                        ready && record(connections.wireguard).available
                          ? (node.count ?? 0)
                          : null
                      }
                      latency={ready ? latest?.latency : undefined}
                      paused={!ready}
                      compact
                    />
                  </div>
                );
              })
            ) : (
              <p className="muted service-empty">
                {wg.data ? "暂无接口" : "正在读取接口…"}
              </p>
            )}
          </div>
          <ErrorLine error={wg.error || wgDelay.error} />
        </Panel>
      )}
    </>
  );
}
