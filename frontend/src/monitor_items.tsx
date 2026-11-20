import { hasChanges } from "./settings_save";
import { SettingInfo } from "./ui";
import {
  ScaledMonitorPreview,
  MockMonitorChart,
  type MonitorPreviewSpec,
} from "./scaled_monitor_preview";
import { HostRuntimePreview } from "./host_runtime_preview";
import { TrafficSourceSetting } from "./traffic_source_setting";
import { LayoutEditor, layoutEditorHelp } from "./layout_editor";
import { useState, type ReactNode } from "react";
import { record, type Dict, type State } from "./api";
import {
  Panel,
  PageHeader,
  ErrorLine,
  Field,
  SettingsSection,
  useResource,
} from "./ui";
import { SaveCard } from "./settings_save";
import { labelIcon } from "./ui_icons";
import type { Act } from "./nodes";

export const MONITOR_ITEMS: Record<string, string[][]> = {
  host: [
    ["load", "平均负载"],
    ["cpu", "CPU"],
    ["memory", "内存"],
    ["network", "网卡流量"],
    ["disk", "磁盘 I/O"],
    ["connections", "网络连接"],
    ["root", "磁盘使用率"],
  ],
  xray: [
    ["traffic", "流量监控"],
    ["latency", "延迟监控"],
    ["connections", "活动连接"],
  ],
  warp: [
    ["traffic", "流量监控"],
    ["latency", "延迟监控"],
    ["connections", "活动连接"],
  ],
  wireguard: [
    ["traffic", "流量监控"],
    ["latency", "延迟监控"],
    ["connections", "活动连接"],
  ],
};
export function monitorEnabled(state: State, module: string, key: string) {
  return (
    record(record(state.monitoring_items)._modules)[module] !== false &&
    record(record(state.monitoring_items)[module])[key] !== false
  );
}
export function MonitorItemsSettings({
  module,
  act,
  layoutEditor = false,
  chartPreviews = {},
  children,
  footer,
  grouped = false,
}: {
  module: string;
  act: Act;
  layoutEditor?: boolean;
  chartPreviews?: Record<string, MonitorPreviewSpec>;
  children?: ReactNode;
  footer?: ReactNode;
  grouped?: boolean;
}) {
  const resource = useResource<Dict>("/api/monitoring/items", 15000);
  const [draft, setDraft] = useState<Dict | null>(null);
  const options = MONITOR_ITEMS[module] || [];
  const defaults: Dict = {
    ...Object.fromEntries(options.map(([key]) => [key, true])),
    ...(module === "host" && layoutEditor
      ? {
          layout: "double",
          runtime_order: ["load", "cpu", "memory", "root"],
          order: options.filter(([key]) => key !== "root").map(([key]) => key),
        }
      : {}),
  };
  const values = { ...defaults, ...record(resource.data?.[module]), ...draft };
  if (module === "host") delete values.partition;
  const order = (
    (Array.isArray(values.order)
      ? values.order
      : options.map(([key]) => key)) as string[]
  ).filter((key) => key !== "root" && options.some(([id]) => id === key));
  return (
    <>
      <Panel className="monitor-items-settings">
        <PageHeader
          title={module === "host" ? "监控指标采集与展示" : "监控项开关"}
          description={
            <>
              {"默认全部开启。关闭后隐藏对应卡片并将监控菜单置灰，历史数据保留；不修改代理、自动选路、探测策略或网络连接。连接监控关闭后停止该模块的连接采集。" +
                (module === "host"
                  ? "各指标开关控制卡片显示，隐藏卡片不会关闭服务器指标采集；下方“采集服务器指标”决定是否采集，默认指标决定首次显示内容。采集频率、保留和历史写入间隔在监控数据卡片中管理。磁盘使用率仅在服务器运行状态和服务总览显示，不生成历史图，也不参与历史卡片排序。"
                  : "")}
              <p>
                {layoutEditor &&
                  layoutEditorHelp(
                    String(values.layout || "double"),
                    false,
                    true,
                  )}
              </p>
            </>
          }
        />
        <SettingsSection className="monitor-choice-group">
          <div className="monitor-items-options">
            {options.map(([key, label]) => (
              <div className="monitor-item-option" key={key}>
                <label className="check">
                  <input
                    type="checkbox"
                    checked={values[key] !== false}
                    onChange={(e) =>
                      setDraft({ ...values, [key]: e.target.checked })
                    }
                  />
                  {labelIcon(label, 16)}
                  {label}
                </label>
                {key === "traffic" && (
                  <SettingInfo
                    label={`${module === "xray" ? "Xray" : module === "warp" ? "WARP" : "WireGuard"}流量监控`}
                    text={
                      module === "xray"
                        ? "流量监控控制 Xray 流量卡片和监控入口的显示；关闭后保留历史，不关闭核心统计，也不停止已启用的流量采集。Xray核心流量统计提供各出口的上传、下载字节计数，FoXray 据此计算速率、累计流量并保存历史。查看实时流量需要同时开启流量监控、Xray核心流量统计及 Xray 模块监控总开关，且核心统计可用；仅开启流量监控不会自动启用核心统计。模块监控总开关关闭后停止采集。"
                        : `${module === "warp" ? "WARP" : "WireGuard"} 直接读取隧道接口计数，无需额外的核心流量统计开关。流量监控控制卡片和监控入口显示；关闭后保留历史，不停止已启用的流量采集。模块监控总开关关闭后停止采集。`
                    }
                  />
                )}
              </div>
            ))}
          </div>
          {module === "xray" && <TrafficSourceSetting act={act} />}
          {module === "host" && layoutEditor && (
            <>
              <Field
                label="历史图布局"
                info="单排每行一张，双排每行两张；运行状态固定在最上方，不受布局和拖动影响。窄屏自动单列。"
              >
                <select
                  aria-label="服务器历史图布局"
                  value={String(values.layout || "double")}
                  onChange={(e) =>
                    setDraft({ ...values, layout: e.target.value })
                  }
                >
                  <option value="single">单排布局</option>
                  <option value="double">双排布局</option>
                </select>
              </Field>
            </>
          )}
        </SettingsSection>
        {module === "host" && layoutEditor && (
          <ScaledMonitorPreview>
            <SettingsSection
              title="运行状态"
              info="拖动圆形指标或聚焦后用方向键排序；预览使用模拟数据，只调整显示顺序。"
            >
              <HostRuntimePreview
                items={values}
                order={values.runtime_order as string[]}
                onChange={(next) =>
                  setDraft({ ...values, runtime_order: next })
                }
              />
            </SettingsSection>
            <LayoutEditor
              layout={String(values.layout || "double")}
              value={order.filter((id) => values[id] !== false)}
              options={options.filter(([key]) => key !== "root")}
              renderCard={(id, label) => (
                <MockMonitorChart
                  title={
                    (
                      { cpu: "CPU 使用率", memory: "内存使用率" } as Record<
                        string,
                        string
                      >
                    )[id] || label
                  }
                  kind={chartPreviews[id]?.kind || "area"}
                  series={
                    chartPreviews[id]?.series ||
                    (
                      {
                        load: ["1 分钟"],
                        cpu: ["使用率"],
                        memory: ["使用率"],
                        disk: ["写入", "读取"],
                        connections: ["连接数"],
                        network: ["下载", "上传"],
                      } as Record<string, string[]>
                    )[id]
                  }
                  colors={
                    chartPreviews[id]?.colors ||
                    (id === "disk"
                      ? ["var(--metric-orange)", "var(--accent)"]
                      : undefined)
                  }
                />
              )}
              onChange={(next) => {
                let index = 0;
                setDraft({
                  ...values,
                  order: order.map((id) =>
                    next.includes(id) ? next[index++] : id,
                  ),
                });
              }}
            />
          </ScaledMonitorPreview>
        )}
        <ErrorLine error={resource.error} />
        {children}
        <SaveCard
          showActions={!grouped}
 dirty={hasChanges(draft, {...defaults,...record(resource.data?.[module])})}
          title="监控项开关"
          defaults={() => setDraft(defaults)}
          discard={() => setDraft(null)}
          save={async () => {
            const ok = await act(
              "/api/monitoring/items?module=" + module,
              values,
            );
            if (ok) {
              await resource.refresh();
              setDraft(null);
            }
            return ok;
          }}
        />
        {footer}
      </Panel>
    </>
  );
}
