import { ResetFilters } from "./list_controls";
import { useConnectionTableControls } from "./connection_table_controls";
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
  const proxyReady = module !== "xray" || scope === "transport" || summary.count_scope === "confirmed-proxy-v2";
  const rows = list(r.data?.items).filter(
    (v) =>
      (status === "all" || v.state === status) &&
      (protocol === "all" || v.protocol === protocol) &&
      [
        v.source,
        v.destination,
        v.node_id,
        v.interface,
        v.domain,
      ]
        .join(" ")
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  const total = (key: string) => {
    const valid = rows.filter((v) => v[key] != null);
    if (!proxyReady) return "—";
    return valid.length
      ? formatCapacity(valid.reduce((n, v) => n + Number(v[key]), 0))
      : "—";
  };
  const detailsTable = useConnectionTableControls(rows, module + "|" + scope);
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
                域名识别可在Xray代理模式中开启；从核心Webhook的HTTP／TLS／QUIC握手目标提取，已有访问日志仅作补充。不反查IP，不解密HTTPS正文；ECH外层名称可能不是真实域名。将鼠标移到域名或“未观测”上可查看识别依据。Xray
                模块仅统计已确认代理出站的业务连接，直连及阻断已剔除；归属未确认的单列且不计入代理数。确认依据为Xray核心Webhook或访问日志的完整IP连接元组；不以配置推断出站，未收到事件时不猜测归属。
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
            || (!proxyReady ? "当前采集端仍为入口统计口径；需配套更新后端，才能显示剔除直连后的代理业务连接数。" : "")
          }
        />
        <div className="monitor-search-row">
          <AdaptiveSearch>
            <Search size={18} />
            <input
              aria-label="查询连接"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="查询源、目标、域名、接口或节点"
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
                        : module === "xray" ? "已确认代理" : "活动连接"}{" "}
                    <strong>
                      {proxyReady && (scope !== "active" || summary.available)
                        ? rows.filter(v => module !== "xray" || scope === "transport" || v.routing_classification === "proxy").length
                        : "—"}
                      {summary.partial ? "（部分）" : ""}
                    </strong>
                  </span>
                  {module === "xray" && scope === "active" && <span>未确认 <strong>{proxyReady ? Number(summary.unclassified_count || 0) : rows.length}</strong> · 已排除直连 <strong>{proxyReady ? Number(summary.direct_excluded || 0) : "—"}</strong></span>}
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
                {proxyReady ? <HostChart
                  data={list(chart.data?.items)}
                  fields={
                    metric === "count" ? ["count"] : ["upload", "download"]
                  }
                  labels={metric === "count" ? [module === "xray" ? "已确认代理" : "活动连接"] : ["上传", "下载"]}
                  unit={metric === "count" ? "count" : "rate"}
                  domain={[
                    Number(chart.data?.start || Date.now() / 1000 - 86400),
                    Number(chart.data?.end || Date.now() / 1000),
                  ]}
                /> : <p className="small muted">等待新的代理业务连接采样，旧入口总量不作为代理曲线展示。</p>}
              </>
            )}
          </Panel>
        ),
        details: (
          <Panel className="connections-table-panel">
            <PageHeader title="连接详情" icon={<List size={20} />} description="列头可排序或筛选，数值按实际字节、速率或秒数比较；搜索和列筛选作用于当前已加载连接，不改变上方历史曲线。" actions={detailsTable.toolbar} />
            <Table
              rows={detailsTable.rows}
              paginationResetKey={detailsTable.paginationKey + "|" + [query,protocol,iface,node,status].join("|")}
              total={list(r.data?.items).length}
              rowKey={(v) => String(v.id)}
              columns={detailsTable.columns([
                {
                  key: "serial",
                  label: "序号",
                  render: (_, i) => i + 1,
                },
                {
                  key: "source",
                  label: "源",
                  width: detailsTable.addressWidth("source"),
                  render: (v) => <span className="connection-address" title={String(v.source)}>{String(v.source)}</span>,
                },
                {
                  key: "destination",
                  label: "目标",
                  width: detailsTable.addressWidth("destination"),
                  render: (v) => <span className="connection-address" title={String(v.destination)}>{String(v.destination)}</span>,
                },
                {
                  key: "protocol",
                  label: "协议",
                  width: "100px",
                  render: (v) => String(v.protocol).toUpperCase(),
                },
                {
                  key: "domain",
                  label: "域名",
                  width: "320px",
                  render: (v) => (
                    <span className="connection-domain" title={[String(v.domain || "未观测"), String(v.domain_note || "")].filter(Boolean).join("\n")}>
                      {String(v.domain || "未观测")}
                    </span>
                  ),
                },
                {
                  key: "state",
                  label: "状态",
                  width: "144px",
                  render: (v) =>
                    ["近期有流量", "Active"].includes(String(v.state)) ? (
                      <span className="connection-active-status">
                        Active <SettingInfo label="Active" text="近期有流量" />
                      </span>
                    ) : (
                      String(v.state)
                    ),
                },
                ...(module === "xray" && scope !== "transport" ? [{key:"routing",label:"出站",width:"160px",render:(v: Dict)=><span title={String(v.routing_note || "旧记录未采集出站归属")}>{v.routing_classification === "proxy" ? "代理" : "未确认（不计入代理数）"}</span>}] : []),
                {
                  key: "interface",
                  label: "接口",
                  width: "144px",
                  render: (v) => (
                    <>
                      <span>
                        {String(
                          v.interface || "未识别",
                        )}
                      </span>
                      <small>{String(v.kind || "")}</small>
                    </>
                  ),
                },
                {
                  key: "upload",
                  label: "上传数据／速率",
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
                  label: "下载数据／速率",
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
              ])}
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
