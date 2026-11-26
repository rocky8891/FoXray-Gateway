import { useEffect, useState } from "react";
import { api, errorMessage, type Dict, type Plan } from "./api";
import { Button, ErrorLine, Modal } from "./ui";
import { CopyButton } from "./resource_controls";
import type { Act } from "./nodes";

export function NetworkBatchDialog({
  kind,
  operation,
  rows,
  act,
  onClose,
}: {
  kind: string;
  operation: string;
  rows: Dict[];
  act: Act;
  onClose: () => void;
}) {
  const aggregate = ["interfaces", "routes", "rules"].includes(kind);
  const [entries, setEntries] = useState<
      { name: string; plan?: Plan; error?: string; applied?: boolean }[]
    >([]),
    [busy, setBusy] = useState(false),
    [allowed, setAllowed] = useState(false),
    [finished, setFinished] = useState(false);
  useEffect(() => {
    let cancelled = false;
    setBusy(true);
    setEntries([]);
    setFinished(false);
    void (async () => {
      if (aggregate) {
        try {
          const plan = await api<Plan>("/api/network/plan", {
            kind,
            operation:
              kind !== "interfaces" && operation === "start" ? "enable"
                : kind !== "interfaces" && operation === "stop" ? "disable" : operation,
            items: rows.map((row) =>
              kind === "interfaces"
                ? { name: row.ifname || row.name }
                : operation === "adopt" ? row : { id: row.id },
            ),
            allow_protected: allowed,
          });
          if (!cancelled)
            setEntries([{ name: `所选 ${rows.length} 项 · 合并预览`, plan }]);
        } catch (e) {
          if (!cancelled)
            setEntries([
              { name: `所选 ${rows.length} 项`, error: errorMessage(e) },
            ]);
        }
        if (!cancelled) setBusy(false);
        return;
      }
      const result = [];
      for (const row of rows) {
        const name = String(row.name || row.ifname || row.id);
        try {
          const plan = await api<Plan>("/api/network/plan", {
            kind,
            operation:
              ["routes", "rules"].includes(kind) &&
              ["start", "stop"].includes(operation)
                ? "replace"
                : operation,
            item: ["routes", "rules"].includes(kind)
              ? { id: row.id, enabled: operation === "start" }
              : { name },
            allow_protected: allowed,
          });
          result.push({ name, plan });
        } catch (e) {
          result.push({ name, error: errorMessage(e) });
        }
        if (cancelled) return;
      }
      setEntries(result);
      setBusy(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [kind, operation, rows, allowed]);
  const apply = async () => {
    setBusy(true);
    const result = [];
    for (const entry of entries) {
      if (!entry.plan) {
        result.push(entry);
        continue;
      }
      try {
        const ok = await act("/api/network/apply", { id: entry.plan.id });
        result.push({
          ...entry,
          applied: ok,
          error: ok ? undefined : "提交失败，请查看任务详情",
        });
      } catch (e) {
        result.push({ ...entry, error: errorMessage(e) });
      }
    }
    setEntries(result);
    setFinished(true);
    setBusy(false);
  };
  const passed =
      entries.filter((e) => (finished ? e.applied : e.plan)).length *
      (aggregate ? rows.length : 1),
    failed =
      entries.filter((e) => e.error).length * (aggregate ? rows.length : 1);
  return (
    <Modal
      title={
        "批量" +
        ({
          start: kind === "wireguard" ? "启动" : "启用",
          stop: kind === "wireguard" ? "停止" : "停用",
          enable: "开启自启",
          disable: "关闭自启",
          delete: "删除",
          adopt: "纳管",
        }[operation] || operation)
      }
      onClose={onClose}
      wide
    >
      <p
        className={
          finished ? (failed ? (passed ? "fair" : "poor") : "accent") : "muted"
        }
      >
        {busy
          ? "正在处理…"
          : finished
            ? failed
              ? passed
                ? "部分提交成功"
                : "提交失败"
              : "提交成功"
            : `已生成 ${passed} 项预览，${failed} 项未通过`}
      </p>
      {!finished && (
        <label className="check">
          <input
            type="checkbox"
            checked={allowed}
            disabled={busy}
            onChange={(e) => setAllowed(e.target.checked)}
          />
          我确认本次允许操作所选受保护接口
        </label>
      )}
      {entries.map((entry, i) => (
        <section key={i} className="batch-plan-entry">
          <strong>{entry.name}</strong>
          <ErrorLine error={entry.error || ""} />
          {entry.plan && (
            <pre className="command-preview">
              {entry.plan.preview.join("\n")}
            </pre>
          )}
        </section>
      ))}
      <p className="small muted">
        {aggregate
          ? "所选对象合并为一份变更与回退计划。"
          : "每个接口独立预览和回退。"}
        提交后请在 120 秒内确认网络正常；未确认的变更自动回退。
      </p>
      <div className="modal-actions">
        <CopyButton
          text={entries
            .map((e) =>
              [
                e.name,
                e.error || (e.applied ? "已提交" : "预览就绪"),
                ...(e.plan?.preview || []),
              ].join("\n"),
            )
            .join("\n\n")}
        />
        <Button disabled={busy} onClick={onClose}>
          {finished ? "完成" : "取消"}
        </Button>
        {!finished && (
          <Button
            variant="primary"
            disabled={busy || !passed}
            onClick={() => void apply()}
          >
            应用 {passed} 项变更
          </Button>
        )}
      </div>
    </Modal>
  );
}
