import { ResetFilters } from "./list_controls";
import { AdaptiveSearch } from "./adaptive_search";
import { useMonitorSections } from "./monitor_sections";
import type { Act } from "./nodes";
import { ConnectionIcon } from "./monitor_icons";
import { useEffect, useState, type ReactNode } from "react";
import {
  ArrowUp,
  ArrowDown,
  Clock,
  RefreshCw,
  ChartNoAxesCombined,
  List,
  Search,
} from "lucide-react";
import { list, record, timestamp, type Dict } from "./api";
import { useTimeRange, TimeRangeControl } from "./time_range";
import { formatCapacity, formatRate, HostChart } from "./host";
import {
  Button,
  Field,
  SettingInfo,
  Panel,
  PageHeader,
  Table,
  ErrorLine,
  Modal,
  JsonDetails,
  useResource,
} from "./ui";
export function ConnectionsPage({
  module,
  act,
  viewControl,
}: {
  module: string;
  act: Act;
  viewControl?: ReactNode;
}) {
  const range = useTimeRange();
  const [scope, setScope] = useState("active"),
    [iface, setIface] = useState("all"),
    [node, setNode] = useState("all"),
    [status, setStatus] = useState("all"),
    [metric, setMetric] = useState("count");
  const layout = useMonitorSections(module, "connections", act, {
    trend:
      scope === "transport"
        ? {
            kind: "message",
            title: "活动连接",
            message: "隧道传输诊断仅展示当前列表；切换业务连接可查看历史曲线。",
          }
        : {
            kind: "area",
            title: "活动连接",
            series: metric === "count" ? ["活动连接"] : ["上传", "下载"],
          },
  });
  const [query, setQuery] = useState(""),
    [protocol, setProtocol] = useState("all"),
    [detail, setDetail] = useState<Dict | null>(null);
  useEffect(() => {
    setNode("all");
    setIface("all");
    setStatus("all");
    setProtocol("all");
  }, [module]);

  const filters =
    "module=" +
    module +
    "&protocol=" +
    protocol +
    "&interface=" +
    encodeURIComponent(iface) +
    "&node=" +
    encodeURIComponent(node);
  const r = useResource<Dict>(
    "/api/connections?" + filters + "&scope=" + scope + "&" + range.query,
    2000,
  );
  const chart = useResource<Dict>(
    "/api/connections/business-history?" + filters + "&" + range.query,
    10000,
  );
  const summary = record(record(r.data?.summaries)[module]);
  const rows = list(r.data?.items).filter(
    (v) =>
      (status === "all" || v.state === status) &&
      (protocol === "all" || v.protocol === protocol) &&
      [
        v.source,
        v.destination,
        v.node_id,
        v.process,
        v.interface,
        v.domain,
        v.application,
      ]
        .join(" ")
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  const total = (key: string) => {
    const valid = rows.filter((v) => v[key] != null);
    return valid.length
      ? formatCapacity(valid.reduce((n, v) => n + Number(v[key]), 0))
      : "—";
  };
  return (
    <div className="connections-page">
      <Panel className="connections-controls">
        <PageHeader
          actions={layout.control}
          title="活动连接监控"
          titleControl={viewControl}
          icon={<ConnectionIcon size={20} />}
          description={
            <>
              查看真实业务连接、双向流量和历史趋势；可切换到隧道传输诊断。
              {scope === "transport" && (
                <p>隧道传输诊断仅展示当前列表；切换业务连接可查看历史曲线。</p>
              )}
              <p>
                {String(
                  summary.note ||
                    "实际经过隧道的业务IP连接，Xray不猜测出站节点。",
                )}
              </p>
              <p>{String(r.data?.note || "")}</p>
              <p>
                域名仅读取已配置的 Xray
                访问日志，没有匹配域名证据时显示“未观测”；不自动开启日志、不反查
                IP。应用仅通过本机套接字精确匹配进程名，转发客户端的进程信息不会随TCP／UDP连接传到网关，需客户端提供。将鼠标移到“未观测／未识别”上可查看本条原因。Xray
                模块统计包括所有实际经过该模块的业务连接，不能可靠拆分为当前出口节点连接数。
              </p>
              <p>
                字节为连接观测累计，不等于所选范围内新增量；“—”表示尚无计数。首个采样不显示虚构速率。
              </p>
              <p>
                图表跟随时间、协议和接口筛选；关键词、状态用于明细。详情每5秒批量保存，曲线按分钟分组峰值记录，缺失采样不补零，保留周期和大小以本模块监控设置为准。
              </p>
            </>
          }
        />
        <ErrorLine
          error={
            r.error ||
            String(r.data?.error || "") ||
            (scope === "active" ? String(summary.error || "") : "")
          }
        />
        <div className="monitor-search-row">
          <AdaptiveSearch>
            <Search size={18} />
            <input
              aria-label="查询连接"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="查询来源、目标、域名、应用、接口或节点"
            />
          </AdaptiveSearch>
          <TimeRangeControl range={range} />
          <ResetFilters
            active={
              !!query ||
              protocol !== "all" ||
              iface !== "all" ||
              node !== "all" ||
              status !== "all" ||
              scope !== "active" ||
              range.isChanged
            }
            summary={[
              query && `搜索：${query}`,
              `协议：${protocol}`,
              `接口：${iface}`,
              `状态：${status}`,
            ]
              .filter(Boolean)
              .join(" · ")}
            onClick={() => {
              setQuery("");
              setProtocol("all");
              setIface("all");
              setNode("all");
              setStatus("all");
              setMetric("count");
              setScope("active");
            }}
          />
          <Button
            onClick={() => {
              void r.refresh();
              void chart.refresh();
            }}
          >
            <RefreshCw size={14} />
            刷新
          </Button>
        </div>
        <div className="filter-bar connections-filters">
          <Field label="连接范围">
            <select
              value={scope}
              onChange={(e) => {
                setScope(e.target.value);
                setNode("all");
                setStatus("all");
              }}
            >
              <option value="active">当前业务连接</option>
              <option value="history">范围内观测连接</option>
              <option value="transport">隧道传输诊断</option>
            </select>
          </Field>
          <Field label="接口">
            <select value={iface} onChange={(e) => setIface(e.target.value)}>
              <option value="all">全部接口</option>
              {[
                ...new Set(
                  list(r.data?.items)
                    .map((v) => String(v.interface || ""))
                    .filter(Boolean),
                ),
              ].map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </Field>
          <Field label="状态">
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="all">全部状态</option>
              {[
                ...new Set(list(r.data?.items).map((v) => String(v.state))),
              ].map((v) => (
                <option key={v} value={v}>
                  {v === "近期有流量" ? "Active" : v}
                </option>
              ))}
            </select>
          </Field>
          <Field label="协议">
            <select
              value={protocol}
              onChange={(e) => setProtocol(e.target.value)}
            >
              <option value="all">全部协议</option>
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
            </select>
          </Field>
          <Field
            label="节点"
            info={
              module === "xray" && scope !== "transport"
                ? "当前业务流没有Xray核心逐连接出站标签，不能按当前出口猜测节点归属。接入核心路由日志并匹配成功后才可归属；隧道传输诊断可按已识别节点筛选。"
                : "按已识别的节点或WireGuard接口筛选。"
            }
          >
            <select
              aria-label="活动连接节点"
              value={node}
              disabled={module === "xray" && scope !== "transport"}
              onChange={(e) => setNode(e.target.value)}
            >
              <option value="all">
                {module === "xray" && scope !== "transport"
                  ? "模块业务总量（节点未归属）"
                  : "全部已识别节点"}
              </option>
              {[
                ...new Map(
                  list(r.data?.items)
                    .filter((v) => v.node_id)
                    .map((v) => [
                      String(v.node_id),
                      String(v.node_name || v.node_id),
                    ]),
                ).entries(),
              ].map(([id, name]) => (
                <option value={id} key={id}>
                  {name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="指标">
            <select
              aria-label="连接图表指标"
              value={metric}
              onChange={(e) => setMetric(e.target.value)}
            >
              <option value="count">连接数</option>
              <option value="rate">上传 / 下载速率</option>
            </select>
          </Field>
        </div>
      </Panel>
      {layout.render({
        trend: (
          <Panel className="connections-chart-panel">
            <PageHeader
              title="活动连接"
              titleExtra={
                <div className="inline-metrics chart-inline-stats">
                  <span>
                    <ConnectionIcon size={14} className="traffic-total" />
                    {scope === "history"
                      ? "观测连接"
                      : scope === "transport"
                        ? "传输连接"
                        : "活动连接"}{" "}
                    <strong>
                      {scope !== "active" || summary.available
                        ? rows.length
                        : "—"}
                      {summary.partial ? "（部分）" : ""}
                    </strong>
                  </span>
                  <span>
                    <ArrowUp size={14} className="traffic-up" />
                    上传字节 <strong>{total("upload")}</strong>
                  </span>
                  <span>
                    <ArrowDown size={14} className="traffic-down" />
                    下载字节 <strong>{total("download")}</strong>
                  </span>
                  <span>
                    <Clock size={14} className="traffic-total" />
                    采集时间 {timestamp(r.data?.time)}
                  </span>
                </div>
              }
              icon={<ChartNoAxesCombined size={20} />}
            />
            {scope !== "transport" && (
              <>
                <ErrorLine
                  error={chart.error || String(chart.data?.error || "")}
                />
                <HostChart
                  data={list(chart.data?.items)}
                  fields={
                    metric === "count" ? ["count"] : ["upload", "download"]
                  }
                  labels={metric === "count" ? ["活动连接"] : ["上传", "下载"]}
                  unit={metric === "count" ? "count" : "rate"}
                  domain={[
                    Number(chart.data?.start || Date.now() / 1000 - 86400),
                    Number(chart.data?.end || Date.now() / 1000),
                  ]}
                />
              </>
            )}
          </Panel>
        ),
        details: (
          <Panel className="connections-table-panel">
            <PageHeader title="连接详情" icon={<List size={20} />} />
            <Table
              rows={rows}
              total={
                scope === "active"
                  ? Number(summary.count || 0)
                  : list(r.data?.items).length
              }
              rowKey={(v) => String(v.id)}
              columns={[
                {
                  key: "serial",
                  label: "序号",
                  render: (_, i) => i + 1,
                },
                {
                  key: "source",
                  label: "来源",
                  render: (v) => String(v.source),
                },
                {
                  key: "destination",
                  label: "目标",
                  render: (v) => String(v.destination),
                },
                {
                  key: "domain",
                  label: "域名线索",
                  render: (v) => (
                    <span title={String(v.domain_note || "历史记录未采集域名")}>
                      {String(v.domain || "未观测")}
                    </span>
                  ),
                },
                {
                  key: "application",
                  label: "本机进程",
                  render: (v) => (
                    <span
                      title={String(v.application_note || "历史记录未采集应用")}
                    >
                      {String(v.application || "未识别")}
                    </span>
                  ),
                },
                {
                  key: "protocol",
                  label: "协议",
                  render: (v) => String(v.protocol).toUpperCase(),
                },
                {
                  key: "state",
                  label: "状态",
                  render: (v) =>
                    ["近期有流量", "Active"].includes(String(v.state)) ? (
                      <span className="connection-active-status">
                        Active <SettingInfo label="Active" text="近期有流量" />
                      </span>
                    ) : (
                      String(v.state)
                    ),
                },
                {
                  key: "node",
                  label: "节点 / 接口",
                  render: (v) => (
                    <>
                      <span>
                        {String(
                          v.node_name || v.node_id || v.interface || "未归属",
                        )}
                      </span>
                      <small>{String(v.kind || "")}</small>
                    </>
                  ),
                },
                {
                  key: "upload",
                  label: "上传数据 / 速率",
                  render: (v) => (
                    <>
                      {v.upload == null ? "—" : formatCapacity(v.upload)}
                      <small>
                        {v.upload_bps == null ? "—" : formatRate(v.upload_bps)}
                      </small>
                    </>
                  ),
                },
                {
                  key: "download",
                  label: "下载数据 / 速率",
                  render: (v) => (
                    <>
                      {v.download == null ? "—" : formatCapacity(v.download)}
                      <small>
                        {v.download_bps == null
                          ? "—"
                          : formatRate(v.download_bps)}
                      </small>
                    </>
                  ),
                },
                {
                  key: "age",
                  label: "观测时长",
                  render: (v) =>
                    Math.floor(Number(v.observed_seconds || 0)) + " 秒",
                },
                {
                  key: "actions",
                  label: "操作",
                  render: (v) => (
                    <Button onClick={() => setDetail(v)}>详情</Button>
                  ),
                },
              ]}
            />

            {detail && (
              <Modal
                title="活动连接详情"
                copyText={JSON.stringify(detail, null, 2)}
                onClose={() => setDetail(null)}
                wide
              >
                <JsonDetails data={detail} />
              </Modal>
            )}
          </Panel>
        ),
      })}
    </div>
  );
}
