import { ProductIcon } from "./product_icons";
import { XrayObservationPolicy, DEFAULT_DIRECT_DNS, DEFAULT_PROXY_DNS } from "./xray_observation_policy";
import { ResetFilters } from "./list_controls";
import { MarkAllReadButton, ReadBadge, logReadKeys, switchReadKeys, useReadState } from "./read_state";
import { hasChanges } from "./settings_save";
import { PaginatedCards } from "./resource_controls";
import { NodeManagementIcon } from "./requirement_icons";
import { TrueLatencyIcon } from "./requirement_icons";
import { useMonitorSections } from "./monitor_sections";
import { PageWidthSettings } from "./presentation_settings";
import {
  HistoryIntervalSettings,
  type HistorySettingsActions,
} from "./history_interval_settings";
import { VPNExclusionsEditor } from "./vpn_exclusions";
import {
  useDiagnosticRunner,
  DiagnosticProgress,
  DiagnosticReport,
} from "./diagnostic_result";
import { LogRetentionSettings } from "./log_settings";
import { NumberInput } from "./ui";
import { labelIcon } from "./ui_icons";
import { VersionPanel, versionLabel } from "./version_panel";
import { DisplaySettings } from "./monitoring_settings";
import { useTimeRange, TimeRangeControl, type TimeRange } from "./time_range";
import { TimeAxisTick, timeTicks, formatAxisTime } from "./chart_layout";
import { ModuleSettings } from "./modules";
import { RuleSetsEditor } from "./custom_rules";
import { SaveCard, SettingsSaveProvider } from "./settings_save";
import {
  AlignedAxisTick,
  CHART_AXIS_WIDTH,
  CHART_MARGIN,
} from "./chart_layout";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  LineChart,
  Line,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import {
  RefreshCw,
  Activity,
  Timer,
  Radio,
  CircleCheck,
  CircleMinus,
} from "lucide-react";
import {
  api,
  errorMessage,
  list,
  record,
  text,
  timestamp,
  type Config,
  type Dict,
  type Operation,
  type State,
} from "./api";
import {
  Button,
  AddressInput,
  Confirm,
  Empty,
  ErrorLine,
  Field,
  JsonDetails,
  Modal,
  PageHeader,
  SettingsSection,
  SettingInfo,
  Panel,
  Table,
  useResource,
  type Column,
} from "./ui";
import {
  filterDefaults,
  filterNodes,
  FilterBar,
  type Act,
  type Filters,
} from "./nodes";
function RecentDots({ rows, kind }: { rows: Dict[]; kind: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [capacity, setCapacity] = useState(0);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    const measure = () => {
      const style = getComputedStyle(element);
      const size = parseFloat(style.getPropertyValue("--dot-width")) || 10;
      const gap = parseFloat(style.columnGap) || 2;
      setCapacity(
        Math.max(
          0,
          Math.min(120, Math.floor((element.clientWidth + gap) / (size + gap))),
        ),
      );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  return (
    <div ref={ref} className="dots" aria-label="最近检测记录">
      {(capacity ? rows.slice(-capacity) : []).map((r, i) => (
        <span
          key={String(r.time) + "-" + i}
          className={
            "dot " +
            ((
              kind === "https"
                ? r.ok
                : r.latency != null && Number(r.latency) >= 0
            )
              ? ""
              : "bad")
          }
          title={timestamp(r.time) + " · " + text(r.latency) + " ms"}
        />
      ))}
    </div>
  );
}
export function History({
  state,
  act,
  module: selectedModule,
  range,
  viewControl,
}: {
  state: State;
  act: Act;
  module?: string;
  range?: TimeRange;
  viewControl?: React.ReactNode;
}) {
  const module =
    selectedModule ||
    (state.nodes.some((n) => n.id === "warp") ? "warp" : "xray");
  const isWarp = module === "warp",
    isWireGuard = module === "wireguard";
  const localRange = useTimeRange();
  const timeRange = range || localRange;
  const [filters, setFilters] = useState<Filters>({
      ...filterDefaults,
      sort: module !== "xray" ? "latency" : filterDefaults.sort,
      status: module !== "xray" ? "all" : "enabled",
    }),
    [kind, setKind] = useState(isWireGuard ? "icmp" : "https"),
    [target, setTarget] = useState("all");

  const query =
    "/api/latency-history?module=" +
    module +
    "&kind=" +
    (kind === "https" ? "https" : "icmp") +
    "&" +
    timeRange.query;
  const { data, error, refresh } = useResource<{
    series: Record<string, Dict[]>;
    start: number;
    end: number;
  }>(query, 10000);
  const filterState =
    isWireGuard && data
      ? {
          ...state,
          nodes: state.nodes.map((n) => {
            const last = data.series[n.id]?.at(-1);
            return {
              ...n,
              metrics: {
                ...n.metrics,
                latency_ms:
                  last && Number(last.latency) >= 0
                    ? Number(last.latency)
                    : null,
              },
            };
          }),
        }
      : state;
  const nodes = filterNodes(filterState, filters).filter(
    (n) => target === "all" || n.id === target,
  );
  const domain: [number, number] = [
    Number(data?.start || Date.now() / 1000 - 86400),
    Number(data?.end || Date.now() / 1000),
  ];
  const chart = nodes
    .flatMap((n) => data?.series[n.id] || [])
    .map((r) => ({ time: r.time }))
    .sort((a, b) => Number(a.time) - Number(b.time));
  const colors = [
    "#00c99a",
    "#8b87ff",
    "#f6b65a",
    "#62b9ff",
    "#f082a9",
    "#bd99ec",
    "#9cc956",
    "#e2906d",
    "#38c5cd",
    "#da83cb",
  ];
  const layout = useMonitorSections(module, "latency", act, {
    trend: {
      kind: "line",
      title: kind === "https" ? "真连接延迟" : "ICMP 延迟",
      series: nodes.map((node) => node.name),
      colors,
    },
  });
  return (
    <>
      <Panel className="latency-controls">
        <PageHeader
          actions={layout.control}
          title="延迟监控"
          titleControl={viewControl}
          icon={<Activity size={20} />}
          tone="blue"
          description="下方最近检测与多节点曲线共用指标、查询、筛选、排序和时间范围"
        />
        <FilterBar
          state={state}
          value={filters}
          onChange={setFilters}
          sortOptions={
            module !== "xray"
              ? [
                  ["latency", isWireGuard ? "ICMP 延迟" : "真连接延迟"],
                  ["name", "名称"],
                ]
              : undefined
          }
          monitor
          interfaceMode={isWireGuard}
          rangeControl={<TimeRangeControl range={timeRange} />}
          refreshControl={
            <Button onClick={() => void refresh()}>
              <RefreshCw size={14} /> 刷新
            </Button>
          }
          toolbar={
            <>
              {" "}
              <ResetFilters
                active={
                  !!filters.query ||
                  filters.group !== "all" ||
                  filters.protocol !== "all" ||
                  filters.status !== (module === "xray" ? "enabled" : "all") ||
                  target !== "all" ||
                  timeRange.isChanged
                }
                summary={[
                  filters.query && `搜索：${filters.query}`,
                  `分组：${filters.group}`,
                  `协议：${filters.protocol}`,
                  `状态：${filters.status}`,
                  timeRange.label,
                ]
                  .filter(Boolean)
                  .join(" · ")}
                onClick={() => {
                  setFilters({
                    ...filterDefaults,
                    status: module !== "xray" ? "all" : "enabled",
                    sort: module !== "xray" ? "latency" : filterDefaults.sort,
                  });
                  setTarget("all");
                  timeRange.reset();
                }}
              />
            </>
          }
        >
          <Field label={isWireGuard ? "接口" : "节点"}>
            <select value={target} onChange={(e) => setTarget(e.target.value)}>
              <option value="all">
                {isWireGuard ? "全部接口" : "全部节点"}
              </option>
              {filterNodes(filterState, filters).map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="指标">
            <select value={kind} onChange={(e) => setKind(e.target.value)}>
              {!isWireGuard && <option value="https">真连接延迟</option>}
              {!isWarp && (
                <>
                  <option value="icmp">ICMP 延迟</option>
                </>
              )}
            </select>
          </Field>
        </FilterBar>

        <ErrorLine error={error} />
      </Panel>
      {layout.render({
        trend: (
          <Panel>
            <PageHeader
              title={kind === "https" ? "真连接延迟" : "ICMP 延迟"}
              icon={
                kind === "https" ? (
                  <TrueLatencyIcon size={20} />
                ) : (
                  <Radio size={20} />
                )
              }
              tone={kind === "https" ? "blue" : "purple"}
              titleExtra={
                <div className="inline-metrics chart-inline-stats">
                  <span>
                    <NodeManagementIcon size={14} className="traffic-total" />
                    可读取{isWireGuard ? "接口" : "节点"}{" "}
                    <strong>
                      {
                        nodes.filter(
                          (n) => (data?.series[n.id] || []).length > 0,
                        ).length
                      }{" "}
                      / {nodes.length}
                    </strong>
                  </span>
                </div>
              }
              description="延迟检测失败保留断点，不连线伪造数据"
            />
            {chart.length ? (
              <div className="large-chart">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={chart} margin={CHART_MARGIN}>
                    <CartesianGrid stroke="var(--line)" vertical={false} />
                    <XAxis
                      type="number"
                      dataKey="time"
                      domain={domain}
                      ticks={timeTicks(domain)}
                      interval={0}
                      tickFormatter={formatAxisTime}
                      allowDataOverflow={true}
                      tick={<TimeAxisTick />}
                      tickMargin={6}
                      height={28}
                    />
                    <YAxis
                      tick={<AlignedAxisTick format={(v) => v + " ms"} />}
                      width={CHART_AXIS_WIDTH}
                      domain={[0, "auto"]}
                      stroke="var(--muted)"
                    />
                    <Tooltip
                      labelFormatter={(v) => timestamp(v)}
                      contentStyle={{
                        background: "var(--card)",
                        border: "1px solid var(--line)",
                        borderRadius: "var(--radius)",
                      }}
                    />
                    <Legend />
                    {nodes.map((n, i) => (
                      <Line
                        key={n.id}
                        data={(data?.series[n.id] || []).map((r) => ({
                          time: r.time,
                          [n.id]:
                            r.latency != null && Number(r.latency) >= 0
                              ? r.latency
                              : null,
                        }))}
                        name={n.name}
                        dataKey={n.id}
                        stroke={colors[i % colors.length]}
                        strokeWidth={1.6}
                        dot={false}
                        connectNulls={false}
                        isAnimationActive={false}
                      />
                    ))}
                  </LineChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <Empty>所选时间范围暂无检测记录</Empty>
            )}
          </Panel>
        ),
        recent: (
          <Panel>
            <h3 className="metric-heading tone-blue">
              <Timer size={20} />
              最近检测
            </h3>
            <PaginatedCards
              className="recent-node-list"
              resetKey={nodes.map((n) => n.id).join(",")}
            >
              {nodes.map((n) => {
                const rows =
                  kind === "https"
                    ? state.state.history[n.id] || []
                    : data?.series[n.id] || [];
                return (
                  <div className="recent-row" key={n.id}>
                    <span className="ellipsis" title={n.name}>
                      {n.name}
                    </span>
                    <span
                      className={
                        "state-label " + (n.enabled ? "accent" : "muted")
                      }
                      title={n.enabled ? "已启用" : "已停用"}
                    >
                      {n.enabled ? (
                        <CircleCheck size={16} aria-label="已启用" />
                      ) : (
                        <CircleMinus size={16} aria-label="已停用" />
                      )}
                    </span>
                    <RecentDots rows={rows} kind={kind} />
                  </div>
                );
              })}
            </PaginatedCards>
            {!nodes.length && <Empty />}
          </Panel>
        ),
      })}
    </>
  );
}
export function Operations({
  kind = "",
  title = "操作日志",
}: {
  kind?: string;
  title?: string;
}) {
  const { isRead, markRead } = useReadState();
  const [before, setBefore] = useState(""),
    [archive, setArchive] = useState<Operation[]>([]),
    [detail, setDetail] = useState<Dict | null>(null),
    [failure, setFailure] = useState("");
  const { data, error, refresh } = useResource<{
    items: Operation[];
    next: string;
  }>(
    (kind === "probe" ? "/api/detection-logs" : "/api/operations") +
      (before ? "?before=" + encodeURIComponent(before) : ""),
    10000,
  );
  useEffect(() => {
    if (data?.items && before)
      setArchive((old) => [
        ...new Map([...old, ...data.items].map((r) => [r.id, r])).values(),
      ]);
  }, [before, data]);
  const rows = [
    ...new Map(
      [...(before ? archive : []), ...(data?.items || [])].map((r) => [
        r.id,
        r,
      ]),
    ).values(),
  ];
  return (
    <Panel>
      <PageHeader
        title={title}
        description="UI操作日志自动标为已读；其他日志查看详情后标为已读。阅读状态保存在当前浏览器，日志仍按服务器保留周期清理。"
        actions={<>
          <MarkAllReadButton disabled={!rows.some(r => !isRead(logReadKeys(r)))} onClick={() => markRead(rows.flatMap(logReadKeys))} />
          <Button onClick={() => void refresh()}>刷新</Button>
        </>}
      />
      <ErrorLine error={error || failure} />
      <Table
        footerActions={
          data?.next && (
            <Button
              onClick={() => {
                setArchive(rows);
                setBefore(data.next);
              }}
            >
              加载更早记录
            </Button>
          )
        }
        rows={rows}
        rowKey={(r) => r.id}
        columns={[
          { key: "serial", label: "序号", render: (_, i) => i + 1 },
          {
            key: "time",
            label: "时间 / 类型",
            render: (r) => (
              <>
                {timestamp(r.time)}
                <small>{r.action}</small>
              </>
            ),
          },
          { key: "message", label: "操作内容", width: "50%", render: (r) => r.message },
          { key: "read", label: "阅读状态", render: (r) => <ReadBadge read={isRead(logReadKeys(r))} /> },
          {
            key: "status",
            label: "结果",
            render: (r) => (
              <span
                className={
                  r.status === "partial"
                    ? "fair"
                    : r.status === "error"
                      ? "poor"
                      : "accent"
                }
              >
                {r.status === "partial"
                  ? "部分成功"
                  : r.status === "error"
                    ? "失败"
                    : "成功"}
              </span>
            ),
          },
          {
            key: "details",
            label: "详情",
            className: "log-details-column",
            render: (r) => (
              <Button
                size="compact"
                iconOnly
                title="查看日志"
                ariaLabel="查看日志"
                onClick={() => {
                  void api<Dict>("/api/operations/" + encodeURIComponent(r.id))
                    .then((data) => { setDetail(data); markRead(logReadKeys(r)); })
                    .catch((e) => setFailure(errorMessage(e)));
                }}
              >
                {labelIcon("查看日志", 18)}
              </Button>
            ),
          },
        ]}
      />
      {detail && (
        <Modal
          title="操作日志详情"
          copyText={JSON.stringify(detail, null, 2)}
          onClose={() => setDetail(null)}
          wide
        >
          <h3>{text(detail.message)}</h3>
          <p>
            {timestamp(detail.time)} · {text(detail.action)}
          </p>
          <JsonDetails data={detail.details} copyData={detail} />
        </Modal>
      )}
    </Panel>
  );
}
export function Switches() {
  const { isRead, markRead } = useReadState();
  const [detail, setDetail] = useState<Dict | null>(null);
  const { data, error, refresh } = useResource<{ items: Dict[] }>(
    "/api/switches",
    5000,
  );
  return (
    <Panel>
      <PageHeader
        title="切换日志"
        description="记录最近 500 次出口切换，含自动／手动与失败原因；保存于服务器"
        actions={<>
          <MarkAllReadButton disabled={!(data?.items || []).some(r => !isRead(switchReadKeys(r)))} onClick={() => markRead((data?.items || []).flatMap(switchReadKeys))} />
          <Button onClick={() => void refresh()}>刷新</Button>
        </>}
      />
      <ErrorLine error={error} />
      <Table
        rows={data?.items || []}
        rowKey={(r) => String(r.time) + String(r.target)}
        columns={[
          { key: "i", label: "序号", render: (_, i) => i + 1 },
          { key: "t", label: "时间", render: (r) => timestamp(r.time) },
          {
            key: "method",
            label: "方式",
            render: (r) => (r.method === "auto" ? "自动" : "手动"),
          },
          { key: "source", label: "原出口", render: (r) => text(r.source) },
          { key: "target", label: "目标出口", render: (r) => text(r.target) },
          {
            key: "duration",
            label: "耗时",
            render: (r) => text(r.duration) + " 秒",
          },
          { key: "error", label: "原因", render: (r) => text(r.error) },
          { key: "read", label: "阅读状态", render: (r) => <ReadBadge read={isRead(switchReadKeys(r))} /> },
          { key: "status", label: "结果", render: (r) => (r.status === "success" ? "成功" : "失败") },
          { key: "details", label: "详情", render: (r) => <Button iconOnly title="查看日志" ariaLabel="查看日志" onClick={() => { setDetail(r); markRead(switchReadKeys(r)); }}>{labelIcon("查看日志", 18)}</Button> },
        ]}
      />
      {detail && <Modal title="切换日志详情" copyText={JSON.stringify(detail, null, 2)} onClose={() => setDetail(null)}>
        <p>{timestamp(detail.time)} · {detail.method === "auto" ? "自动" : "手动"} · {detail.status === "success" ? "成功" : "失败"}</p>
        <p>原出口：{text(detail.source)}</p>
        <p>目标出口：{text(detail.target)}</p>
        <p>耗时：{text(detail.duration)} 秒</p>
        <p>原因：{text(detail.error)}</p>
      </Modal>}
    </Panel>
  );
}
export function ServiceCard({
  service,
  data,
  act,
  scope = "all",
  flat = false,
  hideHeader = false,
}: {
  service: "warp" | "xray";
  data: Dict;
  act: Act;
  scope?: "all" | "service";
  flat?: boolean;
  hideHeader?: boolean;
}) {
  const [confirm, setConfirm] = useState("");
  const tasks = useResource<{ tasks: Dict[] }>("/api/tasks", 2000);
  const busy = tasks.data?.tasks.some(
    (t) => t.status === "running" && String(t.group).startsWith("service."),
  );
  const unavailable = (action: string) =>
    !!busy ||
    ["unknown", "activating", "deactivating", "reloading"].includes(
      String(data.state),
    ) ||
    (action === "start" && data.state === "active") ||
    (["stop", "restart"].includes(action) && data.state !== "active") ||
    (action === "enable" && !!data.autostart) ||
    (action === "disable" && !data.autostart) ||
    (action === "connect" &&
      (data.state !== "active" || !!record(data.tunnel).connected)) ||
    (action === "disconnect" &&
      (data.state !== "active" || !record(data.tunnel).connected)) ||
    (action === "upgrade" &&
      (!data.update_available || !!data.proxy_enabled)) ||
    (action === "rollback" &&
      (!data.rollback_available || !!data.proxy_enabled));
  const state = String(data.state || "unknown"),
    tunnel = record(data.tunnel);
  const actions =
    service === "warp"
      ? [
          "start",
          "stop",
          "restart",
          "connect",
          "disconnect",
          "enable",
          "disable",
          "check",
          "upgrade",
          "rollback",
        ]
      : [
          "start",
          "stop",
          "restart",
          "enable",
          "disable",
          "check",
          "upgrade",
          "rollback",
        ];
  const labels: Record<string, string> = {
    start: "启动",
    stop: "停止",
    restart: "重启",
    connect: "连接隧道",
    disconnect: "断开隧道",
    enable: "启用",
    disable: "停用",
    check: "检查更新",
    upgrade: "升级",
    rollback: "回退",
  };
  return (
    <article className={"service-card" + (flat ? " flat" : "")}>
      {!hideHeader && (
        <div className="page-heading">
          <h3>
            {service === "xray"
              ? "Xray-core 服务"
              : scope === "service"
                ? "WARP 服务"
                : "WARP 服务与隧道"}
          </h3>
          <span className={state === "active" ? "accent" : "muted"}>
            {state === "active"
              ? "运行中"
              : state === "inactive"
                ? "已停止"
                : state}
          </span>
        </div>
      )}
      <div className="service-control-frame">
        <dl className="service-values">
          <div>
            <dt>服务名称</dt>
            <dd title={versionLabel(data.version, "")}>
              {service === "xray"
                ? "foxray-engine.service"
                : "warp-svc.service"}
            </dd>
          </div>
          <div>
            <dt>进程状态</dt>
            <dd>
              <span
                className={
                  state === "active"
                    ? "accent"
                    : ["inactive", "failed", "dead"].includes(state)
                      ? "poor"
                      : "muted"
                }
              >
                {state}
              </span>{" "}
              /{" "}
              <span
                className={
                  data.substate === "running"
                    ? "accent"
                    : data.substate === "dead"
                      ? "poor"
                      : "muted"
                }
              >
                {text(data.substate)}
              </span>
            </dd>
          </div>
          <div>
            <dt>开机自启</dt>
            <dd className={data.autostart ? "accent" : "metric-orange"}>
              {data.autostart ? "enabled" : "disabled"}
            </dd>
          </div>
          {service === "warp" && scope !== "service" && (
            <div>
              <dt>隧道状态</dt>
              <dd>{tunnel.connected ? "已连接" : text(tunnel.state)}</dd>
            </div>
          )}
        </dl>
        <div className="service-actions">
          {actions
            .filter(
              (action) =>
                !["check", "upgrade", "rollback"].includes(action) &&
                (scope !== "service" ||
                  !["connect", "disconnect"].includes(action)),
            )
            .map((action) => (
              <Button
                key={action}
                disabled={unavailable(action)}
                title={
                  action === "upgrade" && !data.update_available
                    ? "请先检查更新；当前没有已确认的新版本"
                    : action === "rollback" && !data.rollback_available
                      ? "没有可用回退版本"
                      : undefined
                }
                onClick={() =>
                  action === "check"
                    ? void act("/api/services/action", { service, action })
                    : setConfirm(action)
                }
              >
                {labels[action]}
              </Button>
            ))}
        </div>
      </div>
      <VersionPanel
        service={service}
        act={act}
        current={String(data.version || "")}
        rollback={
          <Button
            disabled={unavailable("rollback")}
            title={
              !data.rollback_available ? "没有可用回退版本" : "恢复此前版本"
            }
            onClick={() => setConfirm("rollback")}
          >
            回退
          </Button>
        }
      />
      {confirm && (
        <Confirm
          title={
            labels[confirm] + (service === "xray" ? " Xray-core" : " WARP")
          }
          message={
            <>
              <p>服务状态与代理接管状态分别管理。</p>
              <p>
                {service === "warp"
                  ? "WARP 只在隔离网络空间内操作，不回放 Xray 的配置或路由。"
                  : "停止或重启核心会中断它承载的代理连接；VPN 排除保持独立。"}
              </p>
            </>
          }
          ack={
            service === "xray" && ["stop", "restart"].includes(confirm)
              ? "我确认本次核心维护可能中断代理连接"
              : undefined
          }
          danger={["stop", "disconnect"].includes(confirm)}
          onClose={() => setConfirm("")}
          onConfirm={() => {
            const action = confirm;
            setConfirm("");
            void act("/api/services/action", {
              service,
              action,
              allow_outage: true,
            });
          }}
        />
      )}
    </article>
  );
}
export function ServicePanel({
  service,
  data,
  act,
  error,
}: {
  service: "xray" | "warp";
  data?: Dict;
  act: Act;
  error?: string;
}) {
  const status = String(data?.state || "unknown");
  return (
    <Panel className={"component-service-panel " + service + "-service-panel"}>
      <PageHeader
        title="服务管理"
        icon={<ProductIcon product={service} size={20}/>}
        description={
          service === "xray"
            ? "启动／停止／重启分别执行 systemctl start／stop／restart foxray-engine.service，影响核心连接；启用／停用分别执行 systemctl enable／disable foxray-engine.service，仅修改开机自启。代理接管单独控制。"
            : "启动／停止／重启分别执行 systemctl start／stop／restart warp-svc.service；启用／停用分别执行 systemctl enable／disable warp-svc.service，仅修改开机自启。隧道连接在隧道管理中通过 warp-cli --accept-tos connect／disconnect 控制。"
        }
        actions={
          <span
            className={status === "active" ? "accent small" : "muted small"}
          >
            {status === "active"
              ? "运行中"
              : status === "inactive"
                ? "已停止"
                : data
                  ? status
                  : "正在读取"}
          </span>
        }
      />
      {data ? (
        <ServiceCard
          service={service}
          data={data}
          act={act}
          scope="service"
          flat
          hideHeader
        />
      ) : (
        <Empty>正在读取服务状态</Empty>
      )}
      <ErrorLine error={error} />
    </Panel>
  );
}
export function XrayPage({ state, act }: { state: State; act: Act }) {
  const [draft, setDraft] = useState<Config>(structuredClone(state.config)),
    [dirty, setDirty] = useState(false),
    [icmpDraft, setIcmpDraft] = useState<Dict>(
      structuredClone(state.icmp_policy),
    ),
    [confirm, setConfirm] = useState("");
  const services = useResource<{ services: Record<string, Dict> }>(
    "/api/services",
    3000,
  );
  const bypass = useResource<{ targets: Dict[]; manual: string[] }>(
    "/api/bypass",
    60000,
  );
  const current = state.nodes.find((node) => node.active);
  useEffect(() => {
    if (!dirty) {
      setDraft(structuredClone(state.config));
      setIcmpDraft(structuredClone(state.icmp_policy));
    }
  }, [JSON.stringify(state.config), JSON.stringify(state.icmp_policy), dirty]);
  const update = (key: keyof Config, value: unknown) => {
    setDraft((old) => ({ ...old, [key]: value }));
    setDirty(true);
  };
  const restoreCard = (defaults: Dict, discard = false) => {
    setDraft((old) => ({
      ...old,
      ...Object.fromEntries(
        Object.keys(defaults).map((k) => [
          k,
          discard ? state.config[k] : defaults[k],
        ]),
      ),
    }));
    setDirty(true);
  };
  const historyActions = useRef<HistorySettingsActions>(null);
  const proxySave = useRef<((ok: boolean) => void) | null>(null);
  const proxyDefaults = {
    enabled: false,
    custom_rules: [],
    route_profile: "bypass_cn",
    capture_host: true,
    capture_lan: true,
    vpn_exclusions: { mode: "default", interfaces: [], cidrs: [], domains: [] },
    dns_direct_servers: DEFAULT_DIRECT_DNS,
    domain_identification: false,
    dns_force_proxy: false,
    lan_dns_intercept: true,
    dns_proxy_servers: DEFAULT_PROXY_DNS,
  };
  const detectDefaults = {
    latency_enabled: true,
    interval: 30,
    timeout: 10,
    latency_url: "https://www.google.com/generate_204",
    latency_concurrency: 4,
    speed_url: "https://cachefly.cachefly.net/100mb.test",
    speed_timeout: 15,
    speed_enabled: true,
    speed_size_mb: 50,
    speed_concurrency: 1,
  };
  const switchDefaults = {
    mode: "manual",
    selected: state.config.selected,
    failures: 3,
    hold: 300,
    improvement: 5,
    confirmations: 3,
    score_latency_window_minutes: 30,
    score_latency_min_samples: 1,
    score_metrics: ["latency"],
    score_weights: { latency: 80, loss: 10, speed: 10 },
  };
  const weights = draft.score_weights,
    sum = Object.values(weights).reduce((a, b) => a + b, 0);
  const scope =
    [
      state.config.capture_host ? "本机" : "",
      state.config.capture_lan ? "LAN" : "",
    ]
      .filter(Boolean)
      .join(" + ") || "未选范围";
  const changed =
    Object.keys(draft).filter(
      (key) => JSON.stringify(draft[key]) !== JSON.stringify(state.config[key]),
    ).length +
    (JSON.stringify(icmpDraft) !== JSON.stringify(state.icmp_policy) ? 1 : 0);
  useEffect(() => {
    if (dirty && changed === 0) setDirty(false);
  }, [dirty, changed]);
  const numbers = (fields: (string | number)[][], disabled = false) => (
    <div className="form-grid xray-numeric-grid">
      {fields.map(([key, label, min, max]) => (
        <Field
          key={key}
          label={String(label)}
          hint={
            key === "interval"
              ? "0 表示关闭周期真连接检测与自动选优"
              : undefined
          }
        >
          <NumberInput
            type="number"
            min={Number(min)}
            max={Number(max)}
            disabled={disabled || state.read_only}
            value={Number(
              draft[String(key)] ??
                (key === "score_latency_window_minutes"
                  ? 30
                  : key === "score_latency_min_samples"
                    ? 1
                    : 0),
            )}
            onChange={(e) => update(String(key), Number(e.target.value))}
          />
        </Field>
      ))}
    </div>
  );
  return (
    <div className="xray-settings-page module-service-settings">
      <ServicePanel
        service="xray"
        data={services.data?.services.xray}
        error={services.error}
        act={act}
      />
      <div className="xray-config-form">
        <Panel className="xray-proxy-panel">
          <PageHeader
            title="代理模式"
            description="选择分流模式和接管范围；只接管 IPv4 公网 TCP／UDP"
          />
          <div className="xray-runtime-summary settings-section" aria-label="当前代理状态">
            <div>
              <span>当前出口</span>
              <strong title={current?.name}>
                {current?.name || "未启用 Xray 出口"}
              </strong>
            </div>
            <div>
              <span>Xray 代理状态</span>
              <strong className={state.proxy.enabled ? "accent" : "muted"}>
                {state.proxy.enabled ? "已启用" : "未启用"}
              </strong>
            </div>
            <div>
              <span>已保存的接管范围</span>
              <strong>{scope}</strong>
            </div>
          </div>
          <div className="form-grid settings-section" aria-label="分流与接管">
            <Field label="分流模式">
              <select
                value={draft.enabled ? draft.route_profile : "off"}
                disabled={state.read_only}
                onChange={(e) => {
                  update("enabled", e.target.value !== "off");
                  if (e.target.value !== "off") {
                    update("route_profile", e.target.value);
                    if (draft.active_rule_set)
                      update(
                        "rule_sets",
                        list(draft.rule_sets).map((r) =>
                          r.id === draft.active_rule_set
                            ? { ...r, profile: e.target.value }
                            : r,
                        ),
                      );
                  }
                }}
              >
                <option value="off">关闭代理</option>
                <option value="bypass_cn">
                  大陆白名单 · 国内直连／境外代理
                </option>
                <option value="gfwlist">GFWList 分流</option>
                <option value="global">全局代理 · 公网 TCP／UDP</option>
              </select>
            </Field>
            <Field
              label="接管范围"
              hint="可以只接管本机、只接管 LAN，或同时接管"
            >
              <div className="checks xray-capture-options">
                <label className="check">
                  <input
                    type="checkbox"
                    disabled={state.read_only}
                    checked={draft.capture_host}
                    onChange={(e) => update("capture_host", e.target.checked)}
                  />
                  本机流量
                </label>
                <label className="check">
                  <input
                    type="checkbox"
                    disabled={state.read_only}
                    checked={draft.capture_lan}
                    onChange={(e) => update("capture_lan", e.target.checked)}
                  />
                  LAN 流量
                </label>
              </div>
            </Field>
          </div>
          <XrayObservationPolicy runtime={state.dns_proxy} value={draft} onChange={update} disabled={state.read_only} />
          <VPNExclusionsEditor
            value={draft.vpn_exclusions}
            onChange={(v) => update("vpn_exclusions", v)}
            disabled={state.read_only}
          />
          <RuleSetsEditor
            value={list(draft.rule_sets)}
            active={String(draft.active_rule_set || "")}
            legacy={list(draft.custom_rules)}
            profile={draft.route_profile}
            saved={list(state.config.rule_sets)}
            act={act}
            onChange={(sets, active) => {
              update("rule_sets", sets);
              update("active_rule_set", active);
              const chosen = sets.find((r) => r.id === active);
              if (chosen) update("route_profile", chosen.profile);
            }}
          />
          {dirty && !state.proxy.enabled && (
            <p className="small muted">有待保存配置，请先保存，再启用代理。</p>
          )}
          <SaveCard
            dirty={hasChanges(
              Object.fromEntries(
                Object.keys(proxyDefaults).map((k) => [k, draft[k]]),
              ),
              state.config,
            )}
            title="代理模式"
            defaults={() => restoreCard(proxyDefaults)}
            discard={() => restoreCard(proxyDefaults, true)}
            disabled={state.read_only}
            save={() =>
              new Promise<boolean>((resolve) => {
                proxySave.current = resolve;
                setConfirm("proxy");
              })
            }
          />
        </Panel>
        <Panel className="xray-detection-panel">
          <PageHeader
            title="节点检测"
            description="真连接延迟和 ICMP 独立采集；关闭周期检测后仍可手动检测"
          />
          <SettingsSection className="detection-section" title="真连接延迟检测">
            <label className="check">
              <input
                type="checkbox"
                checked={draft.latency_enabled !== false}
                disabled={state.read_only}
                onChange={(e) => update("latency_enabled", e.target.checked)}
              />
              启用真连接延迟检测
            </label>
            <Field label="真连接检测地址">
              <AddressInput
                kind="latency"
                value={draft.latency_url}
                disabled={state.read_only}
                onChange={(v) => update("latency_url", v)}
              />
            </Field>
            {numbers(
              [
                ["interval", "检测间隔（秒）", 0, 3600],
                ["timeout", "单次超时（秒）", 1, 60],
              ],
              draft.latency_enabled === false,
            )}
            {numbers([["latency_concurrency", "延迟检测并发数", 1, 10]])}
          </SettingsSection>
          <SettingsSection className="detection-section" title="速度检测">
            <label className="check">
              <input
                type="checkbox"
                checked={draft.speed_enabled !== false}
                disabled={state.read_only}
                onChange={(e) => update("speed_enabled", e.target.checked)}
              />
              启用速度检测
            </label>
            <Field label="测速文件地址">
              <AddressInput
                kind="speed"
                value={String(draft.speed_url || "")}
                disabled={state.read_only}
                onChange={(v) => update("speed_url", v)}
              />
            </Field>
            {numbers([
              ["speed_timeout", "测速时长（秒）", 1, 120],
              ["speed_size_mb", "最大下载量（MiB）", 1, 500],
              ["speed_concurrency", "测速并发数", 1, 4],
            ])}
          </SettingsSection>
          <ICMPSettings
            state={state}
            act={act}
            embedded
            value={icmpDraft}
            onChange={(value) => {
              setIcmpDraft(value);
              setDirty(true);
            }}
          />
          <SaveCard
            dirty={
              hasChanges(
                Object.fromEntries(
                  Object.keys(detectDefaults).map((k) => [k, draft[k]]),
                ),
                state.config,
              ) || hasChanges(icmpDraft, state.icmp_policy)
            }
            title="节点检测"
            defaults={() => {
              restoreCard(detectDefaults);
              setIcmpDraft({
                enabled: false,
                interval: 300,
                count: 20,
                size: 56,
                timeout: 2,
              });
            }}
            discard={() => {
              restoreCard(detectDefaults, true);
              setIcmpDraft(structuredClone(state.icmp_policy));
            }}
            disabled={state.read_only}
            save={() =>
              act("/api/config", {
                speed_enabled: draft.speed_enabled !== false,
                speed_url: draft.speed_url,
                speed_timeout: draft.speed_timeout,
                speed_size_mb: draft.speed_size_mb,
                speed_concurrency: draft.speed_concurrency,
                latency_concurrency: draft.latency_concurrency,
                latency_enabled: draft.latency_enabled !== false,
                interval: draft.interval,
                timeout: draft.timeout,
                latency_url: draft.latency_url,
                icmp_policy: icmpDraft,
              })
            }
          />
        </Panel>
        <Panel className="xray-selection-panel">
          <PageHeader
            title="自动切换"
            description="手动选择固定节点；自动模式按照所选指标的综合评分选优"
          />
          <SettingsSection title="节点选择">
            <div className="form-grid">
              <Field label="节点选择方式">
                <select
                  disabled={state.read_only}
                  value={draft.mode}
                  onChange={(e) => update("mode", e.target.value)}
                >
                  <option value="manual">手动选择</option>
                  <option value="auto">按综合评分自动选择</option>
                </select>
              </Field>
              <Field
                label="手动选择节点"
                hint={
                  draft.mode === "auto"
                    ? "自动模式由评分决定，切回手动模式时沿用此选择"
                    : undefined
                }
              >
                <select
                  disabled={state.read_only || draft.mode === "auto"}
                  value={draft.selected}
                  onChange={(e) => update("selected", e.target.value)}
                >
                  {state.nodes
                    .filter((node) => node.id !== "warp" && node.enabled)
                    .map((node) => (
                      <option
                        key={node.id}
                        value={node.id}
                        className={
                          node.active ? "current-exit-option" : undefined
                        }
                      >
                        {node.name}
                        {node.active ? "（当前出口）" : ""}
                      </option>
                    ))}
                </select>
              </Field>
            </div>
          </SettingsSection>
          <div className="xray-section-divider settings-section">
            <div className="xray-subheading">
              <h3 className="settings-section-title">
                评分依据与权重{" "}
                <SettingInfo
                  label="评分依据与权重"
                  text="勾选至少一项；按实际可用指标归一为100分，带宽使用最近一次成功测速。窗口内取最近20次检测的成功样本计算延迟均值；少于门槛的节点暂不参与延迟评分。默认30分钟、至少1次。"
                />
              </h3>
              <span className={sum === 100 ? "small muted" : "small poor"}>
                权重合计 {sum} / 100
              </span>
            </div>

            <div className="score-controls">
              {[
                ["latency", "真连接延迟"],
                ["loss", "ICMP 成功率"],
                ["speed", "最近有效带宽"],
              ].map(([key, label]) => (
                <div key={key}>
                  <label className="check">
                    <input
                      type="checkbox"
                      disabled={state.read_only}
                      checked={draft.score_metrics.includes(key)}
                      onChange={(e) =>
                        update(
                          "score_metrics",
                          e.target.checked
                            ? [...draft.score_metrics, key]
                            : draft.score_metrics.filter(
                                (value) => value !== key,
                              ),
                        )
                      }
                    />
                    {label}
                  </label>
                  <input
                    aria-label={label + "权重"}
                    disabled={state.read_only}
                    type="number"
                    min={0}
                    max={100}
                    value={weights[key]}
                    onChange={(e) =>
                      update("score_weights", {
                        ...weights,
                        [key]: Number(e.target.value),
                      })
                    }
                  />
                </div>
              ))}
            </div>
            {numbers([
              [
                "score_latency_window_minutes",
                "评分延迟统计窗口（分钟）",
                1,
                1440,
              ],
              ["score_latency_min_samples", "最少成功样本数（次）", 1, 20],
            ])}

            {(sum !== 100 || !draft.score_metrics.length) && (
              <p className="error-line">
                至少勾选一项评分依据，三个权重的合计必须为 100。
              </p>
            )}
          </div>
          <div className="xray-section-divider settings-section">
            <h3 className="settings-section-title">
              自动切换条件{" "}
              <SettingInfo
                label="自动切换条件"
                text={
                  draft.mode === "auto"
                    ? "满足保持时间、改善分数和确认次数后切换；连续失败达到阈值时触发故障处理。"
                    : "当前为手动模式，不自动切换；参数保留。"
                }
              />
            </h3>
            {numbers(
              [
                ["failures", "连续失败阈值", 1, 20],
                ["hold", "最短保持时间（秒）", 0, 86400],
                ["improvement", "评分改善阈值（分）", 0, 100],
                ["confirmations", "连续确认次数", 1, 20],
              ],
              draft.mode !== "auto",
            )}
          </div>
          <SaveCard
            dirty={hasChanges(
              Object.fromEntries(
                Object.keys(switchDefaults).map((k) => [k, draft[k]]),
              ),
              state.config,
            )}
            title="自动切换"
            defaults={() => restoreCard(switchDefaults)}
            discard={() => restoreCard(switchDefaults, true)}
            disabled={state.read_only}
            save={() =>
              act(
                "/api/config",
                Object.fromEntries(
                  [
                    "mode",
                    "selected",
                    "failures",
                    "hold",
                    "improvement",
                    "confirmations",
                    "score_metrics",
                    "score_weights",
                    "score_latency_window_minutes",
                    "score_latency_min_samples",
                  ].map((key) => [key, draft[key]]),
                ),
              )
            }
          />
        </Panel>
        <Panel className="xray-bypass-panel">
          <PageHeader
            title="入口直连"
            description="VPS／隧道入口必须直连，避免代理循环；手动项与自动项分开维护"
          />
          <details className="xray-advanced settings-section">
            <summary>查看与编辑直连地址</summary>
            <div className="form-grid">
              <Field
                label="手动直连地址"
                hint="每行一个 IPv4 或 CIDR；不会被自动项覆盖"
              >
                <textarea
                  rows={6}
                  disabled={state.read_only}
                  value={draft.bypass_ips.join("\n")}
                  onChange={(e) =>
                    update(
                      "bypass_ips",
                      e.target.value.split(/\s+/).filter(Boolean),
                    )
                  }
                />
              </Field>
              <Field label="自动识别入口（只读）" hint="域名保留并独立解析">
                <textarea
                  readOnly
                  rows={6}
                  value={(bypass.data?.targets || [])
                    .map(
                      (target) =>
                        text(target.host) +
                        " → " +
                        text(target.ips) +
                        " · " +
                        text(target.status),
                    )
                    .join("\n")}
                />
              </Field>
            </div>
            <ErrorLine error={bypass.error} />
          </details>
          <SaveCard
            dirty={hasChanges({ bypass_ips: draft.bypass_ips }, state.config)}
            title="入口直连"
            defaults={() => restoreCard({ bypass_ips: [] })}
            discard={() => restoreCard({ bypass_ips: [] }, true)}
            disabled={state.read_only}
            save={() =>
              act(
                "/api/config",
                Object.fromEntries(
                  ["bypass_ips"].map((key) => [key, draft[key]]),
                ),
              )
            }
          />
        </Panel>
        <HistoryIntervalSettings
          module="xray"
          act={act}
          actionsRef={historyActions}
        />
        <div
          className="save-all-footer xray-savebar"
          role="region"
          aria-label="Xray 配置保存"
        >
          <div>
            <h2 className="metric-heading tone-green">
              {labelIcon("配置与同步", 20)}
              配置与同步
              <SettingInfo label="配置与同步" text="此处统一保存代理、节点选择、检测、评分、直连与 ICMP 配置；恢复默认和放弃保存仅修改草稿，保存并应用后生效。" />
            </h2>
            {dirty && <span role="status">{`涉及 ${changed} 个配置项；保存后统一应用。`}</span>}
          </div>
          <div className="actions">
            <Button
              disabled={state.read_only}
              onClick={() => {
                historyActions.current?.defaults();
                restoreCard({
                  ...proxyDefaults,
                  ...detectDefaults,
                  ...switchDefaults,
                  bypass_ips: [],
                });
                setIcmpDraft({
                  enabled: false,
                  interval: 300,
                  count: 20,
                  size: 56,
                  timeout: 2,
                });
              }}
            >
              全部恢复默认
            </Button>
            <Button
              disabled={state.read_only}
              onClick={() => {
                historyActions.current?.discard();
                setDraft(structuredClone(state.config));
                setIcmpDraft(structuredClone(state.icmp_policy));
                setDirty(false);
              }}
            >
              放弃保存
            </Button>
            <Button
              variant="primary"
              highlighted={dirty || !!historyActions.current?.dirty}
              disabled={
                sum !== 100 || !draft.score_metrics.length || state.read_only
              }
              onClick={() => {
                if (dirty) setConfirm("save");
                else void historyActions.current?.save();
              }}
            >
              全部保存并应用
            </Button>
          </div>
        </div>
      </div>
      {confirm && (
        <Confirm
          title={
            confirm === "save" || confirm === "proxy"
              ? "应用 Xray 配置"
              : confirm === "enable"
                ? "启用 Xray 代理"
                : "关闭 Xray 代理"
          }
          message={
            <p>
              {confirm === "proxy"
                ? "应用本卡片的代理开关、分流模式、接管范围与规则集；已有代理连接可能短暂中断。"
                : confirm === "save"
                  ? "将统一保存并应用代理、节点选择、检测、评分、入口直连和 ICMP 配置；已有代理连接可能短暂中断。"
                  : confirm === "enable"
                    ? "使用已保存的配置启用所选本机／LAN 接管范围。"
                    : "关闭已保存范围的 Xray 接管，恢复系统路径；未保存修改会保留。"}{" "}
              VPN 接口和专用路由保持排除。
            </p>
          }
          onClose={() => {
            proxySave.current?.(false);
            proxySave.current = null;
            setConfirm("");
          }}
          onConfirm={() => {
            const action = confirm;
            setConfirm("");
            void act(
              action === "save" || action === "proxy"
                ? "/api/config"
                : "/api/proxy",
              action === "save"
                ? { ...draft, icmp_policy: icmpDraft }
                : action === "proxy"
                  ? Object.fromEntries(
                      [
                        "enabled",
                        "route_profile",
                        "capture_host",
                        "capture_lan",
                        "custom_rules",
                        "rule_sets",
                        "active_rule_set",
                        "vpn_exclusions",
                        "dns_direct_servers",
                        "domain_identification",
                        "dns_force_proxy",
                        "lan_dns_intercept",
                        "dns_proxy_servers",
                      ].map((k) => [k, draft[k]]),
                    )
                  : { ...state.config, enabled: action === "enable" },
            ).then(async (ok) => {
              if (ok && action === "save") {
                setDirty(false);
                await historyActions.current?.save();
              }
              if (action === "proxy") {
                proxySave.current?.(ok);
                proxySave.current = null;
              }
            });
          }}
        />
      )}
    </div>
  );
}
export function Diagnostics({ act }: { act: Act }) {
  const diagnostic = useDiagnosticRunner(act);
  const [data, setData] = useState<Dict>({
    target: "www.google.com",
    port: 443,
    protocol: "tls",
    query: "www.google.com",
    scope: "host",
    source: "",
  });
  const set = (k: string, v: unknown) => setData((old) => ({ ...old, [k]: v }));
  return (
    <>
      <Panel>
        <PageHeader
          title="路径探测"
          description="区分配置预计路径、核心事件确认的实际出站和目标响应；HTTPS测量首字节，DNS实际查询并验证A记录应答，TCP仅测建连、不代表应用可用。LAN来源只做规则模拟，实际请求由网关发起。"
        />
        <div className="form-grid">
          <Field label="目标 IP 或域名">
            <input
              value={String(data.target)}
              onChange={(e) => set("target", e.target.value)}
            />
          </Field>
          <Field label="协议">
            <select
              value={String(data.protocol)}
              onChange={(e) => {
                const protocol=e.target.value;
                setData(old=>({...old,protocol,port: [53,443].includes(Number(old.port)) ? protocol.startsWith("dns") ? 53 : 443 : old.port}));
              }}
            >
              <option value="tls">HTTPS端到端（TLS＋首字节）</option>
              <option value="dns">DNS查询（UDP，必要时TCP）</option>
              <option value="dns_tcp">DNS查询（TCP）</option>
              <option value="tcp">TCP（仅建连）</option>
              <option value="icmp">ICMP</option>
            </select>
          </Field>
          <Field label="目标端口">
            <input
              disabled={data.protocol === "icmp"}
              type="number"
              min={1}
              max={65535}
              value={Number(data.port)}
              onChange={(e) => set("port", Number(e.target.value))}
            />
          </Field>
          {String(data.protocol).startsWith("dns") && <Field label="DNS查询域名"><input aria-label="DNS查询域名" value={String(data.query || "")} onChange={e=>set("query",e.target.value)} /></Field>}
          <Field label="判断范围">
            <select
              value={String(data.scope)}
              onChange={(e) => set("scope", e.target.value)}
            >
              <option value="host">本机</option>
              <option value="lan">LAN 客户端（规则模拟）</option>
            </select>
          </Field>
          <Field label="LAN 客户端来源 IP">
            <input
              value={String(data.source)}
              onChange={(e) => set("source", e.target.value)}
              placeholder="192.168.1.10"
            />
          </Field>
        </div>
        <Button
          variant="primary"
          disabled={diagnostic.pending}
          onClick={() => void diagnostic.run("/api/diagnostics", data)}
        >
          {diagnostic.pending ? "正在诊断…" : "开始探测"}
        </Button>
        <DiagnosticProgress
          pending={diagnostic.pending}
          message={diagnostic.message}
        />
      </Panel>
      {diagnostic.result && <DiagnosticReport operation={diagnostic.result} />}
      <DiagnosticLog act={act} />
    </>
  );
}
export function DiagnosticLog({ act }: { act: Act }) {
  const { isRead, markRead } = useReadState();
  const diagnostic = useDiagnosticRunner(act);
  const [detail, setDetail] = useState<Operation | null>(null);
  const history = useResource<{ items: Operation[] }>(
    "/api/diagnostics/history",
    10000,
  );
  useEffect(() => {
    const refresh = () => void history.refresh();
    window.addEventListener("foxray:diagnostic-completed", refresh);
    return () =>
      window.removeEventListener("foxray:diagnostic-completed", refresh);
  }, [history.refresh]);
  useEffect(() => {
    if (diagnostic.result) {
      setDetail(diagnostic.result);
      markRead(logReadKeys(diagnostic.result));
    }
  }, [diagnostic.result, markRead]);
  const rows = (history.data?.items || []).filter(r => r.action === "diagnostic.result" || r.action === "diagnostic.finished");
  return (
    <>
      {" "}
      <Panel>
        <PageHeader
          title="诊断日志"
          actions={<>
            <MarkAllReadButton disabled={!rows.some(r => !isRead(logReadKeys(r)))} onClick={() => markRead(rows.flatMap(logReadKeys))} />
            <Button onClick={() => void history.refresh()}>刷新</Button>
          </>}
        />
        <DiagnosticProgress
          pending={diagnostic.pending}
          message={diagnostic.message}
        />
        <ErrorLine error={history.error} />
        <Table
          rows={rows}
          rowKey={(r) => r.id}
          columns={[
            { key: "time", label: "时间", render: (r) => timestamp(r.time) },
            { key: "message", label: "内容", render: (r) => r.message },
            { key: "read", label: "阅读状态", render: (r) => <ReadBadge read={isRead(logReadKeys(r))} /> },
            {
              key: "status",
              label: "结果",
              render: (r) => (r.status === "success" ? "通过" : "未全部通过"),
            },
            {
              key: "actions",
              label: "操作",
              render: (r) => (
                <div className="actions">
                  <Button onClick={() => { setDetail(r); markRead(logReadKeys(r)); }}>查看日志</Button>
                  <Button
                    disabled={
                      diagnostic.pending || !record(r.details).parameters
                    }
                    onClick={() =>
                      void diagnostic.run("/api/diagnostics/replay", {
                        id: r.id,
                      })
                    }
                  >
                    重新执行
                  </Button>
                </div>
              ),
            },
          ]}
        />
        {detail && (
          <Modal
            title="诊断详情"
            copyText={JSON.stringify(detail, null, 2)}
            onClose={() => setDetail(null)}
            wide
          >
            <DiagnosticReport operation={detail} embedded />
          </Modal>
        )}
      </Panel>
    </>
  );
}

export function LogSettings({ act }: { state: State; act: Act }) {
  return <LogRetentionSettings act={act} />;
}
export function NodeBackups({ act }: { act: Act }) {
  const backups = useResource<{ items: Dict[] }>("/api/backups", 10000),
    [restore, setRestore] = useState<Dict | null>(null);
  return (
    <Panel>
      <PageHeader
        title="节点与订阅备份"
        description="仅保存在服务器受限目录，最多 10 份；恢复前自动备份，不恢复 VPN 密钥或系统路由"
        actions={
          <Button
            onClick={() =>
              void act("/api/backups/create", { name: "手动备份" }).then(() =>
                backups.refresh(),
              )
            }
          >
            创建备份
          </Button>
        }
      />
      <Table
        rows={backups.data?.items || []}
        rowKey={(r) => String(r.id)}
        columns={[
          { key: "name", label: "名称", render: (r) => text(r.name) },
          {
            key: "created",
            label: "时间",
            render: (r) => timestamp(r.created),
          },
          {
            key: "counts",
            label: "节点 / 订阅",
            render: (r) =>
              text(r.node_count) + " / " + text(r.subscription_count),
          },
          {
            key: "version",
            label: "版本",
            render: (r) => text(r.app_version),
          },
          {
            key: "action",
            label: "操作",
            render: (r) => <Button onClick={() => setRestore(r)}>恢复</Button>,
          },
        ]}
      />
      {restore && (
        <Confirm
          title="恢复节点与订阅"
          message={
            <p>
              恢复“{text(restore.name)}
              ”。恢复前会创建自动备份，当前出口不在备份中时将拒绝恢复。
            </p>
          }
          onClose={() => setRestore(null)}
          onConfirm={() => {
            const id = restore.id;
            setRestore(null);
            void act("/api/backups/restore", { id });
          }}
        />
      )}
    </Panel>
  );
}
export function ICMPSettings({
  state,
  act,
  value,
  onChange,
  save,
  embedded = false,
}: {
  state: State;
  act: Act;
  value?: Dict;
  onChange?: (value: Dict) => void;
  save?: () => Promise<boolean>;
  embedded?: boolean;
}) {
  const [local, setLocal] = useState<Dict>(state.icmp_policy);
  const icmp = value || local,
    setIcmp = (next: Dict) => (onChange ? onChange(next) : setLocal(next));
  const content = (
    <>
      {!embedded ? (
        <PageHeader
          title="ICMP 检测"
          description="用于代理入口质量检测；禁 Ping 或无响应不会当作有效 100% 丢包参与评分"
        />
      ) : (
        <>
          <h3 className="settings-section-title">
            ICMP 检测
            <SettingInfo
              label="ICMP 检测"
              text="控制入口成功率与平均延迟的独立探测。"
            />
          </h3>
        </>
      )}
      <label className="check">
        <input
          type="checkbox"
          checked={!!icmp.enabled}
          onChange={(e) => setIcmp({ ...icmp, enabled: e.target.checked })}
        />
        启用 ICMP 检测
      </label>
      <div className="form-grid">
        {[
          ["interval", "检测间隔（秒，0 为仅手动）", 0, 3600],
          ["count", "每轮包数", 1, 20],
          ["size", "包大小（字节）", 0, 1400],
          ["timeout", "每包超时（秒）", 1, 5],
        ].map(([key, label, min, max]) => (
          <Field key={key} label={String(label)}>
            <NumberInput
              type="number"
              min={Number(min)}
              max={Number(max)}
              value={Number(icmp[String(key)])}
              onChange={(e) =>
                setIcmp({ ...icmp, [String(key)]: Number(e.target.value) })
              }
            />
          </Field>
        ))}
      </div>
      {save && (
        <SaveCard
          dirty={hasChanges(icmp, state.icmp_policy)}
          title="ICMP 检测"
          disabled={state.read_only}
          defaults={() =>
            setIcmp({
              enabled: false,
              interval: 300,
              count: 20,
              size: 56,
              timeout: 2,
            })
          }
          discard={() => setIcmp(structuredClone(state.icmp_policy))}
          save={save}
        />
      )}
      {!onChange && (
        <SaveCard
          dirty={hasChanges(icmp, state.icmp_policy)}
          title="ICMP 检测"
          defaults={() =>
            setIcmp({
              enabled: false,
              interval: 300,
              count: 20,
              size: 56,
              timeout: 2,
            })
          }
          discard={() => setIcmp(structuredClone(state.icmp_policy))}
          disabled={state.read_only}
          save={() => act("/api/icmp-policy", icmp)}
        />
      )}
    </>
  );
  return embedded ? (
    <section className="monitor-icmp-section settings-section">
      {content}
    </section>
  ) : (
    <Panel>{content}</Panel>
  );
}
function ManagerServicePanel({ state, act }: { state: State; act: Act }) {
  const resource = useResource<Dict>("/api/manager/service", 5000);
  const [confirm, setConfirm] = useState("");
  const [pending, setPending] = useState(false);
  const [scheduled, setScheduled] = useState(false);
  const [message, setMessage] = useState("");
  const [failure, setFailure] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  const data = resource.data;
  const labels: Record<string, string> = {
    start: "启动",
    stop: "停止",
    restart: "重启",
    enable: "启用",
    disable: "停用",
  };
  return (
    <Panel className="component-service-panel manager-service-panel">
      <PageHeader
        title="FoXray 服务管理"
        description={
          <>
            管理应用本身；核心服务与 VPN
            连接由各自单元维护。停止管理服务后网页停止响应，需要通过SSH执行{" "}
            <code>systemctl start foxray.service</code>{" "}
            恢复。启动／停止／重启对应 systemctl start／stop／restart
            foxray.service；启用／停用对应 systemctl enable／disable
            foxray.service，仅修改开机启动状态。
          </>
        }
        actions={
          <span
            className={
              data?.state === "active" ? "accent small" : "muted small"
            }
          >
            {data?.state === "active"
              ? "运行中"
              : data?.state === "inactive"
                ? "已停止"
                : text(data?.state)}
          </span>
        }
      />
      <div className="service-control-frame">
        <dl className="service-values">
          <div>
            <dt>服务名称</dt>
            <dd>foxray.service</dd>
          </div>
          <div>
            <dt>服务状态</dt>
            <dd>
              <span
                className={
                  data?.state === "active"
                    ? "accent"
                    : ["inactive", "failed", "dead"].includes(
                          String(data?.state),
                        )
                      ? "poor"
                      : "muted"
                }
              >
                {text(data?.state)}
              </span>{" "}
              /{" "}
              <span
                className={
                  data?.substate === "running"
                    ? "accent"
                    : data?.substate === "dead"
                      ? "poor"
                      : "muted"
                }
              >
                {text(data?.substate)}
              </span>
            </dd>
          </div>
          <div>
            <dt>开机自启</dt>
            <dd
              className={
                data ? (data.autostart ? "accent" : "metric-orange") : "muted"
              }
            >
              {data ? (data.autostart ? "enabled" : "disabled") : "读取中"}
            </dd>
          </div>
        </dl>
        <div className="service-actions">
          {Object.entries(labels).map(([action, label]) => (
            <Button
              key={action}
              disabled={
                pending ||
                scheduled ||
                state.read_only ||
                !data?.available ||
                (action === "start" && data?.state === "active") ||
                (action === "stop" && data?.state !== "active") ||
                (action === "enable" && !!data?.autostart) ||
                (action === "disable" && !data?.autostart)
              }
              variant={action === "stop" ? "danger" : "secondary"}
              onClick={() => setConfirm(action)}
            >
              {label}
            </Button>
          ))}
        </div>
      </div>
      <VersionPanel service="foxray" act={act} current={state.version} />
      {message && <p className="hint">{message}</p>}
      <ErrorLine error={failure || resource.error} />
      {confirm && (
        <Confirm
          title={labels[confirm] + " FoXray 管理服务"}
          danger={confirm === "stop"}
          message={
            <p>
              {confirm === "stop"
                ? "约 3 秒后停止管理服务，网页随之断开。请确保可以通过 SSH 执行 systemctl start foxray.service 恢复。"
                : confirm === "restart"
                  ? "约 3 秒后重启管理服务，页面会短暂断开并自动重连。"
                  : confirm === "enable" || confirm === "disable"
                    ? "仅修改 FoXray 管理服务的开机启动状态，不立即启停服务。"
                    : "启动 FoXray 管理服务。"}
            </p>
          }
          onClose={() => setConfirm("")}
          onConfirm={() => {
            const action = confirm;
            setConfirm("");
            setPending(true);
            setFailure("");
            void api<Dict>("/api/manager/service", { action, confirmed: true })
              .then((result) => {
                setMessage(String(result.message || "操作已完成"));
                if (result.scheduled) {
                  setScheduled(true);
                  timer.current = setTimeout(() => {
                    setScheduled(false);
                    void resource.refresh();
                  }, 10000);
                } else void resource.refresh();
              })
              .catch((error) => setFailure(errorMessage(error)))
              .finally(() => setPending(false));
          }}
        />
      )}
    </Panel>
  );
}

export function Settings({ state, act }: { state: State; act: Act }) {
  return (
    <SettingsSaveProvider expected={6}>
      <ManagerServicePanel state={state} act={act} />
      <PageWidthSettings act={act} />
      <DisplaySettings act={act} />
      <ModuleSettings state={state} act={act} />
    </SettingsSaveProvider>
  );
}

export const unusedColumns: Column<Dict>[] = [];
void list;
