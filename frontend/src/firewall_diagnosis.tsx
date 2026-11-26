import { CircleDot } from "lucide-react";
import { useState, type FormEvent } from "react";
import { FirewallDiagnosisIcon } from "./firewall_feature_icons";
import { DiagnosticProgress, DiagnosticReport, useDiagnosticRunner } from "./diagnostic_result";
import type { Act } from "./nodes";
import { Button, Field, PageHeader, Panel } from "./ui";
import { DiagnosticLog } from "./views";

export function FirewallDiagnosis({ act }: { act: Act }) {
  const [driver, setDriver] = useState("auto");
  const [protocol, setProtocol] = useState("tcp");
  const diagnostic = useDiagnosticRunner(act);
  function diagnose(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    const data = { ...Object.fromEntries(fields), kind: "firewall", driver, protocol, probe: fields.has("probe") };
    void diagnostic.run("/api/diagnostics", data);
  }
  return <>
    <Panel className="firewall-main-panel firewall-diagnosis-panel">
      <PageHeader
        title="规则诊断"
        description="按运行时规则推演 LAN 转发"
        icon={<FirewallDiagnosisIcon size={20} />}

      />
      <form onSubmit={diagnose}>
        <div className="form-grid">
          <Field label="源 IP"><input name="source" placeholder="192.0.2.10" required /></Field>
          <Field label="目标 IP 或域名"><input name="destination" placeholder="198.51.100.20 或 example.com" required /></Field>
          <Field label="协议">
            <select value={protocol} onChange={event => setProtocol(event.target.value)}>
              <option value="tcp">TCP</option><option value="udp">UDP</option><option value="icmp">ICMP</option>
            </select>
          </Field>
          {protocol !== "icmp" && <Field label="目标端口"><input name="port" type="number" min="1" max="65535" required /></Field>}
          <Field label="入口接口"><input name="ingress" placeholder="ens16" required /></Field>
        </div>
        <details className="disclosure-stack firewall-diagnosis-advanced"><summary>高级选项</summary>
        <Field label="管理工具" info="自动优先使用运行中的 firewalld。手动选择可限定诊断范围。">
          <select aria-label="诊断管理工具" value={driver} onChange={event => setDriver(event.target.value)}>
            <option value="auto">自动选择</option>
            <option value="firewalld">firewalld</option>
            <option value="nftables">nftables</option>
            <option value="iptables">iptables</option>
          </select>
        </Field></details>
        <div className="actions">
          <Button type="submit" variant="primary" disabled={diagnostic.pending}>
            <CircleDot size={14} aria-hidden="true" />
            {diagnostic.pending ? "诊断中…" : "开始诊断"}
          </Button>
          {protocol === "tcp" && (
            <label className="check firewall-diagnostic-probe">
              <input type="checkbox" name="probe" />
              <span>同时从网关测试 IPv4 TCP 建连</span>
            </label>
          )}
        </div>
      </form>
      <DiagnosticProgress pending={diagnostic.pending} message={diagnostic.message} />
    </Panel>
    {diagnostic.result && <DiagnosticReport operation={diagnostic.result} onRefresh={params => void diagnostic.run("/api/diagnostics", params)} />}
    <DiagnosticLog act={act} firewallOnly />
  </>;
}
