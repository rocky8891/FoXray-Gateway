import { useState } from "react";
import { Archive, Download, RefreshCw } from "lucide-react";
import { Button, ErrorLine, PageHeader, Panel, useResource } from "./ui";

export function ConfigBackupSettings() {
  const status = useResource<{ ready: boolean }>("/api/config-backup", 15000);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function download() {
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/config-backup/export", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });
      if (!response.ok) {
        const failure = await response.json().catch(() => ({}));
        throw new Error(failure.error || "无法导出配置备份");
      }
      const name = response.headers.get("Content-Disposition")?.match(/filename="(foxray-config-[\d-]+\.foxbackup)"/)?.[1] ?? "foxray-config.foxbackup";
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
      setBusy(false);
    }
  }
  return (
    <Panel className="presentation-settings config-backup-settings">
      <PageHeader title="配置备份" icon={<Archive size={20} />} />
      <div className="settings-section">
        <p>导出 FoXray 节点和订阅、Xray 配置、WireGuard 隧道、WARP、网络连接、防火墙及 Komari 配置。</p>
        {status.data && !status.data.ready && <p className="muted">请先在服务器登记备份公钥。恢复时使用对应私钥解包，再按恢复手册应用配置。</p>}
        <ErrorLine error={error || status.error} />
      </div>
      <div className="save-card-footer">
        <Button disabled={busy} onClick={() => void status.refresh()}><RefreshCw size={16} />刷新状态</Button>
        <Button variant="primary" disabled={busy || !status.data?.ready} onClick={() => void download()}>
          <Download size={16} />{busy ? "正在生成备份…" : "导出加密备份"}
        </Button>
      </div>
    </Panel>
  );
}
