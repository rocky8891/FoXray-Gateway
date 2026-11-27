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
  const [historyDraft, setHistoryDraft] = useState<string>();
  const globalHistory = module === "all";
  const savedHistory = globalHistory || resource.data?.storage_override
    ? String(resource.data?.storage_interval ?? "")
    : "";
  const history = historyDraft ?? savedHistory;
  const historyMin = kind === "metrics" ? 10 : 1;
  const inheritedHistory = resource.data?.storage_default;
  const policy = {
    interval: 10,
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
      <PageHeader
        title={title}
        icon={<Icon size={20} />}
        actions={actions}
        description={module === "all" ? (
          <ul>
            <li><strong>默认值按「服务 × 监控数据类型」应用</strong>。服务器指标（CPU、负载、内存、磁盘等）共用一组，服务器连接另算一组。Xray、WARP、WireGuard 各自的流量、延迟、连接分别计算，同组的节点或接口共用上限。启用「独立设置」的项目优先使用自己的配置。</li>
            <li><strong>历史保留天数</strong>。每组清理超过保留天数的记录。如果先达到容量上限，更早的记录会提前清理。</li>
            <li><strong>历史保留上限</strong>。这是每组历史数据的估算上限。例如设为 1024 MiB 时，Xray 的流量、延迟和连接各有 1024 MiB。流量和延迟按每条 256 字节估算。清理后 SQLite 文件可能保持原大小，释放的空间可供后续写入复用。</li>
            <li><strong>后台采集间隔</strong>。跟随全局设置的服务器指标、流量和连接按此间隔采集。历史写入、页面刷新和主动延迟探测分别设置。历史写入间隔也可按项覆盖。</li>
          </ul>
        ) : undefined}
      />
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
          <Field
            label={globalHistory ? "默认历史写入间隔（秒）" : "历史写入间隔（秒）"}
            info={globalHistory ? (
              <ul>
                <li>设置后应用于跟随全局的监控数据。留空时沿用按指标类型预设的间隔。</li>
                <li>范围为 1～3600 秒。服务器指标至少 10 秒。实际写入频率受采集或探测频率限制。</li>
              </ul>
            ) : (
              <ul>
                <li>填写 {historyMin}～3600 秒，为本项单独设置历史写入间隔。</li>
                <li>留空跟随全局。采集频率、页面刷新和主动探测各自配置。</li>
              </ul>
            )}
          >
            <NumberInput
              aria-label={globalHistory ? "默认历史写入间隔（秒）" : "历史写入间隔（秒）"}
              min={historyMin}
              max={3600}
              value={history}
              placeholder={globalHistory ? "按指标类型预设" : inheritedHistory == null ? "跟随全局" : `跟随全局 · ${inheritedHistory} 秒`}
              onChange={e => setHistoryDraft(e.target.value)}
            />
          </Field>
          {kind !== "latency" && fields}
        </div>
      </SettingsSection>
      <ErrorLine error={validationError || resource.error} />
      <SaveCard
        dirty={
          extraDirty ||
          hasChanges(draft, {
            interval: 10,
            retention_days: 30,
            max_storage_mb: 256,
            ...record(resource.data?.policy),
            independent:
              module === "all" || Boolean(resource.data?.independent),
          }) ||
          history !== savedHistory
        }
        disabled={!resource.data || !ready}
        title={title}
        defaults={() => {
          extraDefaults?.();
          setHistoryDraft(globalHistory ? "30" : "");
          setDraft(
            module === "all"
              ? { interval: 10, retention_days: 30, max_storage_mb: 256 }
              : { ...record(resource.data?.global), independent: false },
          );
        }}
        discard={() => {
          extraDiscard?.();
          setDraft(null);
          setHistoryDraft(undefined);
        }}
        save={async () => {
          const validHistory = history === "" || (/^\d+$/.test(history) && Number(history) >= historyMin && Number(history) <= 3600);
          const error = validHistory ? validate?.() || "" : `历史写入间隔须为 ${historyMin}～3600 秒的整数`;
          setValidationError(error);
          if (error) return false;
          const ok = await act(
            "/api/monitoring/data-policy?module=" + module + "&kind=" + kind,
            {
              interval: Number(policy.interval),
              retention_days: Number(policy.retention_days),
              max_storage_mb: Number(policy.max_storage_mb),
              ...(module === "all" ? {} : { independent }),
              ...(history !== savedHistory ? { storage_interval: history === "" ? null : Number(history) } : {}),
            },
          );
          if (ok) {
            if (extraSave && !(await extraSave())) return false;
            await resource.refresh();
            setDraft(null);
            setHistoryDraft(undefined);
          }
          return ok;
        }}
      />
    </Panel>
  );
}
