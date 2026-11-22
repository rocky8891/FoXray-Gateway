import { HomeTools } from "./home_tools";
import { AdaptiveSearch } from "./adaptive_search";
import { ResetFilters } from "./list_controls";
import { SelectAllIcon } from "./action_icons";
import {
  ExportButton,
  PaginatedCards,
  SortIcon as ArrowUpDown,
} from "./resource_controls";
import { ListFilter, SortHeading } from "./list_controls";
import type { HomeRandomTheme } from "./overview_random_theme";
import { RandomThemeIcon, RestoreThemeIcon } from "./requirement_icons";
import { useId, useRef, useLayoutEffect } from "react";
import { ConnectedTime } from "./connected_time";
import { AllNodesIcon } from "./log_icons";
import { TrueLatencyIcon } from "./requirement_icons";
import { useOverviewDrag } from "./overview_editor";
import { ConnectionIcon, GatewayIcon, ThemePaletteIcon } from "./monitor_icons";
import { IconSelect } from "./icon_select";
import { overviewPolicy, type OverviewView } from "./overview_policy";
import { ServiceOverviewCards } from "./monitor_overview";
import { rangeHours } from "./presentation_settings";
import { SettingInfo } from "./ui";
import { monitorEnabled } from "./monitor_items";
import { NumberInput } from "./ui";
import { ActionMenu } from "./action_menu";
import { useTimeRange, OverviewTimeControl } from "./time_range";
import { TimeAxisTick, timeTicks, formatAxisTime } from "./chart_layout";
import "flag-icons/css/flag-icons.min.css";
import { useView } from "./view_state";
import {
  AlignedAxisTick,
  CHART_AXIS_WIDTH,
  CHART_MARGIN,
} from "./chart_layout";
import { CurrentExitTraffic } from "./traffic";
import { useMemo, useState, useEffect, useCallback, memo } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
} from "recharts";
import {
  Search,
  Pencil,
  List,
  LayoutGrid,
  CheckCircle2,
  XCircle,
  Save,
  X,
  Award,
  Globe,
  Gauge,
} from "lucide-react";
import {
  api,
  bandwidth,
  errorMessage,
  record,
  text,
  timestamp,
  type Config,
  type Subscription,
  type Dict,
  type Node,
  type State,
} from "./api";
import {
  Button,
  RowActions,
  Confirm,
  Empty,
  ErrorLine,
  Field,
  JsonDetails,
  Modal,
  PageHeader,
  Panel,
  Table,
  useResource,
  type Column,
  Metric,
} from "./ui";
export type Act = (
  path: string,
  data: unknown,
  receive?: (result: Dict) => void,
) => Promise<boolean>;
export type Filters = {
  query: string;
  group: string;
  protocol: string;
  status: string;
  sort: string;
  direction: number;
};
export const filterDefaults: Filters = {
  query: "",
  group: "all",
  protocol: "all",
  status: "all",
  sort: "score",
  direction: -1,
};
export function groupName(n: Node, state: State) {
  return n.subscription
    ? "订阅 · " +
        (state.native.subscriptions.find((s) => s.id === n.subscription)
          ?.name || "未命名订阅")
    : "独立节点";
}
export function filterNodes(state: State, filters: Filters) {
  return state.nodes
    .filter(
      (n) =>
        (!filters.query ||
          (n.name + " " + n.protocol + " " + groupName(n, state))
            .toLowerCase()
            .includes(filters.query.toLowerCase())) &&
        (filters.group === "all" ||
          (n.subscription || "standalone") === filters.group) &&
        (filters.protocol === "all" || n.protocol === filters.protocol) &&
        (filters.status === "all" ||
          n.enabled === (filters.status === "enabled")),
    )
    .sort((a, b) => {
      const val = (n: Node): unknown =>
        filters.sort === "score"
          ? n.score?.score
          : filters.sort === "latency"
            ? n.metrics.latency_ms
            : filters.sort === "speed"
              ? n.metrics.bandwidth_kib_s
              : filters.sort === "protocol"
                ? n.protocol
                : filters.sort === "tested"
                  ? n.metrics.bandwidth_at
                  : filters.sort === "checked"
                    ? n.metrics.checked_at
                    : filters.sort === "success"
                      ? n.metrics.request_success_pct
                      : filters.sort === "connections"
                        ? n.metrics.active_connections
                        : filters.sort === "group"
                          ? groupName(n, state)
                          : filters.sort === "status"
                            ? n.enabled
                              ? "启用"
                              : "停用"
                            : n.name;
      const x = val(a),
        y = val(b);
      if (x == null || y == null) return x == null ? (y == null ? 0 : 1) : -1;
      return (
        (typeof x === "number" && typeof y === "number"
          ? x - y
          : String(x).localeCompare(String(y), "zh-CN")) * filters.direction
      );
    });
}
export function FilterBar({
  state,
  value,
  onChange,
  monitor = false,
  compact = false,
  grid = false,
  selectionControl,
  toolbar,
  children,
  sortOptions,
  interfaceMode = false,
  rangeControl,
  refreshControl,
}: {
  state: State;
  value: Filters;
  onChange: (value: Filters) => void;
  monitor?: boolean;
  compact?: boolean;
  grid?: boolean;
  selectionControl?: React.ReactNode;
  toolbar?: React.ReactNode;
  children?: React.ReactNode;
  sortOptions?: string[][];
  interfaceMode?: boolean;
  rangeControl?: React.ReactNode;
  refreshControl?: React.ReactNode;
}) {
  const set = (key: keyof Filters, v: string | number) =>
    onChange({ ...value, [key]: v });
  const filterableNodes = state.nodes;
  const groups = [
      ...new Set(filterableNodes.map((n) => n.subscription || "standalone")),
    ],
    protocols = [...new Set(state.nodes.map((n) => n.protocol))];
  const sortControl = (
    <Field label="排序">
      <select value={value.sort} onChange={(e) => set("sort", e.target.value)}>
        {(
          sortOptions || [
            ["score", "综合评分"],
            ["latency", "真连接延迟"],
            ["speed", "最近带宽"],
            ["name", "节点名称"],
            ["group", "分组"],
            ["status", "状态"],
          ]
        ).map(([k, v]) => (
          <option key={k} value={k}>
            {v}
          </option>
        ))}
      </select>
    </Field>
  );
  const search = (
      <>
        {" "}
        <AdaptiveSearch>
          <Search size={16} />
          <input
            aria-label={interfaceMode ? "搜索接口" : "搜索节点"}
            placeholder={
              interfaceMode ? "查询接口名称" : "查询节点名称、分组或协议"
            }
            value={value.query}
            onChange={(e) => set("query", e.target.value)}
          />
        </AdaptiveSearch>
      </>
    ),
    controls = (
      <>
        {" "}
        {!interfaceMode && (
          <>
            <Field label="分组">
              <select
                value={value.group}
                onChange={(e) => set("group", e.target.value)}
              >
                <option value="all">全部分组</option>
                {groups.map((g) => (
                  <option key={g} value={g}>
                    {g === "standalone"
                      ? "独立节点"
                      : state.native.subscriptions.find((s) => s.id === g)
                          ?.name || "未命名订阅"}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="协议">
              <select
                value={value.protocol}
                onChange={(e) => set("protocol", e.target.value)}
              >
                <option value="all">全部协议</option>
                {protocols.map((p) => (
                  <option key={p}>{p}</option>
                ))}
              </select>
            </Field>
          </>
        )}
        <Field label="状态">
          <select
            value={value.status}
            onChange={(e) => set("status", e.target.value)}
          >
            <option value="all">全部状态</option>
            <option value="enabled">
              {interfaceMode ? "运行中" : "已启用"}
            </option>
            <option value="disabled">
              {interfaceMode ? "已停止" : "已停用"}
            </option>
          </select>
        </Field>
        {!compact && sortControl}
        {children}
      </>
    ),
    direction = (
      <>
        {" "}
        <Button onClick={() => set("direction", -value.direction)}>
          <ArrowUpDown size={15} />
          {value.direction < 0 ? "降序" : "升序"}
        </Button>
      </>
    );
  return compact ? (
    <div className="management-query-tools">
      {search}
      {grid && (
        <>
          <ListFilter
            label="节点筛选"
            active={[value.group, value.protocol, value.status].some(
              (v) => v !== "all",
            )}
          >
            {controls}
            <Button
              onClick={() =>
                onChange({
                  ...value,
                  group: "all",
                  protocol: "all",
                  status: "all",
                })
              }
            >
              重置
            </Button>
          </ListFilter>
          <ListFilter label="节点排序" icon={<ArrowUpDown size={16} />}>
            {sortControl}
            {direction}
          </ListFilter>
          {selectionControl}
        </>
      )}
    </div>
  ) : monitor ? (
    <div
      className={
        "monitor-filter-bar" + (interfaceMode ? " interface-filters" : "")
      }
    >
      <div className="monitor-search-row">
        {refreshControl && (
          <span className="monitor-refresh-control">{refreshControl}</span>
        )}
        {search}
        <div className="actions">
          {rangeControl}
          {toolbar}
          {direction}
        </div>
      </div>
      <div className="monitor-filter-controls">{controls}</div>
    </div>
  ) : (
    <div className="filter-bar">
      {search}
      {controls}
      {direction}
    </div>
  );
}
function NodeColumnFilter({
  field,
  state,
  value,
  onChange,
}: {
  field: "group" | "protocol" | "status";
  state: State;
  value: Filters;
  onChange: (v: Filters) => void;
}) {
  const name = { group: "分组", protocol: "协议", status: "状态" }[field];
  const options =
    field === "group"
      ? [
          ...new Set(state.nodes.map((n) => n.subscription || "standalone")),
        ].map((id) => [
          id,
          id === "standalone"
            ? "独立节点"
            : state.native.subscriptions.find((s) => s.id === id)?.name ||
              "未命名订阅",
        ])
      : field === "protocol"
        ? [...new Set(state.nodes.map((n) => n.protocol))].map((id) => [id, id])
        : [
            ["enabled", "已启用"],
            ["disabled", "已停用"],
          ];
  return (
    <ListFilter
      label={"节点" + name + "筛选"}
      active={value[field] !== "all"}
      column
    >
      <Field label={name}>
        <select
          value={value[field]}
          onChange={(e) => onChange({ ...value, [field]: e.target.value })}
        >
          <option value="all">全部{name}</option>
          {options.map(([id, label]) => (
            <option key={id} value={id}>
              {label}
            </option>
          ))}
        </select>
      </Field>
      <Button onClick={() => onChange({ ...value, [field]: "all" })}>
        重置
      </Button>
    </ListFilter>
  );
}
export function ScoreValue({ node }: { node: Node }) {
  const value = node.enabled ? node.score?.score : null;
  return (
    <strong
      className={
        "score " +
        (value == null
          ? "muted"
          : value < 60
            ? "poor"
            : value < 80
              ? "fair"
              : "good")
      }
    >
      {value == null ? "—" : value.toFixed(1) + " / 100"}
    </strong>
  );
}
export function delayText(value: unknown) {
  const n = Number(value);
  return value == null || !Number.isFinite(n)
    ? "—"
    : n < 0
      ? "-1 ms"
      : n >= 1000
        ? (n / 1000).toFixed(1) + " s"
        : n.toFixed(1) + " ms";
}
function NodeScore({
  node,
  detailInfo = false,
}: {
  node: Node;
  detailInfo?: boolean;
}) {
  return (
    <div className="metric score-row score-metric">
      <ScoreValue node={node} />
      <span
        className="muted small metric-label"
        title={node.score?.missing?.join("；")}
      >
        <Award size={16} className="score-icon" />
        {node.enabled ? "综合评分" : "已停用"}
        {detailInfo && (
          <SettingInfo
            label="综合评分"
            text={[
              `已选 ${node.score?.selected?.length || 0} 项 / 可用 ${node.score?.basis?.length || 0} 项`,
              `评分用延迟均值：${node.metrics.latency_average_ms == null ? "未测" : Number(node.metrics.latency_average_ms.toFixed(1)) + " ms"}；近${node.metrics.latency_average_window_minutes ?? 30}分钟，${node.metrics.latency_average_samples || 0}次成功样本`,
              ...(node.score?.missing || []),
            ].join("；")}
          />
        )}
      </span>
    </div>
  );
}
function overviewNode(state: State, node: Node): Node {
  if (!node.active) return node;
  const module = node.id === "warp" ? "warp" : "xray",
    summary = record(record(state.connection_monitor)[module]);
  return {
    ...node,
    metrics: {
      ...node.metrics,
      active_connections: summary.available
        ? Number(summary.count || 0)
        : undefined,
      connections_label:
        (module === "xray" ? "Xray" : "WARP") + " 模块业务连接数",
    },
  };
}
export function NodeCard({
  node,
  actions,
  management = false,
  overview = false,
  showScore = true,
  currentExitIcon = false,
  connectedSince,
  monitors = {},
}: {
  node: Node;
  actions?: React.ReactNode;
  management?: boolean;
  overview?: boolean;
  showScore?: boolean;
  currentExitIcon?: boolean;
  connectedSince?: number;
  showDetails?: boolean;
  monitors?: Dict;
}) {
  const metrics = (
    <div className="node-metrics">
      {showScore && node.id !== "warp" && <NodeScore node={node} detailInfo />}
      {monitors.latency !== false && (
        <Metric
          tone="blue"
          icon={<TrueLatencyIcon size={16} />}
          value={
            (node.metrics.request_success_pct == null
              ? "—"
              : Number(node.metrics.request_success_pct)
                  .toFixed(1)
                  .replace(/\.0$/, "") + "%") +
            " · " +
            (node.metrics.latency_ms == null
              ? "未测"
              : delayText(node.metrics.latency_ms))
          }
          label="真连接检测 · 成功率 / 延迟"
        />
      )}
      {monitors.traffic !== false && (
        <Metric
          tone="orange"
          icon={<Gauge size={16} />}
          value={bandwidth(node.metrics.bandwidth_kib_s)}
          label={"上次测速：" + timestamp(node.metrics.bandwidth_at)}
        />
      )}
      {monitors.connections !== false && (
        <Metric
          tone="purple"
          icon={<ConnectionIcon size={16} />}
          value={
            node.metrics.active_connections == null ? (
              "—"
            ) : overview || management ? (
              String(node.metrics.active_connections)
            ) : (
              <span className="connection-count-value">
                <span>{node.metrics.active_connections}</span>
                <span className="connection-count-protocols">
                  <span>{`\u00a0·\u00a0TCP ${node.metrics.active_connections_tcp ?? "—"}`}</span>
                  <span>{`\u00a0·\u00a0UDP ${node.metrics.active_connections_udp ?? "—"}`}</span>
                </span>
              </span>
            )
          }
          label={node.metrics.connections_label || "活动连接数"}
        />
      )}
    </div>
  );
  return (
    <article
      className={
        "node-card " +
        (node.active ? "active " : "") +
        (management ? "management-card " : "") +
        (overview || management ? "overview-node-card" : "")
      }
      data-node-id={node.id}
    >
      <div className="node-heading">
        <div className="node-name">
          <div className="node-title-line">
            <div className="node-title-copy">
              <h3
                title={node.name}
                className={
                  currentExitIcon ? "current-exit-node-title" : undefined
                }
              >
                {currentExitIcon && (
                  <GatewayIcon
                    size={20}
                    className="current-exit-node-icon"
                    aria-hidden="true"
                  />
                )}
                {node.name}
              </h3>
              <p>
                {node.protocol} · {node.source}
              </p>
            </div>
            <div className="node-title-tools">
              {overview && node.active && (
                <ConnectedTime since={connectedSince} connected />
              )}
              <span className="node-title-meta">
                <span
                  className={
                    "node-title-status " +
                    (!node.enabled
                      ? "muted"
                      : node.active || node.status === "可用"
                        ? "accent"
                        : "poor")
                  }
                >
                  {node.active
                    ? "当前出口"
                    : !node.enabled
                      ? "停用"
                      : node.status}
                </span>
                {node.active && <ExitStatusDot node={node} fault={false} />}
                {node.geo?.country && /^[A-Z]{2}$/.test(node.geo.country) ? (
                  <span
                    className={"fi fi-" + node.geo.country.toLowerCase()}
                    role="img"
                    aria-label={"入口 IP 归属地：" + node.geo.country}
                    title={"入口 IP：" + node.geo.ip + " · " + node.geo.country}
                  />
                ) : (
                  <Globe
                    size={19}
                    className="muted"
                    aria-label="入口 IP 归属地未知"
                  />
                )}
              </span>
              {(management || overview) && actions && (
                <div className="node-card-actions">{actions}</div>
              )}
            </div>
          </div>
        </div>
        {!management && !overview && actions && (
          <div className="actions compact">{actions}</div>
        )}
      </div>
      {overview || management ? (
        <div className="node-metric-surface">{metrics}</div>
      ) : (
        metrics
      )}
    </article>
  );
}
const NodeTrends = memo(function NodeTrends({
  node,
  query,
  rangeLabel,
  summary,
  monitors,
}: {
  node: Node;
  query: string;
  rangeLabel: string;
  summary: Dict;
  monitors: Dict;
}) {
  return (
    <>
      {monitors.latency !== false && (
        <NodeLatencyTrend node={node} query={query} rangeLabel={rangeLabel} />
      )}
      {monitors.connections !== false && (
        <ConnectionTrend node={node} query={query} summary={summary} />
      )}
    </>
  );
});
function NodeLatencyTrend({
  node,
  query,
  rangeLabel,
  kind = "https",
}: {
  node: Node;
  query: string;
  rangeLabel: string;
  kind?: "https" | "icmp";
}) {
  const resource = useResource<{ series: Record<string, Dict[]> }>(
    "/api/latency-history?node=" +
      encodeURIComponent(node.id) +
      "&kind=" +
      kind +
      "&" +
      query,
  );
  return (
    <section className="exit-chart-section home-chart" data-monitor={kind}>
      <div className="chart-heading-row">
        <h3 className="metric-heading tone-blue">
          <TrueLatencyIcon size={18} />
          {kind === "icmp" ? "ICMP延迟" : "真连接延迟"}
        </h3>
        <span className="small muted">
          {rangeLabel}平均延迟：
          <strong className="metric-blue">
            {rangeAverage(resource.data?.series[node.id] || [])}
          </strong>
        </span>
      </div>
      <SimpleLine
        data={resource.data?.series[node.id] || []}
        domain={[
          (resource.data as Dict)?.start as number,
          (resource.data as Dict)?.end as number,
        ]}
        color="var(--metric-blue)"
      />
      <ErrorLine error={resource.error} />
    </section>
  );
}
function ConnectionTrend({
  node,
  query,
  summary,
}: {
  node: Node;
  query: string;
  summary: Dict;
}) {
  const [clock, setClock] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setClock(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const fresh =
    summary.available === true &&
    Number(summary.time) > 0 &&
    clock / 1000 - Number(summary.time) <=
      Math.max(15, Number(summary.stale_after || 15));
  const [protocol, setProtocol] = useState("all");
  const module = node.id === "warp" ? "warp" : "xray";
  const label = module === "xray" ? "Xray" : "WARP";
  const resource = useResource<Dict>(
    "/api/connections/business-history?module=" +
      module +
      "&protocol=" +
      protocol +
      "&" +
      query,
  );
  const count = summary.available
    ? Number(
        (protocol === "all"
          ? summary.count
          : record(summary.protocols)[protocol]) ?? 0,
      )
    : null;
  const formattedCount =
    count == null || !Number.isFinite(count)
      ? "—"
      : count >= 1000
        ? Number((count / 1000).toFixed(1)) + "k"
        : String(count);
  return (
    <section
      className="exit-chart-section home-chart"
      data-monitor="connections"
    >
      <div className="chart-heading-row">
        <div className="chart-title-controls">
          <div className="connection-title-filter">
            <h3 className="metric-heading tone-purple">
              <ConnectionIcon size={18} />
              {label} 模块活动连接数
            </h3>
            <div className="connection-protocol-control chart-inline-control">
              <IconSelect
                showIcon={false}
                label="活动连接曲线协议"
                value={protocol}
                onChange={setProtocol}
                options={[
                  {
                    value: "all",
                    label: "全部连接",
                    icon: <ConnectionIcon size={14} />,
                    tone: "purple",
                  },
                  {
                    value: "tcp",
                    label: "TCP",
                    icon: <ConnectionIcon size={14} />,
                    tone: "blue",
                  },
                  {
                    value: "udp",
                    label: "UDP",
                    icon: <ConnectionIcon size={14} />,
                    tone: "green",
                  },
                ]}
              />
            </div>
          </div>
          <span
            className="connection-current small muted"
            title={
              count == null
                ? "当前连接数不可用"
                : "当前连接数：" + count.toLocaleString("zh-CN")
            }
          >
            <strong
              className="metric-purple"
              aria-label={
                count == null ? "当前连接数不可用" : "当前连接数：" + count
              }
            >
              {formattedCount}
            </strong>
            <span
              className="current-exit-status-dot"
              role="img"
              aria-label={fresh ? "连接数获取正常" : "连接数获取失败或超时"}
              title={fresh ? "连接数获取正常" : "连接数获取失败或超时"}
              style={{ background: fresh ? "#00ff00" : "#ff0000" }}
            />
          </span>
        </div>
      </div>
      <SimpleLine
        data={((resource.data?.items as Dict[]) || []).map((row) => ({
          ...row,
          latency: row.count,
        }))}
        unit="count"
        color="var(--metric-purple)"
        domain={[
          Number(resource.data?.start || Date.now() / 1000 - 86400),
          Number(resource.data?.end || Date.now() / 1000),
        ]}
      />
      <ErrorLine error={resource.error || String(resource.data?.error || "")} />
    </section>
  );
}

function rangeAverage(rows: Dict[]) {
  const valid = rows.filter((r) => r.latency != null && Number(r.latency) >= 0);
  let total = 0,
    count = 0;
  for (const r of valid) {
    const n = Math.max(1, Number(r.count || 1) - Number(r.failures || 0));
    total += Number(r.latency) * n;
    count += n;
  }
  return count ? (total / count).toFixed(1) + " ms" : "未测";
}
export function SimpleLine({
  data,
  percent = false,
  unit = "ms",
  color = "var(--accent)",
  domain,
}: {
  data: Dict[];
  domain?: [number, number];
  percent?: boolean;
  unit?: "ms" | "count";
  color?: string;
}) {
  const rows = data.map((r) => ({
    ...r,
    value: Number(r.latency) >= 0 ? Number(r.latency) : null,
  }));
  return rows.length ? (
    <div className="chart">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={rows} margin={CHART_MARGIN}>
          <CartesianGrid stroke="var(--line)" vertical={false} />
          <XAxis
            type="number"
            dataKey="time"
            domain={
              domain || [
                Number(data[0]?.time),
                Number(data[data.length - 1]?.time),
              ]
            }
            ticks={timeTicks(
              domain || [
                Number(data[0]?.time),
                Number(data[data.length - 1]?.time),
              ],
            )}
            interval={0}
            tickFormatter={formatAxisTime}
            allowDataOverflow={true}
            tick={<TimeAxisTick />}
            tickMargin={6}
            height={28}
          />
          <YAxis
            tick={
              <AlignedAxisTick
                format={(v) =>
                  v + (percent ? "%" : unit === "count" ? "" : " ms")
                }
              />
            }
            width={CHART_AXIS_WIDTH}
            domain={percent ? [0, 100] : [0, "auto"]}
            stroke="var(--chart-axis, #71808c)"
          />
          <Tooltip
            labelFormatter={(v) => timestamp(v)}
            contentStyle={{
              background: "var(--card)",
              border: "1px solid var(--line)",
              borderRadius: "var(--radius)",
            }}
          />
          <Line
            dataKey="value"
            name={
              percent ? "成功率 %" : unit === "count" ? "活动连接数" : "延迟 ms"
            }
            stroke={color}
            strokeWidth={1.8}
            dot={false}
            activeDot={{ r: 3 }}
            connectNulls={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  ) : (
    <div className="chart">
      <Empty>
        {unit === "count"
          ? "所选范围暂无活动连接数记录"
          : "所选范围暂无检测数据"}
      </Empty>
    </div>
  );
}
function ExitStatusDot({ node, fault }: { node?: Node; fault: boolean }) {
  const broken =
    fault ||
    !node ||
    !node.enabled ||
    /故障|不可用|失败|异常|断开/.test(node.status);
  const score = node?.score?.score;
  const state = broken
    ? { color: "#ff5b6e", text: "故障" }
    : score == null || !Number.isFinite(score)
      ? { color: "var(--muted)", text: "评分未生成" }
      : score >= 90
        ? { color: "#00ff00", text: "良好" }
        : score >= 60
          ? { color: "#e5c454", text: "一般" }
          : { color: "#ff983b", text: "较差" };
  const label = `当前出口状态：${state.text}${!broken && score != null ? `，评分 ${score}` : ""}`;
  return (
    <span
      className="current-exit-status-dot"
      role="img"
      aria-label={label}
      title={label}
      style={{ background: state.color }}
    />
  );
}
function ExitUptime({ since }: { since?: number }) {
  const [clock, setClock] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const timer = setInterval(() => setClock(Date.now() / 1000), 1000);
    return () => clearInterval(timer);
  }, []);
  if (!since) return <span className="small muted">运行时间未记录</span>;
  const seconds = Math.max(0, Math.floor(clock - since));
  return (
    <span
      className="small muted exit-uptime"
      title={"上次实际切换：" + timestamp(since)}
    >
      已运行 {Math.floor(seconds / 86400)} 天{" "}
      {Math.floor((seconds % 86400) / 3600)} 小时{" "}
      {Math.floor((seconds % 3600) / 60)} 分钟 {seconds % 60} 秒
    </span>
  );
}
type NodeTableSort = { key: string; direction: number };
export function sortOverviewNodes(nodes: Node[], sort: NodeTableSort | null) {
  if (!sort) return nodes;
  const value = (node: Node): string | number | null | undefined => {
    switch (sort.key) {
      case "name":
        return node.name;
      case "group":
        return node.subscription ? "订阅节点" : "独立节点";
      case "protocol":
        return node.protocol;
      case "status":
        return node.active ? "当前出口" : node.status;
      case "score":
        return node.enabled ? node.score?.score : null;
      case "latency":
        return node.metrics.latency_ms != null && node.metrics.latency_ms >= 0
          ? node.metrics.latency_ms
          : null;
      case "speed":
        return node.metrics.bandwidth_kib_s != null &&
          node.metrics.bandwidth_kib_s >= 0
          ? node.metrics.bandwidth_kib_s
          : null;
      default:
        return null;
    }
  };
  return nodes
    .map((node, index) => ({ node, index }))
    .sort((a, b) => {
      const x = value(a.node),
        y = value(b.node);
      if (x == null || y == null)
        return x == null ? (y == null ? a.index - b.index : 1) : -1;
      const compared =
        typeof x === "number" && typeof y === "number"
          ? x - y
          : String(x).localeCompare(String(y), "zh-CN");
      return compared * sort.direction || a.index - b.index;
    })
    .map((row) => row.node);
}
function RandomThemeButton({
  failed,
  localOnly,
  onClick,
}: {
  failed: boolean;
  localOnly?: boolean;
  onClick: () => void;
}) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <div
      className="overview-random-button"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.stopPropagation();
          setOpen(false);
        }
      }}
    >
      <button
        type="button"
        className="button"
        aria-label="随机主题"
        data-control-icon="true"
        aria-describedby={open ? id : undefined}
        onClick={onClick}
      >
        <RandomThemeIcon size={18} />
      </button>
      {open && (
        <div id={id} role="tooltip" className="overview-random-help">
          <strong>随机主题</strong>
          <span>
            {localOnly ? "只读预览：随机效果仅在当前页面试用，不自动保存；可恢复原布局，刷新后恢复正式设置。" : failed
              ? "保存失败，已保留恢复操作；可恢复随机前布局，或再次随机重新计时。"
              : "随机切换卡片排列和各模块样式。1分钟无新随机操作将自动保存当前主页布局，恢复按钮随之消失；连续点击随机会重新计时。恢复可撤销本轮随机。"}
          </span>
        </div>
      )}
    </div>
  );
}

export function Home({
  state,
  page = "dashboard",
  act,
  navigation,
  preview = false,
  onLayout,
  randomTheme,
}: {
  state: State;
  page?: OverviewView;
  act?: Act;
  navigation?: React.ReactNode;
  preview?: boolean;
  onLayout?: (view: OverviewView) => void;
  randomTheme?: HomeRandomTheme;
}) {
  const [tableView, setTableView] = useState(false);
  const [tableSort, setTableSort] = useState<NodeTableSort | null>(null);
  const [status, setStatus] = useState("all");
  const [editing, setEditing] = useState(false),
    [editDraft, setEditDraft] = useState<Dict>({}),
    [savingLayout, setSavingLayout] = useState(false);
  const editOriginal = useRef<Record<string, string[]>>({});
  const layoutDirty = Object.keys(editDraft).length > 0;
  useEffect(() => {
    editOriginal.current = {};
    setEditing(false);
    setEditDraft({});
  }, [page]);
  const presentation = {
    ...record(state.manager_settings.presentation),
    ...editDraft,
  };
  useEffect(() => {
    if (!preview && page === "services") randomTheme?.register(presentation);
  });
  const timeRange = useTimeRange(rangeHours(presentation, true));
  const { cards: shown, layout } = overviewPolicy(presentation, page);
  const servicesView = page === "services";
  const current = state.nodes.find((n) => n.active),
    enabled = state.nodes.filter((n) => n.id !== "warp" && n.enabled).length,
    nodes = filterNodes(state, { ...filterDefaults, status }).filter(
      (n) => n.id !== "warp",
    );
  const module = current?.id === "warp" ? "warp" : "xray";
  const monitors = {
    ...record(record(state.monitoring_items)[module]),
    ...(record(record(state.monitoring_items)._modules)[module] === false
      ? { traffic: false, latency: false, connections: false }
      : {}),
  };
  const chartMonitors = { ...monitors };
  const visibleCards = shown.filter(
    (id) =>
      id.startsWith("service-") ||
      id === "info" ||
      (id === "traffic" && monitorEnabled(state, module, "traffic")) ||
      ((id === "https" || id === "icmp") &&
        current &&
        monitors.latency !== false) ||
      (id === "connections" && current && monitors.connections !== false),
  );
  if (!shown.includes("https")) chartMonitors.latency = false;
  if (!shown.includes("connections")) chartMonitors.connections = false;
  const summary = record(record(state.connection_monitor)[module]);
  const currentCard = current ? overviewNode(state, current) : null;
  const groups = useMemo(() => {
    const map = new Map<string, Node[]>();
    for (const node of nodes) {
      const group = groupName(node, state);
      map.set(group, [...(map.get(group) || []), node]);
    }
    return [...map.entries()].sort(
      (a, b) => Number(b[0] === "独立节点") - Number(a[0] === "独立节点"),
    );
  }, [nodes, state]);
  const allOrderedNodes = groups
    .flatMap(([, members]) => members)
    .sort((a, b) => {
      const ai = shown.indexOf(a.id),
        bi = shown.indexOf(b.id);
      return (
        (ai < 0 ? Number.MAX_SAFE_INTEGER : ai) -
        (bi < 0 ? Number.MAX_SAFE_INTEGER : bi)
      );
    });
  const selectedNodes = Array.isArray(presentation.nodes_visible_ids)
    ? (presentation.nodes_visible_ids as string[])
    : [];
  const orderedNodes = allOrderedNodes
    .filter(
      (n) =>
        presentation.nodes_visibility_mode !== "selected" ||
        selectedNodes.includes(n.id),
    )
    .slice(
      0,
      Number(presentation.nodes_limit) > 0
        ? Number(presentation.nodes_limit)
        : undefined,
    );

  const move = useCallback(
    (scope: string, from: string, to: string, ids: string[]) => {
      const prefix = page === "services" ? "services_" : "overview_";
      const key = ["host", "xray", "warp"].includes(scope)
        ? "services_" + scope + "_order"
        : scope === "wireguard"
          ? "services_wireguard_interfaces"
          : scope === "nodes"
            ? "nodes_order"
            : prefix + "cards";
      setEditDraft((old) => {
        const base = (old[key] ||
          record(state.manager_settings.presentation)[key]) as
          | string[]
          | undefined;
        const order = [...new Set([...(base || []), ...ids])];
        const a = order.indexOf(from),
          b = order.indexOf(to);
        if (a < 0 || b < 0) return old;
        editOriginal.current[key] ??= [...order];
        [order[a], order[b]] = [order[b], order[a]];
        const next = { ...old };
        if (JSON.stringify(order) === JSON.stringify(editOriginal.current[key]))
          delete next[key];
        else next[key] = order;
        return next;
      });
    },
    [page, state.manager_settings.presentation],
  );
  const editRoot = useOverviewDrag(
    editing && page !== "nodes" && !savingLayout,
    page === "nodes" ? (layout === "single" ? "single" : "grid") : layout,
    move,
  );
  const [viewportCapacity,setViewportCapacity] = useState(6);
  useLayoutEffect(() => {
    if (preview || page !== "nodes") return;
    const panel = editRoot.current?.querySelector<HTMLElement>(".dashboard-nodes");
    if (!panel) return;
    const measure = () => {
      const columns = tableView || layout === "single" || panel.clientWidth < 680 ? 1 : 2;
      panel.style.setProperty("--home-node-columns", String(columns));
      const css=getComputedStyle(panel),footer=panel.querySelector<HTMLElement>(".resource-pagination");
      const viewport=panel.querySelector<HTMLElement>(".paged-overview,.overview-node-table>.table-wrap");
      const usable=Math.max(50,viewport?.clientHeight||panel.clientHeight-parseFloat(css.paddingTop)-parseFloat(css.paddingBottom)-(footer?.offsetHeight||44)-16);
      const head=panel.querySelector("thead")?.getBoundingClientRect().height||40;
      const count=tableView?Math.max(1,Math.floor((usable-head)/72)):Math.max(1,Math.floor((usable+10)/230))*columns;
      setViewportCapacity(old=>old===count?old:Math.min(100,count));
      const rowCount=Math.max(1,Math.floor((usable+10)/230));
      panel.style.setProperty("--home-node-card-height",Math.min(230,Math.floor((usable-10*(rowCount-1))/rowCount))+"px");
    };
    const observer = new ResizeObserver(measure);
    observer.observe(panel);
    const footer = panel.querySelector(".resource-pagination");
    if (footer) observer.observe(footer);
    measure();
    return () => observer.disconnect();
  }, [page, preview, tableView, layout, editRoot]);
  const editControls =
    page !== "nodes" &&
    (act || preview) &&
    (editing ? (
      <>
        <Button
          title="取消"
          disabled={savingLayout}
          onClick={() => {
            setEditing(false);
            setEditDraft({});
          }}
        >
          <X size={18} />
        </Button>
        <Button
          title="保存"
          variant={layoutDirty ? "primary" : undefined}
          highlighted={layoutDirty}
          disabled={savingLayout || !layoutDirty}
          onClick={async () => {
            setSavingLayout(true);
            try {
              if (
                !Object.keys(editDraft).length ||
                (randomTheme?.pending
                  ? await randomTheme.saveNow(editDraft)
                  : act &&
                    (await act("/api/monitoring/presentation", editDraft)))
              ) {
                setEditing(false);
                setEditDraft({});
              }
            } finally {
              setSavingLayout(false);
            }
          }}
        >
          <Save size={18} />
        </Button>
      </>
    ) : (
      <Button
        aria-label="编辑样式"
        title="编辑样式"
        onClick={() => {
          editOriginal.current = {};
          setEditing(true);
          setEditDraft({});
        }}
      >
        <Pencil size={18} />
      </Button>
    ));
  return (
    <div
      ref={editRoot}
      className={
        "overview-home" +
        (page === "nodes" ? " nodes-default-view" : "") +
        (editing ? " overview-editing" : "")
      }
      style={{ display: "contents" }}
    >
      {!preview && (
        <HomeTools
          fullWidth={Number(presentation.content_width_percent || 80) === 100}
          view={page}
        >
          {navigation}
          {act && (
            <Button
              aria-label="主题样式"
              title="主题样式"
              onClick={() => {
                onLayout?.(page);
              }}
            >
              <ThemePaletteIcon size={18} />
            </Button>
          )}
          {servicesView && act && randomTheme && (
            <div
              className="overview-random-theme-tools"
              data-random-deadline={randomTheme.deadline || undefined}
            >
              <RandomThemeButton
                failed={randomTheme.failed}
                localOnly={randomTheme.localOnly}
                onClick={() => {
                  randomTheme.shuffle(presentation);
                  setEditing(false);
                  setEditDraft({});
                  editOriginal.current = {};
                }}
              />
              {randomTheme.pending && (
                <div className="overview-random-restore">
                  <Button
                    title="恢复随机前布局"
                    onClick={() => {
                      setEditing(false);
                      setEditDraft({});
                      editOriginal.current = {};
                      randomTheme.restore();
                    }}
                  >
                    <RestoreThemeIcon size={18} />
                  </Button>
                </div>
              )}
            </div>
          )}
          <div className="overview-edit-controls">{editControls}</div>
          {page === "dashboard" && <OverviewTimeControl range={timeRange} />}
          {page === "nodes" && (
            <>
              <Button
                title={tableView ? "切换宫格" : "切换列表"}
                onClick={() => setTableView(!tableView)}
              >
                {tableView ? <LayoutGrid size={18} /> : <List size={18} />}
              </Button>
              <div className="overview-node-filter">
                <IconSelect
                  label="主页节点筛选"
                  value={status}
                  onChange={setStatus}
                  options={[
                    {
                      value: "enabled",
                      label: `启用节点 · ${enabled}`,
                      icon: <CheckCircle2 size={18} />,
                      tone: "green",
                    },
                    {
                      value: "disabled",
                      label: `停用节点 · ${state.nodes.filter((n) => n.id !== "warp").length - enabled}`,
                      icon: <XCircle size={18} />,
                      tone: "orange",
                    },
                    {
                      value: "all",
                      label: "全部节点",
                      icon: <AllNodesIcon size={18} />,
                      tone: "blue",
                    },
                  ]}
                />
              </div>
            </>
          )}
        </HomeTools>
      )}
      {(page === "dashboard" || servicesView) && (
        <Panel className="dashboard-current">
          {state.state.recovery_issue && (
            <p className="hint poor">
              {state.state.recovery_issue}；请在 Xray
              设置检查接管状态，详情见操作日志。
            </p>
          )}
          {state.config.interval === 0 && (
            <p className="hint">检测间隔为 0：周期检测与自动选优已关闭。</p>
          )}
          <div
            className={`dashboard-exit-grid overview-layout-${layout}`}
            data-overview-anchor={visibleCards[0]}
            data-card-count={visibleCards.length}
            style={
              {
                ...Object.fromEntries(
                  shown.map((id, i) => ["--order-" + id, i]),
                ),
                "--overview-secondary-count": Math.max(
                  1,
                  visibleCards.length - 1,
                ),
              } as React.CSSProperties
            }
          >
            {!visibleCards.length && (
              <Empty>尚未选择卡片，可在布局中添加。</Empty>
            )}
            {shown.some((id) => id.startsWith("service-")) && (
              <ServiceOverviewCards
                state={{
                  ...state,
                  manager_settings: { ...state.manager_settings, presentation },
                }}
                cards={shown.filter((id) => id.startsWith("service-"))}
              />
            )}
            {shown.includes("info") && (
              <section className="dashboard-node-info" data-monitor="info">
                <h3 className="metric-heading current-exit-heading">
                  <GatewayIcon size={18} />
                  当前出口
                  <ExitUptime since={state.state.active_since} />
                  <ExitStatusDot
                    node={current}
                    fault={!!state.state.recovery_issue}
                  />
                </h3>
                {current ? (
                  <>
                    <NodeCard
                      node={currentCard!}
                      connectedSince={state.state.active_since}
                      monitors={monitors}
                      overview
                      management
                      showDetails
                      currentExitIcon
                      actions={
                        act ? (
                          <OverviewActions state={state} act={act} />
                        ) : undefined
                      }
                    />
                  </>
                ) : (
                  <>
                    <Empty>代理已断开，请选择节点恢复连接</Empty>
                    {act && <OverviewActions state={state} act={act} />}
                  </>
                )}
              </section>
            )}
            {shown.includes("traffic") &&
              monitorEnabled(state, module, "traffic") && (
                <CurrentExitTraffic state={state} range={timeRange} />
              )}
            {current &&
              shown.includes("icmp") &&
              monitors.latency !== false && (
                <NodeLatencyTrend
                  node={current}
                  query={timeRange.query}
                  rangeLabel={timeRange.label}
                  kind="icmp"
                />
              )}
            {current && (
              <NodeTrends
                node={current}
                query={timeRange.query}
                rangeLabel={timeRange.label}
                summary={summary}
                monitors={chartMonitors}
              />
            )}
          </div>
        </Panel>
      )}
      {page === "nodes" && (
        <Panel className="dashboard-nodes">
          <div
            id="overview-node-list"
            className={
              tableView
                ? "overview-node-table"
                : "overview-node-grid nodes-layout-" + layout
            }
            style={
              {
                "--node-secondary-count": Math.max(1, orderedNodes.length - 1),
              } as React.CSSProperties
            }
          >
            {tableView && orderedNodes.length > 0 ? (
              <NodeTable
                nodes={sortOverviewNodes(orderedNodes, tableSort)}
                autoPageSize={preview ? undefined : viewportCapacity}
                sort={tableSort || undefined}
                onSort={(key) =>
                  setTableSort((old) => ({
                    key,
                    direction:
                      old?.key === key
                        ? -old.direction
                        : ["score", "speed"].includes(key)
                          ? -1
                          : 1,
                  }))
                }
                actions={() => null}
                showActions={false}
              />
            ) : (
              <PaginatedCards
                className="paged-overview"
                autoPageSize={preview ? undefined : viewportCapacity}
                resetKey={orderedNodes.map((n) => n.id).join(",")}
              >
                {orderedNodes.map((node) => (
                  <NodeCard
                    key={node.id}
                    node={node.id === current?.id ? currentCard! : node}
                    connectedSince={
                      node.active ? state.state.active_since : undefined
                    }
                    overview
                    monitors={monitors}
                  />
                ))}
              </PaginatedCards>
            )}
            {!orderedNodes.length && <Empty>暂无符合显示设置的节点</Empty>}
          </div>
        </Panel>
      )}
    </div>
  );
}
export function NodeTable({
  nodes,
  actions,
  selection,
  onSelect,
  onSort,
  sort,
  showActions = true,
  headerFilters = {},
  onOpen,
  total,
  selectedCount,
  pageSizeLimit,
  autoPageSize,
}: {
  pageSizeLimit?: number;
  autoPageSize?: number;
  total?: number;
  selectedCount?: number;
  onOpen?: (node: Node) => void;
  headerFilters?: Record<string, React.ReactNode>;
  nodes: Node[];
  actions: (n: Node) => React.ReactNode;
  selection?: Set<string>;
  onSelect?: (id: string, checked: boolean) => void;
  onSort?: (key: string) => void;
  sort?: { key: string; direction: number };
  showActions?: boolean;
}) {
  const label = (key: string, name: string) =>
    onSort ? (
      <span className="table-heading-tools">
        <SortHeading
          label={name}
          direction={sort?.key === key ? sort.direction : undefined}
          onClick={() => onSort(key)}
        />
        {headerFilters[key]}
      </span>
    ) : (
      name
    );
  const columns: Column<Node>[] = [
    ...(selection && onSelect
      ? [
          {
            key: "selected",
            label: "选择",
            heading: (
              <input
                type="checkbox"
                aria-label="全选当前筛选节点"
                ref={(element) => {
                  if (element)
                    element.indeterminate =
                      nodes.some((n) => selection.has(n.id)) &&
                      !nodes.every((n) => selection.has(n.id));
                }}
                checked={
                  nodes.length > 0 && nodes.every((n) => selection.has(n.id))
                }
                onChange={(e) =>
                  nodes.forEach((n) => onSelect(n.id, e.target.checked))
                }
              />
            ),
            render: (n: Node) => (
              <input
                type="checkbox"
                aria-label={"选择 " + n.name}
                checked={selection.has(n.id)}
                onChange={(e) => onSelect(n.id, e.target.checked)}
              />
            ),
          },
        ]
      : []),
    { key: "serial", label: "序号", render: (_, i) => i + 1 },
    {
      key: "name",
      label: "节点",
      className: "node-column-name",
      heading: label("name", "节点"),
      render: (n) =>
        onOpen ? (
          <button
            type="button"
            className="resource-name node-name-full"
            title={n.name}
            onClick={() => onOpen(n)}
          >
            {n.name}
          </button>
        ) : (
          <span className="node-name-full" title={n.name}>
            {n.name}
          </span>
        ),
    },
    {
      key: "group",
      width: "100px",
      label: "分组",
      heading: label("group", "分组"),
      render: (n) => (n.subscription ? "订阅节点" : "独立节点"),
    },
    {
      key: "protocol",
      width: "110px",
      label: "协议",
      heading: label("protocol", "协议"),
      render: (n) => n.protocol,
    },
    {
      key: "status",
      width: "100px",
      label: "状态",
      heading: label("status", "状态"),
      render: (n) => (n.active ? "当前出口" : n.status),
    },
    {
      key: "score",
      width: "126px",
      label: "最新评分",
      heading: label("score", "最新评分"),
      render: (n) => <ScoreValue node={n} />,
    },
    {
      key: "latency",
      width: "132px",
      label: "真连接延迟",
      heading: label("latency", "真连接延迟"),
      render: (n) =>
        n.metrics.latency_ms == null ? "未测" : n.metrics.latency_ms + " ms",
    },
    {
      key: "speed",
      width: "170px",
      label: "最近带宽",
      heading: label("speed", "最近带宽"),
      render: (n) => (
        <>
          {bandwidth(n.metrics.bandwidth_kib_s)}
          <small>{timestamp(n.metrics.bandwidth_at)}</small>
        </>
      ),
    },
    {
      key: "actions",
      label: "操作",
      render: actions,
      className: "node-actions",
    },
  ];
  void label;
  return (
    <Table
      rows={nodes}
      pageSizeLimit={pageSizeLimit}
      autoPageSize={autoPageSize}
      total={total}
      selectedCount={selectedCount}
      columns={
        showActions
          ? columns
          : columns.filter((column) => column.key !== "actions")
      }
      rowKey={(n) => n.id}
    />
  );
}
export function Nodes({
  state,
  act,
  refresh,
}: {
  state: State;
  act: Act;
  refresh: () => Promise<void>;
}) {
  const [filters, setFilters] = useState<Filters>(filterDefaults),
    [view, setView] = useView("xray.nodes", "list"),
    [selection, setSelection] = useState<Set<string>>(new Set()),
    [importing, setImporting] = useState(false),
    [editing, setEditing] = useState<Node | null>(null),
    [config, setConfig] = useState<Dict | null>(null),
    [confirm, setConfirm] = useState<{ op: string; ids: string[] } | null>(
      null,
    );
  const visible = filterNodes(state, filters);
  const selected = [...selection].filter((id) =>
    visible.some((n) => n.id === id),
  );
  const select = (id: string, on: boolean) =>
    setSelection((old) => {
      const next = new Set(old);
      on ? next.add(id) : next.delete(id);
      return next;
    });
  const actions = (n: Node) => (
    <ActionMenu
      label={n.name}
      primary={
        n.id !== "warp"
          ? { label: "编辑", onClick: () => setEditing(n) }
          : undefined
      }
      items={[
        {
          label: "检测",
          items: [
            {
              label: "延迟",
              onClick: () => void act("/api/probe", { ids: [n.id] }),
            },
            {
              label: "测速",
              disabled: state.config.speed_enabled === false,
              onClick: () => void act("/api/speed", { ids: [n.id] }),
            },
          ],
        },
        {
          label: "切换",
          disabled: !n.enabled || n.active,
          onClick: () => setConfirm({ op: "activate", ids: [n.id] }),
        },
        {
          label: n.enabled ? "停用" : "启用",
          onClick: () =>
            setConfirm({ op: n.enabled ? "disable" : "enable", ids: [n.id] }),
        },
        ...(n.id !== "warp"
          ? [
              {
                label: "配置",
                onClick: () => {
                  void api<Dict>("/api/native/view", { id: n.id }).then(
                    setConfig,
                  );
                },
              },
              {
                label: "删除",
                onClick: () => setConfirm({ op: "delete", ids: [n.id] }),
              },
            ]
          : []),
      ]}
    />
  );
  return (
    <Panel className="node-management management-panel">
      <PageHeader
        title="节点管理"
        description="Xray-core 节点管理 · 每个节点独立检测与切换"
      />
      <div className="management-toolbar">
        <Button onClick={() => void refresh()}>刷新</Button>
        <Button variant="primary" onClick={() => setImporting(true)}>
          导入节点
        </Button>
        <Button
          disabled={!selected.length}
          onClick={() => setConfirm({ op: "enable", ids: selected })}
        >
          启用
        </Button>
        <Button
          disabled={!selected.length}
          onClick={() => setConfirm({ op: "disable", ids: selected })}
        >
          停用
        </Button>
        <ActionMenu
          label="所选节点"
          triggerLabel="批量操作"
          items={[
            {
              label: "检测",
              disabled: !selected.length,
              items: [
                {
                  label: "检测延迟",
                  onClick: () => void act("/api/probe", { ids: selected }),
                },
                {
                  label: "检测速度",
                  disabled: state.config.speed_enabled === false,
                  onClick: () => void act("/api/speed", { ids: selected }),
                },
              ],
            },
            {
              label: "删除所选",
              disabled: !selected.length,
              onClick: () => setConfirm({ op: "delete", ids: selected }),
            },
          ]}
        />
        <FilterBar
          state={state}
          value={filters}
          onChange={setFilters}
          compact
          grid={view === "grid"}
          selectionControl={
            <Button
              iconOnly
              ariaLabel={
                visible.length > 0 && visible.every((n) => selection.has(n.id))
                  ? "取消全选"
                  : "全选"
              }
              onClick={() =>
                setSelection(
                  visible.length > 0 &&
                    visible.every((n) => selection.has(n.id))
                    ? new Set()
                    : new Set(visible.map((n) => n.id)),
                )
              }
              title="选择当前筛选范围全部节点，包含其他分页及停用节点"
            >
              <SelectAllIcon />
            </Button>
          }
        />
        <div className="management-view-switch">
          <ResetFilters
            active={
              filters.query !== "" ||
              filters.group !== "all" ||
              filters.protocol !== "all" ||
              filters.status !== "all"
            }
            selectedCount={selected.length}
            summary={[
              filters.query && `搜索：${filters.query}`,
              filters.group !== "all" && `分组：${filters.group}`,
              filters.protocol !== "all" && `协议：${filters.protocol}`,
              filters.status !== "all" && `状态：${filters.status}`,
              `${visible.length} 项`,
            ]
              .filter(Boolean)
              .join(" · ")}
            onClick={() => {
              setSelection(new Set());
              setFilters({
                ...filters,
                query: "",
                group: "all",
                protocol: "all",
                status: "all",
              });
            }}
          />
          <ExportButton
            name="Xray节点"
            rows={
              selected.length
                ? visible.filter((n) => selected.includes(n.id))
                : visible
            }
            columns={[
              ["节点", (n) => n.name],
              ["协议", (n) => n.protocol],
              ["分组", (n) => groupName(n, state)],
              [
                "状态",
                (n) =>
                  n.active ? "当前出口" : n.enabled ? "已启用" : "已停用",
              ],
              ["真连接延迟", (n) => n.metrics?.latency_ms],
              ["最近带宽", (n) => n.metrics?.bandwidth_kib_s],
            ]}
          />
          {["list", "grid"].map((v) => (
            <Button
              key={v}
              variant={view === v ? "primary" : "secondary"}
              highlighted={view === v}
              onClick={() => setView(v)}
            >
              {v === "list" ? "列表" : "宫格"}
            </Button>
          ))}
        </div>
      </div>
      {view === "grid" ? (
        <PaginatedCards
          total={state.nodes.length}
          selected={selected.length}
          resetKey={JSON.stringify(filters)}
        >
          {visible.map((n) => (
            <div
              key={n.id}
              role="checkbox"
              aria-label={"选择 " + n.name}
              aria-checked={selection.has(n.id)}
              tabIndex={0}
              className={
                "selectable-card" + (selection.has(n.id) ? " is-selected" : "")
              }
              onClick={(e) => {
                if (
                  !(e.target as Element).closest(
                    "button,a,input,select,textarea,.setting-info",
                  )
                )
                  select(n.id, !selection.has(n.id));
              }}
              onKeyDown={(e) => {
                if (
                  e.target === e.currentTarget &&
                  (e.key === " " || e.key === "Enter")
                ) {
                  e.preventDefault();
                  select(n.id, !selection.has(n.id));
                }
              }}
            >
              <NodeCard
                node={overviewNode(state, n)}
                connectedSince={n.active ? state.state.active_since : undefined}
                actions={actions(n)}
                management
                overview
              />
            </div>
          ))}
        </PaginatedCards>
      ) : (
        <NodeTable
          total={state.nodes.length}
          selectedCount={selected.length}
          onOpen={setEditing}
          headerFilters={Object.fromEntries(
            (["group", "protocol", "status"] as const).map((key) => [
              key,
              <NodeColumnFilter
                key={key}
                field={key}
                state={state}
                value={filters}
                onChange={setFilters}
              />,
            ]),
          )}
          nodes={visible}
          actions={actions}
          selection={selection}
          onSelect={select}
          sort={{ key: filters.sort, direction: filters.direction }}
          onSort={(key) =>
            setFilters({
              ...filters,
              sort: key,
              direction:
                filters.sort === key
                  ? -filters.direction
                  : ["score", "speed"].includes(key)
                    ? -1
                    : 1,
            })
          }
        />
      )}{" "}
      {importing && (
        <ImportNodes act={act} onClose={() => setImporting(false)} />
      )}{" "}
      {editing && (
        <EditNode node={editing} act={act} onClose={() => setEditing(null)} />
      )}{" "}
      {config && (
        <Modal
          title="节点配置（认证参数已隐藏）"
          copyText={JSON.stringify(config, null, 2)}
          onClose={() => setConfig(null)}
          wide
        >
          <JsonDetails data={config} />
        </Modal>
      )}
      {confirm && (
        <Confirm
          title={
            confirm.op === "delete"
              ? "删除所选节点"
              : confirm.op === "activate"
                ? "切换节点"
                : "批量" + (confirm.op === "enable" ? "启用" : "停用")
          }
          danger={confirm.op === "delete"}
          message={
            <>
              <p>涉及 {confirm.ids.length} 个节点。</p>
              {confirm.ids.map((id) => (
                <p key={id}>{state.nodes.find((n) => n.id === id)?.name}</p>
              ))}
              <p>
                停用项不自动检测或参与评分。当前出口被删除或停用时，先验证回退。
              </p>
            </>
          }
          onClose={() => setConfirm(null)}
          onConfirm={() => {
            const c = confirm;
            setConfirm(null);
            void act(
              c.op === "activate"
                ? "/api/native/activate"
                : "/api/native/batch",
              c.op === "activate"
                ? { id: c.ids[0] }
                : { ids: c.ids.filter((id) => id !== "warp"), operation: c.op },
            );
          }}
        />
      )}
    </Panel>
  );
}
function ImportNodes({ act, onClose }: { act: Act; onClose: () => void }) {
  const [content, setContent] = useState(""),
    [preview, setPreview] = useState<Dict | null>(null),
    [error, setError] = useState("");
  return (
    <Modal
      description={
        <>
          <p>
            支持单节点链接、Base64 订阅正文和 sing-box
            outbounds。认证参数只保存在服务器本地。
          </p>
        </>
      }
      title="导入节点"
      onClose={onClose}
      wide
    >
      <Field label="节点链接或 JSON">
        <textarea
          autoComplete="off"
          rows={9}
          value={content}
          onChange={(e) => {
            setContent(e.target.value);
            setPreview(null);
          }}
        />
      </Field>
      <ErrorLine error={error} />
      {preview && (
        <div className="import-preview">
          <p>
            共 {text(preview.count)} 项，新增 {text(preview.new_count)} 项，冲突{" "}
            {Array.isArray(preview.conflicts) ? preview.conflicts.length : 0}{" "}
            项。
          </p>
          <JsonDetails data={preview.conflicts} />
          <div className="actions">
            <Button
              onClick={() => {
                void act("/api/nodes/import/commit", {
                  id: preview.id,
                  policy: "skip",
                }).then((ok) => {
                  if (ok) {
                    setContent("");
                    onClose();
                  }
                });
              }}
            >
              跳过重复项导入
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                void act("/api/nodes/import/commit", {
                  id: preview.id,
                  policy: "replace",
                }).then((ok) => {
                  if (ok) {
                    setContent("");
                    onClose();
                  }
                });
              }}
            >
              替换冲突项导入
            </Button>
          </div>
        </div>
      )}
      <div className="modal-actions">
        <Button onClick={onClose}>取消</Button>
        <Button
          variant="primary"
          disabled={!content.trim()}
          onClick={() => {
            void api<Dict>("/api/nodes/import/preview", { content })
              .then(setPreview)
              .catch((e) => setError(errorMessage(e)));
          }}
        >
          检查重复与冲突
        </Button>
      </div>
    </Modal>
  );
}
function EditNode({
  node,
  act,
  onClose,
}: {
  node: Node;
  act: Act;
  onClose: () => void;
}) {
  const [name, setName] = useState(node.name),
    [priority, setPriority] = useState(node.priority),
    [outbound, setOutbound] = useState(""),
    [error, setError] = useState("");
  return (
    <Modal title="编辑节点" onClose={onClose} wide>
      <Field label="节点名称">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={80}
        />
      </Field>
      <Field label="优先级">
        <NumberInput
          type="number"
          min={1}
          max={999}
          value={priority}
          onChange={(e) => setPriority(Number(e.target.value))}
        />
      </Field>
      <Button
        onClick={() => {
          void api<Dict>("/api/native/view", { id: node.id })
            .then((data) =>
              setOutbound(JSON.stringify(record(data.node).outbound, null, 2)),
            )
            .catch((e) => setError(errorMessage(e)));
        }}
      >
        查看／编辑连接配置
      </Button>
      {outbound && (
        <Field label="配置 JSON（保留 <redacted> 表示沿用原认证参数）">
          <textarea
            rows={14}
            value={outbound}
            onChange={(e) => setOutbound(e.target.value)}
          />
        </Field>
      )}
      <ErrorLine error={error} />
      <div className="modal-actions">
        <Button onClick={onClose}>取消</Button>
        <Button
          variant="primary"
          onClick={() => {
            let patch: unknown;
            try {
              patch = outbound ? JSON.parse(outbound) : undefined;
            } catch {
              setError("JSON 格式无效");
              return;
            }
            void act("/api/native/node", {
              id: node.id,
              name,
              priority,
              outbound: patch,
            }).then((ok) => {
              if (ok) onClose();
            });
          }}
        >
          保存并应用
        </Button>
      </div>
    </Modal>
  );
}
export function Subscriptions({
  state,
  act,
  refresh,
}: {
  state: State;
  act: Act;
  refresh: () => Promise<void>;
}) {
  const [edit, setEdit] = useState<Dict | null>(null),
    [remove, setRemove] = useState<Dict | null>(null),
    [deleteNodes, setDeleteNodes] = useState(false),
    [view, setView] = useView("xray.subscriptions", "list");
  const [query, setQuery] = useState(""),
    [status, setStatus] = useState("all"),
    [sort, setSort] = useState("name"),
    [descending, setDescending] = useState(false),
    [selected, setSelected] = useState<string[]>([]);
  const subscriptions = state.native.subscriptions;
  const statusText = (s: Subscription) =>
    s.enabled === false
      ? "已停用"
      : s.error
        ? "更新异常"
        : s.updated
          ? "已更新"
          : "待更新";
  const onSort = (key: string) => {
    setDescending(
      sort === key
        ? !descending
        : ["count", "created", "updated"].includes(key),
    );
    setSort(key);
  };
  const sortHeading = (key: string, label: string) => (
    <SortHeading
      label={label}
      direction={sort === key ? (descending ? -1 : 1) : undefined}
      onClick={() => onSort(key)}
    />
  );
  const sortDirection = (key: string): "ascending" | "descending" | "none" =>
    sort === key ? (descending ? "descending" : "ascending") : "none";
  const rows = subscriptions
    .filter(
      (s) =>
        s.name.toLowerCase().includes(query.trim().toLowerCase()) &&
        (status === "all" ||
          (status === "enabled"
            ? s.enabled !== false
            : status === "disabled"
              ? s.enabled === false
              : s.auto_update)),
    )
    .sort((a, b) => {
      const value = (s: Subscription): string | number =>
        sort === "mode"
          ? Number(s.auto_update)
          : sort === "status"
            ? statusText(s)
            : sort === "name"
              ? s.name
              : Number(s[sort as "count" | "created" | "updated"] || 0);
      const av = value(a),
        bv = value(b);
      const compared =
        typeof av === "string" && typeof bv === "string"
          ? av.localeCompare(bv, "zh-CN", { numeric: true })
          : Number(av) - Number(bv);
      return (
        (descending ? -1 : 1) *
        (compared ||
          a.name.localeCompare(b.name, "zh-CN", { numeric: true }) ||
          a.id.localeCompare(b.id))
      );
    });
  const selectedIDs = selected.filter((id) => rows.some((s) => s.id === id));
  const batch = async (operation: string, ids = selectedIDs) => {
    if (
      await act("/api/subscriptions/batch", {
        operation,
        ids,
        conflict_policy: "skip",
      })
    )
      setSelected([]);
  };
  const allSubscriptionsSelected =
    rows.length > 0 && rows.every((s) => selectedIDs.includes(s.id));
  const selectSubscriptions = (checked: boolean) =>
    setSelected(
      checked
        ? [...new Set([...selectedIDs, ...rows.map((s) => s.id)])]
        : selectedIDs.filter((id) => !rows.some((s) => s.id === id)),
    );
  const subscriptionStatusControl = (
    <Field label="状态">
      <select
        aria-label="订阅状态"
        value={status}
        onChange={(e) => setStatus(e.target.value)}
      >
        <option value="all">全部状态</option>
        <option value="enabled">已启用</option>
        <option value="disabled">已停用</option>
        <option value="auto">自动更新</option>
      </select>
    </Field>
  );
  const selection = (s: Subscription, compact = false) => (
    <label className="check">
      <input
        aria-label={"选择订阅 " + s.name}
        type="checkbox"
        checked={selectedIDs.includes(s.id)}
        onChange={(e) =>
          setSelected(
            e.target.checked
              ? [...selectedIDs, s.id]
              : selectedIDs.filter((id) => id !== s.id),
          )
        }
      />
      {!compact && "选择"}
    </label>
  );
  const added = (s: Subscription) =>
    s.created ? timestamp(s.created) : "未记录";
  const updated = (s: Subscription) =>
    s.updated ? timestamp(s.updated) : "尚未成功更新";
  const actions = (s: Subscription) => (
    <div className="actions subscription-actions">
      <RowActions label={s.name}>
        <Button
          disabled={s.enabled === false}
          onClick={() =>
            void act("/api/subscriptions/update", {
              id: s.id,
              conflict_policy: "skip",
            })
          }
        >
          更新
        </Button>
        <Button
          onClick={() =>
            void batch(s.enabled === false ? "enable" : "disable", [s.id])
          }
        >
          {s.enabled === false ? "启用" : "停用"}
        </Button>
        <Button
          disabled={!s.auto_update}
          onClick={() => void batch("stop_updates", [s.id])}
        >
          停止更新
        </Button>
        <Button onClick={() => setEdit({ ...s, url: "" })}>编辑</Button>
        <Button
          variant="danger"
          onClick={() => {
            setDeleteNodes(false);
            setRemove(s as unknown as Dict);
          }}
        >
          删除
        </Button>
      </RowActions>
    </div>
  );
  return (
    <Panel className="subscription-management management-panel">
      <PageHeader
        title="订阅管理"
        description="管理订阅与所属节点；启用、停用同时作用于订阅和节点，停止更新仅关闭自动更新。"
      />
      <div className="management-toolbar subscription-toolbar">
        <Button onClick={() => void refresh()}>刷新</Button>
        <Button
          variant="primary"
          onClick={() =>
            setEdit({ name: "", url: "", interval: 3600, auto_update: false })
          }
        >
          添加订阅
        </Button>
        <Button
          disabled={!selectedIDs.length}
          onClick={() => void batch("enable")}
        >
          启用
        </Button>
        <Button
          disabled={!selectedIDs.length}
          onClick={() => void batch("disable")}
        >
          停用
        </Button>
        <ActionMenu
          label="所选订阅"
          triggerLabel="批量操作"
          items={[
            {
              label: "批量更新",
              disabled: !selectedIDs.length,
              onClick: () => void batch("update"),
            },
            {
              label: "停止更新",
              disabled: !selectedIDs.length,
              onClick: () => void batch("stop_updates"),
            },
            {
              label: "批量删除",
              disabled: !selectedIDs.length,
              onClick: () => {
                setDeleteNodes(false);
                setRemove({
                  ids: selectedIDs,
                  name: `所选 ${selectedIDs.length} 个订阅`,
                  count: subscriptions
                    .filter((s) => selectedIDs.includes(s.id))
                    .reduce((n, s) => n + s.count, 0),
                });
              },
            },
          ]}
        />
        <div className="management-query-tools">
          <AdaptiveSearch>
            <Search size={16} />
            <input
              aria-label="搜索订阅"
              placeholder="查询订阅名称"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </AdaptiveSearch>
          {view === "grid" && (
            <>
              <ListFilter label="订阅筛选" active={status !== "all"}>
                {subscriptionStatusControl}
                <Button onClick={() => setStatus("all")}>重置</Button>
              </ListFilter>
              <ListFilter label="订阅排序" icon={<ArrowUpDown size={16} />}>
                <Field label="排序">
                  <select
                    aria-label="订阅排序"
                    value={sort}
                    onChange={(e) => setSort(e.target.value)}
                  >
                    <option value="name">名称</option>
                    <option value="count">节点数量</option>
                    <option value="created">添加时间</option>
                    <option value="updated">更新时间</option>
                    <option value="mode">更新方式</option>
                    <option value="status">状态</option>
                  </select>
                </Field>
                <Button onClick={() => setDescending(!descending)}>
                  {descending ? "降序" : "升序"}
                </Button>
              </ListFilter>
              <Button
                iconOnly
                ariaLabel={allSubscriptionsSelected ? "取消全选" : "全选"}
                onClick={() =>
                  setSelected(
                    allSubscriptionsSelected
                      ? selectedIDs.filter(
                          (id) => !rows.some((s) => s.id === id),
                        )
                      : [
                          ...new Set([
                            ...selectedIDs,
                            ...rows.map((s) => s.id),
                          ]),
                        ],
                  )
                }
                title="选择当前筛选范围全部订阅，包含其他分页和停用订阅"
              >
                <SelectAllIcon />
              </Button>
            </>
          )}
        </div>
        <div className="management-view-switch">
          <ResetFilters
            active={query !== "" || status !== "all"}
            selectedCount={selectedIDs.length}
            summary={[
              query && `搜索：${query}`,
              status !== "all" && `状态：${status}`,
              `${rows.length} 项`,
            ]
              .filter(Boolean)
              .join(" · ")}
            onClick={() => {
              setSelected([]);
              setQuery("");
              setStatus("all");
            }}
          />
          <ExportButton
            name="Xray订阅"
            rows={
              selectedIDs.length
                ? rows.filter((r) => selectedIDs.includes(r.id))
                : rows
            }
            columns={[
              ["订阅", (r) => r.name],
              ["节点数量", (r) => r.count],
              ["状态", (r) => statusText(r)],
              ["更新方式", (r) => (r.auto_update ? "自动更新" : "手动更新")],
              ["添加时间", (r) => added(r)],
              ["上次更新", (r) => updated(r)],
            ]}
          />
          {[
            ["list", "列表"],
            ["grid", "宫格"],
          ].map(([id, label]) => (
            <Button
              key={id}
              variant={view === id ? "primary" : "secondary"}
              highlighted={view === id}
              onClick={() => setView(id)}
            >
              {label}
            </Button>
          ))}
        </div>
      </div>
      {view === "grid" ? (
        <PaginatedCards
          className="subscription-grid"
          total={subscriptions.length}
          selected={selectedIDs.length}
          resetKey={JSON.stringify([query, status, sort, descending])}
        >
          {rows.map((s) => (
            <article
              className={
                "subscription-card selectable-card" +
                (selectedIDs.includes(s.id) ? " is-selected" : "")
              }
              key={s.id}
              data-subscription-id={s.id}
              role="checkbox"
              aria-label={"选择 " + s.name}
              aria-checked={selectedIDs.includes(s.id)}
              tabIndex={0}
              onClick={(e) => {
                if (
                  !(e.target as Element).closest(
                    "button,a,input,select,textarea,.setting-info",
                  )
                )
                  setSelected(
                    selectedIDs.includes(s.id)
                      ? selected.filter((id) => id !== s.id)
                      : [...selected, s.id],
                  );
              }}
              onKeyDown={(e) => {
                if (
                  e.target === e.currentTarget &&
                  (e.key === " " || e.key === "Enter")
                ) {
                  e.preventDefault();
                  setSelected(
                    selectedIDs.includes(s.id)
                      ? selected.filter((id) => id !== s.id)
                      : [...selected, s.id],
                  );
                }
              }}
            >
              <div className="subscription-card-heading">
                <div className="subscription-title-copy">
                  <h3 title={s.name}>{s.name}</h3>
                </div>
                <span
                  className={
                    "subscription-update-status " +
                    (s.enabled === false
                      ? "muted"
                      : s.error
                        ? "poor"
                        : "accent")
                  }
                >
                  {statusText(s)}
                </span>
                {actions(s)}
              </div>
              <dl className="subscription-meta">
                <div>
                  <dt>节点数量</dt>
                  <dd>{s.count}</dd>
                </div>
                <div>
                  <dt>订阅状态</dt>
                  <dd className="subscription-enabled-status">
                    {s.enabled === false ? "已停用" : "已启用"}
                    {" · "}
                    {s.auto_update ? "自动更新" : "手动更新"}
                  </dd>
                </div>
                <div>
                  <dt>添加时间</dt>
                  <dd>{added(s)}</dd>
                </div>
                <div>
                  <dt>上次成功更新</dt>
                  <dd>{updated(s)}</dd>
                </div>
              </dl>
              {s.error && <ErrorLine error={s.error} />}
            </article>
          ))}
        </PaginatedCards>
      ) : (
        <Table
          total={subscriptions.length}
          selectedCount={selectedIDs.length}
          rows={rows}
          rowKey={(s) => s.id}
          columns={[
            {
              key: "selected",
              label: "选择",
              heading: (
                <input
                  type="checkbox"
                  aria-label="全选筛选订阅"
                  ref={(element) => {
                    if (element)
                      element.indeterminate =
                        selectedIDs.length > 0 && !allSubscriptionsSelected;
                  }}
                  checked={allSubscriptionsSelected}
                  onChange={(e) => selectSubscriptions(e.target.checked)}
                />
              ),
              render: (s) => selection(s, true),
            },
            { key: "serial", label: "序号", render: (_, i) => i + 1 },
            {
              key: "name",
              label: "订阅",
              heading: sortHeading("name", "订阅"),
              sortDirection: sortDirection("name"),
              render: (s) => (
                <button
                  type="button"
                  className="resource-name subscription-name-full"
                  title={s.name}
                  onClick={() => setEdit({ ...s, url: "" })}
                >
                  {s.name}
                </button>
              ),
            },
            {
              key: "count",
              label: "节点数量",
              heading: sortHeading("count", "节点数量"),
              sortDirection: sortDirection("count"),
              render: (s) => s.count,
            },
            {
              key: "mode",
              label: "更新方式",
              heading: sortHeading("mode", "更新方式"),
              sortDirection: sortDirection("mode"),
              render: (s) => (s.auto_update ? "自动更新" : "手动更新"),
            },
            {
              key: "created",
              label: "添加时间",
              heading: sortHeading("created", "添加时间"),
              sortDirection: sortDirection("created"),
              render: added,
            },
            {
              key: "updated",
              label: "上次成功更新",
              heading: sortHeading("updated", "上次成功更新"),
              sortDirection: sortDirection("updated"),
              render: updated,
            },
            {
              key: "status",
              label: "状态",
              heading: (
                <span className="table-heading-tools">
                  {sortHeading("status", "状态")}
                  <ListFilter
                    label="订阅状态筛选"
                    column
                    active={status !== "all"}
                  >
                    {subscriptionStatusControl}
                    <Button onClick={() => setStatus("all")}>重置</Button>
                  </ListFilter>
                </span>
              ),
              sortDirection: sortDirection("status"),
              render: (s) => (
                <span className={s.error ? "poor" : "muted"} title={s.error}>
                  {statusText(s)}
                </span>
              ),
            },
            {
              key: "actions",
              label: "操作",
              className: "subscription-actions-cell",
              render: actions,
            },
          ]}
        />
      )}
      {!rows.length && view === "grid" && (
        <Empty>
          {subscriptions.length
            ? "没有匹配的订阅。"
            : "暂无订阅，可添加 HTTPS 订阅地址。"}
        </Empty>
      )}
      {edit && (
        <Modal
          title={edit.id ? "编辑订阅" : "添加订阅"}
          onClose={() => setEdit(null)}
        >
          <Field label="名称">
            <input
              value={String(edit.name)}
              onChange={(e) => setEdit({ ...edit, name: e.target.value })}
            />
          </Field>
          <Field
            label={edit.id ? "订阅地址（留空保留原地址）" : "HTTPS 订阅地址"}
          >
            <input
              autoComplete="off"
              type="url"
              value={String(edit.url)}
              onChange={(e) => setEdit({ ...edit, url: e.target.value })}
            />
          </Field>
          <label className="check">
            <input
              type="checkbox"
              checked={!!edit.auto_update}
              onChange={(e) =>
                setEdit({ ...edit, auto_update: e.target.checked })
              }
            />
            自动更新
          </label>
          <Field label="更新间隔（秒）">
            <NumberInput
              type="number"
              min={300}
              max={86400}
              value={Number(edit.interval)}
              onChange={(e) =>
                setEdit({ ...edit, interval: Number(e.target.value) })
              }
            />
          </Field>
          <div className="modal-actions">
            <Button onClick={() => setEdit(null)}>取消</Button>
            <Button
              variant="primary"
              onClick={() => {
                void act("/api/native/subscription", edit).then((ok) => {
                  if (ok) setEdit(null);
                });
              }}
            >
              保存
            </Button>
          </div>
        </Modal>
      )}
      {remove && (
        <Modal title="删除订阅" onClose={() => setRemove(null)}>
          <p>删除“{String(remove.name)}”？默认保留已有节点并转为独立节点。</p>
          <label className="check">
            <input
              type="checkbox"
              checked={deleteNodes}
              onChange={(e) => setDeleteNodes(e.target.checked)}
            />
            同时删除订阅包含的 {String(remove.count)} 个节点
          </label>
          <div className="modal-actions">
            <Button onClick={() => setRemove(null)}>取消</Button>
            <Button
              variant="danger"
              onClick={() => {
                void act("/api/subscriptions/batch", {
                  ids: remove.ids || [remove.id],
                  operation: "delete",
                  delete_nodes: deleteNodes,
                }).then((ok) => {
                  if (ok) {
                    setRemove(null);
                    setSelected([]);
                  }
                });
              }}
            >
              删除订阅
            </Button>
          </div>
        </Modal>
      )}
    </Panel>
  );
}
export type ConfigUpdater = (value: Config) => void;

function OverviewActions({ state, act }: { state: State; act: Act }) {
  const [switching, setSwitching] = useState(false),
    [disconnect, setDisconnect] = useState(false);
  const current = state.nodes.find((n) => n.active);
  const nodes = state.nodes
    .filter((n) => n.id !== "warp" && n.enabled)
    .sort((a, b) => (b.score?.score ?? -1) - (a.score?.score ?? -1));
  return (
    <>
      <ActionMenu
        label="当前节点"
        triggerLabel="操作"
        items={[
          {
            label: "延迟",
            disabled: !current,
            onClick: () => void act("/api/probe", { ids: [current!.id] }),
          },
          {
            label: "测速",
            disabled: !current || state.config.speed_enabled === false,
            onClick: () => void act("/api/speed", { ids: [current!.id] }),
          },
          { label: "切换", onClick: () => setSwitching(true) },
          {
            label: "断开",
            disabled: !current,
            onClick: () => setDisconnect(true),
          },
        ]}
      />
      {switching && (
        <Modal title="切换出口" onClose={() => setSwitching(false)}>
          <div className="exit-choices">
            {nodes.map((n) => (
              <div key={n.id} className="exit-choice">
                <span title={n.name}>{n.name}</span>
                <ScoreValue node={n} />
                <Button
                  disabled={n.active}
                  onClick={() => {
                    setSwitching(false);
                    void act("/api/native/activate", { id: n.id });
                  }}
                >
                  {n.active ? "当前出口" : "切换"}
                </Button>
              </div>
            ))}
          </div>
        </Modal>
      )}
      {disconnect && (
        <Confirm
          title="临时断开代理"
          message="只断开当前模块的代理连接，保留节点与配置；其他模块和 VPN 保持运行。Xray 断开后，系统可使用已连接的 WARP。已有代理连接会中断。"
          onClose={() => setDisconnect(false)}
          onConfirm={() => {
            setDisconnect(false);
            void act("/api/proxy/pause", {});
          }}
        />
      )}
    </>
  );
}
