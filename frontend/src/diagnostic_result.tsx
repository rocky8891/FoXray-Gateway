import { useEffect, useRef, useState } from "react";
import { CheckCircle2, CircleAlert, LoaderCircle } from "lucide-react";
import {
  api,
  errorMessage,
  record,
  timestamp,
  type Dict,
  type Operation,
} from "./api";
import { JsonDetails, PageHeader, Panel } from "./ui";
import type { Act } from "./nodes";

export function useDiagnosticRunner(act: Act) {
  const [job, setJob] = useState(""),
    [pending, setPending] = useState(false),
    [message, setMessage] = useState(""),
    [result, setResult] = useState<Operation | null>(null);
  const generation = useRef(0);
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
export function DiagnosticReport({
  operation,
  embedded = false,
}: {
  operation: Operation;
  embedded?: boolean;
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
  const body = (
    <>
      <PageHeader
        title="诊断结果"
        icon={passed && !tcpOnly ? <CheckCircle2 size={20} /> : <CircleAlert size={20} />}
        description={timestamp(operation.time)}
      />
      <p className={passed ? tcpOnly ? "fair" : "good" : "poor"} role="status">
        <strong>{resultTitle}</strong> ·{" "}
        {operation.message}
      </p>
      <dl className="diagnostic-facts">
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
      </dl>
      {(d.test_error || d.error) && (
        <p className="poor" role="alert">
          原因：{String(d.test_error || d.error)}
        </p>
      )}
      <details open>
        <summary>执行日志</summary>
        <pre className="diagnostic-steps">
          {Array.isArray(d.steps) && d.steps.length
            ? d.steps.join("\n")
            : "此历史记录未保存逐步日志；可重新执行获取。"}
        </pre>
      </details>
      <details>
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
