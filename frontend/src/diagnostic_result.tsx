import { LogDetailHeading } from "./log_detail_heading";
import { AdaptiveSearch } from "./adaptive_search";
import { ResetFilters } from "./list_controls";
import { ALL_FIREWALL_ZONES } from "./firewall_view_state";
import { logReadKeys, useReadState } from "./read_state";
import { useEffect, useRef, useState } from "react";
import { CheckCircle2, CircleAlert, LoaderCircle, Search } from "lucide-react";
import {
  api,
  errorMessage,
  list,
  text,
  record,
  timestamp,
  type Dict,
  type Operation,
} from "./api";
import { Button, JsonDetails, Modal, Panel, Table } from "./ui";
import type { Act } from "./nodes";

export function useDiagnosticRunner(act: Act) {
  const {markRead}=useReadState();
  const [job, setJob] = useState(""),
    [pending, setPending] = useState(false),
    [message, setMessage] = useState(""),
    [result, setResult] = useState<Operation | null>(null);
  const generation = useRef(0);
  useEffect(()=>{
    if(result)markRead([...logReadKeys(result),...(job?["task:"+job]:[])]);
  },[result,job,markRead]);
  const run = async (path: string, data: unknown) => {
    setResult(null);
    setMessage("正在提交诊断…");
    setPending(true);
    setJob("");
    const ok = await act(path, data, (response) =>
      setJob(String(response.id || "")),
    );
    if (!ok) {
      setPending(false);
      setMessage("诊断未能提交，请查看红色操作提示。");
    }
  };
  useEffect(() => {
    if (!job) return;
    const current = ++generation.current;
    let timer: ReturnType<typeof setTimeout> | undefined,
      failures = 0,
      completedWithoutResult = 0,
      done = false;
    const startedAt = Date.now();
    const poll = async () => {
      try {
        const data = await api<Dict>(
          "/api/diagnostics/result?job=" + encodeURIComponent(job),
        );
        if (current !== generation.current) return;
        failures = 0;
        if (data.result) {
          const operation = data.result as unknown as Operation;
          setResult(operation);
          setPending(false);
          setMessage("");
          done = true;
          window.dispatchEvent(new CustomEvent("foxray:diagnostic-completed"));
        } else {
          const task = record(data.task);
          setMessage(
            String(task.message || "诊断执行中，正在等待解析和连接测试结果…"),
          );
          if (task.status === "error" || task.status === "cancelled") {
            setPending(false);
            done = true;
          } else if (
            task.status === "success" &&
            ++completedWithoutResult >= 3
          ) {
            setMessage(
              "诊断任务已结束，但未读取到结果，请刷新诊断日志确认保存状态。",
            );
            setPending(false);
            done = true;
          }
        }
      } catch (error) {
        if (current !== generation.current) return;
        failures++;
        setMessage("诊断状态读取失败：" + errorMessage(error));
        if (failures >= 5) {
          setPending(false);
          done = true;
        }
      }
      if (!done && Date.now() - startedAt > 45000) {
        setMessage("等待诊断结果超时，请刷新诊断日志查看执行状态。");
        setPending(false);
        done = true;
      }
      if (!done && current === generation.current)
        timer = setTimeout(poll, 1000);
    };
    void poll();
    return () => {
      generation.current++;
      if (timer) clearTimeout(timer);
    };
  }, [job]);
  return { run, pending, message, result };
}
export function DiagnosticProgress({
  pending,
  message,
}: {
  pending: boolean;
  message: string;
}) {
  if (!message) return null;
  return (
    <p
      className={"diagnostic-progress " + (pending ? "muted" : "poor")}
      role="status"
    >
      {pending && <LoaderCircle size={16} className="spin" />}
      {message}
    </p>
  );
}
function FirewallDiagnosticFacts({ data, onRefresh }: { data: Dict; onRefresh?: (params: Dict) => void }) {
  const params = record(data.parameters);
  const result = record(data.firewall);
  const route = record(result.route), reverse = record(result.reverse_route);
  const nat = record(result.nat);
  const [query, setQuery] = useState("");
  const candidates = list(result.rules).filter(row => JSON.stringify(row).toLowerCase().includes(query.trim().toLowerCase()));
  const [selected, setSelected] = useState<Dict | null>(null);
  return <>
    {result.status != null && <p role="status"><strong>{text(result.verdict || "未确认")}</strong> · {text(params.ingress)} ({text(result.zone)}) → {text(route.dev)} ({text(result.egress_zone)}) · {text(result.verdict_reason || "运行时规则候选")}</p>}
    <dl className="diagnostic-facts">
      <div><dt>目标</dt><dd>{text(params.destination)}{params.port ? `:${text(params.port)}` : ""}</dd></div>
      <div><dt>解析地址</dt><dd>{text(result.resolved_ip || "未取得")}</dd></div>
      <div><dt>指定入口与区域</dt><dd>{text(params.ingress)}{result.zone ? ` · ${text(result.zone)} · ${text(result.binding)}` : ""}</dd></div>
      <div><dt>内核路由查询</dt><dd>{text(route.dev || "未取得")}{result.egress_zone ? ` · ${text(result.egress_zone)}` : ""}{result.path_scope === "input" ? " · 本机入站" : result.path_scope === "forward" ? " · 转发" : ""}</dd></div>
      <div><dt>网关到源地址的路由</dt><dd>{text(reverse.dev || "未取得")}</dd></div>
      <div><dt>FoXray 配置预计路径</dt><dd>{text(data.expected || "未取得")}</dd></div>
      {nat.status != null && <div><dt>地址转换推演</dt><dd>{text(nat.source)} → {text(nat.destination)}{nat.port ? ` · 端口 ${text(nat.port)}` : ""} · {text(nat.status)}</dd></div>}
    </dl>
    {data.gateway_probe === true && <>
      <p><strong>网关 TCP 建连测试</strong> · 由网关发起</p>
      <dl className="diagnostic-facts">
        <div><dt>建连结果</dt><dd>{data.tcp_ms == null ? "未完成" : `${Number(data.tcp_ms).toFixed(2)} ms（TCP 建连）`}</dd></div>
        <div><dt>核心事件观测路径</dt><dd>{text(data.observed_path || record(data.actual_node).note || "未取得匹配事件")}</dd></div>
      </dl>
    </>}
    {params.probe === true && data.gateway_probe !== true && result.resolved_ip != null && <p className="hint">网关 TCP 建连测试仅支持 IPv4，本次已完成规则推演。</p>}
    {result.status != null && <p className="hint">基于运行时快照静态推演新请求。连接跟踪和实际地址转换需单独核实。</p>}
    {Array.isArray(result.warnings) && result.warnings.length > 0 && <details><summary>查看推演限制</summary>
      <ul>{result.warnings.map((warning, index) => <li key={index}>{String(warning)}</li>)}</ul>
    </details>}
    {onRefresh && reverse.dev && reverse.dev !== params.ingress && <Button onClick={() => onRefresh({...params, ingress: reverse.dev, probe: false})}>按 {text(reverse.dev)} 重新推演</Button>}
    {result.status != null && <section className="diagnostic-candidates">
      <h4 className="diagnostic-candidates-title">运行时候选规则</h4>
      <Table
        keepHeaders
        resizeInModal
        columnSizingKey="diagnostic-candidates"
        toolbar={<div className="diagnostic-candidates-tools">
          {onRefresh && <Button onClick={() => onRefresh({...params, probe: false})}>刷新</Button>}
          <AdaptiveSearch tableWidthKey="diagnostic-candidates"><Search size={15} />
            <input aria-label="搜索候选规则" placeholder="搜索候选规则" value={query} onChange={event => setQuery(event.target.value)} />
          </AdaptiveSearch>
          <ResetFilters active={!!query} onClick={() => setQuery("")} />
        </div>}
        rows={candidates}
        rowKey={(row) => String(candidates.indexOf(row))}
        columns={[
          { key: "name", label: "规则", render: row => {
            const name = text(row.name || "未命名规则");
            if (!row.id) return name;
            const action = String(row.action);
            let category = "all";
            if (row.kind === "cross-zone") category = "cross-zone";
            else if (row.kind === "forward" || ["dnat", "redirect"].includes(action)) category = "forward";
            else if (row.kind === "masquerade" || ["snat", "masquerade"].includes(action)) category = "masquerade";
            const driver = String(result.driver || "firewalld");
            const ruleZone = driver !== "firewalld" ? "host"
              : row.rule_origin ? ALL_FIREWALL_ZONES
              : String(row.zone || result.zone || "public");
            const params = new URLSearchParams({
              firewallRule: String(row.id),
              ruleDriver: driver,
              ruleZone,
              ruleCategory: category,
            });
            return <a className="table-link diagnostic-rule-link" href={`/?${params}`} target="_blank" rel="noopener noreferrer">{name}</a>;
          } },
          { key: "stage", label: "处理阶段", render: row => text(row.stage || (row.kind === "forward" ? "目的地址转换" : row.kind === "masquerade" ? "源地址转换" : "过滤")) },
          { key: "action", label: "动作", render: row => row.policy_default && row.action === "ACCEPT" ? "允许（策略默认 ACCEPT）" : text(row.action) },
          { key: "range", label: "匹配范围", render: row => row.ingress_zone ? `${text(row.ingress_zone)} → ${text(row.egress_zone)}` : `${text(row.source || "任意来源")} → ${text(row.destination || "任意目标")}${row.port ? ` · ${text(row.port)}` : ""}${row.out_interface ? ` · 出接口 ${text(row.out_interface)}` : ""}` },
          { key: "evidence", label: "匹配依据", render: row => text(row.evidence || "无可解析字段") },
          { key: "details", label: "详情", render: row => <Button size="compact" onClick={() => setSelected(row)}>查看</Button> },
        ]}
      />
    </section>}
    {selected && <Modal title="候选规则详情" onClose={() => setSelected(null)}><JsonDetails data={selected} /></Modal>}
  </>;
}

export function DiagnosticReport({
  operation,
  embedded = false,
  onRefresh,
  copyText,
}: {
  operation: Operation;
  embedded?: boolean;
  copyText?: string;
  onRefresh?: (params: Dict) => void;
}) {
  const d = {
      ...record(record(operation.details).legacy_result),
      ...record(operation.details),
    },
    p = record(d.parameters),
    passed = d.ok == null ? operation.status === "success" : d.ok === true,
    tcpOnly = p.protocol === "tcp",
    resultTitle = tcpOnly
      ? passed ? "TCP建连完成，目标可用性未验证" : "TCP建连未完成"
      : String(p.protocol).startsWith("dns")
        ? passed ? "DNS应答验证通过" : "DNS查询未通过"
        : passed ? "连接测试通过" : "连接测试未通过";
  const firewall = p.kind === "firewall";
  const body = (
    <>
      <LogDetailHeading
        title={firewall ? "防火墙规则诊断结果" : "路径探测诊断结果"}
        copyText={copyText}
        icon={passed && !tcpOnly ? <CheckCircle2 size={20} /> : <CircleAlert size={20} />}
        description={timestamp(operation.time)}
      />
      <p className={passed ? tcpOnly ? "fair" : "good" : "poor"} role="status">
        <strong>{firewall ? d.firewall ? "规则推演完成" : "规则推演未完成" : resultTitle}</strong> ·{" "}
        {operation.message}
      </p>
      {firewall ? <FirewallDiagnosticFacts data={d} onRefresh={onRefresh} /> : <dl className="diagnostic-facts">
        <div>
          <dt>{d.dns_routing_mode ? "请求填写的DNS目标" : "目标"}</dt>
          <dd>
            {String(p.target || p.host || "—")}:{String(p.port || 443)}
          </dd>
        </div>
        <div>
          <dt>测试范围</dt>
          <dd>
            {p.scope === "lan" ? "LAN规则模拟；实际测试由网关发起" : "网关本机"}
          </dd>
        </div>
        <div>
          <dt>解析地址</dt>
          <dd>{String(d.resolved_ip || "解析未完成")}</dd>
        </div>
        <div>
          <dt>配置接管判断（非实际路径）</dt>
          <dd>{String(d.expected || "未取得路径")}</dd>
        </div>
        {p.protocol!=="icmp" && <div>
          <dt>本次实际节点</dt>
          <dd>{record(d.actual_node).status === "observed" ? String(record(d.actual_node).display) : String(record(d.actual_node).note || "未确认：未收到本次连接匹配的核心事件")}</dd>
        </div>
        }
        {Boolean(record(d.route_observer).note) && record(d.actual_node).status !== "observed" && <div><dt>节点观测状态</dt><dd>{String(record(d.route_observer).note)}</dd></div>}
        {Boolean(d.observed_path) && <div><dt>事件确认路径</dt><dd>{String(d.observed_path)}</dd></div>}
        {d.dns_routing_mode != null && <div><dt>DNS测试机制</dt><dd>{String(d.dns_routing_mode)}</dd></div>}
        {d.dns_actual_upstream != null && <div><dt>实际DNS上游</dt><dd>{String(d.dns_actual_upstream)}</dd></div>}
        {d.dns_query_ms != null && <div><dt>DNS查询应答耗时</dt><dd>{Number(d.dns_query_ms).toFixed(2)} ms（包含发送查询与接收应答，不是本地UDP套接字或TCP接管耗时）</dd></div>}
        {d.dns_query_name != null && <div><dt>DNS查询与记录类型</dt><dd>{String(d.dns_query_name)} · A</dd></div>}
        {Array.isArray(d.dns_answers) && <div><dt>DNS应答地址</dt><dd>{d.dns_answers.join("、")}</dd></div>}
        {Array.isArray(d.probe_connections) && <div><dt>实际查询传输协议</dt><dd>{[...new Set(d.probe_connections.map(v=>String(record(v).network).toUpperCase()))].join(" → ")}</dd></div>}
        {d.http_ttfb_ms != null && d.http_response_received === true && <div>
          <dt>HTTPS首字节延迟</dt><dd>{Number(d.http_ttfb_ms).toFixed(2)} ms（含建连、TLS及请求响应；不是本地TCP接管耗时）</dd>
        </div>}
        {d.tls_handshake_ms != null && <div><dt>TLS握手阶段</dt><dd>{Number(d.tls_handshake_ms).toFixed(2)} ms</dd></div>}
        {d.dns_ms != null && <div><dt>DNS解析（单独计时）</dt><dd>{d.dns_lookup_performed === false ? "目标为IP，未执行DNS查询" : `${Number(d.dns_ms).toFixed(2)} ms${p.protocol === "tls" ? "；不计入HTTPS首字节" : ""}`}</dd></div>}
        {d.http_status != null && <div><dt>目标HTTP状态</dt><dd>{String(d.http_status)}</dd></div>}
        {d.tcp_ms != null && (
          <div>
            <dt>TCP接管／建连</dt>
            <dd>{Number(d.tcp_ms).toFixed(2)} ms（可能仅为本地TUN响应，不能作为目标访问延迟）</dd>
          </div>
        )}
        {p.protocol === "tls" && (
          <div>
            <dt>TLS校验</dt>
            <dd>{d.tls_verified ? "证书校验通过" : "未通过或未完成"}</dd>
          </div>
        )}
        {d.icmp_success_pct != null && (
          <div>
            <dt>ICMP</dt>
            <dd>
              成功率 {String(d.icmp_success_pct)}% · {String(d.icmp_average_ms)}{" "}
              ms
            </dd>
          </div>
        )}
      </dl>}
      {(d.test_error || d.error) && (
        <p className="poor" role="alert">
          原因：{String(d.test_error || d.error)}
        </p>
      )}
      <details className="disclosure-stack" open>
        <summary>执行日志</summary>
        <pre className="diagnostic-steps">
          {Array.isArray(d.steps) && d.steps.length
            ? d.steps.join("\n")
            : "此历史记录未保存逐步日志；可重新执行获取。"}
        </pre>
      </details>
      <details className="disclosure-stack">
        <summary>原始结果</summary>
        <JsonDetails data={d} />
      </details>
    </>
  );
  return embedded ? (
    <div className="diagnostic-report">{body}</div>
  ) : (
    <Panel className="diagnostic-report">{body}</Panel>
  );
}
