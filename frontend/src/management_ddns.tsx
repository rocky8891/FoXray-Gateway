import { useRef, useState } from "react";
import { Globe, Plus, RefreshCw } from "lucide-react";
import { managementRequest } from "./management_security";
import { Button, ErrorLine, Field, Modal, PageHeader, Panel, useResource } from "./ui";

export type DDNSItem = {
  domain: string;
  provider: "cloudflare" | "duckdns";
  ipv4: string;
  enabled: boolean;
  source?: "auto" | "manual" | "interface" | "disabled";
  interface?: string;
  interval_minutes?: number;
  running?: boolean;
  result?: { checked: string; ip?: string; error?: string };
};

const providers = { cloudflare: "Cloudflare", duckdns: "DuckDNS" };

type DDNSStatus = { items: DDNSItem[]; interfaces?: {name: string; ipv4: string}[] };

export function ManagementDDNSSettings() {
  const status = useResource<DDNSStatus>("/api/management/ddns", 15000);
  const [editor, setEditor] = useState<DDNSItem | null>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [source, setSource] = useState<NonNullable<DDNSItem["source"]>>("auto");
  const [testMessage, setTestMessage] = useState("");
  const editorForm = useRef<HTMLFormElement>(null);
  const [remove, setRemove] = useState<DDNSItem>();

  async function submit(payload: Record<string, string | boolean>) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await managementRequest("/api/management/ddns", payload);
      setEditor(undefined);
      setRemove(undefined);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "DDNS 操作失败");
    } finally {
      await status.refresh();
      setBusy(false);
    }
  }

  function openEditor(item: DDNSItem | null) {
    setError("");
    setTestMessage("");
    setSource(item?.source ?? (item?.ipv4 ? "manual" : "auto"));
    setEditor(item);
  }

  function formValues(form: HTMLFormElement) {
    const fields = new FormData(form);
    return {
      domain: editor?.domain ?? String(fields.get("domain") ?? ""),
      provider: String(fields.get("provider") ?? ""),
      token: String(fields.get("token") ?? ""),
      source,
      interface: String(fields.get("interface") ?? ""),
      ipv4: String(fields.get("ipv4") ?? ""),
      interval_minutes: String(fields.get("interval_minutes") ?? "5"),
      enabled: fields.has("enabled"),
    };
  }

  async function testConnection(form: HTMLFormElement) {
    if (busy || !form.reportValidity()) return;
    setBusy(true);
    setError("");
    setTestMessage("");
    try {
      const result = await managementRequest<{ message: string; ipv4: string }>("/api/management/ddns", {operation: "test", ...formValues(form)});
      setTestMessage(result.ipv4 ? `${result.message} · ${result.ipv4}` : result.message);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "连接测试失败");
    } finally {
      setBusy(false);
    }
  }

  return <Panel className="presentation-settings management-ddns-settings">
    <PageHeader title="DDNS" icon={<Globe size={20} />} actions={<Button disabled={busy} onClick={() => openEditor(null)}><Plus size={16} />新增 DDNS</Button>} />
    <div className="settings-section">
      {status.data?.items.length === 0 && <p className="muted ddns-empty">暂无 DDNS 配置</p>}
      {status.data?.items.map(item => <div className="ddns-entry" key={item.domain}>
        <div>
          <strong>{item.domain}</strong>
          <small className="muted">{providers[item.provider]} · {item.source === "disabled" ? "IPv4 已禁用" : item.source === "interface" ? item.interface : item.ipv4 || "自动检测公网 IPv4"} · {item.enabled && item.source !== "disabled" ? `每 ${item.interval_minutes ?? 5} 分钟更新` : "已暂停"}</small>
          {item.result?.error ? <ErrorLine error={item.result.error} /> : item.result?.ip && <small className="muted">最近更新 {item.result.ip} · {new Date(item.result.checked).toLocaleString()}</small>}
        </div>
        <div className="actions">
          <Button disabled={busy || item.running || item.source === "disabled"} onClick={() => void submit({operation:"update",domain:item.domain})}><RefreshCw size={16} />立即更新</Button>
          <Button disabled={busy || item.running} onClick={() => openEditor(item)}>编辑</Button>
          <Button disabled={busy || item.running} onClick={() => {setError("");setRemove(item);}}>删除</Button>
        </div>
      </div>)}
      {!remove && editor === undefined && <ErrorLine error={error || status.error} />}
    </div>
    {editor !== undefined && <Modal title={editor ? "编辑 DDNS" : "新增 DDNS"} onClose={() => {if (!busy) setEditor(undefined);}}>
      <form ref={editorForm} onSubmit={event => { event.preventDefault(); void submit({operation: "save", ...formValues(event.currentTarget)}); }}>
        <Field label="服务商"><select name="provider" defaultValue={editor?.provider ?? "cloudflare"}><option value="cloudflare">Cloudflare</option><option value="duckdns">DuckDNS</option></select></Field>
        <Field label="域名"><input name="domain" defaultValue={editor?.domain} readOnly={!!editor} autoComplete="off" placeholder="填入你自己的域名" required maxLength={253} /></Field>
        <Field label="令牌" hint={editor ? "留空保留当前服务商的令牌" : location.protocol === "http:" ? "当前使用 HTTP，令牌会经未加密连接传输" : undefined}><input name="token" type="password" autoComplete="new-password" required={!editor} maxLength={256} /></Field>
        <Field label="IPv4 地址来源"><select value={source} onChange={event => setSource(event.target.value as typeof source)}>
          <option value="auto">自动检测公网 IPv4</option><option value="interface">使用网口 IPv4</option><option value="manual">手动填写</option><option value="disabled">禁用 IPv4 更新</option>
        </select></Field>
        {source === "interface" && <Field label="网口"><select name="interface" defaultValue={editor?.interface || ""} required>
          <option value="">选择网口</option>
          {editor?.interface && !status.data?.interfaces?.some(item => item.name === editor.interface) && <option value={editor.interface}>{editor.interface}（当前无 IPv4）</option>}
          {status.data?.interfaces?.map(item => <option key={item.name} value={item.name}>{item.name} ({item.ipv4})</option>)}
        </select></Field>}
        {source === "manual" && <Field label="公网 IPv4"><input name="ipv4" defaultValue={editor?.ipv4} required autoComplete="off" /></Field>}
        <Field label="更新间隔（分钟）"><input name="interval_minutes" type="number" min={1} max={1440} defaultValue={editor?.interval_minutes ?? 5} required /></Field>
        <label className="check"><input type="checkbox" name="enabled" defaultChecked={editor?.enabled ?? true} />自动更新</label>
        {testMessage && <p role="status" className="accent">{testMessage}</p>}
        <ErrorLine error={error} />
        <div className="modal-actions"><Button disabled={busy} onClick={() => { const form = editorForm.current; if (form) void testConnection(form); }}>测试连接</Button><Button disabled={busy} onClick={() => setEditor(undefined)}>取消</Button><Button type="submit" variant="primary" disabled={busy}>保存</Button></div>
      </form>
    </Modal>}
    {remove && <Modal title="删除 DDNS 配置" onClose={() => {if (!busy) setRemove(undefined);}}>
      <p>移除 {remove.domain} 的自动更新配置。服务商已有 DNS 记录仍会保留。</p>
      <ErrorLine error={error} />
      <div className="modal-actions"><Button disabled={busy} onClick={() => setRemove(undefined)}>取消</Button><Button variant="danger" disabled={busy} onClick={() => void submit({operation:"delete",domain:remove.domain})}>删除</Button></div>
    </Modal>}
  </Panel>;
}
