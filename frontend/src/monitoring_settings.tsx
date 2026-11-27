import { ManagementDDNSSettings } from "./management_ddns";
import { ManagementUserSettings, ManagementHTTPSSettings } from "./management_security";
import { ConfigBackupSettings } from "./config_backup";
import { hasChanges } from "./settings_save";
import type { OverviewView } from "./overview_policy";
import { ProbeIntervalField } from "./probe_interval_settings";
import { PresentationSettings } from "./presentation_settings";
import { DataPolicyCard } from "./data_policy";
import { MonitorItemsSettings } from "./monitor_items";
import { NumberInput } from "./ui";
import { SaveCard, SettingsSaveProvider } from "./settings_save";
import { useEffect, useState } from "react";
import { type Dict, type State } from "./api";
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
  const display = useResource<Dict>("/api/monitoring/display", 15000);
  const [windowDraft, setWindowDraft] = useState<string>();
  const savedWindow = String(display.data?.traffic_chart_window_seconds ?? 30);
  const chartWindow = windowDraft ?? savedWindow;
  return (
    <DataPolicyCard
      module={module}
      kind="traffic"
      title="流量"
      act={act}
      fields={
        <>
          <TrafficWindowField value={chartWindow} onChange={setWindowDraft} disabled={!display.data} />
          <ErrorLine error={display.error} />
        </>
      }
      extraDirty={chartWindow !== savedWindow}
      extraDefaults={() => { if (display.data) setWindowDraft("30"); }}
      extraDiscard={() => setWindowDraft(undefined)}
      validate={() => {
        const seconds = Number(chartWindow);
        return /^\d+$/.test(chartWindow) && seconds >= 1 && seconds <= 3600
          ? ""
          : "流量曲线汇总间隔须为 1～3600 秒的整数";
      }}
      extraSave={async () => {
        if (chartWindow === savedWindow) return true;
        if (!(await act("/api/monitoring/display", { traffic_chart_window_seconds: Number(chartWindow) }))) return false;
        await display.refresh();
        setWindowDraft(undefined);
        return true;
      }}
    />
  );
}

function TrafficWindowField({ value, onChange, disabled }: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <Field
      label="流量曲线汇总间隔（秒）"
      info={<ul><li><strong>共享设置</strong>。全局设置、主页监控设置及各服务的流量设置使用同一个值，应用于跟随全局的 Xray、WARP 和 WireGuard 流量曲线。</li><li><strong>取值范围</strong>。1～3600 秒，默认 30 秒。后台采集、历史写入和页面刷新各自设置。</li><li><strong>图表覆盖</strong>。可在图表中临时选择其他窗口，长时间范围可能进一步合并数据。</li></ul>}
    >
      <NumberInput
        aria-label="流量曲线汇总间隔（秒）"
        min={1}
        max={3600}
        value={value}
        disabled={disabled}
        onChange={e => onChange(e.target.value)}
      />
    </Field>
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

export function DisplaySettings({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/monitoring/display", 15000),
    [draft, setDraft] = useState<Dict | null>(null);
  const p = {
    status_refresh_seconds: 5,
    chart_refresh_seconds: 15,
    live_refresh_seconds: 5,
    live_sampling_seconds: 0,
    traffic_chart_window_seconds: 30,
    ...r.data,
    ...draft,
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
              setDraft(old => ({ ...old, status_refresh_seconds: Number(e.target.value) }))
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
              setDraft(old => ({ ...old, live_refresh_seconds: Number(e.target.value) }))
            }
          />
        </Field>
        <Field
          label="页面历史图刷新（秒）"
          info={<ul><li><strong>取值范围：</strong>1～3600 秒，默认 10 秒。</li><li><strong>作用：</strong>控制浏览器重新获取图表数据的间隔。</li><li><strong>独立设置：</strong>后台采集、主动探测和历史写入继续使用各自间隔。</li></ul>}
        >
          <NumberInput
            type="number"
            min={1}
            max={3600}
            value={Number(p.chart_refresh_seconds)}
            onChange={(e) =>
              setDraft(old => ({ ...old, chart_refresh_seconds: Number(e.target.value) }))
            }
          />
        </Field>
        <TrafficWindowField
          value={String(p.traffic_chart_window_seconds)}
          onChange={value => setDraft(old => ({ ...old, traffic_chart_window_seconds: Number(value) }))}
        />
      </div>
      <ErrorLine error={r.error} />
      <SaveCard
        dirty={hasChanges(draft, r.data)}
        disabled={!r.data}
        title="显示与刷新"
        defaults={() =>
          setDraft({
            status_refresh_seconds: 5,
            chart_refresh_seconds: 15,
            live_refresh_seconds: 5,
            traffic_chart_window_seconds: 30,
          })
        }
        discard={() => setDraft(null)}
        save={async () => {
          const changes = Object.fromEntries(Object.entries(draft || {}).filter(([key, value]) => value !== r.data?.[key]));
          if (!Object.keys(changes).length) return true;
          const ok = await act("/api/monitoring/display", changes);
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

export function monitoringSettingsTabs(state: State) {
  return [
    ["overview", "主页"],
    ["server", "服务器"],
    ...(state.components.xray ? [["xray", "Xray"]] : []),
    ...(state.components.warp ? [["warp", "WARP"]] : []),
    ...(state.components.wireguard ? [["wireguard", "WireGuard"]] : []),
  ];
}

export function MonitoringSettings({
  state,
  act,
  initialOverviewView,
  module = "overview",
}: {
  state: State;
  act: Act;
  initialOverviewView?: OverviewView;
  module?: string;
}) {
  const [overviewView, setOverviewView] = useState<OverviewView | undefined>(() => {
    const saved = localStorage.getItem("foxray.monitor.settings.overview-view");
    return initialOverviewView ||
      (saved === "services" || saved === "dashboard" || saved === "nodes" ? saved : undefined);
  });
  useEffect(() => {
    if (overviewView) localStorage.setItem("foxray.monitor.settings.overview-view", overviewView);
  }, [overviewView]);
  const tabs = monitoringSettingsTabs(state);
  const active = tabs.some(([id]) => id === module) ? module : "overview";
  return (
    <>
      <SettingsSaveProvider
        key={active}
        expected={active === "overview" ? 2 : 4}
      >
        {active === "overview" ? (
          <>
            <PresentationSettings
              overview
              act={act}
              state={state}
              configureDefault
              usePageSave
              editorView={overviewView}
              onEditorViewChange={setOverviewView}
            />
            <DisplaySettings act={act} />
          </>
        ) : (
          <>
            {active === "server" ? (
              <SettingsSaveProvider inline expected={2}>
                {footer => (
                  <MonitorItemsSettings module="host" act={act} grouped footer={footer}>
                    <HostSettings act={act} embedded />
                  </MonitorItemsSettings>
                )}
              </SettingsSaveProvider>
            ) : <MonitorItemsSettings module={active} act={act} />}
            {active === "server" ? (
              <>
                <DataPolicyCard module="host" kind="metrics" title="监控数据" act={act} />
                <DataPolicyCard
                  module={active === "server" ? "host" : active}
                  kind="connections"
                  title="活动连接"
                  act={act}
                />
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

export function GlobalPreferencesCards({ act }: { act: Act }) {
  return (
    <>
      <PresentationSettings act={act} />
      <DisplaySettings act={act} />
      <DataPolicyCard module="all" kind="" title="监控数据默认设置" act={act} />
      <ManagementUserSettings />
      <ManagementDDNSSettings />
      <ManagementHTTPSSettings />
      <ConfigBackupSettings />
    </>
  );
}
