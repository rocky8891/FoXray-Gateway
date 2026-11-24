import { DiagnosticIcon } from "./diagnostic_icon";
import { useState, type FormEvent } from "react";
import { api, errorMessage, list, record, text, type Dict } from "./api";
import { Button, ErrorLine, Field, Panel } from "./ui";

export function FirewallDiagnosis({driver}: {driver:string}) {
  const [protocol, setProtocol] = useState("tcp");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<Dict | null>(null);
  const [error, setError] = useState("");
  async function diagnose(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    const query = new URLSearchParams({driver, protocol});
    for (const key of ["source", "destination", "ingress", "port"]) query.set(key, String(fields.get(key) ?? "").trim());
    setBusy(true);
    setResult(null);
    setError("");
    try { setResult(await api<Dict>("/api/network/firewall/diagnosis?" + query)); }
    catch (failure) { setError(errorMessage(failure)); }
    finally { setBusy(false); }
  }
  const nat = record(result?.nat), route = record(result?.route), reverse = record(result?.reverse_route);
  return <Panel className="firewall-main-panel management-panel">
    <div className="firewall-rules-heading"><h3><DiagnosticIcon size={20} className="firewall-heading-icon" />规则诊断</h3><span className="muted">运行时配置 · 只读静态推演</span></div>
    <form onSubmit={diagnose}>
      <div className="form-grid">
        <Field label="源 IP"><input name="source" placeholder="192.0.2.10" required /></Field>
        <Field label="目标 IP"><input name="destination" placeholder="198.51.100.20" required /></Field>
        <Field label="入口接口"><input name="ingress" placeholder="ens16" required /></Field>
        <Field label="协议"><select value={protocol} onChange={event=>setProtocol(event.target.value)}><option value="tcp">TCP</option><option value="udp">UDP</option><option value="icmp">ICMP</option></select></Field>
        {protocol !== "icmp" && <Field label="目标端口"><input name="port" type="number" min="1" max="65535" required /></Field>}
      </div>
      <div className="actions"><Button type="submit" variant="primary" disabled={busy || driver === "ufw"}>{busy ? "推演中…" : "开始推演"}</Button></div>
    </form>
    <ErrorLine error={error} />
    {result && <div className="settings-section" aria-live="polite">
      <p><strong>{text(result.status)}</strong></p>
      <p>入口区域 {text(result.zone)} · {text(result.binding)}{result.egress_zone ? " → " + text(result.egress_zone) : ""}</p>
      <p>正向出口 {text(route.dev)} · 本机回程出口 {text(reverse.dev)}</p>
      <p>{text(nat.status)} · {text(nat.source)} → {text(nat.destination)}{nat.port ? " · 端口 " + text(nat.port) : ""}</p>
      {Array.isArray(result.warnings) && result.warnings.map((warning,index)=><p className="hint" key={index}>{String(warning)}</p>)}
      <h4>运行时候选规则</h4>
      {list(result.rules).length === 0 && <p className="muted">没有匹配到可解析的候选规则</p>}
      {list(result.rules).map((rule,index)=><details key={index} className="advanced-form">
        <summary>{text(rule.name)} · {text(rule.action)} · {text(rule.source || "任意来源")} → {text(rule.destination || "任意目标")}{rule.port ? " · 端口 " + text(rule.port) : ""}</summary>
        <p>{text(rule.evidence)}{rule.runtime_only ? " · 仅运行时生效" : ""}</p>
        {rule.comparison != null && <p className="hint">{text(rule.comparison)} 对应规则 {text(rule.overlaps_with)}</p>}
        {rule.raw != null && <pre className="command-preview">{text(rule.raw)}</pre>}
      </details>)}
    </div>}
  </Panel>;
}
