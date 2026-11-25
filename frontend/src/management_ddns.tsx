import { useState, type FormEvent } from "react";
import { Globe, Plus, RefreshCw } from "lucide-react";
import { managementRequest } from "./management_security";
import { Button, ErrorLine, Field, Modal, PageHeader, Panel, useResource } from "./ui";

export type DDNSItem = {
  domain: string;
  provider: "cloudflare" | "duckdns";
  ipv4: string;
  enabled: boolean;
  running?: boolean;
  result?: { checked: string; ip?: string; error?: string };
};

const providers = { cloudflare: "Cloudflare", duckdns: "DuckDNS" };

export function ManagementDDNSSettings() {
  const status = useResource<{ items: DDNSItem[] }>("/api/management/ddns", 15000);
  const [editor, setEditor] = useState<DDNSItem | null>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
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

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    void submit({
      operation: "save",
      domain: editor?.domain ?? String(fields.get("domain") ?? ""),
      provider: String(fields.get("provider") ?? ""),
      token: String(fields.get("token") ?? ""),
      ipv4: String(fields.get("ipv4") ?? ""),
      enabled: fields.has("enabled"),
    });
  }

  return <Panel className="presentation-settings management-ddns-settings">
    <PageHeader title="DDNS" icon={<Globe size={20} />} actions={<Button disabled={busy} onClick={() => {setError("");setEditor(null);}}><Plus size={16} />新增 DDNS</Button>} />
    <div className="settings-section">
      {status.data?.items.length === 0 && <p className="muted">暂无 DDNS 配置</p>}
      {status.data?.items.map(item => <div className="ddns-entry" key={item.domain}>
        <div>
          <strong>{item.domain}</strong>
          <small className="muted">{providers[item.provider]} · {item.ipv4 || "自动检测公网 IPv4"} · {item.enabled ? "自动更新已启用" : "已暂停"}</small>
          {item.result?.error ? <ErrorLine error={item.result.error} /> : item.result?.ip && <small className="muted">最近更新 {item.result.ip} · {new Date(item.result.checked).toLocaleString()}</small>}
        </div>
        <div className="actions">
          <Button disabled={busy || item.running} onClick={() => void submit({operation:"update",domain:item.domain})}><RefreshCw size={16} />立即更新</Button>
          <Button disabled={busy || item.running} onClick={() => {setError("");setEditor(item);}}>编辑</Button>
          <Button disabled={busy || item.running} onClick={() => {setError("");setRemove(item);}}>删除</Button>
        </div>
      </div>)}
      {!remove && editor === undefined && <ErrorLine error={error || status.error} />}
    </div>
    {editor !== undefined && <Modal title={editor ? "编辑 DDNS" : "新增 DDNS"} onClose={() => {if (!busy) setEditor(undefined);}}>
      <form onSubmit={save}>
        <Field label="服务商"><select name="provider" defaultValue={editor?.provider ?? "cloudflare"}><option value="cloudflare">Cloudflare</option><option value="duckdns">DuckDNS</option></select></Field>
        <Field label="域名"><input name="domain" defaultValue={editor?.domain} readOnly={!!editor} autoComplete="off" placeholder="填入你自己的域名" required maxLength={253} /></Field>
        <Field label="令牌" hint={editor ? "留空保留当前服务商的令牌" : location.protocol === "http:" ? "当前使用 HTTP，令牌会经未加密连接传输" : undefined}><input name="token" type="password" autoComplete="new-password" required={!editor} maxLength={256} /></Field>
        <Field label="公网 IPv4" hint="留空时自动检测"><input name="ipv4" defaultValue={editor?.ipv4} autoComplete="off" placeholder="自动检测" /></Field>
        <label className="check"><input type="checkbox" name="enabled" defaultChecked={editor?.enabled ?? true} />自动更新</label>
        <ErrorLine error={error} />
        <div className="modal-actions"><Button disabled={busy} onClick={() => setEditor(undefined)}>取消</Button><Button type="submit" variant="primary" disabled={busy}>保存</Button></div>
      </form>
    </Modal>}
    {remove && <Modal title="删除 DDNS 配置" onClose={() => {if (!busy) setRemove(undefined);}}>
      <p>移除 {remove.domain} 的自动更新配置。服务商已有 DNS 记录仍会保留。</p>
      <ErrorLine error={error} />
      <div className="modal-actions"><Button disabled={busy} onClick={() => setRemove(undefined)}>取消</Button><Button variant="danger" disabled={busy} onClick={() => void submit({operation:"delete",domain:remove.domain})}>删除</Button></div>
    </Modal>}
  </Panel>;
}
