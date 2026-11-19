import { hasChanges } from "./settings_save";
import { AdaptiveSearch } from "./adaptive_search";
import { ResetFilters } from "./list_controls";
import {
  DetectionControl,
  useWarpMetricSelection,
  warpTaskProgress,
} from "./warp_detection_control";
import { ListFilter, SortHeading } from "./list_controls";
import { Search, RefreshCw, RotateCcw, PlugZap, Unplug } from "lucide-react";
import { ActionMenu } from "./action_menu";
import { HistoryIntervalSettings } from "./history_interval_settings";
import { NumberInput } from "./ui";
import { DNSPage } from "./dns";
import { labelIcon } from "./ui_icons";
import { SaveCard, SettingsSaveProvider } from "./settings_save";
import { useEffect, useState } from "react";
import {
  api,
  errorMessage,
  list,
  record,
  text,
  timestamp,
  bandwidth,
  type Dict,
  type Task,
} from "./api";
import {
  SettingInfo,
  AddressInput,
  Button,
  Confirm,
  Empty,
  ErrorLine,
  Field,
  Modal,
  PageHeader,
  SettingsSection,
  SecondaryNav,
  Panel,
  Table,
  useResource,
} from "./ui";
import { ServicePanel } from "./views";
import type { Act } from "./nodes";
export function WarpPage({ act }: { act: Act }) {
  const scout = useResource<Dict>("/api/warp/scout", 15000);
  const [metrics, setMetrics] = useWarpMetricSelection(
    "foxray.warp.candidate-test.metrics",
  );
  const [currentMetrics, setCurrentMetrics] = useWarpMetricSelection(
    "foxray.warp.current-test.metrics",
  );
  const [sort, setSort] = useState({ key: "rank", direction: 1 });
  const [candidateQuery, setCandidateQuery] = useState(""),
    [candidateStatus, setCandidateStatus] = useState("all");
  const [tab, setTab] = useState(() => {
    const saved = localStorage.getItem("foxray.warp.tab");
    if (saved === "services" || saved === "mode") {
      localStorage.setItem("foxray.warp.tab", "settings");
      return "settings";
    }
    if (saved === "candidates") {
      localStorage.setItem("foxray.warp.tab", "tunnel");
      return "tunnel";
    }
    return saved && ["tunnel", "settings", "dns"].includes(saved)
      ? saved
      : "tunnel";
  });
  const [protocol, setProtocol] = useState("masque-h2"),
    [port, setPort] = useState(443),
    [selected, setSelected] = useState<Set<string>>(new Set()),
    [confirm, setConfirm] = useState<Dict | null>(null),
    [failure, setFailure] = useState("");
  const [submitting, setSubmitting] = useState(false),
    [pendingTask, setPendingTask] = useState<string | null>(null);
  const busy = submitting || pendingTask !== null;
  const [runningScope, setRunningScope] = useState<
      "current" | "candidate" | null
    >(null),
    [runningMetrics, setRunningMetrics] = useState<string[]>([]),
    [pendingTaskData, setPendingTaskData] = useState<Task | null>(null),
    [cancelling, setCancelling] = useState(false);
  const cancelDetection = async () => {
    if (!pendingTask || cancelling) return;
    setCancelling(true);
    try {
      await api("/api/tasks/cancel", { id: pendingTask });
    } catch (e) {
      setFailure(errorMessage(e));
      setCancelling(false);
    }
  };
  useEffect(() => {
    if (!pendingTask) return;
    const controller = new AbortController();
    let cancelled = false,
      rounds = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const check = async () => {
      try {
        const data = await api<{ tasks: Task[] }>(
          "/api/tasks",
          undefined,
          controller.signal,
        );
        if (cancelled) return;
        const task = data.tasks.find((task) => task.id === pendingTask);
        setPendingTaskData(task || null);
        if (task && !["running", "queued"].includes(task.status)) {
          await scout.refreshLatest();
          if (cancelled) return;
          if (!["success", "partial"].includes(task.status))
            setFailure(task.message || "任务未完成，请查看任务详情");
          setPendingTask(null);
          setRunningScope(null);
          setPendingTaskData(null);
          setCancelling(false);
          return;
        }
        if (++rounds >= 3600) throw new Error("任务仍未完成，请查看任务详情");
        timer = setTimeout(check, 500);
      } catch (e) {
        if (!cancelled && !controller.signal.aborted) {
          setFailure(errorMessage(e));
          setPendingTask(null);
          setRunningScope(null);
          setPendingTaskData(null);
          setCancelling(false);
        }
      }
    };
    void check();
    return () => {
      cancelled = true;
      controller.abort();
      clearTimeout(timer);
    };
  }, [pendingTask, scout.refreshLatest]);
  const current = record(scout.data?.current),
    sourceRows = list(record(scout.data?.collections)[protocol]).map(
      (r, index) => ({ ...r, rank: index + 1 }),
    ),
    protocols = record(scout.data?.protocols),
    ports = Array.isArray(protocols[protocol])
      ? (protocols[protocol] as number[])
      : [443];
  const rows: Dict[] = sourceRows
    .filter(
      (r: Dict) =>
        String(r.endpoint)
          .toLowerCase()
          .includes(candidateQuery.trim().toLowerCase()) &&
        (candidateStatus === "all" ||
          (candidateStatus === "current"
            ? !!r.current
            : candidateStatus === "passed"
              ? !!r.durable
              : !r.durable)),
    )
    .sort((a: Dict, b: Dict) => {
      const value = (r: Dict): string | number | null =>
        sort.key === "endpoint"
          ? String(r.endpoint)
          : sort.key === "state"
            ? r.current
              ? 3
              : r.durable
                ? 2
                : r.handshake
                  ? 1
                  : 0
            : r[sort.key] == null
              ? null
              : Number(r[sort.key]);
      const x = value(a),
        y = value(b);
      if (x == null || y == null) return x == null ? (y == null ? 0 : 1) : -1;
      return (
        (typeof x === "string" && typeof y === "string"
          ? x.localeCompare(y, undefined, { numeric: true })
          : Number(x) - Number(y)) * sort.direction
      );
    });
  const visibleSelected = rows
    .filter((r) => selected.has(String(r.endpoint)))
    .map((r) => String(r.endpoint));
  const candidateStatusControl = (
    <Field label="隧道状态">
      <select
        aria-label="候选端点状态"
        value={candidateStatus}
        onChange={(e) => setCandidateStatus(e.target.value)}
      >
        <option value="all">全部状态</option>
        <option value="current">当前隧道</option>
        <option value="passed">探测通过</option>
        <option value="failed">未通过或未测</option>
      </select>
    </Field>
  );
  const heading = (key: string, label: string) => (
    <SortHeading
      label={label}
      direction={sort.key === key ? sort.direction : undefined}
      onClick={() =>
        setSort({
          key,
          direction:
            sort.key === key ? -sort.direction : key === "speed_kib_s" ? -1 : 1,
        })
      }
    />
  );
  useEffect(() => {
    setSelected(new Set());
    setPort(protocol === "wg" ? 2408 : 443);
  }, [protocol]);
  const runAction = async (path: string, data: Dict) => {
    if (busy) return;
    setSubmitting(true);
    setFailure("");
    setRunningScope(
      data.operation === "test-current"
        ? "current"
        : data.operation === "scan"
          ? "candidate"
          : null,
    );
    setRunningMetrics(
      Array.isArray(data.metrics) ? (data.metrics as string[]) : [],
    );
    setPendingTaskData(null);
    setCancelling(false);
    try {
      let taskId = "";
      const ok = await act(path, data, (result) => {
        taskId = String(result.id || "");
      });
      if (ok) {
        if (taskId) setPendingTask(taskId);
        else await scout.refreshLatest();
      }
      if (!ok || !taskId) setRunningScope(null);
    } finally {
      setSubmitting(false);
    }
  };
  const action = (operation: string, extra: Dict = {}) =>
    runAction("/api/warp/scout", {
      operation,
      protocol,
      port,
      count: 10,
      ...extra,
    });
  const setChecked = (endpoint: string, on: boolean) =>
    setSelected((old) => {
      const next = new Set(old);
      on ? next.add(endpoint) : next.delete(endpoint);
      return next;
    });
  const stamp = (r: Dict, key: string) =>
    r[key]
      ? timestamp(r[key])
      : r.tested_at
        ? timestamp(r.tested_at)
        : "未记录";
  const protocolFields = (
    <div className="warp-protocol-controls">
      <Field label="隧道类型">
        <select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
          <option value="masque-h2">MASQUE TCP</option>
          <option value="masque">MASQUE UDP</option>
          <option value="wg">WireGuard UDP</option>
        </select>
      </Field>
      <Field label="端口">
        <select value={port} onChange={(e) => setPort(Number(e.target.value))}>
          {ports.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </Field>
    </div>
  );
  return (
    <>
      <SecondaryNav label="WARP">
        <div className="module-tabs" role="tablist">
          {[
            ["tunnel", "隧道管理"],
            ["dns", "DNS"],
            ["settings", "设置"],
          ].map(([id, label]) => (
            <button
              key={id}
              role="tab"
              aria-selected={tab === id}
              onClick={() => {
                setTab(id);
                localStorage.setItem("foxray.warp.tab", id);
              }}
            >
              {labelIcon(label, 14)}
              {label}
            </button>
          ))}
        </div>
      </SecondaryNav>
      {tab === "tunnel" && (
        <Panel className="warp-tunnel-panel">
          <PageHeader
            title="WARP 隧道管理"
            description="连接、断开和检测当前官方隧道。RTT／丢包通过该隧道命名空间检测 1.1.1.1；真延迟访问设置中的 HTTP(S) 目标，速度使用设置中的下载地址。检测项首次默认全选，后续记住上次选择。点击左侧执行，右侧调整项目；运行时可取消。"
          />
          <div className="service-control-frame">
            <div className="warp-status tunnel-state-tiles">
              <div>
                <span className="small muted">当前状态</span>
                <strong className={current.connected ? "accent" : "fair"}>
                  {current.connected ? "已连接" : text(current.state)}
                </strong>
              </div>
              <div>
                <span className="small muted">实际端点／配置端点</span>
                <strong>
                  {text(current.endpoint_address || current.override)}
                </strong>
              </div>
              <div>
                <span className="small muted">隧道类型</span>
                <strong>{text(current.transport || current.protocol)}</strong>
              </div>
              <div>
                <span className="small muted">最近确认</span>
                <strong>{timestamp(current.checked_at)}</strong>
              </div>
            </div>
            <div className="warp-status current-warp-metrics tunnel-state-tiles">
              {[
                ["rtt_ms", "RTT", " ms", "rtt_at"],
                ["loss_pct", "丢包", "%", "loss_at"],
                ["true_latency_ms", "真延迟", " ms", "true_latency_at"],
                ["speed_kib_s", "速度", "", "speed_at"],
              ].map(([key, label, unit, timeKey]) => (
                <div key={key}>
                  <span className="small muted">{label}</span>
                  <strong>
                    {record(scout.data?.current_metrics)[key] == null
                      ? "未测"
                      : key === "speed_kib_s"
                        ? bandwidth(record(scout.data?.current_metrics)[key])
                        : text(record(scout.data?.current_metrics)[key]) + unit}
                  </strong>
                  <small className="warp-metric-time">
                    检测时间：
                    {timestamp(record(scout.data?.current_metrics)[timeKey])}
                  </small>
                  {record(scout.data?.current_metrics)[
                    (
                      {
                        rtt_ms: "latency",
                        loss_pct: "loss",
                        true_latency_ms: "true_latency",
                        speed_kib_s: "speed",
                      } as Record<string, string>
                    )[key] + "_attempt_ok"
                  ] === false && (
                    <SettingInfo
                      label={label + "本次检测未通过"}
                      text={String(
                        record(scout.data?.current_metrics)[
                          (
                            {
                              true_latency_ms: "true_latency_error",
                              speed_kib_s: "speed_error",
                            } as Record<string, string>
                          )[key]
                        ] || "本次未获得有效结果；有数值时为上次成功记录。",
                      )}
                    />
                  )}
                </div>
              ))}
            </div>
            <div className="toolbar warp-tunnel-actions">
              <Button
                disabled={busy || !!current.connected}
                onClick={() => setConfirm({ service_action: "connect" })}
              >
                <PlugZap size={15} />
                连接
              </Button>
              <Button
                variant="danger"
                disabled={busy || !current.connected}
                onClick={() => setConfirm({ service_action: "disconnect" })}
              >
                <Unplug size={15} />
                断开
              </Button>
              <DetectionControl
                label="当前隧道"
                metrics={currentMetrics}
                onChange={setCurrentMetrics}
                disabled={busy || !current.connected}
                locked={busy}
                running={busy && runningScope === "current"}
                progress={warpTaskProgress(pendingTaskData, runningMetrics)}
                onCancel={
                  pendingTask ? () => void cancelDetection() : undefined
                }
                cancelling={cancelling}
                disabledReason={!current.connected ? "请先连接隧道" : undefined}
                onRun={() =>
                  void action("test-current", {
                    metrics: currentMetrics,
                  })
                }
              />
            </div>
          </div>
          <ErrorLine error={scout.error} />
          {!scout.data?.installed && (
            <p className="hint">
              独立探测工具未安装，当前官方隧道状态仍可查看。
            </p>
          )}
        </Panel>
      )}
      {tab === "tunnel" && (
        <>
          <Panel className="management-panel warp-candidates-panel">
            <PageHeader
              title="候选端点"
              description={
                <>
                  {"按协议发现端点，选择检测指标；每批最多检测 10 个"}
                  <p>
                    RTT／丢包与带宽分别保留检测时间。未建立隧道不记作 0 ms 或
                    100% 丢包。每批最多检测 10 个候选。
                  </p>
                </>
              }
            />
            <div className="management-toolbar">
              {protocolFields}
              <Button
                variant="primary"
                disabled={busy}
                onClick={() => void action("discover")}
              >
                <RefreshCw size={15} />
                重新发现
              </Button>
              <Button disabled={busy} onClick={() => void action("restore")}>
                <RotateCcw size={15} />
                恢复候选
              </Button>
              <DetectionControl
                label="候选端点"
                metrics={metrics}
                onChange={setMetrics}
                disabled={
                  busy || !visibleSelected.length || visibleSelected.length > 10
                }
                locked={busy}
                running={busy && runningScope === "candidate"}
                progress={warpTaskProgress(pendingTaskData, runningMetrics)}
                onCancel={
                  pendingTask ? () => void cancelDetection() : undefined
                }
                cancelling={cancelling}
                disabledReason={
                  !visibleSelected.length
                    ? "请先选择候选端点"
                    : visibleSelected.length > 10
                      ? "每批最多选择 10 个端点"
                      : undefined
                }
                onRun={() =>
                  void action("scan", {
                    endpoints: visibleSelected,
                    metrics: metrics,
                  })
                }
              />
              <div className="management-query-tools">
                <AdaptiveSearch>
                  <Search size={16} />
                  <input
                    aria-label="搜索候选端点"
                    placeholder="查询端点地址"
                    value={candidateQuery}
                    onChange={(e) => setCandidateQuery(e.target.value)}
                  />
                </AdaptiveSearch>
              </div>
              <ResetFilters
                active={candidateQuery !== "" || candidateStatus !== "all"}
                selectedCount={visibleSelected.length}
                summary={[
                  candidateQuery && `搜索：${candidateQuery}`,
                  candidateStatus !== "all" && `状态：${candidateStatus}`,
                  `${rows.length} 项`,
                ]
                  .filter(Boolean)
                  .join(" · ")}
                onClick={() => {
                  setSelected(new Set());
                  setCandidateQuery("");
                  setCandidateStatus("all");
                }}
              />
            </div>

            <ErrorLine error={failure || scout.error} />
            {busy && (
              <p className="small muted" role="status">
                检测进行中，完成后自动更新列表。
              </p>
            )}
            <Table
              rows={rows}
              total={sourceRows.length}
              selectedCount={visibleSelected.length}
              rowKey={(r) => String(r.endpoint)}
              columns={[
                {
                  key: "select",
                  label: "选择",
                  heading: (
                    <input
                      type="checkbox"
                      aria-label="全选当前协议端点"
                      ref={(element) => {
                        if (element)
                          element.indeterminate =
                            visibleSelected.length > 0 &&
                            visibleSelected.length < rows.length;
                      }}
                      checked={
                        rows.length > 0 &&
                        rows.every((r) => selected.has(String(r.endpoint)))
                      }
                      onChange={(e) =>
                        setSelected(
                          e.target.checked
                            ? new Set(rows.map((r) => String(r.endpoint)))
                            : new Set(),
                        )
                      }
                    />
                  ),
                  render: (r) => (
                    <input
                      type="checkbox"
                      aria-label={"选择 " + text(r.endpoint)}
                      checked={selected.has(String(r.endpoint))}
                      onChange={(e) =>
                        setChecked(String(r.endpoint), e.target.checked)
                      }
                    />
                  ),
                },
                {
                  key: "rank",
                  label: "排名",
                  heading: heading("rank", "排名"),
                  render: (_, i) => i + 1,
                },
                {
                  key: "endpoint",
                  label: "端点",
                  heading: heading("endpoint", "端点"),
                  render: (r) => (
                    <>
                      <strong>{text(r.endpoint)}</strong>
                      {r.current && <small className="accent">当前隧道</small>}
                    </>
                  ),
                },
                {
                  key: "state",
                  label: "隧道状态",
                  heading: (
                    <span className="table-heading-tools">
                      {heading("state", "隧道状态")}
                      <ListFilter
                        label="候选隧道状态筛选"
                        column
                        active={candidateStatus !== "all"}
                      >
                        {candidateStatusControl}
                        <Button onClick={() => setCandidateStatus("all")}>
                          重置
                        </Button>
                      </ListFilter>
                    </span>
                  ),
                  render: (r) =>
                    r.current
                      ? "当前已连接"
                      : r.durable
                        ? "独立探测通过"
                        : r.handshake
                          ? "连接后中断"
                          : r.tested
                            ? "未建连"
                            : "未检测",
                },
                {
                  key: "rtt",
                  label: "隧道 RTT",
                  heading: heading("rtt_ms", "隧道 RTT"),
                  render: (r) => (
                    <>
                      {r.rtt_ms == null
                        ? r.latency_attempt_ok === false
                          ? "检测失败"
                          : "未测"
                        : text(r.rtt_ms) + " ms"}
                      {(r.rtt_at || r.rtt_ms != null) && (
                        <small>{stamp(r, "rtt_at")}</small>
                      )}
                    </>
                  ),
                },
                {
                  key: "loss",
                  label: "隧道丢包",
                  heading: heading("loss_pct", "隧道丢包"),
                  render: (r) => (
                    <>
                      {r.loss_pct == null
                        ? r.loss_attempt_ok === false
                          ? "检测失败"
                          : "未测"
                        : text(r.loss_pct) + "%"}
                      {(r.loss_at || r.loss_pct != null) && (
                        <small>{stamp(r, "loss_at")}</small>
                      )}
                    </>
                  ),
                },
                {
                  key: "speed",
                  label: "最近带宽",
                  heading: heading("speed_kib_s", "最近带宽"),
                  render: (r) => (
                    <>
                      {r.speed_kib_s == null && r.speed_attempt_ok === false
                        ? "测速失败"
                        : bandwidth(r.speed_kib_s)}
                      {r.speed_attempt_ok === false && (
                        <SettingInfo
                          label="下载测速结果"
                          text={`${text(r.speed_error)}；本次检测：${timestamp(r.speed_attempt_at)}${r.speed_kib_s != null ? "。当前数值为上次成功带宽。" : ""}`}
                        />
                      )}
                      {r.speed_kib_s != null && (
                        <small>{stamp(r, "speed_at")}</small>
                      )}
                    </>
                  ),
                },
                {
                  key: "true_latency",
                  label: "真延迟",
                  heading: heading("true_latency_ms", "真延迟"),
                  render: (r) => (
                    <>
                      {r.true_latency_ms == null
                        ? r.true_latency_attempt_ok === false
                          ? "检测失败"
                          : "未测"
                        : text(r.true_latency_ms) + " ms"}
                      {r.true_latency_at != null && (
                        <small>{timestamp(r.true_latency_at)}</small>
                      )}
                      {r.true_latency_attempt_ok === false && (
                        <SettingInfo
                          label="真延迟检测失败"
                          text={String(
                            r.true_latency_error ||
                              "本次未获得有效延迟；保留上次成功记录。",
                          )}
                        />
                      )}
                    </>
                  ),
                },
                {
                  key: "actions",
                  label: "操作",
                  render: (r) => (
                    <ActionMenu
                      label={text(r.endpoint)}
                      items={[
                        {
                          label: "检测",
                          disabled: busy || !metrics.length,
                          onClick: () =>
                            void action("scan", {
                              endpoints: [r.endpoint],
                              metrics: metrics,
                            }),
                        },
                        {
                          label: r.current ? "当前隧道" : "切换",
                          disabled: busy || !!r.current || !r.durable,
                          onClick: () =>
                            setConfirm({
                              operation: "activate",
                              endpoint: r.endpoint,
                            }),
                        },
                      ]}
                    />
                  ),
                },
              ]}
            />
            {!rows.length && <Empty>请选择协议并发现候选。</Empty>}
          </Panel>
        </>
      )}
      {confirm && (
        <Confirm
          title={
            confirm.service_action === "connect"
              ? "连接 WARP 隧道"
              : confirm.service_action === "disconnect"
                ? "断开 WARP 隧道"
                : "切换 WARP 隧道"
          }
          message={
            <>
              <p>
                {confirm.service_action
                  ? "将" +
                    (confirm.service_action === "connect" ? "连接" : "断开") +
                    "隔离 WARP 隧道。"
                  : "将应用所选隧道类型和端点：" +
                    text(confirm.endpoint || "自动端点") +
                    "。"}
              </p>
              <p>
                切换只影响隔离 WARP；失败恢复原设置，Xray 与 VPN 路由不回退。
              </p>
            </>
          }
          onClose={() => setConfirm(null)}
          onConfirm={() => {
            const c = confirm;
            setConfirm(null);
            setFailure("");
            if (c.service_action) {
              void runAction("/api/services/action", {
                service: "warp",
                action: c.service_action,
                allow_outage: true,
              });
              return;
            }
            void runAction("/api/warp/scout", { ...c, protocol, port });
          }}
        />
      )}
      {tab === "settings" && <WarpSettings act={act} />}
      {tab === "dns" && <DNSPage act={act} initialScope="warp" />}
    </>
  );
}
export function WarpSettings({ act }: { act: Act }) {
  const services = useResource<{ services: Record<string, Dict> }>(
    "/api/services",
    15000,
  );
  const scout = useResource<Dict>("/api/warp/scout", 15000);
  return (
    <SettingsSaveProvider expected={3}>
      <ServicePanel
        service="warp"
        data={services.data?.services.warp}
        error={services.error}
        act={act}
      />
      <WarpMonitorSettings act={act} />
      <ErrorLine error={scout.error} />
      <WarpPolicy data={scout.data} act={act} refresh={scout.refresh} />
      <HistoryIntervalSettings module="warp" act={act} />
    </SettingsSaveProvider>
  );
}
function WarpPolicy({
  data,
  act,
  refresh,
}: {
  data: Dict | null;
  act: Act;
  refresh: () => Promise<void>;
}) {
  const original = record(data?.policy),
    [policy, setPolicy] = useState<Dict | null>(null);
  const draft = policy || original;
  return (
    <Panel>
      <PageHeader
        title="切换模式"
        description="手动模式保持当前端点；自动模式仅在满足检测、保持时间和改善阈值后切换隔离 WARP"
      />
      <div className="form-grid">
        <Field label="选择模式">
          <select
            value={draft.auto ? "auto" : "manual"}
            onChange={(e) =>
              setPolicy({ ...draft, auto: e.target.value === "auto" })
            }
          >
            <option value="manual">手动模式</option>
            <option value="auto">基于策略自动选择</option>
          </select>
        </Field>
        {[
          ["interval", "检测间隔（秒）", 600],
          ["hold", "最短保持时间（秒）", 600],
          ["improvement", "改善阈值（%）", 10],
          ["confirmations", "连续确认次数", 2],
        ].map(([key, label, fallback]) => (
          <Field label={String(label)} key={key}>
            <NumberInput
              type="number"
              value={Number(draft[String(key)] ?? fallback)}
              onChange={(e) =>
                setPolicy({ ...draft, [String(key)]: Number(e.target.value) })
              }
            />
          </Field>
        ))}
      </div>
      <SaveCard
        dirty={hasChanges(policy, original)}
        title="切换模式"
        disabled={!data}
        defaults={() =>
          setPolicy({
            auto: false,
            interval: 1800,
            improvement: 15,
            hold: 300,
            metrics: ["latency", "loss"],
          })
        }
        discard={() => setPolicy(null)}
        save={async () => {
          const ok = await act("/api/warp/policy", {
            ...draft,
            interval: Number(draft.interval ?? 600),
            hold: Number(draft.hold ?? 600),
            improvement: Number(draft.improvement ?? 10),
            confirmations: Number(draft.confirmations ?? 2),
          });
          if (ok) {
            await refresh();
            setPolicy(null);
          }
          return ok;
        }}
      />
    </Panel>
  );
}
export function Komari({ url }: { url: string }) {
  const [height, setHeight] = useState(1100);
  const src = url || "http://192.168.1.8:25774/";
  const origin = new URL(src).origin;
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== origin || e.data?.type !== "foxray:komari-height")
        return;
      const value = Number(e.data.height);
      if (Number.isInteger(value) && value >= 200 && value < 50000)
        setHeight(Math.max(440, value));
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [origin]);
  return (
    <Panel>
      <PageHeader
        title="Komari监控"
        description="完整嵌入服务器监控页面，管理与设置可在新标签页打开"
        actions={
          <a
            className="button secondary"
            href={src + "admin"}
            target="_blank"
            rel="noopener noreferrer"
          >
            Komari 管理 ↗
          </a>
        }
      />
      <iframe
        title="Komari监控主页"
        src={src}
        className="komari-frame"
        style={{ height }}
        scrolling="no"
        onLoad={(e) =>
          e.currentTarget.contentWindow?.postMessage(
            { type: "foxray:komari-measure" },
            origin,
          )
        }
        sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox"
      />
    </Panel>
  );
}
void Modal;

function WarpMonitorSettings({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/warp/monitor-policy", 15000);
  const [draft, setDraft] = useState<Dict | null>(null);
  const defaults: Dict = {
    enabled: true,
    speed_enabled: true,
    interval: 60,
    timeout: 10,
    latency_url: "https://www.google.com/generate_204",
    speed_url: "https://speed.cloudflare.com/__down?bytes=10000000",
    speed_timeout: 15,
    speed_size_mb: 50,
  };
  const p = { ...defaults, ...(draft || r.data) };
  const set = (key: string, value: unknown) => setDraft({ ...p, [key]: value });
  const numeric = (
    key: string,
    label: string,
    min: number,
    max: number,
    disabled: boolean,
  ) => (
    <Field key={key} label={label}>
      <NumberInput
        type="number"
        min={min}
        max={max}
        disabled={disabled}
        value={Number(p[key])}
        onChange={(e) => set(key, Number(e.target.value))}
      />
    </Field>
  );
  return (
    <Panel className="warp-detection-settings">
      <PageHeader
        title="隧道检测"
        description="真连接检测与测速分别控制；仅通过 WARP 隧道检测，不会自动连接隧道。"
      />
      <SettingsSection className="detection-section" title="真连接延迟检测">
        <label className="check">
          <input
            type="checkbox"
            checked={!!p.enabled}
            onChange={(e) => set("enabled", e.target.checked)}
          />
          启用真连接延迟检测
        </label>
        <Field label="真连接检测地址">
          <AddressInput
            kind="latency"
            value={String(p.latency_url)}
            onChange={(v) => set("latency_url", v)}
          />
        </Field>
        <div className="form-grid">
          {numeric("interval", "检测间隔（秒）", 10, 3600, !p.enabled)}
          {numeric("timeout", "单次超时（秒）", 1, 60, !p.enabled)}
        </div>
      </SettingsSection>
      <SettingsSection
        className="detection-section"
        title="速度检测"
        info="按需执行测速；关闭后不执行当前隧道及候选端点的下载测速。"
      >
        <label className="check">
          <input
            type="checkbox"
            checked={!!p.speed_enabled}
            onChange={(e) => set("speed_enabled", e.target.checked)}
          />
          启用速度检测
        </label>
        <Field label="测速文件地址">
          <AddressInput
            kind="speed"
            value={String(p.speed_url)}
            onChange={(v) => set("speed_url", v)}
          />
        </Field>
        <div className="form-grid">
          {numeric("speed_timeout", "测速时长（秒）", 1, 120, !p.speed_enabled)}
          {numeric(
            "speed_size_mb",
            "最大下载量（MiB）",
            1,
            500,
            !p.speed_enabled,
          )}
        </div>
      </SettingsSection>
      <ErrorLine error={r.error} />
      <SaveCard
        dirty={hasChanges(draft, { ...defaults, ...r.data })}
        title="隧道检测"
        disabled={!r.data}
        defaults={() => setDraft(defaults)}
        discard={() => setDraft(null)}
        save={async () => {
          const ok = await act("/api/warp/monitor-policy", p);
          if (ok) {
            await r.refresh();
            setDraft(null);
          }
          return ok;
        }}
      />
    </Panel>
  );
}
