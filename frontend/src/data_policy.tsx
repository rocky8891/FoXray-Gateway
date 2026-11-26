import { hasChanges } from "./settings_save";
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
  extraDirty = false,
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
  extraDirty?: boolean;
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
                  ? <ul><li><strong>取值范围：</strong>1～300 秒。</li><li><strong>继承关系：</strong>启用独立设置时使用本项，取消后跟随全局。</li><li><strong>影响范围：</strong>调整采集间隔不重启转发服务，也不改变主动探测频率。</li></ul>
                  : key === "max_storage_mb"
                    ? <ul><li><strong>取值范围：</strong>1～10240 MiB。</li><li><strong>统计口径：</strong>按各服务、各监控项的有效数据分别估算。</li><li><strong>磁盘表现：</strong>清理数据后，共享数据库文件不会立即缩小。</li></ul>
                    : <ul><li><strong>取值范围：</strong>1～365 天。</li><li><strong>保留策略：</strong>独立设置优先于全局，到期清理对应监控记录。</li><li><strong>影响范围：</strong>不改变主动检测策略。</li></ul>
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
                  ? <ul><li><strong>取值范围：</strong>1～3600 秒，默认 1 秒。</li><li><strong>记录内容：</strong>保存已有检测结果；默认值通常可以保留每次检测。</li><li><strong>频率关系：</strong>不改变真连接或 ICMP 探测频率，实际写入不会快于检测。</li></ul>
                  : kind === "connections"
                    ? <ul><li><strong>取值范围：</strong>1～3600 秒。</li><li><strong>记录密度：</strong>控制连接数历史保存频率，不会快于实际采集。</li><li><strong>明细与图表：</strong>明细按批次写入，图表按查询范围合并；不补造历史记录。</li></ul>
                    : <ul><li><strong>取值范围：</strong>1～3600 秒，默认 60 秒。</li><li><strong>采集与写入：</strong>实时采集产生当前速率，本项决定历史保存密度；实际写入不会快于采集。</li><li><strong>生效范围：</strong>短间隔只影响新数据。图表可能继续合并历史点，页面刷新只负责读取。</li></ul>
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
        dirty={
          extraDirty ||
          hasChanges(draft, {
            interval: 2,
            retention_days: 30,
            max_storage_mb: 256,
            ...record(resource.data?.policy),
            independent:
              module === "all" || Boolean(resource.data?.independent),
          }) ||
          (storageDraft != null &&
            storageDraft !==
              Number(resource.data?.storage_interval ?? storageDefault))
        }
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
