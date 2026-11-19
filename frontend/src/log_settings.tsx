import { useState } from "react";
import { record, type Dict } from "./api";
import { Panel, PageHeader, NumberInput, ErrorLine, useResource } from "./ui";
import { SaveCard, SettingsSaveProvider } from "./settings_save";
import type { Act } from "./nodes";

const defaults: Dict = {
  days_enabled: true,
  retention_days: 30,
  rows_enabled: false,
  max_rows: 10000,
  size_enabled: false,
  max_bytes: 1000000000,
  size_unit: "GB",
};
function LogTypeSettings({
  kind,
  title,
  act,
}: {
  kind: string;
  title: string;
  act: Act;
}) {
  const resource = useResource<Dict>("/api/logs/retention", 15000),
    [draft, setDraft] = useState<Dict | null>(null);
  const policy = { ...defaults, ...record(resource.data?.[kind]), ...draft };
  const set = (key: string, value: unknown) =>
    setDraft({ ...policy, [key]: value });
  const factor = policy.size_unit === "MB" ? 1000000 : 1000000000;
  return (
    <Panel className="log-retention-card">
      <PageHeader
        title={title}
        description="启用的保留条件同时生效，先达到的限制先清理；关闭的条件不参与清理。大小按该类日志有效载荷估算，不代表共享数据库文件的物理大小。"
      />
      <div className="data-policy-fields">
        <div className="log-limit">
          <label className="check">
            <input
              type="checkbox"
              checked={!!policy.days_enabled}
              onChange={(e) => set("days_enabled", e.target.checked)}
            />
            保留周期（天）
          </label>
          <NumberInput
            aria-label={title + "保留周期"}
            min={1}
            max={3650}
            disabled={!policy.days_enabled}
            value={Number(policy.retention_days)}
            onChange={(e) => set("retention_days", Number(e.target.value))}
          />
        </div>
        <div className="log-limit">
          <label className="check">
            <input
              type="checkbox"
              checked={!!policy.rows_enabled}
              onChange={(e) => set("rows_enabled", e.target.checked)}
            />
            保留条数（条）
          </label>
          <NumberInput
            aria-label={title + "保留条数"}
            min={1}
            max={100000000}
            disabled={!policy.rows_enabled}
            value={Number(policy.max_rows)}
            onChange={(e) => set("max_rows", Number(e.target.value))}
          />
        </div>
        <div className="log-limit">
          <label className="check">
            <input
              type="checkbox"
              checked={!!policy.size_enabled}
              onChange={(e) => set("size_enabled", e.target.checked)}
            />
            保留大小
          </label>
          <div className="size-with-unit">
            <NumberInput
              aria-label={title + "保留大小"}
              min={0.001}
              step="any"
              disabled={!policy.size_enabled}
              value={Number(policy.max_bytes) / factor}
              onChange={(e) =>
                set("max_bytes", Math.round(Number(e.target.value) * factor))
              }
            />
            <select
              aria-label={title + "大小单位"}
              value={String(policy.size_unit)}
              onChange={(e) => set("size_unit", e.target.value)}
            >
              <option>MB</option>
              <option>GB</option>
            </select>
          </div>
        </div>
      </div>
      <ErrorLine error={resource.error} />
      <SaveCard
        disabled={!resource.data}
        title={title + "保留策略"}
        defaults={() => setDraft({ ...defaults })}
        discard={() => setDraft(null)}
        save={async () => {
          const ok = await act("/api/logs/retention?kind=" + kind, policy);
          if (ok) {
            await resource.refresh();
            setDraft(null);
          }
          return ok;
        }}
      />
    </Panel>
  );
}
export function LogRetentionSettings({ act }: { act: Act }) {
  return (
    <SettingsSaveProvider expected={4}>
      {[
        ["operations", "操作日志"],
        ["probes", "检测日志"],
        ["switches", "切换日志"],
        ["diagnostics", "诊断日志"],
      ].map(([kind, title]) => (
        <LogTypeSettings key={kind} kind={kind} title={title} act={act} />
      ))}
    </SettingsSaveProvider>
  );
}
