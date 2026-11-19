import type { OverviewView } from "./overview_policy";
import { ConnectionIcon } from "./monitor_icons";
import { monitorEnabled } from "./monitor_items";
import { IconSelect } from "./icon_select";
import { Activity, ChartNoAxesCombined } from "lucide-react";
import { DNSPage } from "./dns";
import { ConnectionsPage } from "./connections";
import { UpdateBadge } from "./version_panel";
import { labelIcon } from "./ui_icons";
import { useTimeRange } from "./time_range";
import { WireGuardMonitor, WireGuardSettings } from "./wg_monitor";
import { WarpSettings } from "./warp";
import { useState } from "react";
import {
  History,
  DiagnosticLog,
  Operations,
  Switches,
  XrayPage,
  Settings,
  LogSettings,
  NodeBackups,
} from "./views";
import { Nodes, Subscriptions, type Act } from "./nodes";
import { TrafficPage } from "./traffic";
import { HostHistoryPage } from "./host";
import { MonitoringSettings } from "./monitoring_settings";
import { SecondaryNav, Panel, Empty } from "./ui";
import { scopedState, record, type State } from "./api";
function useTab(key: string, initial: string, override?: string) {
  const [tab, setTab] = useState(
    () => override || localStorage.getItem(key) || initial,
  );
  return [
    tab,
    (value: string) => {
      setTab(value);
      localStorage.setItem(key, value);
    },
  ] as const;
}
export function ModuleHeading({
  title,
  tabs,
  value,
  onChange,
}: {
  title: string;
  description: string;
  tabs: string[][];
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <SecondaryNav label={title}>
      <div className="module-tabs" role="tablist">
        {tabs.map(([id, label]) => (
          <button
            key={id}
            role="tab"
            aria-selected={value === id}
            onClick={() => onChange(id)}
          >
            {labelIcon(label, 14)}
            {label}
            {["global", "xray", "warp", "wireguard"].includes(id) && (
              <UpdateBadge service={id === "global" ? "foxray" : id} />
            )}
          </button>
        ))}
      </div>
    </SecondaryNav>
  );
}
export function MonitoringHub({
  state,
  act,
  initialOverviewView,
}: {
  state: State;
  act: Act;
  initialOverviewView?: OverviewView;
}) {
  const [tab, setTab] = useTab(
      "foxray.monitor.module.v322",
      "host",
      initialOverviewView ? "settings" : undefined,
    ),
    [requestedView, setView] = useState("traffic");
  const range = useTimeRange();
  const tabs = [
    ["host", "服务器"],
    ...(state.components.xray ? [["xray", "Xray"]] : []),
    ...(state.components.warp ? [["warp", "WARP"]] : []),
    ...(state.components.wireguard ? [["wireguard", "WireGuard"]] : []),
    ["settings", "设置"],
  ];
  const active = tabs.some(([id]) => id === tab) ? tab : "host";
  const view = monitorEnabled(state, active, requestedView)
    ? requestedView
    : ["traffic", "latency", "connections"].find((key) =>
        monitorEnabled(state, active, key),
      ) || "";
  const viewControl = (
    <div className="monitor-view-picker">
      <IconSelect
        label="监控视图"
        value={view}
        onChange={setView}
        options={[
          {
            value: "traffic",
            label: "流量监控",
            icon: <ChartNoAxesCombined size={20} />,
            tone: "green",
            disabled: !monitorEnabled(state, active, "traffic"),
          },
          {
            value: "latency",
            label: "延迟监控",
            icon: <Activity size={20} />,
            tone: "blue",
            disabled: !monitorEnabled(state, active, "latency"),
          },
          {
            value: "connections",
            label: "活动连接",
            icon: <ConnectionIcon size={20} />,
            tone: "purple",
            disabled: !monitorEnabled(state, active, "connections"),
          },
          ...(!view
            ? [
                {
                  value: "",
                  label: "监控已关闭",
                  icon: <ConnectionIcon size={20} />,
                  tone: "muted",
                  disabled: true,
                },
              ]
            : []),
        ]}
      />
    </div>
  );
  return (
    <div className="module-page monitoring-module-page">
      <ModuleHeading
        title="监控"
        description="服务器、Xray、WARP 和 WireGuard 独立监控"
        tabs={tabs}
        value={active}
        onChange={setTab}
      />
      {active === "xray" || active === "warp" || active === "wireguard" ? (
        <>
          {!view ? (
            <Panel>
              {viewControl}
              <Empty>本模块监控项均已关闭，可在监控设置中开启。</Empty>
            </Panel>
          ) : view === "connections" ? (
            <ConnectionsPage
              act={act}
              module={active}
              viewControl={viewControl}
            />
          ) : active === "wireguard" ? (
            <WireGuardMonitor
              state={state}
              act={act}
              view={view}
              viewControl={viewControl}
              range={range}
            />
          ) : view === "traffic" ? (
            <TrafficPage
              key={active}
              state={scopedState(state, active)}
              act={act}
              viewControl={viewControl}
              range={range}
              module={active}
            />
          ) : (
            <History
              act={act}
              key={active}
              state={scopedState(state, active)}
              viewControl={viewControl}
              range={range}
              module={active}
            />
          )}
        </>
      ) : active === "settings" ? (
        <MonitoringSettings
          state={state}
          act={act}
          initialOverviewView={initialOverviewView}
        />
      ) : record(record(state.monitoring_items)._modules).host === false ? (
        <Panel>
          <Empty>服务器监控已关闭，可在全局监控总开关中开启。</Empty>
        </Panel>
      ) : (
        <HostHistoryPage
          act={act}
          items={record(record(state.monitoring_items).host)}
        />
      )}
    </div>
  );
}
export function XrayHub({ state: fullState, act }: { state: State; act: Act }) {
  const state = scopedState(fullState, "xray");
  const [tab, setTab] = useTab("foxray.xray.tab", "nodes");
  return (
    <div className="module-page">
      <ModuleHeading
        title="Xray 管理"
        description="代理模式、核心服务、节点与订阅集中管理"
        tabs={[
          ["nodes", "节点管理"],
          ["subscriptions", "订阅管理"],
          ["dns", "DNS"],
          ["backups", "备份"],
          ["proxy", "设置"],
        ]}
        value={tab}
        onChange={setTab}
      />
      {tab === "nodes" ? (
        <Nodes state={state} act={act} />
      ) : tab === "subscriptions" ? (
        <Subscriptions state={state} act={act} />
      ) : tab === "dns" ? (
        <DNSPage act={act} />
      ) : tab === "backups" ? (
        <NodeBackups act={act} />
      ) : (
        <XrayPage state={scopedState(state, "xray")} act={act} />
      )}
    </div>
  );
}
export function LogsHub({ state, act }: { state: State; act: Act }) {
  const [tab, setTab] = useTab("foxray.logs.tab.v301", "events");
  return (
    <div className="module-page">
      <ModuleHeading
        title="日志"
        description="检测任务、出口切换与管理操作统一归档，支持查看详情"
        tabs={[
          ["events", "操作日志"],
          ["probes", "检测日志"],
          ["switches", "切换日志"],
          ["diagnostics", "诊断日志"],
          ["settings", "设置"],
        ]}
        value={tab}
        onChange={setTab}
      />
      {tab === "diagnostics" ? (
        <DiagnosticLog act={act} />
      ) : tab === "settings" ? (
        <LogSettings state={state} act={act} />
      ) : tab === "probes" ? (
        <Operations kind="probe" title="检测日志" />
      ) : tab === "events" ? (
        <Operations />
      ) : (
        <Switches />
      )}
    </div>
  );
}

export function GlobalSettingsHub({ state, act }: { state: State; act: Act }) {
  const [tab, setTab] = useTab("foxray.global-settings.tab", "global");
  const tabs = [
    ["monitoring", "监控"],
    ["logs", "日志"],
    ...(state.components.xray ? [["xray", "Xray"]] : []),
    ...(state.components.warp ? [["warp", "WARP"]] : []),
    ...(state.components.wireguard ? [["wireguard", "WireGuard"]] : []),
    ["global", "FoXray"],
  ];
  const active = tabs.some(([id]) => id === tab) ? tab : "global";
  return (
    <div className="module-page">
      <ModuleHeading
        title="设置"
        description="各模块设置的统一入口"
        tabs={tabs}
        value={active}
        onChange={setTab}
      />
      {active === "wireguard" ? (
        <WireGuardSettings act={act} />
      ) : active === "xray" ? (
        <XrayPage state={scopedState(state, "xray")} act={act} />
      ) : active === "warp" ? (
        <WarpSettings act={act} />
      ) : active === "monitoring" ? (
        <MonitoringSettings state={state} act={act} />
      ) : active === "logs" ? (
        <LogSettings state={state} act={act} />
      ) : (
        <Settings state={state} act={act} />
      )}
    </div>
  );
}
