import { useRef, useState } from "react";
import { Archive, Download } from "lucide-react";
import { Button, ErrorLine, Field, PageHeader, Panel, useResource } from "./ui";

export function ConfigBackupSettings() {
  const status = useResource<{ ready: boolean }>("/api/config-backup", 15000);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [encrypted, setEncrypted] = useState(true);
  const password = useRef<HTMLInputElement>(null);
  async function download() {
    if (busy || !status.data?.ready) return;
    const value = encrypted ? password.current?.value ?? "" : "";
    if (encrypted && !value) { setError("请输入备份密码"); return; }
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/config-backup/export", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: value }),
      });
      if (!response.ok) {
        const failure = await response.json().catch(() => ({}));
        throw new Error(failure.error || "无法导出配置备份");
      }
      const name = response.headers.get("Content-Disposition")?.match(/filename="(foxray-config-[\d-]+\.(?:foxbackup|zip))"/)?.[1] ?? (encrypted ? "foxray-config.foxbackup" : "foxray-config.zip");
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement("a");
      link.href = url;
      link.download = name;
      document.body.append(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "无法导出配置备份");
    } finally {
      if (password.current) password.current.value = "";
      setBusy(false);
    }
  }
  return (
    <Panel className="presentation-settings config-backup-settings">
      <PageHeader title="配置备份" icon={<Archive size={20} />} />
      <div className="settings-section">
        <p>导出 FoXray 节点和订阅、Xray 配置、WireGuard 隧道、WARP、网络连接、防火墙及 Komari 配置。</p>
        {status.data?.ready ? (
          <>
            <label className="check"><input type="checkbox" checked={encrypted} disabled={busy} onChange={event => setEncrypted(event.target.checked)} />使用密码加密</label>
            {encrypted ? <Field label="备份密码"><input ref={password} type="password" autoComplete="new-password" maxLength={256} disabled={busy} onKeyDown={event => { if (event.key === "Enter") void download(); }} /></Field>
              : <p className="muted">ZIP 文件包含可直接读取的服务配置和密钥。</p>}
          </>
        ) : status.data && <p className="muted">请通过 SSH 隧道打开本机管理地址后导出备份。</p>}
        <ErrorLine error={error || status.error} />
      </div>
      <div className="save-card-footer">
        <Button variant="primary" disabled={busy || !status.data?.ready} onClick={() => void download()}>
          <Download size={16} />{busy ? "正在生成备份…" : "导出备份"}
        </Button>
      </div>
    </Panel>
  );
}
