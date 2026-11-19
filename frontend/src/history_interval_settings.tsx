import { hasChanges } from "./settings_save";
import { useState, useImperativeHandle, type Ref } from "react";
import { record, type Dict } from "./api";
import {
  Panel,
  PageHeader,
  Field,
  NumberInput,
  ErrorLine,
  useResource,
} from "./ui";
import { SaveCard } from "./settings_save";
import type { Act } from "./nodes";

export type HistorySettingsActions = {
  defaults: () => void;
  discard: () => void;
  save: () => Promise<boolean>;
  dirty: boolean;
};
export function HistoryIntervalSettings({
  module,
  act,
  actionsRef,
}: {
  module: string;
  act: Act;
  actionsRef?: Ref<HistorySettingsActions>;
}) {
  const r = useResource<Dict>(
    "/api/monitoring/history-policy?module=" + module,
    15000,
  );
  const [draft, setDraft] = useState<Dict | null>(null);
  const values = {
    traffic: 60,
    latency: 1,
    connections: 60,
    ...record(r.data?.intervals),
    ...draft,
  };
  const save = async () => {
    if (!r.data) return false;
    if (!draft) return true;
    const ok = await act(
      "/api/monitoring/history-policy?module=" + module,
      values,
    );
    if (ok) {
      await r.refresh();
      setDraft(null);
    }
    return ok;
  };
  useImperativeHandle(actionsRef, () => ({
    defaults: () => setDraft({ traffic: 60, latency: 1, connections: 60 }),
    discard: () => setDraft(null),
    save,
    dirty: !!draft,
  }));
  return (
    <Panel className="compact-settings-card">
      <PageHeader title="监控历史写入间隔" />
      <div className="form-grid">
        {[
          ["traffic", "流量"],
          ["latency", "延迟"],
          ["connections", "活动连接"],
        ].map(([kind, label]) => (
          <Field
            key={kind}
            label={label + "历史写入间隔（秒）"}
            info={
              kind === "latency"
                ? "1～3600秒，默认1秒。仅控制已有检测结果的保存密度，不改变主动探测频率。"
                : "1～3600秒，默认60秒。实际记录不会快于采集；仅影响新数据，不补造已有历史，图表按查询范围合并。"
            }
          >
            <NumberInput
              min={1}
              max={3600}
              value={Number(values[kind as keyof typeof values])}
              onChange={(e) =>
                setDraft({ ...values, [kind]: Number(e.target.value) })
              }
            />
          </Field>
        ))}
      </div>
      <ErrorLine error={r.error} />
      <SaveCard
 dirty={hasChanges(draft, {traffic:60,latency:1,connections:60,...record(r.data?.intervals)})}
        title="监控历史写入间隔"
        disabled={!r.data}
        defaults={() => setDraft({ traffic: 60, latency: 1, connections: 60 })}
        discard={() => setDraft(null)}
        save={async () => {
          const ok = await act(
            "/api/monitoring/history-policy?module=" + module,
            values,
          );
          if (ok) {
            await r.refresh();
            setDraft(null);
          }
          return ok;
        }}
      />
    </Panel>
  );
}
