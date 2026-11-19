import { useState, type ReactNode } from "react";
import {
  Database,
  Server,
  Cable,
  ChartNoAxesCombined,
  Waypoints,
} from "lucide-react";
import { record, type Dict } from "./api";
import {
  Panel,
  PageHeader,
  SettingsSection,
  Field,
  NumberInput,
  ErrorLine,
  useResource,
} from "./ui";
import { SaveCard } from "./settings_save";
import type { Act } from "./nodes";

export function DataPolicyCard({
  module,
  kind,
  title,
  act,
  actions,
  fields,
  extraSave,
  extraDefaults,
  extraDiscard,
  ready = true,
  validate,
}: {
  module: string;
  kind: string;
  title: string;
  act: Act;
  actions?: ReactNode;
  fields?: ReactNode;
  extraSave?: () => Promise<boolean>;
  extraDefaults?: () => void;
  extraDiscard?: () => void;
  ready?: boolean;
  validate?: () => string;
}) {
  const resource = useResource<Dict>(
    "/api/monitoring/data-policy?module=" + module + "&kind=" + kind,
    15000,
  );
  const [draft, setDraft] = useState<Dict | null>(null);
  const [validationError, setValidationError] = useState("");
  const [storageDraft, setStorageDraft] = useState<number | null>(null);
  const hasHistory = module !== "all" && kind !== "metrics";
  const storageDefault = kind === "latency" ? 1 : 60;
  const storage =
    storageDraft ?? Number(resource.data?.storage_interval ?? storageDefault);
  const policy = {
    interval: 2,
    retention_days: 30,
    max_storage_mb: 256,
    ...record(resource.data?.policy),
    ...draft,
  };
  const independent =
    module === "all" ||
    Boolean(draft?.independent ?? resource.data?.independent);
  const set = (key: string, value: unknown) =>
    setDraft({ ...policy, independent, ...{ [key]: value } });
  const Icon =
    module === "all"
      ? Database
      : kind === "metrics"
        ? Server
        : kind === "connections"
          ? Cable
          : kind === "latency"
            ? Waypoints
            : ChartNoAxesCombined;
  return (
    <Panel className="data-policy-card">
      <PageHeader title={title} icon={<Icon size={20} />} actions={actions} />
      <SettingsSection>
        {module !== "all" && (
          <label className="check policy-inheritance">
            <input
              type="checkbox"
              checked={independent}
              onChange={(e) =>
                setDraft(
                  e.target.checked
                    ? { ...policy, independent: true }
                    : { ...record(resource.data?.global), independent: false },
                )
              }
            />
            独立设置（优先于全局；取消后跟随全局）
          </label>
        )}
        <div className="data-policy-fields">
          {kind === "latency" && fields}
          {(
            [
              ["retention_days", "历史保留天数（天）", 1, 365],
              ["max_storage_mb", "历史保留上限（MiB）", 1, 10240],
              ...(kind === "latency"
                ? []
                : [["interval", "后台采集间隔（秒）", 1, 300]]),
            ] as [string, string, number, number][]
          ).map(([key, label, min, max]) => (
            <Field
              key={key}
              label={label}
              info={
                key === "interval"
                  ? "1～300秒。独立设置优先于全局，取消后跟随全局；调整无需重启转发服务。主动探测保持独立，不受此间隔影响。"
                  : key === "max_storage_mb"
                    ? "1～10240 MiB。按各服务、各监控项的有效数据估算，不合并占用；共享数据库文件不会立即缩小。"
                    : "1～365天。独立设置优先于全局；到期清理已有监控记录，不改变检测策略。"
              }
            >
              <NumberInput
                min={min}
                max={max}
                value={Number(policy[key as keyof typeof policy])}
                disabled={!independent}
                onChange={(e) => set(key, Number(e.target.value))}
              />
            </Field>
          ))}
          {hasHistory && (
            <Field
              label="历史写入间隔（秒）"
              info={
                kind === "latency"
                  ? "1～3600秒。只保存已有检测结果，不改变真连接或ICMP主动探测频率；实际记录不会快于检测。默认1秒表示通常保留每次检测。"
                  : kind === "connections"
                    ? "1～3600秒。决定连接数历史的记录密度，实际不会快于采集；明细按批次写入，图表按查询范围合并，不补造历史。"
                    : "1～3600秒。实时采集决定当前速率，历史记录间隔决定保存密度；默认60秒，实际记录不会快于采集。短间隔只影响新数据，历史图按范围合并，页面更新间隔只负责读取。"
              }
            >
              <NumberInput
                aria-label="历史写入间隔（秒）"
                min={1}
                max={3600}
                value={storage}
                onChange={(e) => setStorageDraft(Number(e.target.value))}
              />
            </Field>
          )}
          {kind !== "latency" && fields}
        </div>
      </SettingsSection>
      <ErrorLine error={validationError || resource.error} />
      <SaveCard
        disabled={!resource.data || !ready}
        title={title}
        defaults={() => {
          extraDefaults?.();
          setStorageDraft(storageDefault);
          setDraft(
            module === "all"
              ? { interval: 2, retention_days: 30, max_storage_mb: 256 }
              : { ...record(resource.data?.global), independent: false },
          );
        }}
        discard={() => {
          extraDiscard?.();
          setDraft(null);
          setStorageDraft(null);
        }}
        save={async () => {
          const error = validate?.() || "";
          setValidationError(error);
          if (error) return false;
          const ok = await act(
            "/api/monitoring/data-policy?module=" + module + "&kind=" + kind,
            {
              interval: Number(policy.interval),
              retention_days: Number(policy.retention_days),
              max_storage_mb: Number(policy.max_storage_mb),
              ...(module === "all" ? {} : { independent }),
              ...(hasHistory ? { storage_interval: storage } : {}),
            },
          );
          if (ok) {
            if (extraSave && !(await extraSave())) return false;
            await resource.refresh();
            setDraft(null);
            setStorageDraft(null);
          }
          return ok;
        }}
      />
    </Panel>
  );
}
