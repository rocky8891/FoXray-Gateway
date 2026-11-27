import { hostMetrics, formatCapacity, LOAD_OPTIONS, CPU_OPTIONS, MEMORY_OPTIONS } from "./host_metrics";
import { hasChanges } from "./settings_save";
import type { HomeRandomTheme } from "./overview_random_theme";
import { MetricHover } from "./metric_hover";
import { useAppearanceTheme } from "./theme_appearance";
import { NodeManagementIcon } from "./requirement_icons";
import { overviewSurfaceVariables } from "./overview_surfaces";
import {
  ConnectionIcon,
  GatewayIcon,
  ServiceOverviewIcon,
} from "./monitor_icons";
import { defaultOverviewView, type OverviewView } from "./overview_policy";
import { IconSelect } from "./icon_select";
import { MonitorItemsSettings } from "./monitor_items";
import { labelIcon } from "./ui_icons";
import { useTimeRange, TimeRangeControl, type TimeRange } from "./time_range";
import { TimeAxisTick, timeTicks, formatAxisTime } from "./chart_layout";
import { SaveCard } from "./settings_save";
import { AlignedAxisTick, CHART_AXIS_WIDTH } from "./chart_layout";
import {
  memo,
  useId,
  useEffect,
  useMemo,
  useState,
  useRef,
  type ReactNode,
} from "react";
import {
  AreaChart,
  Area,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ComposedChart,
  Line,
} from "recharts";
import {
  Download,
  ArrowUp,
  ArrowDown,
  RefreshCw,
  Activity,
  Cpu,
  MemoryStick,
  HardDrive,
  Monitor,
  Network,
} from "lucide-react";
import { list, record, text, timestamp, type Dict, type State } from "./api";
import {
  Button,
  Empty,
  ErrorLine,
  Field,
  PageHeader,
  SettingsSection,
  Panel,
  useResource,
  Modal,
} from "./ui";
import { Home, type Act } from "./nodes";
export { formatCapacity } from "./host_metrics";
export function formatRate(value: unknown) {
  const n = Number(value);
  if (value == null || !Number.isFinite(n)) return "—";
  return n >= 1000000
    ? (n / 1000000).toFixed(2) + " MB/s"
    : (n / 1000).toFixed(1) + " KB/s";
}
function duration(value: unknown) {
  const n = Number(value) || 0;
  return (
    Math.floor(n / 86400) +
    " 天 " +
    Math.floor((n % 86400) / 3600) +
    " 小时 " +
    Math.floor((n % 3600) / 60) +
    " 分钟"
  );
}
function selected(rows: Dict[], name: string) {
  return rows.filter(
    (r) =>
      name === "all" || (name === "physical" && r.physical) || r.name === name,
  );
}
function total(rows: Dict[], key: string) {
  const valid = rows.filter((r) => r[key] != null);
  return valid.length ? valid.reduce((s, r) => s + Number(r[key]), 0) : null;
}
function diskLatency(rows: Dict[]) {
  const measured = rows.filter(
    (r) => r.await_ms != null && r.total_iops != null,
  );
  const ops = measured.reduce((sum, r) => sum + Number(r.total_iops), 0);
  return ops > 0
    ? measured.reduce(
        (sum, r) => sum + Number(r.await_ms) * Number(r.total_iops),
        0,
      ) / ops
    : null;
}
const DiskIOChart = memo(function DiskIOChart({
  data,
  domain,
  group,
}: {
  data: Dict[];
  domain: [number, number];
  group: "rate" | "activity";
}) {
  const [hidden, setHidden] = useState<string[]>([]);
  const fields = [
    { key: "read", label: "读取", color: "#8dd7b2", axis: "rate" },
    { key: "write", label: "写入", color: "#efc66d", axis: "rate" },
    { key: "iops", label: "读写次数", color: "#ed91a4", axis: "ops" },
    { key: "latency", label: "读写延迟", color: "#7188f0", axis: "latency" },
  ].filter((field) =>
    group === "rate" ? field.axis === "rate" : field.axis !== "rate",
  );
  return (
    <div className="host-chart disk-io-chart">
      {data.length ? (
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart
            data={data}
            margin={{
              top: 12,
              right: 20,
              left: 0,
              bottom: 5,
            }}
          >
            <CartesianGrid vertical={false} stroke="var(--line)" />
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
              yAxisId={group === "rate" ? "rate" : "latency"}
              width={CHART_AXIS_WIDTH}
              tick={
                <AlignedAxisTick
                  format={
                    group === "rate"
                      ? formatRate
                      : (v) => Number(v).toFixed(1) + " ms"
                  }
                />
              }
            />
            {group === "activity" && (
              <YAxis
                yAxisId="ops"
                orientation="right"
                width={0}
                tick={{
                  fontSize: 11,
                  fill: "#ed91a4",
                  dx: -12,
                  textAnchor: "end",
                }}
                tickFormatter={(v) => Number(v).toFixed(0)}
              />
            )}
            <Tooltip
              labelFormatter={(v) => timestamp(v)}
              formatter={(v, name) => [
                name === "读取" || name === "写入"
                  ? formatRate(v)
                  : v == null
                    ? "—"
                    : Number(v).toFixed(2) +
                      (name === "读写次数" ? " 次/s" : " ms"),
                String(name),
              ]}
              contentStyle={{
                background: "var(--card)",
                color: "var(--text)",
                border: "1px solid var(--line)",
                borderRadius: "var(--radius)",
              }}
            />
            <Legend
              onClick={(entry) => {
                const key = String(entry.dataKey);
                setHidden((old) =>
                  old.includes(key)
                    ? old.filter((k) => k !== key)
                    : [...old, key],
                );
              }}
            />
            {fields.map((f) =>
              f.axis === "rate" ? (
                <Area
                  key={f.key}
                  yAxisId={f.axis}
                  dataKey={f.key}
                  name={f.label}
                  stroke={f.color}
                  fill={f.color}
                  fillOpacity={0.1}
                  hide={hidden.includes(f.key)}
                  connectNulls={false}
                  isAnimationActive={false}
                  dot={false}
                />
              ) : (
                <Line
                  key={f.key}
                  yAxisId={f.axis}
                  dataKey={f.key}
                  name={f.label}
                  stroke={f.color}
                  hide={hidden.includes(f.key)}
                  connectNulls={false}
                  isAnimationActive={false}
                  dot={false}
                  strokeWidth={1.6}
                />
              ),
            )}
          </ComposedChart>
        </ResponsiveContainer>
      ) : (
        <Empty>开始采集后生成曲线；不补造历史数据。</Empty>
      )}
    </div>
  );
});
function Gauge({
  value,
  label,
  caption,
  children,
  metric,
  tone,
  icon,
  selector,
  displayValue,
}: {
  selector?: ReactNode;
  displayValue?: { amount: string; unit: string };
  value: unknown;
  label: string;
  caption: string;
  children?: ReactNode;
  metric?: string;
  tone: string;
  icon: ReactNode;
}) {
  const valid = value != null && Number.isFinite(Number(value)),
    n = valid ? Number(value) : 0;
  return (
    <div
      data-monitor={metric}
      style={{ order: `var(--order-${metric})` }}
      className={"gauge tone-" + tone}
    >
      <MetricHover
        className={"gauge-ring " + (n >= 95 ? "poor" : n >= 80 ? "fair" : "")}
        label={label}
        detail={
          (valid
            ? displayValue
              ? displayValue.amount + displayValue.unit
              : n.toFixed(1) + "%"
            : "未采集") +
          " · " +
          caption
        }
      >
        <svg viewBox="7.5 7.5 113 113" aria-hidden="true">
          <circle className="ring-track" cx="64" cy="64" r="52" />
          <circle
            className="ring-value"
            cx="64"
            cy="64"
            r="52"
            strokeDasharray={2 * Math.PI * 52}
            strokeDashoffset={
              2 * Math.PI * 52 * (1 - Math.min(100, Math.max(0, n)) / 100)
            }
            transform="rotate(-90 64 64)"
          />
        </svg>
        <strong>
          {valid ? displayValue?.amount || n.toFixed(1) : "—"}
          {valid && <small>{displayValue?.unit ?? "%"}</small>}
        </strong>
        <div className="gauge-heading">
          {selector || (
            <span className="gauge-label">
              {icon}
              {label}
            </span>
          )}
          {children && <div className="gauge-control">{children}</div>}
        </div>
      </MetricHover>
      <p>{caption}</p>
    </div>
  );
}
export function PerformanceOverview({
  data,
  items = {},
  act,
}: {
  data: Dict;
  items?: Dict;
  act?: Act;
}) {
  const metrics = hostMetrics(data);
  const current = record(data.current);
  const { key: load } = metrics.load;
  const { key: cpu, percent: cpuValue } = metrics.cpu;
  const { key: memory, percent: memoryValue } = metrics.memory;
  const { key: partition } = metrics.root;
  const mount = list(current.mounts).find(m => m.path === partition);
  const changeDefault = (key: string) => (value: string) => {
    if (act) void act("/api/system/policy", { [key]: value });
  };
  const selector = (
    label: string,
    value: string,
    change: (value: string) => void,
    options: string[][],
    icon: ReactNode,
  ) => act ? (
    <div className="gauge-metric-selector">
      <IconSelect
        label={label}
        value={value}
        onChange={change}
        triggerText=" "
        options={options.map(([value, title]) => ({
          value,
          label: title,
          icon,
          tone: label.startsWith("负载")
            ? "purple"
            : label.startsWith("CPU")
              ? "blue"
              : label.startsWith("内存")
                ? "green"
                : "orange",
        }))}
      />
    </div>
  ) : undefined;
  return (
    <div
      className="performance-overview"
      style={Object.fromEntries(
        (
          (items.runtime_order as string[]) || ["load", "cpu", "memory", "root"]
        ).map((key, i) => ["--order-" + key, i]),
      )}
    >
      <div className="gauge-grid">
        {items.load !== false && (
          <Gauge
            metric="load"
            tone="purple"
            icon={<Activity size={18} />}
            value={metrics.load.percent}
            label={metrics.load.label}
            displayValue={{ amount: metrics.load.value, unit: "" }}
            selector={selector(
              "负载主页指标",
              load,
              changeDefault("default_load"),
              LOAD_OPTIONS.map(([key, title]) => [key, "负载 · " + title]),
              <Activity size={18} />,
            )}
            caption={
              "1 / 5 / 15 分钟：" +
              [current.load1, current.load5, current.load15]
                .map((v) => (v == null ? "—" : Number(v).toFixed(2)))
                .join(" / ")
            }
          />
        )}
        {items.cpu !== false && (
          <Gauge
            metric="cpu"
            tone="blue"
            icon={<Cpu size={18} />}
            value={cpuValue}
            label={metrics.cpu.label}
            selector={selector(
              "CPU主页指标",
              cpu,
              changeDefault("default_cpu"),
              CPU_OPTIONS.map(([key, title]) => [key, "CPU · " + title]),
              <Cpu size={18} />,
            )}
            caption={
              cpu === "cpu_percent"
                ? `${cpuValue == null ? "—" : ((cpuValue / 100) * Number(current.cores)).toFixed(2)} / ${text(current.cores)} 核`
                : (CPU_OPTIONS.find(([key]) => key === cpu)?.[1] || "") +
                  " · 采样周期占比"
            }
          />
        )}
        {items.memory !== false && (
          <Gauge
            metric="memory"
            tone="green"
            icon={<MemoryStick size={18} />}
            value={memoryValue}
            label={metrics.memory.label}
            selector={selector(
              "内存主页指标",
              memory,
              changeDefault("default_memory"),
              MEMORY_OPTIONS.map(([key, title]) => [key, "内存 · " + title]),
              <MemoryStick size={18} />,
            )}
            displayValue={memory !== "memory_percent" ? { amount: metrics.memory.value, unit: "" } : undefined}
            caption={
              memory === "memory_percent"
                ? `${formatCapacity(current.memory_used)} / ${formatCapacity(current.memory_total)}`
                : `占总内存 ${memoryValue == null ? "—" : memoryValue.toFixed(1) + "%"} · ${formatCapacity(current.memory_total)}`
            }
          />
        )}
        {items.root !== false && (
          <Gauge
            metric="root"
            tone="orange"
            icon={<HardDrive size={18} />}
            value={metrics.root.percent}
            label={metrics.root.label}
            selector={selector(
              "分区主页指标",
              partition,
              changeDefault("default_partition"),
              [
                ...new Set([
                  partition,
                  ...list(current.mounts).map((m) => String(m.path)),
                ]),
              ].map((path) => [
                path,
                path === "/" ? "分区 · /" : "分区 · " + path,
              ]),
              <HardDrive size={18} />,
            )}
            caption={`${formatCapacity(mount?.used)} / ${formatCapacity(mount?.total)}`}
          />
        )}
      </div>
    </div>
  );
}

function HostStatus({
  data,
  flat = false,
  actions,
}: {
  data: Dict;
  flat?: boolean;
  actions?: ReactNode;
}) {
  const Wrapper = flat ? "section" : Panel;
  const current = record(data.current),
    policy = record(data.policy);
  return (
    <Wrapper className="host-status">
      <PageHeader
        title="运行状态"
        icon={<Monitor size={20} />}
        actions={actions}
        description={`${text(current.os)} · Kernel ${text(current.kernel)} · ${text(
          String(current.cpu_model || "")
            .trim()
            .replace(/^\d+(?:st|nd|rd|th)\s+Gen\s+/i, ""),
        )}`}
        titleExtra={
          <span className="small muted host-runtime-uptime">
            已运行 {duration(current.uptime_seconds)}
          </span>
        }
      />
      {policy.enabled === false && (
        <p className="small muted">采集已暂停，显示最后一次采样。</p>
      )}
      {current.supported === false && <Empty>{text(current.status)}</Empty>}
    </Wrapper>
  );
}
export const HostChart = memo(
  function HostChart({
    data,
    fields,
    labels,
    unit = "rate",
    domain,
    series,
    legend = true,
  }: {
    data: Dict[];
    fields: string[];
    labels: string[];
    unit?: string;
    domain?: [number, number];
    series?: { key: string; label: string; color: string; data: Dict[] }[];
    legend?: boolean;
  }) {
    const gradientId = useId().replace(/:/g, "");
    const colors = [
      "var(--accent)",
      unit === "rate" ? "var(--metric-orange)" : "#7888ef",
      "var(--traffic-upload)",
    ];
    const plots =
      series ||
      fields.map((key, index) => ({
        key,
        label: labels[index],
        color: colors[index % colors.length],
        data,
      }));
    return (
      <div className="host-chart">
        {data.length ? (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart
              data={data}
              margin={{ top: 12, right: 20, left: 0, bottom: 5 }}
            >
              <defs>
                {plots.map((plot, index) => (
                  <linearGradient
                    key={plot.key}
                    id={gradientId + "-" + index}
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop offset="0" stopColor={plot.color} stopOpacity={0.3} />
                    <stop
                      offset="1"
                      stopColor={plot.color}
                      stopOpacity={0.02}
                    />
                  </linearGradient>
                ))}
              </defs>
              <CartesianGrid vertical={false} stroke="var(--line)" />
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
                width={CHART_AXIS_WIDTH}
                allowDecimals={unit !== "count"}
                domain={unit === "percent" ? [0, 100] : undefined}
                tick={
                  <AlignedAxisTick
                    format={(v) =>
                      unit === "rate"
                        ? formatRate(v)
                        : unit === "bytes"
                          ? formatCapacity(v)
                          : Number(v).toFixed(
                              unit === "percent" || unit === "count" ? 0 : 1,
                            ) + (unit === "percent" ? "%" : "")
                    }
                  />
                }
              />
              <Tooltip
                labelFormatter={(v) => timestamp(v)}
                formatter={(v, name) => [
                  unit === "rate"
                    ? formatRate(v)
                    : unit === "bytes"
                      ? formatCapacity(v)
                      : Number(v).toFixed(unit === "count" ? 0 : 2) +
                        (unit === "percent" ? "%" : ""),
                  String(name),
                ]}
                contentStyle={{
                  background: "var(--card)",
                  color: "var(--text)",
                  border: "1px solid var(--line)",
                  borderRadius: "var(--radius)",
                }}
              />
              {legend && <Legend />}
              {plots.map((plot, index) => (
                <Area
                  key={plot.key}
                  data={plot.data}
                  type="linear"
                  dataKey={plot.key}
                  name={plot.label}
                  stroke={plot.color}
                  fill={`url(#${gradientId}-${index})`}
                  strokeWidth={1.8}
                  connectNulls={false}
                  isAnimationActive={false}
                  dot={false}
                />
              ))}
            </AreaChart>
          </ResponsiveContainer>
        ) : (
          <Empty>开始采集后生成曲线；不补造历史数据。</Empty>
        )}
      </div>
    );
  },
  (a, b) =>
    a.data === b.data &&
    a.series === b.series &&
    a.unit === b.unit &&
    a.legend === b.legend &&
    a.fields.join("\0") === b.fields.join("\0") &&
    a.labels.join("\0") === b.labels.join("\0") &&
    a.domain?.[0] === b.domain?.[0] &&
    a.domain?.[1] === b.domain?.[1],
);
function MetricPicker({
  label,
  options,
  value,
  onChange,
  exclusive,
}: {
  label: string;
  options: string[][];
  value: string[];
  onChange: (v: string[]) => void;
  exclusive?: string;
}) {
  const pickerRef = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const close = (e: PointerEvent) => {
      const el = pickerRef.current;
      if (el && e.target instanceof window.Node && !el.contains(e.target))
        el.open = false;
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, []);
  return (
    <details ref={pickerRef} className="metric-picker">
      <summary aria-label={label}>
        {label} · {value.length} 项
      </summary>
      <div className="metric-picker-menu">
        {options.map(([key, title]) => (
          <label className="check" key={key}>
            <input
              type="checkbox"
              checked={value.includes(key)}
              onChange={(e) => {
                let next = e.target.checked
                  ? [...value, key]
                  : value.filter((k) => k !== key);
                if (e.target.checked && exclusive) {
                  next =
                    key === exclusive
                      ? [key]
                      : next.filter((k) => k !== exclusive);
                }
                if (next.length) onChange(next);
              }}
            />
            {title}
          </label>
        ))}
      </div>
    </details>
  );
}
function DevicePicker({
  kind,
  rows,
  value,
  onChange,
}: {
  kind: string;
  rows: Dict[];
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="field device-picker">
      <select
        aria-label={kind === "network" ? "选择网卡" : "选择磁盘"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="physical">
          物理{kind === "network" ? "网卡" : "磁盘"}合计
        </option>
        <option value="all">所有（含虚拟设备）</option>
        {rows.map((r) => (
          <option key={String(r.name)} value={String(r.name)}>
            {String(r.name)}
          </option>
        ))}
      </select>
    </label>
  );
}
export function OverviewPage({
  state,
  act,
  onLayout,
  randomTheme,
}: {
  state: State;
  act: Act;
  onLayout: (view: OverviewView) => void;
  randomTheme: HomeRandomTheme;
}) {
  useAppearanceTheme();
  const configuredView = defaultOverviewView(
    state.manager_settings.presentation,
  );
  const [tab, setTab] = useState<OverviewView>(() => {
    return configuredView;
  });
  useEffect(() => setTab(configuredView), [configuredView]);
  const navigation = <OverviewViewPicker value={tab} onChange={setTab} />;
  const presentation = {
    ...record(state.manager_settings.presentation),
    ...randomTheme.values,
  };
  return (
    <div
      className="overview-page"
      style={overviewSurfaceVariables(presentation)}
    >
      <Home
        state={{
          ...state,
          manager_settings: { ...state.manager_settings, presentation },
        }}
        randomTheme={randomTheme}
        act={act}
        page={tab}
        navigation={navigation}
        onLayout={onLayout}
      />
    </div>
  );
}

export function OverviewViewPicker({
  value,
  onChange,
  label = "主页切换",
}: {
  value: OverviewView;
  onChange: (value: OverviewView) => void;
  label?: string;
}) {
  return (
    <div className="overview-view-picker">
      <IconSelect
        label={label}
        triggerTitle={label === "主页切换" ? label : undefined}
        value={value}
        onChange={(next) => onChange(next as OverviewView)}
        focusOnPointerSelection={label !== "主页切换"}
        options={[
          {
            value: "services",
            label: "服务总览",
            icon: <ServiceOverviewIcon size={18} />,
            tone: "green",
          },
          {
            value: "dashboard",
            label: "当前出口",
            icon: <GatewayIcon size={18} />,
            tone: "blue",
          },
          {
            value: "nodes",
            label: "节点一览",
            icon: <NodeManagementIcon size={18} />,
            tone: "purple",
          },
        ]}
      />
    </div>
  );
}

export const HostHistoryPage = memo(function HostHistoryPage({
  items: enabledItems = {},
  presentation = {},
  act,
}: {
  items?: Dict;
  presentation?: Dict;
  act?: Act;
}) {
  const [layoutOpen, setLayoutOpen] = useState(false);
  const system = useResource<Dict>("/api/system", 2000);
  const timeRange = useTimeRange();
  const [nic, setNic] = useState("physical"),
    [disk, setDisk] = useState("physical"),
    [loads, setLoads] = useState(["load1"]),
    [cpus, setCPUs] = useState(["cpu_percent"]),
    [memories, setMemories] = useState(["memory_percent"]),
    [ioGroup, setIOGroup] = useState<"rate" | "activity">(() =>
      localStorage.getItem("foxray.host.ioGroup") === "activity"
        ? "activity"
        : "rate",
    );
  const policy = record(system.data?.policy);
  const defaultNic = String(policy.default_interface || "physical");
  const defaultDisk = String(policy.default_disk || "physical");
  const defaultLoad = String(policy.default_load || "load1");
  const defaultCPU = String(policy.default_cpu || "cpu_percent");
  const defaultMemory = String(policy.default_memory || "memory_percent");
  useEffect(() => setNic(defaultNic), [defaultNic]);
  useEffect(() => setDisk(defaultDisk), [defaultDisk]);
  useEffect(() => setLoads([defaultLoad]), [defaultLoad]);
  useEffect(() => setCPUs([defaultCPU]), [defaultCPU]);
  useEffect(() => setMemories([defaultMemory]), [defaultMemory]);
  const query = "?" + timeRange.query;
  const history = useResource<Dict>("/api/system/history" + query, 60000),
    items = list(history.data?.items),
    current = record(system.data?.current);
  const net = useMemo(
    () =>
      items.map((r) => {
        const rows = selected(list(r.interfaces), nic);
        return {
          time: r.time,
          download: total(rows, "download_bps"),
          upload: total(rows, "upload_bps"),
        };
      }),
    [history.data, nic],
  );
  const io = useMemo(
    () =>
      items.map((r) => {
        const rows = selected(list(r.disks), disk);
        return {
          time: r.time,
          read: total(rows, "read_bps"),
          write: total(rows, "write_bps"),
          iops: total(
            rows.map((r) => ({
              ...r,
              total_iops:
                r.total_iops ??
                (r.read_iops == null || r.write_iops == null
                  ? null
                  : Number(r.read_iops) + Number(r.write_iops)),
            })),
            "total_iops",
          ),
          latency: diskLatency(rows),
        };
      }),
    [history.data, disk],
  );
  const domain: [number, number] = [
    Number(history.data?.start || Date.now() / 1000 - 86400),
    Number(history.data?.end || Date.now() / 1000),
  ];
  const exportData = () => {
    const rows = [
      [
        "时间",
        "CPU(%)",
        "内存(%)",
        "负载1分钟",
        "负载5分钟",
        "负载15分钟",
        "下载(B/s)",
        "上传(B/s)",
        "磁盘读取(B/s)",
        "磁盘写入(B/s)",
        "读写次数(次/s)",
        "读写延迟(ms)",
      ],
      ...items.map((r, i) => [
        new Date(Number(r.time) * 1000).toISOString(),
        r.cpu_percent,
        r.memory_percent,
        r.load1,
        r.load5,
        r.load15,
        net[i]?.download,
        net[i]?.upload,
        io[i]?.read,
        io[i]?.write,
        io[i]?.iops,
        io[i]?.latency,
      ]),
    ];
    const blob = new Blob(
      ["\ufeff" + rows.map((r) => r.map((v) => v ?? "").join(",")).join("\n")],
      { type: "text/csv;charset=utf-8" },
    );
    const url = URL.createObjectURL(blob),
      a = document.createElement("a");
    a.href = url;
    a.download =
      "foxray-server-monitor-" + new Date().toISOString().slice(0, 10) + ".csv";
    a.click();
    URL.revokeObjectURL(url);
  };
  if (
    ["load", "cpu", "memory", "root", "network", "disk", "connections"].every(
      (key) => enabledItems[key] === false,
    )
  )
    return (
      <Panel>
        <Empty>服务器监控项均已关闭，可在监控设置中开启。</Empty>
      </Panel>
    );
  return (
    <div className="host-monitor-page" style={overviewSurfaceVariables(presentation)}>
      {layoutOpen && act && (
        <Modal
          className="monitor-layout-modal"
          title="服务器监控布局"
          onClose={() => setLayoutOpen(false)}
        >
          <MonitorItemsSettings
            module="host"
            act={act}
            layoutEditor
            chartPreviews={{
              load: {
                kind: "area",
                series: loads.map(
                  (k) => LOAD_OPTIONS.find((o) => o[0] === k)?.[1] || k,
                ),
              },
              cpu: {
                kind: "area",
                series: cpus.map(
                  (k) => CPU_OPTIONS.find((o) => o[0] === k)?.[1] || k,
                ),
              },
              memory: {
                kind: "area",
                series: memories.map(
                  (k) => MEMORY_OPTIONS.find((o) => o[0] === k)?.[1] || k,
                ),
              },
              disk:
                ioGroup === "activity"
                  ? {
                      kind: "line",
                      series: ["读取延迟", "写入延迟", "读写次数"],
                      colors: [
                        "var(--metric-blue)",
                        "var(--metric-purple)",
                        "var(--metric-orange)",
                      ],
                    }
                  : {
                      kind: "area",
                      series: ["写入", "读取"],
                      colors: ["var(--metric-orange)", "var(--accent)"],
                    },
            }}
          />
        </Modal>
      )}
      <Panel className="host-runtime-panel">
        {system.data && (
          <HostStatus
            data={system.data}
            flat
            actions={
              <>
                <TimeRangeControl range={timeRange} />
                <Button
                  onClick={() => {
                    void system.refresh();
                    void history.refresh();
                  }}
                >
                  <RefreshCw size={15} />
                  刷新
                </Button>
                <Button disabled={!items.length} onClick={exportData}>
                  <Download size={15} />
                  导出 CSV
                </Button>
                {act && (
                  <Button onClick={() => setLayoutOpen(true)}>
                    {labelIcon("布局", 15)}布局
                  </Button>
                )}
              </>
            }
          />
        )}

        <ErrorLine error={system.error || history.error} />
        {system.data &&
          ["load", "cpu", "memory", "root"].some(
            (key) => enabledItems[key] !== false,
          ) &&
          record(system.data.current).supported !== false && (
            <PerformanceOverview data={system.data} items={enabledItems} act={act} />
          )}
      </Panel>
      <div
        className={
          "charts-grid host-history-charts host-layout-" +
          (enabledItems.layout === "double" ? "double" : "single")
        }
        style={Object.fromEntries(
          (
            (enabledItems.order as string[]) || [
              "load",
              "cpu",
              "memory",
              "network",
              "disk",
              "connections",
            ]
          ).map((key, i) => ["--order-" + key, i]),
        )}
      >
        {enabledItems.load !== false && (
          <Panel className="host-load-panel">
            <PageHeader
              title="平均负载"
              icon={<Activity size={20} />}
              tone="purple"
              actions={
                <MetricPicker
                  label="负载指标"
                  options={LOAD_OPTIONS}
                  value={loads}
                  onChange={setLoads}
                />
              }
              description="1、5、15 分钟负载；与 CPU 使用率含义不同"
            />
            <HostChart
              data={items}
              fields={loads}
              labels={loads.map(
                (k) => LOAD_OPTIONS.find((o) => o[0] === k)?.[1] || k,
              )}
              unit="load"
              domain={domain}
            />
          </Panel>
        )}

        {enabledItems.cpu !== false && (
          <Panel className="host-cpu-panel">
            <PageHeader
              title="CPU 使用率"
              icon={<Cpu size={20} />}
              tone="blue"
              description="使用率与 hi（硬中断）、si（软中断）、st（虚拟机等待宿主机 CPU）；均为采样周期占比"
              actions={
                <MetricPicker
                  label="CPU 指标"
                  options={CPU_OPTIONS}
                  value={cpus}
                  onChange={setCPUs}
                />
              }
            />
            <HostChart
              data={items}
              fields={cpus}
              labels={cpus.map(
                (k) => CPU_OPTIONS.find((o) => o[0] === k)?.[1] || k,
              )}
              unit="percent"
              domain={domain}
            />
          </Panel>
        )}
        {enabledItems.memory !== false && (
          <Panel className="host-memory-panel">
            <PageHeader
              title="内存使用率"
              icon={<MemoryStick size={20} />}
              description="默认使用率；容量指标切换为单一容量刻度，shared 与 buff/cache 不应简单相加"
              actions={
                <MetricPicker
                  label="内存指标"
                  options={MEMORY_OPTIONS}
                  value={memories}
                  onChange={setMemories}
                  exclusive="memory_percent"
                />
              }
            />
            <HostChart
              data={items}
              fields={memories}
              labels={memories.map(
                (k) => MEMORY_OPTIONS.find((o) => o[0] === k)?.[1] || k,
              )}
              unit={memories.includes("memory_percent") ? "percent" : "bytes"}
              domain={domain}
            />
          </Panel>
        )}
        {enabledItems.disk !== false && (
          <Panel className="host-disk-panel">
            <PageHeader
              title="磁盘 I/O"
              icon={<HardDrive size={20} />}
              tone="orange"
              description={
                ioGroup === "rate"
                  ? "磁盘读取与写入速度；点击图例显示或隐藏"
                  : "左轴为平均读写延迟（ms），右轴为 IOPS（次/s）；点击图例显示或隐藏"
              }
              actions={
                <>
                  <DevicePicker
                    kind="disk"
                    rows={list(current.disks)}
                    value={disk}
                    onChange={setDisk}
                  />
                  <label className="field device-picker">
                    <select
                      aria-label="磁盘监控指标"
                      value={ioGroup}
                      onChange={(e) => {
                        const value = e.target.value as "rate" | "activity";
                        setIOGroup(value);
                        localStorage.setItem("foxray.host.ioGroup", value);
                      }}
                    >
                      <option value="rate">读取 / 写入速度</option>
                      <option value="activity">读写延迟 / IOPS</option>
                    </select>
                  </label>
                </>
              }
            />
            <DiskIOChart data={io} domain={domain} group={ioGroup} />
          </Panel>
        )}
        {enabledItems.network !== false && (
          <Panel className="host-network-panel host-inline-metric-panel">
            <PageHeader
              title="网卡流量"
              icon={<Network size={20} />}
              tone="blue"
              description="网卡上传、下载速率与累计收发量；可选择物理网卡或指定接口"
              titleExtra={
                <div className="inline-metrics host-network-metrics">
                  <span>
                    <ArrowUp size={14} className="traffic-up" />
                    上传{" "}
                    <strong>
                      {formatRate(
                        total(
                          selected(list(current.interfaces), nic),
                          "upload_bps",
                        ),
                      )}
                    </strong>
                  </span>
                  <span>
                    <ArrowDown size={14} className="traffic-down" />
                    下载{" "}
                    <strong>
                      {formatRate(
                        total(
                          selected(list(current.interfaces), nic),
                          "download_bps",
                        ),
                      )}
                    </strong>
                  </span>
                  <span>
                    <ArrowUp size={14} className="traffic-up" />
                    总发送{" "}
                    <strong>
                      {formatCapacity(
                        total(
                          selected(list(current.interfaces), nic),
                          "send_total",
                        ),
                      )}
                    </strong>
                  </span>
                  <span>
                    <ArrowDown size={14} className="traffic-down" />
                    总接收{" "}
                    <strong>
                      {formatCapacity(
                        total(
                          selected(list(current.interfaces), nic),
                          "receive_total",
                        ),
                      )}
                    </strong>
                  </span>
                </div>
              }
              actions={
                <DevicePicker
                  kind="network"
                  rows={list(current.interfaces)}
                  value={nic}
                  onChange={setNic}
                />
              }
            />

            <HostChart
              data={net}
              fields={["download", "upload"]}
              labels={["下载", "上传"]}
              domain={domain}
            />
          </Panel>
        )}
        {enabledItems.connections !== false && (
          <div
            className="host-connections-wrap"
            style={{ order: "var(--order-connections)" }}
          >
            <ServerConnections range={timeRange} />
          </div>
        )}
      </div>
    </div>
  );
});
export function HostSettings({ act, embedded = false }: { act: Act; embedded?: boolean }) {
  const resource = useResource<Dict>("/api/system", 10000),
    [form, setForm] = useState<Dict | null>(null);
  useEffect(() => {
    if (resource.data && form == null) setForm(record(resource.data.policy));
  }, [resource.data, form]);
  const current = record(resource.data?.current),
    set = (key: string, value: unknown) =>
      setForm((old) => ({ ...old, [key]: value }));
  const Container = embedded ? "div" : Panel;
  return (
    <Container className="compact-settings-card host-metric-settings">
      {!embedded && <PageHeader
        title="默认指标与采集"
        description="采集开关决定是否采集服务器指标；上方各指标开关只控制显示，两者作用不同。采集频率、保留和历史写入间隔在监控数据卡片中管理。"
      />}
      <ErrorLine error={resource.error} />
      {form && (
        <>
          <SettingsSection>
            <label className="check">
              <input
                type="checkbox"
                checked={!!form.enabled}
                onChange={(e) => set("enabled", e.target.checked)}
              />
              采集服务器指标
            </label>
            <div className="host-defaults-row">
              {(
                [
                  ["default_load", "默认负载", LOAD_OPTIONS],
                  ["default_cpu", "默认 CPU", CPU_OPTIONS],
                  ["default_memory", "默认内存", MEMORY_OPTIONS],
                ] as [string, string, string[][]][]
              ).map(([key, label, options]) => (
                <Field key={key} label={label} info={key === "default_load" ? "平均负载表示系统运行和等待任务的压力，不等同于CPU使用率。这里选择两页共用的 1、5 或 15 分钟负载。圆环按 CPU 核数计算进度。" : undefined}>
                  <select
                    value={String(form[key] || options[0]?.[0] || "/")}
                    onChange={(e) => set(key, e.target.value)}
                  >
                    {options.map(([id, title]) => (
                      <option key={id} value={id}>
                        {title}
                      </option>
                    ))}
                  </select>
                </Field>
              ))}
              <div className="field">
                <span>默认网卡</span>
                <DevicePicker
                  kind="network"
                  rows={list(current.interfaces)}
                  value={String(form.default_interface)}
                  onChange={(v) => set("default_interface", v)}
                />
              </div>
              <div className="field">
                <span>默认磁盘</span>
                <DevicePicker
                  kind="disk"
                  rows={list(current.disks)}
                  value={String(form.default_disk)}
                  onChange={(v) => set("default_disk", v)}
                />
              </div>
              <Field
                label="默认分区"
                info="决定运行状态和服务总览默认显示的分区；页面内临时选择不会改变默认值。"
              >
                <select
                  aria-label="默认分区"
                  value={String(form.default_partition || "/")}
                  onChange={(event) =>
                    set("default_partition", event.target.value)
                  }
                >
                  {[
                    ...new Set([
                      String(form.default_partition || "/"),
                      ...list(current.mounts).map((m) => String(m.path)),
                    ]),
                  ].map((path) => (
                    <option key={path} value={path}>
                      {path === "/" ? "/（根分区）" : path}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
          </SettingsSection>
          <div className={embedded ? undefined : "form-footer"}>
            <SaveCard
              showActions={!embedded}
 dirty={hasChanges(form, resource.data?.policy)}
              title="服务器"
              defaults={() =>
                setForm({
                  enabled: true,
                  interval: 2,
                  storage_interval: 60,
                  retention_days: 30,
                  default_interface: "physical",
                  default_disk: "physical",
                  default_load: "load1",
                  default_cpu: "cpu_percent",
                  default_memory: "memory_percent",
                  default_partition: "/",
                })
              }
              discard={() =>
                setForm(structuredClone(record(resource.data?.policy)))
              }
              save={async () => {
                const ok = await act(
                  "/api/system/policy",
                  Object.fromEntries(
                    Object.entries(form).filter(
                      ([key]) =>
                        ![
                          "interval",
                          "retention_days",
                          "max_storage_mb",
                          "storage_interval",
                        ].includes(key),
                    ),
                  ),
                );
                if (ok) void resource.refresh();
                return ok;
              }}
            />
          </div>
        </>
      )}
    </Container>
  );
}

function ServerConnections({ range }: { range: TimeRange }) {
  const [protocol, setProtocol] = useState("all");
  const current = useResource<Dict>("/api/connections/host", 2000);
  const history = useResource<Dict>(
    "/api/connections/business-history?module=host&protocol=" +
      protocol +
      "&" +
      range.query,
    10000,
  );
  const protocols = record(current.data?.protocols);
  const count = current.data?.available
    ? protocol === "all"
      ? Number(current.data.count)
      : Number(protocols[protocol] ?? 0)
    : null;
  return (
    <Panel className="server-connections-panel host-inline-metric-panel">
      <PageHeader
        title="网络连接"
        icon={<ConnectionIcon size={20} />}
        description="主机内核连接跟踪项；支持 TCP、UDP、ICMP 等协议，包含空闲未过期流。"
        titleExtra={
          <div className="inline-metrics server-connection-metrics">
            <span>
              <ConnectionIcon size={14} className="traffic-total" />
              当前连接数 <strong>{count ?? "—"}</strong>
            </span>
          </div>
        }
        actions={
          <label className="server-protocol-picker">
            <select
              value={protocol}
              onChange={(e) => setProtocol(e.target.value)}
            >
              <option value="all">全部协议</option>
              {["tcp", "udp", "icmp", "icmpv6", "other"].map((p) => (
                <option key={p} value={p}>
                  {p.toUpperCase()}
                </option>
              ))}
            </select>
          </label>
        }
      />

      <ErrorLine
        error={
          current.error || String(current.data?.error || "") || history.error
        }
      />
      <HostChart
        data={list(history.data?.items)}
        fields={["count"]}
        labels={["连接数"]}
        unit="count"
        domain={[
          Number(history.data?.start || Date.now() / 1000 - 86400),
          Number(history.data?.end || Date.now() / 1000),
        ]}
      />
    </Panel>
  );
}
