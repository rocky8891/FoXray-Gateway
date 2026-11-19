import { hasChanges } from "./settings_save";
import type { OverviewView } from "./overview_policy";
import { ProbeIntervalField } from "./probe_interval_settings";
import { PresentationSettings } from "./presentation_settings";
import { DataPolicyCard } from "./data_policy";
import { MonitorItemsSettings } from "./monitor_items";
import { NumberInput } from "./ui";
import { labelIcon } from "./ui_icons";
import { SaveCard, SettingsSaveProvider } from "./settings_save";
import { useState } from "react";
import { record, type Dict, type State } from "./api";
import { HostSettings } from "./host";
import { ErrorLine, Field, PageHeader, Panel, useResource } from "./ui";
import type { Act } from "./nodes";

function TrafficSettings({
  act,
  module = "xray",
}: {
  act: Act;
  module?: string;
}) {
  return (
    <DataPolicyCard module={module} kind="traffic" title="流量" act={act} />
  );
}

function NodeDetectionSettings({
  act,
  module = "xray",
}: {
  state: State;
  act: Act;
  module?: string;
}) {
  return (
    <DataPolicyCard
      module={module}
      kind="latency"
      fields={<ProbeIntervalField module={module} act={act} />}
      title="延迟"
      act={act}
    />
  );
}

function HostDataSettings({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/system", 10000),
    [storage, setStorage] = useState<number | null>(null);
  const saved = Number(record(r.data?.policy).storage_interval ?? 60);
  return (
    <DataPolicyCard
      module="host"
      kind="metrics"
      title="监控数据"
      act={act}
      ready={!!r.data}
      extraDirty={storage != null && storage !== saved}
      validate={() => {
        const n = storage ?? saved,
          min = Math.max(10, Number(record(r.data?.policy).interval || 2));
        return Number.isInteger(n) && n >= min && n <= 3600
          ? ""
          : `历史写入间隔须为${min}～3600秒的整数，且不能小于采集间隔`;
      }}
      fields={
        <Field
          label="历史写入间隔（秒）"
          info="10～3600秒。汇总后台采集到的服务器指标并保存历史；不会改变后台采集、页面刷新或主动探测频率。实际记录不会快于采集。"
        >
          <NumberInput
            aria-label="服务器历史写入间隔（秒）"
            min={10}
            max={3600}
            value={storage ?? saved}
            onChange={(e) => setStorage(Number(e.target.value))}
          />
        </Field>
      }
      extraDefaults={() => setStorage(60)}
      extraDiscard={() => setStorage(null)}
      extraSave={async () => {
        if (storage == null || storage === saved) return true;
        if (!(await act("/api/system/policy", { storage_interval: storage })))
          return false;
        await r.refresh();
        setStorage(null);
        return true;
      }}
    />
  );
}

export function DisplaySettings({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/monitoring/display", 15000),
    [draft, setDraft] = useState<Dict | null>(null);
  const p = {
    status_refresh_seconds: 5,
    chart_refresh_seconds: 10,
    live_refresh_seconds: 2,
    live_sampling_seconds: 0,
    traffic_chart_window_seconds: 30,
    ...(draft || r.data),
  };
  return (
    <Panel className="compact-settings-card">
      <PageHeader
        title="页面读取与刷新"
        description="只控制浏览器读取已采集数据的频率，不改变后台采集、主动探测或历史写入。"
      />
      <div className="form-grid">
        <Field
          label="页面状态刷新（秒）"
          info="每 1～3600 秒获取节点、服务和配置状态，默认 5 秒。"
        >
          <NumberInput
            type="number"
            min={1}
            max={3600}
            value={Number(p.status_refresh_seconds)}
            onChange={(e) =>
              setDraft({ ...p, status_refresh_seconds: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          label="页面实时指标刷新（秒）"
          info="每 1～3600 秒更新流量速率及主机实时数值，默认 2 秒。"
        >
          <NumberInput
            type="number"
            min={1}
            max={3600}
            value={Number(p.live_refresh_seconds)}
            onChange={(e) =>
              setDraft({ ...p, live_refresh_seconds: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          label="页面历史图刷新（秒）"
          info="每 1～3600 秒重新获取图表数据，默认 10 秒；后台采集、主动探测和历史写入频率不变。"
        >
          <NumberInput
            type="number"
            min={1}
            max={3600}
            value={Number(p.chart_refresh_seconds)}
            onChange={(e) =>
              setDraft({ ...p, chart_refresh_seconds: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          label="流量曲线汇总间隔（秒）"
          info="1～3600秒，默认30秒。按真实字节与采样时长计算区间均速，只改变Xray、WARP和WireGuard流量曲线；不改实时速率、采集频率、历史写入或累计流量。长时间范围可能进一步合并；流量监控可临时选择精细或自定义窗口。"
        >
          <NumberInput
            aria-label="流量曲线汇总间隔（秒）"
            min={1}
            max={3600}
            value={Number(p.traffic_chart_window_seconds)}
            onChange={(e) =>
              setDraft({
                ...p,
                traffic_chart_window_seconds: Number(e.target.value),
              })
            }
          />
        </Field>
      </div>
      <ErrorLine error={r.error} />
      <SaveCard
        dirty={hasChanges(draft, r.data)}
        title="显示与刷新"
        defaults={() =>
          setDraft({
            status_refresh_seconds: 5,
            chart_refresh_seconds: 10,
            live_refresh_seconds: 2,
            live_sampling_seconds: 0,
            traffic_chart_window_seconds: 30,
          })
        }
        discard={() => setDraft(null)}
        save={async () => {
          const ok = await act("/api/monitoring/display", {
            status_refresh_seconds: p.status_refresh_seconds,
            live_refresh_seconds: p.live_refresh_seconds,
            chart_refresh_seconds: p.chart_refresh_seconds,
            traffic_chart_window_seconds: p.traffic_chart_window_seconds,
          });
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

export function MonitoringSettings({
  state,
  act,
  initialOverviewView,
}: {
  state: State;
  act: Act;
  initialOverviewView?: OverviewView;
}) {
  const [overviewView, setOverviewView] = useState<OverviewView | undefined>(
    initialOverviewView,
  );
  const [module, setModule] = useState("overview"),
    tabs = [
      ["overview", "主页"],
      ["server", "服务器"],
      ...(state.components.xray ? [["xray", "Xray"]] : []),
      ...(state.components.warp ? [["warp", "WARP"]] : []),
      ...(state.components.wireguard ? [["wireguard", "WireGuard"]] : []),
    ];
  const active = tabs.some(([id]) => id === module) ? module : "overview";
  return (
    <>
      <div className="monitor-settings-tabs">
        {tabs.map(([id, label]) => (
          <button
            key={id}
            className={active === id ? "selected" : ""}
            aria-pressed={active === id}
            onClick={() => setModule(id)}
          >
            {labelIcon(label, 14)}
            {label}
          </button>
        ))}
      </div>
      <SettingsSaveProvider
        key={active}
        expected={active === "overview" ? 1 : 4}
      >
        {active === "overview" ? (
          <PresentationSettings
            overview
            act={act}
            state={state}
            configureDefault
            editorView={overviewView}
            onEditorViewChange={setOverviewView}
          />
        ) : (
          <>
            <MonitorItemsSettings
              module={active === "server" ? "host" : active}
              act={act}
            />
            {active === "server" ? (
              <>
                <HostDataSettings act={act} />
                <DataPolicyCard
                  module={active === "server" ? "host" : active}
                  kind="connections"
                  title="活动连接"
                  act={act}
                />
                <HostSettings act={act} />
              </>
            ) : (
              <>
                <TrafficSettings act={act} module={active} />
                <DataPolicyCard
                  module={active === "server" ? "host" : active}
                  kind="connections"
                  title="活动连接"
                  act={act}
                />
                <NodeDetectionSettings
                  state={state}
                  act={act}
                  module={active}
                />
              </>
            )}
          </>
        )}
      </SettingsSaveProvider>
    </>
  );
}

export function GlobalMonitoringSettings({ act }: { act: Act }) {
  return (
    <SettingsSaveProvider expected={5}>
      <PresentationSettings act={act} />
      <DisplaySettings act={act} />
      <DataPolicyCard module="all" kind="" title="监控数据默认设置" act={act} />
    </SettingsSaveProvider>
  );
}
