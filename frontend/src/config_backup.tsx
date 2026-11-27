import { useRef, useState } from "react";
import { Download } from "lucide-react";
import { Button, ErrorLine, Field, PageHeader, Panel, useResource } from "./ui";

function ConfigBackupIcon() {
  return (
    <svg viewBox="0 0 1024 1024" width={20} height={20} aria-hidden="true" focusable="false">
      <path fill="#1A70CC" d="M512 0C229.627709 0 0 229.627709 0 512S229.627709 1024 512 1024 1024 794.372291 1024 512 794.372291 0 512 0z m0 996.655196c-267.269721 0-484.655196-217.385475-484.655196-484.655196s217.385475-484.655196 484.655196-484.655196 484.655196 217.385475 484.655196 484.655196-217.385475 484.655196-484.655196 484.655196zM842.082682 325.963799c-13.844022-5.949497-29.861899 0.572067-35.811397 14.416089L790.024581 378.70838c-50.456313-104.688268-157.776089-175.281341-278.024581-175.281341-158.348156 0-279.969609 107.77743-302.394637 268.185028-2.059441 14.988156 8.352179 28.717765 23.225922 30.777207 1.258547 0.228827 2.517095 0.228827 3.775642 0.228827 13.386369 0 25.056536-9.839553 27.001565-23.569162 18.763799-134.321341 116.244022-221.046704 248.277095-221.046704 100.454972 0 189.926257 59.838212 230.543016 148.165363l-48.511285-20.594414c-13.844022-5.949497-29.861899 0.572067-35.811396 14.41609-5.949497 13.844022 0.572067 29.861899 14.416089 35.811396l103.887374 44.049162c0.114413 0 0.114413 0 0.228827 0.114414 0.68648 0.34324 1.487374 0.457654 2.173855 0.68648 2.745922 0.915307 5.491844 1.487374 8.237765 1.487375 1.830615 0 3.661229-0.228827 5.491844-0.572067 0.457654-0.114413 0.915307-0.34324 1.487374-0.457654 0.228827-0.114413 0.572067 0 0.800894-0.114414 0.457654-0.114413 0.800894-0.34324 1.258548-0.572067 1.144134-0.34324 2.173855-0.800894 3.203575-1.37296 0.572067-0.34324 1.258547-0.68648 1.830615-1.029721 1.029721-0.68648 2.059441-1.372961 2.974748-2.059441 0.572067-0.457654 1.029721-0.800894 1.487374-1.258548 0.915307-0.915307 1.830615-1.830615 2.631509-2.860335 0.34324-0.457654 0.800894-0.915307 1.029721-1.372961 1.029721-1.487374 2.059441-3.203575 2.745921-4.919777v-0.114413c0.114413-0.114413 0.114413-0.34324 0.228827-0.457654l43.934749-103.42972c6.292737-13.615196-0.228827-29.747486-14.072849-35.58257z m-50.913967 195.532514c-14.873743-2.173855-28.717765 8.352179-30.777207 23.225922-18.878212 134.321341-116.244022 221.046704-248.277095 221.046704-100.454972 0-189.926257-59.838212-230.543016-148.165364l48.511285 20.594414c3.432402 1.487374 7.093631 2.173855 10.640446 2.173855 10.640447 0 20.708827-6.292737 25.17095-16.589945 5.949497-13.844022-0.572067-29.861899-14.416089-35.811396l-103.887374-44.049162c-0.114413 0-0.228827 0-0.228827-0.114414-0.457654-0.228827-1.029721-0.228827-1.487375-0.457653-0.572067-0.114413-1.144134-0.34324-1.716201-0.572067h-0.114413c-1.487374-0.457654-2.974749-0.68648-4.576536-0.800894h-0.457654c-1.372961-0.114413-2.860335-0.114413-4.34771 0-0.228827 0-0.572067 0-0.800893 0.114413-0.800894 0.114413-1.601788 0.114413-2.517095 0.228827-0.572067 0.114413-1.029721 0.34324-1.487375 0.457654-0.34324 0.114413-0.68648 0.228827-0.915307 0.34324-1.258547 0.34324-2.517095 0.800894-3.661229 1.372961l-0.68648 0.34324c-1.258547 0.572067-2.402682 1.258547-3.546816 2.059441-0.228827 0.114413-0.34324 0.34324-0.457654 0.457654-2.288268 1.716201-4.347709 3.775642-5.949497 6.063911-0.114413 0.114413-0.228827 0.34324-0.34324 0.457653-0.68648 1.029721-1.258547 2.173855-1.716201 3.317989-0.228827 0.457654-0.457654 0.800894-0.686481 1.144134v0.114414c0 0.114413-0.114413 0.228827-0.114413 0.34324l-43.934749 103.42972c-5.949497 13.844022 0.572067 29.861899 14.41609 35.811397 3.432402 1.487374 7.093631 2.173855 10.640447 2.173855 10.640447 0 20.708827-6.292737 25.170949-16.589944l16.246704-38.328492c50.456313 104.688268 157.776089 175.281341 278.024581 175.281341 158.348156 0 279.969609-107.77743 302.394637-268.185028 1.716201-14.988156-8.695419-28.832179-23.569162-30.89162z" />
    </svg>
  );
}

export function ConfigBackupSettings() {
  const status = useResource<{ ready: boolean }>("/api/config-backup", 15000);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [encrypted, setEncrypted] = useState(true);
  const [directory, setDirectory] = useState("");
  const [savedPath, setSavedPath] = useState("");
  const password = useRef<HTMLInputElement>(null);
  async function download() {
    if (busy || !status.data?.ready) return;
    const value = encrypted ? password.current?.value ?? "" : "";
    if (encrypted && !value) { setError("请输入备份密码"); return; }
    setBusy(true);
    setError("");
    setSavedPath("");
    try {
      const response = await fetch("/api/config-backup/export", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: value, directory: directory.trim() }),
      });
      if (!response.ok) {
        const failure = await response.json().catch(() => ({}));
        throw new Error(failure.error || "无法导出配置备份");
      }
      if (directory.trim()) {
        const result = await response.json();
        setSavedPath(String(result.path || ""));
        return;
      }
      const name = response.headers.get("Content-Disposition")?.match(/filename="(foxray-config-[\d.-]+\.(?:foxbackup|zip))"/)?.[1] ?? (encrypted ? "foxray-config.foxbackup" : "foxray-config.zip");
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
      <PageHeader title="配置备份" icon={<ConfigBackupIcon />} />
      <div className="settings-section">
        <p className="config-backup-export-description">导出 FoXray 管理服务配置、Xray 配置（含节点和订阅）、WireGuard 隧道配置，以及 WARP、网络连接、防火墙和 Komari 配置。</p>
        {status.data?.ready ? (
          <>
            <label className="check"><input type="checkbox" checked={encrypted} disabled={busy} onChange={event => setEncrypted(event.target.checked)} />使用密码加密</label>
            {encrypted ? <Field label="备份密码"><input ref={password} type="password" autoComplete="new-password" maxLength={256} disabled={busy} onKeyDown={event => { if (event.key === "Enter") void download(); }} /></Field>
              : <p className="muted">ZIP 文件包含可直接读取的服务配置和密钥。</p>}
            <Field label="服务器目录（可选）" hint="填写已存在的绝对路径。留空时由浏览器下载。"><input value={directory} onChange={event => setDirectory(event.target.value)} placeholder="例如 /var/backups/foxray" disabled={busy} /></Field>
            {location.protocol !== "https:" && <p className="hint">当前使用 HTTP，备份及加密密码会经过未加密连接。请在可信网络中操作。</p>}
            {savedPath && <p role="status">已保存到 <code>{savedPath}</code>。请从该目录通过 SSH/SFTP 安全下载。</p>}
          </>
        ) : status.data && <p className="muted">当前实例无法导出备份。</p>}
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
