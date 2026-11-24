import { CircleDot } from "lucide-react";
import { useState, type FormEvent } from "react";
import { DiagnosticIcon } from "./diagnostic_icon";
import { DiagnosticProgress, DiagnosticReport, useDiagnosticRunner } from "./diagnostic_result";
import type { Act } from "./nodes";
import { Button, Field, Panel } from "./ui";
import { DiagnosticLog } from "./views";

export function FirewallDiagnosis({ driver, act }: { driver: string; act: Act }) {
  const [protocol, setProtocol] = useState("tcp");
  const diagnostic = useDiagnosticRunner(act);
  function diagnose(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    const data = { ...Object.fromEntries(fields), kind: "firewall", driver, protocol, probe: fields.has("probe") };
    void diagnostic.run("/api/diagnostics", data);
  }
  return <>
    <Panel className="firewall-main-panel management-panel">
      <div className="firewall-rules-heading">
        <h3><DiagnosticIcon size={20} className="firewall-heading-icon" />规则诊断</h3>
        <span className="muted">按运行时规则推演 LAN 转发</span>
      </div>
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
        <div className="actions">
          <Button type="submit" variant="primary" disabled={diagnostic.pending || driver === "ufw"}>
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
    {diagnostic.result && <DiagnosticReport operation={diagnostic.result} />}
    <DiagnosticLog act={act} firewallOnly />
  </>;
}
