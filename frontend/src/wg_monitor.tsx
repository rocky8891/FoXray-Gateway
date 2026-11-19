import { hasChanges } from "./settings_save";
import { HistoryIntervalSettings } from "./history_interval_settings";
import { NumberInput } from "./ui";
import { WireGuardServicePanel } from "./version_panel";
import type { TimeRange } from "./time_range";
import { TrafficPage } from "./traffic";
import { History } from "./views";
import { useState } from "react";
import { NetworkPage } from "./network";
import { ModuleHeading } from "./hubs";
import { SaveCard, SettingsSaveProvider } from "./settings_save";
import {
  Panel,
  PageHeader,
  Field,
  ErrorLine,
  SettingsSection,
  useResource,
} from "./ui";
import { record, list, type Dict, type State, type Node } from "./api";
import type { Act } from "./nodes";
const defaults = {
  enabled: false,
  interval: 60,
  count: 3,
  size: 56,
  timeout: 2,
  targets: {},
};
export function WireGuardSettings({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/wireguard/monitor-policy", 10000),
    wg = useResource<Dict>("/api/network/wireguard", 10000);
  const [draft, setDraft] = useState<Dict | null>(null);
  const p: Dict = draft || r.data || defaults;
  const set = (k: string, v: unknown) => setDraft({ ...p, [k]: v });
  return (
    <SettingsSaveProvider expected={2}>
      <WireGuardServicePanel act={act} />
      <Panel className="compact-settings-card">
        <PageHeader
          title="ICMP 探测"
          description="默认关闭；指定对端隧道 IP，通过对应 WireGuard 接口探测，不影响代理节点评分。"
        />
        <ErrorLine error={r.error || wg.error} />
        <SettingsSection>
          <label className="check">
            <input
              type="checkbox"
              checked={!!p.enabled}
              onChange={(e) => set("enabled", e.target.checked)}
            />
            启用 WireGuard ICMP 探测
          </label>
          <div className="form-grid">
            {[
              ["interval", "探测间隔（秒）", 10, 3600],
              ["count", "每轮包数", 1, 10],
              ["size", "包大小（字节）", 0, 1400],
              ["timeout", "每包超时（秒）", 1, 5],
            ].map(([k, l, min, max]) => (
              <Field key={String(k)} label={String(l)}>
                <NumberInput
                  type="number"
                  min={Number(min)}
                  max={Number(max)}
                  value={Number(p[String(k)])}
                  onChange={(e) => set(String(k), Number(e.target.value))}
                />
              </Field>
            ))}
            {list(wg.data?.items).map((v) => {
              const name = String(v.name),
                candidates = Array.isArray(v.probe_candidates)
                  ? v.probe_candidates.map(String)
                  : [],
                saved = String(record(p.targets)[name] || ""),
                auto =
                  typeof record(p.auto_targets)[name] === "boolean"
                    ? !!record(p.auto_targets)[name]
                    : !saved;
              return (
                <Field key={name} label={name + " 对端隧道 IP"}>
                  <label className="check">
                    <input
                      type="checkbox"
                      checked={auto}
                      onChange={(e) =>
                        set("auto_targets", {
                          ...record(p.auto_targets),
                          [name]: e.target.checked,
                        })
                      }
                    />
                    自动识别
                    {auto && !candidates.length
                      ? "（未发现明确的对端主机路由，请手动填写）"
                      : ""}
                  </label>
                  <input
                    list={"wg-targets-" + name}
                    value={auto ? candidates[0] || "" : saved}
                    placeholder="选择候选或手动输入对端隧道 IP"
                    onChange={(e) =>
                      setDraft({
                        ...p,
                        auto_targets: {
                          ...record(p.auto_targets),
                          [name]: false,
                        },
                        targets: {
                          ...record(p.targets),
                          [name]: e.target.value.trim(),
                        },
                      })
                    }
                  />
                  <datalist id={"wg-targets-" + name}>
                    {candidates.map((ip) => (
                      <option key={ip} value={ip} />
                    ))}
                  </datalist>
                </Field>
              );
            })}
          </div>
        </SettingsSection>
        <SaveCard
 dirty={hasChanges(draft,r.data || defaults)}
          title="WireGuard ICMP"
          defaults={() => setDraft(structuredClone(defaults))}
          discard={() => setDraft(null)}
          save={async () => {
            const ok = await act("/api/wireguard/monitor-policy", p);
            if (ok) {
              await r.refresh();
              setDraft(null);
            }
            return ok;
          }}
        />
      </Panel>
      <HistoryIntervalSettings module="wireguard" act={act} />
    </SettingsSaveProvider>
  );
}
let lastWireGuardTab = "interfaces";
export function WireGuardHub({ act }: { act: Act }) {
  const [tab, setTab] = useState(lastWireGuardTab);
  return (
    <div className="module-page">
      <ModuleHeading
        title="WireGuard"
        description="独立管理多个 VPN 接口"
        tabs={[
          ["interfaces", "接口"],
          ["settings", "设置"],
        ]}
        value={tab}
        onChange={(value) => {
          lastWireGuardTab = value;
          setTab(value);
        }}
      />
      {tab === "settings" ? (
        <WireGuardSettings act={act} />
      ) : (
        <NetworkPage kind="wireguard" act={act} />
      )}
    </div>
  );
}
export function WireGuardMonitor({
  state,
  act,
  view,
  range,
  viewControl,
}: {
  state: State;
  act: Act;
  view: string;
  range: TimeRange;
  viewControl?: React.ReactNode;
}) {
  const wg = useResource<Dict>("/api/network/wireguard", 10000);
  const policy = useResource<Dict>("/api/wireguard/monitor-policy", 10000);
  const nodes: Node[] = list(wg.data?.items).map((r) => ({
    id: "wg:" + r.name,
    name: String(r.name),
    protocol: "wireguard",
    source: "WireGuard",
    native: false,
    enabled: r.active === true,
    priority: 0,
    active: false,
    status: r.active ? "运行中" : "已停止",
    metrics: {},
    score: {
      id: "wg:" + r.name,
      score: null,
      eligible: false,
      complete: false,
      basis: [],
      selected: [],
      caps: {},
      missing: [],
      reason: "",
      latency_average_ms: 0,
      latency_average_samples: 0,
    },
  }));
  const scoped = { ...state, nodes };
  return (
    <>
      <ErrorLine error={wg.error || policy.error} />
      {view === "traffic" ? (
        <TrafficPage
          state={scoped}
          act={act}
          module="wireguard"
          viewControl={viewControl}
          range={range}
        />
      ) : (
        <>
          <History
            act={act}
            state={scoped}
            module="wireguard"
            range={range}
            viewControl={viewControl}
          />
        </>
      )}
    </>
  );
}
