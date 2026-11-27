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
          info={<ul><li><strong>取值范围：</strong>1～3600 秒，默认 10 秒。</li><li><strong>作用：</strong>控制浏览器重新获取图表数据的间隔。</li><li><strong>独立设置：</strong>后台采集、主动探测和历史写入继续使用各自间隔。</li></ul>}
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
          info={<ul><li><strong>取值范围：</strong>1～3600 秒，默认 30 秒。按真实字节量与采样时长计算区间均速。</li><li><strong>影响范围：</strong>用于 Xray、WARP 和 WireGuard 流量曲线。实时速率、采集、历史写入和累计流量保持各自设置。</li><li><strong>长时间范围：</strong>图表可能继续合并数据。可在流量监控中临时选择精细或自定义窗口。</li></ul>}
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
