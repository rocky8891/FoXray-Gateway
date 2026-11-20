import { ResetFilters } from "./list_controls";
import { SortIcon as ArrowUpDown } from "./resource_controls";
import { SortHeading } from "./list_controls";
import { NodeManagementIcon, TrafficRangeIcon } from "./requirement_icons";
import { useMonitorSections } from "./monitor_sections";
import {
  trafficWindow,
  TrafficWindowControl,
  useTrafficWindow,
} from "./traffic_window";
import { TrafficHistorySettings } from "./traffic_history_settings";
import { HostChart, formatRate, formatCapacity } from "./host";
import { useTimeRange, TimeRangeControl, type TimeRange } from "./time_range";
import { FilterBar, filterNodes, filterDefaults, type Filters } from "./nodes";

import { useMemo, useState, useEffect } from "react";
import {
  RefreshCw,
  ArrowDown,
  ArrowUp,
  ChartNoAxesCombined,
  SlidersHorizontal,
  Table2,
  Sigma,
} from "lucide-react";

import { list, text, timestamp, type Dict, type State } from "./api";
import {
  Button,
  ErrorLine,
  Field,
  PageHeader,
  Panel,
  Modal,
  Table,
  useResource,
  type Column,
} from "./ui";
import type { Act } from "./nodes";
const colors = [
  "#00c99a",
  "#7b89ff",
  "#f4b85c",
  "#e77faa",
  "#68c8ef",
  "#b18af4",
  "#e4d06b",
  "#7bc18c",
  "#d6926b",
  "#92a8c9",
];
export function trafficGaps(rows: Dict[], interval: number) {
  const out: Dict[] = [];
  let previous = 0;
  for (const row of [...rows].sort((a, b) => Number(a.time) - Number(b.time))) {
    const stamp = Number(row.time);
    if (previous && stamp - previous > interval * 1.8)
      out.push({ time: previous + interval });
    out.push(row);
    previous = stamp;
  }
  return out;
}
function bytes(value: unknown, rate = false) {
  if (value == null || !Number.isFinite(Number(value)) || Number(value) < 0)
    return "—";
  return rate ? formatRate(value) : formatCapacity(value);
}
export function TrafficPage({
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
  const [historySettings, setHistorySettings] = useState(false);
  const [aggregation, setAggregation] = useState("auto");
  const chartWindow = useTrafficWindow();
  const module =
    selectedModule ||
    (state.nodes.some((n) => n.id === "warp") ? "warp" : "xray");
  const isWireGuard = module === "wireguard";
  const defaults = {
    ...filterDefaults,
    status: module === "xray" ? "enabled" : "all",
  };
  const live = useResource<Dict>("/api/traffic", 2000);
  const [filters, setFilters] = useState<Filters>({
    ...defaults,
  });
  const visibleIDs = new Set(filterNodes(state, filters).map((n) => n.id));
  const localRange = useTimeRange();
  const timeRange = range || localRange;
  const [node, setNode] = useState("all"),
    [metric, setMetric] = useState("both"),
    [sort, setSort] = useState(() => {
      try {
        const saved = JSON.parse(
          localStorage.getItem("foxray.traffic.sort") || "null",
        );
        if (
          saved &&
          [
            "name",
            "source",
            "status",
            "time",
            "upload_bps",
            "download_bps",
            "total_upload",
            "total_download",
            "total_traffic",
          ].includes(saved.key) &&
          (saved.direction === 1 || saved.direction === -1)
        )
          return saved as { key: string; direction: number };
      } catch {}
      return { key: "name", direction: 1 };
    });
  useEffect(() => {
    localStorage.setItem("foxray.traffic.sort", JSON.stringify(sort));
  }, [sort]);
  const history = useResource<Dict>(
    "/api/traffic/history?module=" +
      module +
      "&" +
      timeRange.query +
      "&window=" +
      trafficWindow(timeRange, aggregation, chartWindow),
    60000,
  );
  const rangeByID = useMemo(() => {
    const totals = new Map<string, { upload: number; download: number }>();
    if (history.error || history.data?.complete === false) return totals;
    for (const sample of list(history.data?.items)) {
      if (sample.live_tail) continue;
      const upload = Number(sample.upload),
        download = Number(sample.download);
      if (
        !Number.isFinite(upload) ||
        !Number.isFinite(download) ||
        upload < 0 ||
        download < 0
      )
        continue;
      const id = String(sample.id),
        previous = totals.get(id);
      totals.set(id, {
        upload: (previous?.upload || 0) + upload,
        download: (previous?.download || 0) + download,
      });
    }
    return totals;
  }, [history.data, history.loading, history.error]);
  const liveByID = new Map(
    list(live.data?.items).map((r) => [String(r.id), r]),
  );
  const all = state.nodes
      .map(
        (n) =>
          liveByID.get(n.id) || {
            id: n.id,
            name: n.name,
            source: isWireGuard
              ? "WireGuard"
              : module === "warp"
                ? "WARP"
                : "Xray",
            available: false,
            status: "暂无可读取的接口计数",
          },
      )
      .sort((a, b) => String(a.id).localeCompare(String(b.id))),
    chosen = all.filter(
      (r) => visibleIDs.has(String(r.id)) && (node === "all" || r.id === node),
    ),
    available = chosen.filter((r) => r.available);
  const current = String(state.state.active),
    ids = new Set(chosen.map((r) => String(r.id)));
  const rows = chosen
    .map((row): Dict => {
      const totals = rangeByID.get(String(row.id));
      return {
        ...row,
        total_upload: totals?.upload ?? null,
        total_download: totals?.download ?? null,
      };
    })
    .sort((a, b) => {
      const value = (r: Dict): string | number | null => {
        if (["name", "source", "status"].includes(sort.key))
          return String(r[sort.key] || "");
        if (["upload_bps", "download_bps"].includes(sort.key) && !r.available)
          return null;
        if (
          ["total_upload", "total_download", "total_traffic"].includes(sort.key)
        ) {
          if (r.total_upload == null || r.total_download == null) return null;
          return sort.key === "total_traffic"
            ? Number(r.total_upload) + Number(r.total_download)
            : Number(r[sort.key]);
        }
        return Number(r[sort.key] || 0);
      };
      const x = value(a),
        y = value(b);
      if (x == null && y != null) return 1;
      if (y == null && x != null) return -1;
      const compared =
        typeof x === "string" && typeof y === "string"
          ? x.localeCompare(y, "zh-CN", { numeric: true })
          : Number(x) - Number(y);
      return (
        compared * sort.direction || String(a.id).localeCompare(String(b.id))
      );
    });
  const idsKey = [...ids].sort().join(",");
  const samples = useMemo(() => {
    if (history.error || history.data?.complete === false) return [];
    const selected = new Set(idsKey.split(","));
    return list(history.data?.items).filter((r) => selected.has(String(r.id)));
  }, [history.data, history.error, idsKey]);
  const observed = samples.some((r) => !r.live_tail);
  const rangeTotal = (key: string) =>
    observed
      ? samples.reduce(
          (sum, r) => sum + (r.live_tail ? 0 : Number(r[key] || 0)),
          0,
        )
      : null;
  const points = useMemo(() => {
    const buckets = new Map<number, Dict>();
    for (const r of samples) {
      const stamp = Number(r.time);
      const p = buckets.get(stamp) || { time: stamp };
      p[String(r.id)] =
        metric === "total_rate"
          ? Number(r.upload_bps) + Number(r.download_bps)
          : r[metric];
      buckets.set(stamp, p);
    }
    return trafficGaps(
      [...buckets.values()].sort((a, b) => Number(a.time) - Number(b.time)),
      Math.max(
        Number(history.data?.bucket_seconds || 60),
        Number(history.data?.sampling_interval || live.data?.interval || 1),
      ),
    );
  }, [samples, metric]);
  const graphTargets = JSON.stringify(
    chosen.map((r) => ({ id: String(r.id), name: String(r.name) })),
  );
  const plotSeries = useMemo(() => {
    const chosen: { id: string; name: string }[] = JSON.parse(graphTargets);
    return metric === "both"
      ? ["download_bps", "upload_bps"].map((direction) => {
          const buckets = new Map<number, Map<string, Dict>>();
          const bucket = Math.max(
            Number(history.data?.bucket_seconds || 60),
            Number(history.data?.sampling_interval || live.data?.interval || 1),
          );
          for (const r of samples) {
            const stamp = Math.floor(Number(r.time) / bucket) * bucket;
            const group = buckets.get(stamp) || new Map<string, Dict>();
            if (
              !group.has(String(r.id)) ||
              Number(group.get(String(r.id))?.time) < Number(r.time)
            )
              group.set(String(r.id), r);
            buckets.set(stamp, group);
          }
          return {
            key: direction,
            label: direction === "upload_bps" ? "上传" : "下载",
            color:
              direction === "upload_bps"
                ? "var(--metric-orange)"
                : "var(--accent)",
            data: trafficGaps(
              [...buckets].map(([, rows]) => ({
                time: Math.max(
                  ...[...rows.values()].map((r) => Number(r.time)),
                ),
                [direction]: [...rows.values()].reduce(
                  (n, r) => n + Number(r[direction] || 0),
                  0,
                ),
              })),
              bucket,
            ),
          };
        })
      : chosen.map((r, index) => ({
          key: String(r.id),
          label: String(r.name),
          color: colors[index % colors.length],
          data: trafficGaps(
            samples
              .filter((x) => x.id === r.id)
              .map((x) => ({
                time: x.time,
                [String(r.id)]:
                  metric === "total_rate"
                    ? Number(x.upload_bps) + Number(x.download_bps)
                    : x[metric],
              })),
            Math.max(
              Number(history.data?.bucket_seconds || 60),
              Number(
                history.data?.sampling_interval || live.data?.interval || 1,
              ),
            ),
          ),
        }));
  }, [samples, metric, graphTargets, history.data?.bucket_seconds]);
  const layout = useMonitorSections(module, "traffic", act, {
    trend: {
      kind: "area",
      series: plotSeries.map((row) => row.label),
      colors: plotSeries.map((row) => row.color),
    },
  });
  const headings = (key: string, label: string) => (
    <SortHeading
      label={label}
      direction={sort.key === key ? sort.direction : undefined}
      onClick={() =>
        setSort({
          key,
          direction:
            sort.key === key
              ? -sort.direction
              : ["name", "source", "status", "time"].includes(key)
                ? 1
                : -1,
        })
      }
    />
  );
  const columns: Column<Dict>[] = [
    { key: "serial", label: "序号", render: (_, i) => i + 1 },
    {
      key: "name",
      label: isWireGuard ? "接口" : "节点",
      heading: headings("name", isWireGuard ? "接口" : "节点"),
      render: (r) => (
        <>
          <strong className="node-name-full" title={String(r.name)}>
            {text(r.name)}
          </strong>
          <small>
            {state.nodes.find((n) => n.id === r.id)?.protocol ||
              String(r.source)}
            {r.id === current && (
              <>
                {" "}
                · <span className="current-exit-marker">当前出口</span>
              </>
            )}
          </small>
        </>
      ),
    },
    {
      key: "source",
      label: "类型",
      heading: headings("source", "类型"),
      render: (r) => text(r.source),
    },
    {
      key: "upload",
      label: "实时上传",
      heading: headings("upload_bps", "实时上传"),
      render: (r) => (
        <span className="traffic-up">
          {r.available ? bytes(r.upload_bps, true) : "—"}
        </span>
      ),
    },
    {
      key: "download",
      label: "实时下载",
      heading: headings("download_bps", "实时下载"),
      render: (r) => (
        <span className="traffic-down">
          {r.available ? bytes(r.download_bps, true) : "—"}
        </span>
      ),
    },
    {
      key: "total_up",
      label: "累计上传",
      heading: headings("total_upload", "累计上传"),
      render: (r) => bytes(r.total_upload),
    },
    {
      key: "total_down",
      label: "累计下载",
      heading: headings("total_download", "累计下载"),
      render: (r) => bytes(r.total_download),
    },
    {
      key: "total_traffic",
      label: "下载＋上传累计",
      heading: headings("total_traffic", "下载＋上传累计"),
      render: (r) =>
        r.total_upload == null || r.total_download == null
          ? "—"
          : bytes(Number(r.total_upload) + Number(r.total_download)),
    },
    {
      key: "status",
      label: "采集状态",
      heading: headings("status", "采集状态"),
      render: (r) => (
        <>
          <span className={r.available ? "accent" : "muted"}>
            {r.available ? "可读取" : "不可读取"}
          </span>
          <small>{text(r.status)}</small>
        </>
      ),
    },
    {
      key: "time",
      label: "采样时间",
      heading: headings("time", "采样时间"),
      render: (r) => timestamp(r.time),
    },
  ];
  return (
    <div className="traffic-page">
      {historySettings && (
        <Modal title="流量记录设置" onClose={() => setHistorySettings(false)}>
          <TrafficHistorySettings act={act} module={module} />
        </Modal>
      )}
      <Panel className="traffic-filters">
        <PageHeader
          actions={layout.control}
          title="流量监控"
          titleControl={viewControl}
          icon={<SlidersHorizontal size={20} />}
          description="下方实时流量与统计表共用查询、筛选、时间范围和指标"
        />
        <FilterBar
          state={state}
          value={{ ...filters, sort: sort.key, direction: sort.direction }}
          onChange={(v) => {
            setFilters(v);
            setSort({ key: v.sort, direction: v.direction });
          }}
          monitor
          interfaceMode={isWireGuard}
          rangeControl={<TimeRangeControl range={timeRange} />}
          refreshControl={
            <Button
              onClick={() => {
                void live.refresh();
                void history.refresh();
              }}
            >
              <RefreshCw size={14} />
              刷新
            </Button>
          }
          sortOptions={[
            ["name", isWireGuard ? "接口名称" : "节点名称"],
            ["upload_bps", "实时上传"],
            ["download_bps", "实时下载"],
            ["total_traffic", "累计流量"],
            ["time", "采样时间"],
          ]}
          toolbar={
            <>
              <ResetFilters
                active={
                  !!filters.query ||
                  filters.group !== "all" ||
                  filters.protocol !== "all" ||
                  filters.status !== defaults.status ||
                  node !== "all" ||
                  metric !== "both" ||
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
                    ...defaults,
                  });
                  setNode("all");
                  setMetric("both");
                  timeRange.reset();
                  setSort({ key: "name", direction: 1 });
                }}
              />
            </>
          }
        >
          {" "}
          <Field label={isWireGuard ? "接口" : "节点"}>
            <select value={node} onChange={(e) => setNode(e.target.value)}>
              <option value="all">
                {isWireGuard ? "全部接口" : "全部节点"}
              </option>
              {all.map((r) => (
                <option
                  key={String(r.id)}
                  value={String(r.id)}
                  className={
                    r.id === current ? "current-exit-option" : undefined
                  }
                >
                  {String(r.name)}
                  {r.id === current ? "（当前出口）" : ""}
                </option>
              ))}
            </select>
          </Field>
          <Field label="指标">
            <select value={metric} onChange={(e) => setMetric(e.target.value)}>
              <option value="both">上传 / 下载合计</option>
              <option value="download_bps">下载速率</option>
              <option value="upload_bps">上传速率</option>
              <option value="total_rate">上传＋下载</option>
            </select>
          </Field>
        </FilterBar>
      </Panel>
      {layout.render({
        trend: (
          <Panel className="traffic-history">
            <PageHeader
              title="流量趋势"
              titleExtra={
                <div className="chart-title-stats">
                  <TrafficWindowControl
                    compact
                    value={aggregation}
                    onChange={setAggregation}
                  />
                  <div className="inline-metrics traffic-overview traffic-inline-metrics">
                    <span>
                      <NodeManagementIcon size={16} className="traffic-total" />
                      可读取{isWireGuard ? "接口" : "节点"}{" "}
                      <strong>
                        {available.length} / {chosen.length}
                      </strong>
                    </span>

                    <span>
                      <ArrowUp size={16} className="traffic-up" />
                      实时上传{" "}
                      <strong className="traffic-up">
                        {bytes(
                          available.length
                            ? available.reduce(
                                (n, r) => n + Number(r.upload_bps || 0),
                                0,
                              )
                            : null,
                          true,
                        )}
                      </strong>
                    </span>
                    <span>
                      <ArrowDown size={16} className="traffic-down" />
                      实时下载{" "}
                      <strong className="traffic-down">
                        {bytes(
                          available.length
                            ? available.reduce(
                                (n, r) => n + Number(r.download_bps || 0),
                                0,
                              )
                            : null,
                          true,
                        )}
                      </strong>
                    </span>
                    <span className="range-flow-totals">
                      <span className="muted range-flow-label">
                        <TrafficRangeIcon className="traffic-total" />
                        {timeRange.label}流量
                      </span>
                      <span>
                        <ArrowUp size={14} className="traffic-up" />
                        上传{" "}
                        <strong className="traffic-up">
                          {bytes(rangeTotal("upload"))}
                        </strong>
                      </span>
                      <span>
                        <ArrowDown size={14} className="traffic-down" />
                        下载{" "}
                        <strong className="traffic-down">
                          {bytes(rangeTotal("download"))}
                        </strong>
                      </span>
                      <span>
                        <Sigma size={14} className="traffic-total" />
                        总计{" "}
                        <strong className="traffic-total">
                          {bytes(
                            observed
                              ? Number(rangeTotal("upload")) +
                                  Number(rangeTotal("download"))
                              : null,
                          )}
                        </strong>
                      </span>
                    </span>
                  </div>
                </div>
              }
              actions={
                <Button onClick={() => setHistorySettings(true)}>
                  记录间隔设置
                </Button>
              }
              icon={<ChartNoAxesCombined size={20} />}
              description="按真实字节汇总区间均速；汇总窗口只改变图表，累计量与采集频率不变，缺测保留断点"
            />
            <ErrorLine error={history.error} />
            <HostChart
              data={points}
              fields={[]}
              labels={[]}
              domain={[
                Number(history.data?.start || Date.now() / 1000 - 86400),
                Number(history.data?.end || Date.now() / 1000),
              ]}
              series={plotSeries}
            />
          </Panel>
        ),
        statistics: (
          <Panel className="traffic-statistics">
            <PageHeader
              title="流量统计"
              icon={<Table2 size={20} />}
              description={
                <>
                  {`统计范围：${timeRange.label}；实时速率为当前采样，累计流量按所选范围统计。${history.loading ? "正在更新…" : ""}`}
                  <p>
                    所选范围没有历史样本或读取失败时，累计显示“—”；已采集的零流量显示为
                    0。 历史采集与清理范围以监控设置为准。
                  </p>
                </>
              }
            />
            <ErrorLine error={history.error} />
            <Table
              columnSizingKey={"traffic-" + module}
              rows={rows}
              total={all.length}
              columns={columns}
              rowKey={(r) => String(r.id)}
            />
          </Panel>
        ),
      })}
    </div>
  );
}

export function CurrentExitTraffic({
  state,
  range: selectedRange,
}: {
  state: State;
  range?: TimeRange;
}) {
  const chartWindow = useTrafficWindow();
  const [aggregation, setAggregation] = useState("auto");
  const active = state.state.active,
    live = useResource<Dict>("/api/traffic", 2000),
    localRange = useTimeRange();
  const timeRange = selectedRange || localRange;
  const history = useResource<Dict>(
    "/api/traffic/history?node=" +
      encodeURIComponent(active) +
      "&" +
      timeRange.query +
      "&window=" +
      trafficWindow(timeRange, aggregation, chartWindow),
    60000,
  );
  const row = list(live.data?.items).find((r) => r.id === active);
  const points = useMemo(
    () =>
      trafficGaps(
        list(history.data?.items),
        Math.max(
          Number(history.data?.bucket_seconds || 60),
          Number(history.data?.sampling_interval || live.data?.interval || 1),
        ),
      ),
    [history.data],
  );
  return (
    <section
      className="exit-chart-section current-exit-traffic"
      data-monitor="traffic"
    >
      <div className="page-heading">
        <h3 className="metric-heading tone-green">
          <ChartNoAxesCombined size={18} />
          实时流量
          <TrafficWindowControl
            compact
            value={aggregation}
            onChange={setAggregation}
          />
        </h3>
        {!selectedRange && <TimeRangeControl range={timeRange} />}
        <div className="current-flow-metrics inline">
          <span>
            <ArrowUp size={14} className="traffic-up" /> 上传{" "}
            <strong className="traffic-up">
              {row?.available ? bytes(row.upload_bps, true) : "—"}
            </strong>
          </span>
          <span>
            <ArrowDown size={14} className="traffic-down" /> 下载{" "}
            <strong className="traffic-down">
              {row?.available ? bytes(row.download_bps, true) : "—"}
            </strong>
          </span>
          <span className="traffic-total">
            <ArrowUpDown size={14} /> {timeRange.label}累计{" "}
            <strong>
              {list(history.data?.items).length
                ? bytes(
                    list(history.data?.items).reduce(
                      (sum, r) =>
                        sum + Number(r.upload || 0) + Number(r.download || 0),
                      0,
                    ),
                  )
                : "—"}
            </strong>
          </span>
        </div>
      </div>
      <ErrorLine error={live.error || history.error} />
      <div className="current-flow-chart">
        <HostChart
          data={points}
          fields={["download_bps", "upload_bps"]}
          labels={["下载", "上传"]}
          legend={false}
          domain={[
            Number(history.data?.start || Date.now() / 1000 - 86400),
            Number(history.data?.end || Date.now() / 1000),
          ]}
        />
      </div>
    </section>
  );
}
