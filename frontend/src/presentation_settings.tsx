import { homeMotionEnabled, setHomeMotionEnabled } from "./home_header";
import { PageBackgroundSettings } from "./page_background";
import { NodeVisibilityPicker } from "./node_visibility_picker";
import { hasChanges } from "./settings_save";
import { SettingInfo } from "./ui";
import { PageDisplayRangeIcon } from "./requirement_icons";
import { ColorInput } from "./color_input";
import { createPortal } from "react-dom";
import { OverviewSettingsCard } from "./overview_settings_card";
import { OverviewFooter } from "./overview_footer";
import { ClockSettings } from "./overview_clock";
import {
  useAppearanceTheme,
  themedAppearance,
  appearancePatch,
} from "./theme_appearance";
import { ServiceThemePicker } from "./service_theme_picker";
import {
  serviceThemeSnapshot,
  listThemes,
  customServiceTheme,
} from "./service_themes";
import {
  OverviewSurfaceSettings,
  overviewSurfaceVariables,
  overviewSurfaceError,
  overviewTextError,
} from "./overview_surfaces";
import { SlidersHorizontal } from "lucide-react";
import {
  DEFAULT_SERVICE_THEME,
  SERVICE_STYLE_FIELDS,
  SERVICE_CIRCLE_STYLES,
  normalizeServiceLayoutChange,
  SERVICE_METRICS,
  serviceStyle,
  serviceStyleAvailable,
  splitWireGuardCards,
  wireguardNameStyle,
  overviewPolicy,
  defaultOverviewView,
  SERVICE_CARDS,
  EXIT_CARDS,
  type OverviewView,
} from "./overview_policy";
import { LayoutEditor, layoutEditorHelp } from "./layout_editor";
import { useState, useEffect, useRef, useLayoutEffect } from "react";
import { api, list, record, type Dict, type State } from "./api";
import {
  Button,
  ErrorLine,
  Field,
  NumberInput,
  PageHeader,
  Panel,
  SettingsSection,
  useResource,
} from "./ui";
import { SaveCard } from "./settings_save";
import { Home, type Act } from "./nodes";
import { OverviewViewPicker } from "./host";
import { useOverviewDrag } from "./overview_editor";

const units: Record<string, number> = {
  minute: 1 / 60,
  hour: 1,
  day: 24,
  month: 720,
  year: 8760,
};
export const OVERVIEW_CARDS = [
  ["info", "当前出口"],
  ["traffic", "实时流量"],
  ["https", "真连接延迟"],
  ["connections", "活动连接数"],
  ["icmp", "ICMP延迟"],
  ["service-host", "服务器总览"],
  ["service-xray", "Xray总览"],
  ["service-warp", "WARP总览"],
  ["service-wireguard", "WireGuard总览"],
];
const defaults: Dict = {
  range_value: 24,
  range_unit: "hour",
  overview_show_clock: false,
  overview_show_data_status: false,
  overview_independent: false,
  overview_range_value: 24,
  overview_range_unit: "hour",
  overview_layout: "grid",
  overview_cards: ["info", "traffic", "https", "connections"],
};
export function rangeHours(value: unknown, overview = false) {
  const p = record(value),
    prefix = overview && p.overview_independent ? "overview_" : "";
  return (
    Number(p[prefix + "range_value"] || 24) *
    (units[String(p[prefix + "range_unit"] || "hour")] || 1)
  );
}
export function PresentationSettings({
  act,
  overview = false,
  services = false,
  nodes = false,
  state,
  configureDefault = false,
  editorView,
  onEditorViewChange,
  titleInDialog = false,
}: {
  act: Act;
  overview?: boolean;
  services?: boolean;
  nodes?: boolean;
  state?: State;
  configureDefault?: boolean;
  editorView?: OverviewView;
  onEditorViewChange?: (view: OverviewView) => void;
  titleInDialog?: boolean;
}) {
  return overview ? (
    <OverviewLayoutSettings
      act={act}
      state={state}
      initialView={nodes ? "nodes" : services ? "services" : "dashboard"}
      configureDefault={configureDefault}
      editorView={editorView}
      onEditorViewChange={onEditorViewChange}
      titleInDialog={titleInDialog}
    />
  ) : (
    <>
      <GlobalPresentationSettings act={act} />
      <PageWidthSettings act={act} />
      <PageBackgroundSettings />
      <ClockSettings act={act} />
    </>
  );
}
export function PageWidthSettings({ act }: { act: Act }) {
  const [motionOverride, setMotionOverride] = useState(homeMotionEnabled);
  const resource = useResource<Dict>("/api/monitoring/presentation", 15000),
    [draft, setDraft] = useState<Dict | null>(null);
  const saved = {
      content_width_percent: Number(resource.data?.content_width_percent ?? 99),
      module_width_percent: Number(resource.data?.module_width_percent ?? 95),
      home_header_auto_hide: resource.data?.home_header_auto_hide === true,
      home_header_idle_seconds: Number(resource.data?.home_header_idle_seconds ?? 5),
    },
    value = { ...saved, ...draft };
  return (
    <Panel className="presentation-settings page-width-settings">
      <PageHeader
        title="页面显示范围"
        icon={<PageDisplayRangeIcon size={20} />}
        description="分别设置首页和非首页的内容宽度。"
      />
      <SettingsSection>
        <div className="form-grid">
          <Field
            label="首页显示宽度（%）"
            info="80% 保留两侧留白。首页右侧放不下工具按钮时，工具栏自动收起。悬停、点击或聚焦右上角标识即可展开。"
          >
            <NumberInput
              aria-label="首页显示宽度（%）"
              min={80}
              max={100}
              value={value.content_width_percent}
              onChange={(e) =>
                setDraft({
                  ...value,
                  content_width_percent: Number(e.target.value),
                })
              }
            />
          </Field>
          <Field label="非首页显示宽度（%）" info="适用于监控、设置等非首页，按页面可用宽度计算。">
            <NumberInput
              aria-label="非首页显示宽度（%）"
              min={80}
              max={100}
              value={value.module_width_percent}
              onChange={(e) => setDraft({ ...value, module_width_percent: Number(e.target.value) })}
            />
          </Field>
          <Field
            label="隐藏首页标题栏"
            info="首页无操作达到设定时间后自动收起。点击 FoXray 图标可展开或收起。"
          >
            <select
              aria-label="隐藏首页标题栏"
              value={value.home_header_auto_hide ? "on" : "off"}
              onChange={(e) =>
                setDraft({
                  ...value,
                  home_header_auto_hide: e.target.value === "on",
                })
              }
            >
              <option value="off">关闭</option>
              <option value="on">开启</option>
            </select>
          </Field>
          {value.home_header_auto_hide && <Field label="无操作后隐藏（秒）">
            <NumberInput aria-label="无操作后隐藏（秒）" min={1} max={3600}
              value={value.home_header_idle_seconds}
              onChange={e => setDraft({ ...value, home_header_idle_seconds: Number(e.target.value) })} />
          </Field>}
          <Field label="首页动效" info="默认跟随浏览器。选择始终播放后立即生效，仅应用于这个浏览器中的 FoXray 首页。">
            <select aria-label="首页动效" value={motionOverride ? "on" : "auto"} onChange={event => {
              const enabled = event.target.value === "on";
              setHomeMotionEnabled(enabled);
              setMotionOverride(enabled);
            }}>
              <option value="auto">跟随浏览器</option>
              <option value="on">始终播放</option>
            </select>
          </Field>
        </div>
      </SettingsSection>
      <SaveCard
        title="页面显示范围"
        dirty={hasChanges(draft, saved)}
        disabled={
          !resource.data ||
          !Number.isInteger(value.home_header_idle_seconds) || value.home_header_idle_seconds < 1 || value.home_header_idle_seconds > 3600 ||
          [value.content_width_percent, value.module_width_percent].some(width =>
            !Number.isInteger(width) || width < 80 || width > 100
          )
        }
        defaults={() =>
          setDraft({ content_width_percent: 99, module_width_percent: 95, home_header_auto_hide: false, home_header_idle_seconds: 5 })
        }
        discard={() => setDraft(null)}
        save={async () => {
          if (!draft) return true;
          if (!(await act("/api/monitoring/presentation", draft))) return false;
          await resource.refresh();
          setDraft(null);
          return true;
        }}
      />
    </Panel>
  );
}
function GlobalPresentationSettings({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/monitoring/presentation", 15000),
    modules = useResource<Dict>("/api/monitoring/modules", 15000);
  const [draft, setDraft] = useState<Dict>({}),
    [moduleDraft, setModuleDraft] = useState<Dict | null>(null);
  const p = { ...defaults, ...r.data, ...draft },
    values = {
      host: true,
      xray: true,
      warp: true,
      wireguard: true,
      ...modules.data,
      ...moduleDraft,
    };
  return (
    <Panel className="presentation-settings monitor-master-settings">
      <PageHeader
        title="监控总开关"
        description="控制各模块监控的采集、保存和显示；保留单项设置与已有历史，不停用转发服务，主动探测策略保持独立。"
      />
      <SettingsSection>
        <div className="monitor-master-switches">
          <div className="monitor-items-options">
            {[
              ["host", "服务器"],
              ["xray", "Xray"],
              ["warp", "WARP"],
              ["wireguard", "WireGuard"],
            ].map(([id, label]) => (
              <label className="check" key={id}>
                <input
                  type="checkbox"
                  checked={values[id as keyof typeof values] !== false}
                  onChange={(e) =>
                    setModuleDraft({ ...values, [id]: e.target.checked })
                  }
                />
                {label}
              </label>
            ))}
          </div>
          <div className="actions">
            <Button
              onClick={() =>
                setModuleDraft({
                  host: true,
                  xray: true,
                  warp: true,
                  wireguard: true,
                })
              }
            >
              全选
            </Button>
            <Button
              onClick={() =>
                setModuleDraft({
                  host: false,
                  xray: false,
                  warp: false,
                  wireguard: false,
                })
              }
            >
              全关
            </Button>
          </div>
        </div>
        <div className="form-grid">
          <Field label="默认时间范围">
            <NumberInput
              aria-label="统一默认时间范围"
              min={1}
              max={525600}
              value={Number(p.range_value)}
              onChange={(e) =>
                setDraft({ ...draft, range_value: Number(e.target.value) })
              }
            />
          </Field>
          <Field label="单位">
            <RangeUnit
              value={String(p.range_unit)}
              onChange={(value) => setDraft({ ...draft, range_unit: value })}
            />
          </Field>
        </div>
      </SettingsSection>
      <ErrorLine error={r.error || modules.error} />
      <SaveCard
        dirty={
          hasChanges(draft, { ...defaults, ...r.data }) ||
          hasChanges(moduleDraft, {
            host: true,
            xray: true,
            warp: true,
            wireguard: true,
            ...modules.data,
          })
        }
        title="监控总开关"
        disabled={!r.data || !modules.data}
        defaults={() => {
          setDraft({ range_value: 24, range_unit: "hour" });
          setModuleDraft({
            host: true,
            xray: true,
            warp: true,
            wireguard: true,
          });
        }}
        discard={() => {
          setDraft({});
          setModuleDraft(null);
        }}
        save={async () => {
          if (moduleDraft) {
            if (!(await act("/api/monitoring/modules", values))) return false;
            await modules.refresh();
            setModuleDraft(null);
          }
          if (
            Object.keys(draft).length &&
            !(await act("/api/monitoring/presentation", draft))
          )
            return false;
          await r.refresh();
          setDraft({});
          return true;
        }}
      />
    </Panel>
  );
}
function RangeUnit({
  value,
  onChange,
  disabled = false,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <select
      aria-label="时间范围单位"
      value={value}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
    >
      {[
        ["minute", "分钟"],
        ["hour", "小时"],
        ["day", "天"],
        ["month", "月（30天）"],
        ["year", "年（365天）"],
      ].map(([id, label]) => (
        <option key={id} value={id}>
          {label}
        </option>
      ))}
    </select>
  );
}
const viewLabels: Record<OverviewView, string> = {
  services: "服务总览",
  dashboard: "当前出口",
  nodes: "节点一览",
};
function OverviewLayoutSettings({
  act: performAct,
  state,
  initialView,
  configureDefault,
  editorView,
  onEditorViewChange,
  titleInDialog,
}: {
  act: Act;
  state?: State;
  initialView: OverviewView;
  configureDefault: boolean;
  editorView?: OverviewView;
  onEditorViewChange?: (view: OverviewView) => void;
  titleInDialog: boolean;
}) {
  const [peek, setPeek] = useState(false);
  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.shiftKey && !e.altKey && !e.metaKey) setPeek(true);
      if (e.key === "Escape") setPeek(false);
    };
    const up = (e: KeyboardEvent) => {
      if (!e.ctrlKey || !e.shiftKey) setPeek(false);
    };
    const close = () => setPeek(false);
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    window.addEventListener("blur", close);
    document.addEventListener("visibilitychange", close);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
      window.removeEventListener("blur", close);
      document.removeEventListener("visibilitychange", close);
    };
  }, []);
  const [sectionBusy, setSectionBusy] = useState(false);
  const [sectionError, setSectionError] = useState("");
  const [themeName, setThemeName] = useState("");
  const [topSaveTarget, setTopSaveTarget] = useState<HTMLDivElement | null>(
    null,
  );
  const wireguard = useResource<Dict>("/api/network/wireguard", 15000);
  const r = useResource<Dict>("/api/monitoring/presentation", 15000),
    [draftByTheme, setDraftByTheme] = useState<Record<string, Dict>>({}),
    [localView, setLocalView] = useState<OverviewView>(() =>
      configureDefault && state
        ? defaultOverviewView(state.manager_settings.presentation)
        : initialView,
    );
  const themeKey = useAppearanceTheme();
  const draft = draftByTheme[themeKey] || {};
  const setDraft = (value: Dict | ((old: Dict) => Dict)) =>
    setDraftByTheme((all) => ({
      ...all,
      [themeKey]:
        typeof value === "function" ? value(all[themeKey] || {}) : value,
    }));
  const act: Act = async (path, data, receive) => {
    if (path === "/api/monitoring/presentation" && data) {
      const latest = await api<Dict>(path);
      return performAct(
        path,
        appearancePatch(record(data), latest, themeKey),
        receive,
      );
    }
    return performAct(path, data, receive);
  };
  const view = editorView ?? localView;
  const setView = onEditorViewChange || setLocalView;
  const initialized = useRef(!configureDefault || !!state);
  useEffect(() => {
    if (r.data && !initialized.current) {
      setView(defaultOverviewView(r.data));
      initialized.current = true;
    }
  }, [r.data]);
  const savedPresentation = {
      ...defaults,
      ...themedAppearance(state?.manager_settings.presentation),
      ...themedAppearance(r.data),
    };
  const p = { ...savedPresentation, ...draft },
    scoped = overviewPolicy(p, view),
    prefix =
      view === "services"
        ? "services_"
        : view === "nodes"
          ? "nodes_"
          : "overview_";
  const nodeOptions = (state?.nodes || [])
    .filter((n) => n.id !== "warp")
    .map((n) => [n.id, n.name]);
  const cards =
    view === "nodes"
      ? [
          ...scoped.cards.filter((id) =>
            nodeOptions.some(([key]) => key === id),
          ),
          ...nodeOptions
            .map(([id]) => id)
            .filter((id) => !scoped.cards.includes(id)),
        ]
      : scoped.cards;
  const options =
    view === "nodes"
      ? nodeOptions
      : OVERVIEW_CARDS.filter(([id]) =>
          view === "services"
            ? id.startsWith("service-")
            : !id.startsWith("service-") || scoped.cards.includes(id),
        );
  const set = (key: string, value: unknown) =>
    setDraft({ ...draft, [key]: value });
  const surfaceError =
    overviewSurfaceError(p.overview_surfaces) ||
    overviewTextError(p.overview_text) ||
    (![1, 2, 3, 4].includes(Number(p.services_wireguard_count || 2))
      ? "显示接口数量须为1～4"
      : "") ||
    (!Number.isInteger(Number(p.services_wireguard_diameter || 0)) ||
    (Number(p.services_wireguard_diameter || 0) !== 0 &&
      (Number(p.services_wireguard_diameter) < 40 ||
        Number(p.services_wireguard_diameter) > 1200))
      ? "圆形直径须为0或40～1200的整数"
      : "") ||
    (!Number.isInteger(Number(p.services_wireguard_label_size || 14)) ||
    Number(p.services_wireguard_label_size || 14) < 8 ||
    Number(p.services_wireguard_label_size || 14) > 24 ||
    !Number.isInteger(Number(p.services_wireguard_value_size || 24)) ||
    Number(p.services_wireguard_value_size || 24) < 12 ||
    Number(p.services_wireguard_value_size || 24) > 40
      ? "WireGuard名称字号8～24，数值字号12～40"
      : "") ||
    (!Number.isInteger(Number(p.services_wireguard_ring_opacity ?? 100)) ||
    Number(p.services_wireguard_ring_opacity ?? 100) < 0 ||
    Number(p.services_wireguard_ring_opacity ?? 100) > 100 ||
    !Number.isInteger(Number(p.services_wireguard_ring_shadow || 0)) ||
    Number(p.services_wireguard_ring_shadow || 0) < 0 ||
    Number(p.services_wireguard_ring_shadow || 0) > 40
      ? "圆环不透明度须为0～100，阴影须为0～40的整数"
      : "") ||
    (!Number.isInteger(Number(p.nodes_limit || 0)) ||
    Number(p.nodes_limit || 0) < 0 ||
    Number(p.nodes_limit || 0) > 4096
      ? "节点显示数量须为0～4096的整数"
      : "");
  const setCards = (next: string[]) =>
    set(prefix + (view === "nodes" ? "order" : "cards"), next);
  const defaultFields: Dict = {
    default_overview_view: "services",
    service_custom_themes: [],
    services_host_order: ["cpu", "load", "memory", "root"],
    services_xray_order: SERVICE_METRICS,
    services_warp_order: SERVICE_METRICS,
    services_layout: "grid",
    overview_layout: "grid",
    nodes_layout: "grid",
    services_cards: SERVICE_CARDS,
    overview_cards: EXIT_CARDS,
    nodes_order: [],
    nodes_limit: 0,
    nodes_visibility_mode: "all",
    nodes_visible_ids: [],
    overview_text: {},
    overview_surfaces: {},
    services_style: "metrics",
    services_host_style: "metrics",
    services_xray_style: "metrics",
    services_warp_style: "metrics",
    services_wireguard_layout: "metrics",
    services_wireguard_style: "center",
    services_wireguard_interfaces: [],
    services_wireguard_ring_color: "",
    services_wireguard_label_size: 14,
    services_wireguard_count: 2,
    services_wireguard_axis: "auto",
    services_wireguard_diameter: 0,
    services_wireguard_value_size: 24,
    services_wireguard_ring_opacity: 100,
    services_wireguard_ring_shadow: 0,
    overview_show_clock: false,
    overview_show_data_status: false,
    overview_independent: false,
    overview_range_value: 24,
    overview_range_unit: "hour",
    ...DEFAULT_SERVICE_THEME,
  };
  const scopeEdit = (keys: string[], reset: boolean) =>
    setDraft((old) => {
      const next = { ...old };
      for (const key of keys) {
        if (key.startsWith("overview_surfaces:")) {
          const layer = key.split(":")[1];
          const layers = {
            ...record(
              next.overview_surfaces ||
                themedAppearance(r.data).overview_surfaces,
            ),
          };
          if (reset)
            layers[layer] = {
              enabled: false,
              color: "#171f2b",
              opacity: 80,
              blur: 12,
              border_enabled: false,
              border_color: "#40516a",
              border_opacity: 60,
              shadow: 12,
            };
          else if (record(themedAppearance(r.data).overview_surfaces)[layer])
            layers[layer] = record(themedAppearance(r.data).overview_surfaces)[
              layer
            ];
          else delete layers[layer];
          next.overview_surfaces = layers;
        } else if (reset) next[key] = defaultFields[key];
        else delete next[key];
      }
      return next;
    });
  const scopeSave = async (keys: string[]) => {
    setSectionBusy(true);
    setSectionError("");
    const submitted: Dict = {};
    try {
      const latest = themedAppearance(
        await api<Dict>("/api/monitoring/presentation"),
      );
      for (const key of keys) {
        if (key.startsWith("overview_surfaces:")) {
          const layer = key.split(":")[1];
          if (draft.overview_surfaces)
            submitted.overview_surfaces = {
              ...record(latest.overview_surfaces),
              [layer]: record(draft.overview_surfaces)[layer],
            };
        } else if (key in draft) submitted[key] = draft[key];
      }
      if (
        Object.keys(submitted).length &&
        !(await act("/api/monitoring/presentation", submitted))
      )
        return;
      await r.refresh();
      setDraft((old) => {
        const next = { ...old };
        for (const key of keys) {
          if (key.startsWith("overview_surfaces:")) {
            const layer = key.split(":")[1];
            if (
              JSON.stringify(record(old.overview_surfaces)[layer]) ===
              JSON.stringify(record(submitted.overview_surfaces)[layer])
            ) {
              const layers = { ...record(old.overview_surfaces) };
              delete layers[layer];
              next.overview_surfaces = {
                ...record(latest.overview_surfaces),
                ...record(submitted.overview_surfaces),
                ...layers,
              };
            }
          } else if (
            JSON.stringify(old[key]) === JSON.stringify(submitted[key])
          )
            delete next[key];
        }
        return next;
      });
    } catch (e) {
      setSectionError(String(e));
    } finally {
      setSectionBusy(false);
    }
  };
  const actions = (keys: string[], label: string) => (
    <div className="overview-section-actions" role="group" aria-label={label}>
      <Button disabled={sectionBusy} onClick={() => scopeEdit(keys, true)}>
        恢复默认
      </Button>
      <Button disabled={sectionBusy} onClick={() => scopeEdit(keys, false)}>
        放弃保存
      </Button>
      <Button
        variant="primary"
        highlighted={keys.some((key) => key in draft && hasChanges(draft[key], savedPresentation[key]))}
        disabled={sectionBusy || !!surfaceError}
        onClick={() => void scopeSave(keys)}
      >
        保存并应用
      </Button>
    </div>
  );
  return (
    <>
      {peek &&
        state &&
        createPortal(
          <div
            className="overview-layout-peek"
            popover="manual"
            ref={(el) => el?.showPopover()}
            role="region"
            aria-label="快捷布局预览"
            style={{ width: `${Number(p.content_width_percent || 99)}%` }}
          >
            <div className="overview-peek-hint">
              布局预览 · 松开 Ctrl + Shift 返回设置
            </div>
            <OverviewPreview
              state={state}
              presentation={p}
              view={view}
              onChange={() => {}}
              readonly
            />
          </div>,
          document.body,
        )}
      <div
        className={
          "presentation-settings overview-layout-settings overview-settings-stack" +
          (view === "nodes" ? " nodes-display-settings" : "")
        }
      >
        <div className="overview-layout-scroll">
          <OverviewSettingsCard
            title="总体布局"
            header={
              <div className="overview-layout-toolbar">
                {!titleInDialog && (
                  <PageHeader
                    title="主页布局"
                    titleControl={
                      <OverviewViewPicker
                        value={view}
                        onChange={setView}
                        label="编辑主页页面"
                      />
                    }
                    description={
                      <>
                        {
                          "服务总览、当前出口和节点一览分别保存排列与自定义顺序。当前出口和服务总览提供三种排列，节点一览提供双列与单列；拖动预览或使用方向键调整，保存后应用，不改变监控采集、节点启用或转发服务。按住 Ctrl + Shift 可临时查看与主页同位置的预览，松开返回。"
                        }
                        <p>
                          {layoutEditorHelp(
                            String(scoped.layout),
                            view === "nodes",
                          )}
                        </p>
                      </>
                    }
                  />
                )}
                <div className="overview-top-save" ref={setTopSaveTarget} />
              </div>
            }
          >
            {state ? (
              <OverviewPreview
                state={state}
                presentation={p}
                view={view}
                onChange={(key, value) => set(key, value)}
              />
            ) : (
              <LayoutEditor
                value={cards}
                options={options}
                layout={scoped.layout}
                collection={view === "nodes"}
                onChange={setCards}
              />
            )}
          </OverviewSettingsCard>
          <OverviewSettingsCard
            title="排版布局"
            actions={actions(
              [
                prefix + "layout",
                "overview_show_clock",
                "overview_show_data_status",
                ...(view === "nodes"
                  ? [
                      "nodes_limit",
                      "nodes_visibility_mode",
                      "nodes_visible_ids",
                    ]
                  : []),
                ...(configureDefault ? ["default_overview_view"] : []),
                ...(view === "services" ? ["service_custom_themes"] : []),
                prefix + (view === "nodes" ? "order" : "cards"),
                ...(view === "services"
                  ? [
                      "services_host_order",
                      "services_xray_order",
                      "services_warp_order",
                    ]
                  : []),
                ...(view === "services"
                  ? [
                      "services_host_style",
                      "services_xray_style",
                      "services_warp_style",
                      "services_wireguard_layout",
                      "services_wireguard_style",
                    ]
                  : []),
              ],
              "排版布局操作",
            )}
          >
            <SettingsSection title="展示内容">
              {view !== "nodes" ? (
                <div className="monitor-items-options">
                  {options.map(([id, label]) => (
                    <label className="check" key={id}>
                      <input
                        type="checkbox"
                        checked={cards.includes(id)}
                        disabled={!cards.includes(id) && cards.length >= 4}
                        onChange={(e) =>
                          setCards(
                            e.target.checked
                              ? [...cards, id]
                              : cards.filter((v) => v !== id),
                          )
                        }
                      />
                      {label}
                    </label>
                  ))}
                  <div className="monitor-items-options overview-footer-options">
                    {[
                      ["overview_show_clock", "时间"],
                      ["overview_show_data_status", "数据状态"],
                    ].map(([key, label]) => (
                      <label className="check" key={key}>
                        <input
                          type="checkbox"
                          checked={p[key] === true}
                          onChange={(e) => set(key, e.target.checked)}
                        />
                        {label}
                      </label>
                    ))}
                  </div>
                </div>
              ) : (
                <>
                  <div className="form-grid node-display-fields">
                    <Field
                      label="显示数量"
                      validateNumber
                      info="0为全部；按已保存顺序截取，不改变节点启用状态。"
                    >
                      <NumberInput
                        aria-label="节点显示数量"
                        min={0}
                        max={4096}
                        value={Number(p.nodes_limit || 0)}
                        onChange={(e) =>
                          set("nodes_limit", Number(e.target.value))
                        }
                      />
                    </Field>
                    <Field label="显示节点">
                      <NodeVisibilityPicker
                        nodes={(state?.nodes || []).filter(
                          (n) => n.id !== "warp",
                        )}
                        mode={String(p.nodes_visibility_mode || "all")}
                        selected={
                          Array.isArray(p.nodes_visible_ids)
                            ? (p.nodes_visible_ids as string[])
                            : []
                        }
                        onModeChange={(v) => set("nodes_visibility_mode", v)}
                        onChange={(v) => set("nodes_visible_ids", v)}
                      />
                    </Field>
                    <div
                      className="field node-footer-field"
                      role="group"
                      aria-label="显示信息"
                    >
                      <span className="field-title">显示信息</span>
                      <div className="overview-footer-options">
                        {[
                          ["overview_show_clock", "时间"],
                          ["overview_show_data_status", "数据状态"],
                        ].map(([key, label]) => (
                          <label className="check" key={key}>
                            <input
                              type="checkbox"
                              aria-label={label}
                              checked={p[key] === true}
                              onChange={(e) => set(key, e.target.checked)}
                            />
                            {label}
                          </label>
                        ))}
                      </div>
                    </div>
                  </div>
                </>
              )}
            </SettingsSection>
            {view !== "nodes" && (
              <>
                <div className="form-grid">
                  {configureDefault && (
                    <>
                      <Field
                        label="默认样式"
                        info={<ul><li><strong>默认页面。</strong>可选择当前出口、服务总览或节点一览。</li><li><strong>生效时机。</strong>首次打开或重新进入一级主页时使用保存的默认项。</li><li><strong>临时切换。</strong>页面内切换视图不会覆盖已保存的默认项。</li></ul>}
                      >
                        <select
                          aria-label="主页默认样式"
                          value={defaultOverviewView(p)}
                          onChange={(e) => {
                            set("default_overview_view", e.target.value);
                            setView(e.target.value as OverviewView);
                          }}
                        >
                          {Object.entries(viewLabels).map(([id, label]) => (
                            <option key={id} value={id}>
                              {label}
                            </option>
                          ))}
                        </select>
                      </Field>
                    </>
                  )}
                  <Field
                    label="卡片排列"
                    info="田字宫格；第一张占上方，其余在下方；或第一张占左侧，其余在右侧。小屏自动单列。"
                  >
                    <select
                      aria-label="主页卡片排列"
                      value={scoped.layout}
                      onChange={(e) =>
                        view === "services"
                          ? setDraft({
                              ...draft,
                              ...normalizeServiceLayoutChange(
                                p,
                                e.target.value,
                              ),
                            })
                          : set(prefix + "layout", e.target.value)
                      }
                    >
                      <option value="grid">田字宫格</option>
                      <option value="top">上整下分</option>
                      <option value="sidebar">左整右分</option>
                    </select>
                  </Field>
                  {view === "services" && (
                    <Field
                      label="主题"
                      info={<ul><li><strong>预览与应用。</strong>选择后立即更新卡片排列、模块样式和预览。保存后应用。</li><li><strong>保留项目。</strong>已选卡片、自定义顺序、接口选择和外观配色。手动修改后自动识别为对应主题或自定义。</li><li><strong>随机主题。</strong>可抽取内置或自定义主题，也可混搭四张卡片的样式。</li></ul>}
                    >
                      <ServiceThemePicker
                        presentation={p}
                        onChange={(values) => {
                          const next = { ...themedAppearance(values) };
                          delete next.__appearance_resolved;
                          setDraft({ ...draft, ...next });
                        }}
                      />
                    </Field>
                  )}
                  {view === "services" && (
                    <Field
                      label="保存自定义主题"
                      info={<ul><li><strong>保存内容。</strong>记录当前服务总览的布局、指标顺序、接口选择、字号和外观。</li><li><strong>保存方式。</strong>填写名称，再点击下方「保存并应用」。</li><li><strong>同名主题。</strong>使用相同名称时更新已有主题。</li></ul>}
                    >
                      <div className="custom-theme-save">
                        <input
                          aria-label="自定义主题名称"
                          value={themeName}
                          maxLength={40}
                          placeholder="主题名称"
                          onChange={(e) => setThemeName(e.target.value)}
                        />
                        <Button
                          disabled={
                            !themeName.trim() ||
                            (listThemes(p).length >= 20 &&
                              !listThemes(p).some(
                                (t) => t.name === themeName.trim(),
                              ))
                          }
                          onClick={() => {
                            const existing = listThemes(p).find(
                              (t) => t.name === themeName.trim(),
                            );
                            const theme = {
                              id:
                                existing?.id || "custom-" + crypto.randomUUID(),
                              name: themeName.trim(),
                              values: serviceThemeSnapshot(p),
                            };
                            set(
                              "service_custom_themes",
                              existing
                                ? listThemes(p).map((t) =>
                                    t.id === existing.id ? theme : t,
                                  )
                                : [...listThemes(p), theme],
                            );
                            setThemeName("");
                          }}
                        >
                          保存
                        </Button>
                        <Button
                          disabled={!customServiceTheme(p)}
                          onClick={() =>
                            set(
                              "service_custom_themes",
                              listThemes(p).filter(
                                (t) => t.id !== customServiceTheme(p)?.id,
                              ),
                            )
                          }
                        >
                          删除
                        </Button>
                      </div>
                    </Field>
                  )}
                  {view === "dashboard" && (
                    <Field
                      label="预定义布局"
                      info="恢复当前出口四卡片组合；下方可自定义卡片，最多显示4张。显示ICMP卡片不会开启检测。"
                    >
                      <select
                        aria-label="主页预定义布局"
                        value={
                          JSON.stringify(cards) === JSON.stringify(EXIT_CARDS)
                            ? "preset"
                            : "custom"
                        }
                        onChange={() => setCards(EXIT_CARDS)}
                      >
                        <option value="preset">当前出口（四卡片）</option>
                        <option value="custom" disabled>
                          自定义
                        </option>
                      </select>
                    </Field>
                  )}
                </div>
                {view === "services" && (
                  <div className="form-grid service-independent-styles">
                    {[
                      ["host", "服务器"],
                      ["xray", "Xray"],
                      ["warp", "WARP"],
                      ["wireguard", "WireGuard"],
                    ].map(([module, title]) => {
                      const standalone = module === "wireguard" && splitWireGuardCards(p);
                      const aggregate = module === "wireguard" && !standalone;
                      const style = serviceStyle(p, module),
                        circle =
                          aggregate &&
                          SERVICE_CIRCLE_STYLES.includes(style);
                      return (
                        <Field
                          key={module}
                          label={title + (standalone ? "接口" : "") + "卡片样式"}
                          info={standalone ? "独立接口卡片在所有总览布局中均可使用完整样式。" : "每张卡片独立保存样式，可自由组合。圆形按可用宽高自动缩放并调整间距和位置，空间不足时统一缩小字号，保持单行完整。小圆环的圆心名称会移到上方。无适用样式时，矩形回退为对称矩形、圆形回退为四圆横排。指标和接口子卡片可在主页编辑模式拖动交换位置。竖排只用于左右布局左侧的大卡片，右侧保持横排。"}
                        >
                          <select
                            aria-label={title + (standalone ? "接口" : "") + "卡片样式"}
                            value={circle ? wireguardNameStyle(p) : style}
                            onChange={(e) => {
                              const value = e.target.value;
                              if (
                                module === "wireguard" &&
                                [
                                  "center",
                                  "classic",
                                  "corner",
                                  "corner-left",
                                  "above",
                                ].includes(value)
                              )
                                setDraft({
                                  ...draft,
                                  services_wireguard_layout: "radial",
                                  services_wireguard_style: value,
                                });
                              else set(SERVICE_STYLE_FIELDS[module], value);
                            }}
                          >
                            <option value="metrics">
                              {aggregate ? "矩形" : "对称矩形"}
                            </option>
                            {aggregate ? (
                              <>
                                <option value="above">圆形</option>
                              </>
                            ) : (
                              <>
                                <option value="radial">四圆横排</option>
                                {serviceStyleAvailable(
                                  p,
                                  module,
                                  "radial-square",
                                ) && (
                                  <option value="radial-square">
                                    四圆方形
                                  </option>
                                )}
                                {serviceStyleAvailable(p, module, "radial-vertical") && (
                                    <option value="radial-vertical">
                                      四圆竖排
                                    </option>
                                  )}
                                {serviceStyleAvailable(
                                  p,
                                  module,
                                  "radial-diamond",
                                ) && (
                                  <option value="radial-diamond">
                                    四圆菱形
                                  </option>
                                )}
                                {serviceStyleAvailable(
                                  p,
                                  module,
                                  "radial-wave",
                                ) && (
                                  <option value="radial-wave">四圆波浪</option>
                                )}
                                {serviceStyleAvailable(p, module, "radial-lightning") && (
                                  <option value="radial-lightning">四圆闪电</option>
                                )}
                                {serviceStyleAvailable(p, module, "radial-reverse-lightning") && (
                                  <option value="radial-reverse-lightning">四圆电闪</option>
                                )}
                                {serviceStyleAvailable(
                                  p,
                                  module,
                                  "radial-mixed",
                                ) && (
                                  <option value="radial-mixed">四圆混合</option>
                                )}
                                {serviceStyleAvailable(
                                  p,
                                  module,
                                  "radial-diamond-compact",
                                ) && (
                                  <option value="radial-diamond-compact">
                                    四圆心形
                                  </option>
                                )}
                              </>
                            )}
                          </select>
                        </Field>
                      );
                    })}
                  </div>
                )}
              </>
            )}
          </OverviewSettingsCard>
          <OverviewSurfaceSettings
            presentation={p}
            onChange={(value) => set("overview_surfaces", value)}
            onTextChange={(value) => set("overview_text", value)}
            actions={actions(
              ["overview_surfaces", "overview_text"],
              "卡片外观操作",
            )}
          />
          {view === "services" && (
            <OverviewSettingsCard
              title="WireGuard 卡片"
              info={<ul><li><strong>自动选择。</strong>未指定接口时，按列表顺序显示所选数量的接口。</li><li><strong>指定接口。</strong>指定后使用已保存的名称和顺序；只影响主页展示，不启停接口。</li><li><strong>恢复自动。</strong>在「显示接口 1」选择「自动选择所选数量的接口」。</li></ul>}
              actions={actions(
                [
                  "services_wireguard_axis",
                  "services_wireguard_diameter",
                  "services_wireguard_count",
                  "services_wireguard_interfaces",
                  "services_wireguard_ring_color",
                  "services_wireguard_ring_opacity",
                  "services_wireguard_ring_shadow",
                  "services_wireguard_label_size",
                  "services_wireguard_value_size",
                ],
                "WireGuard卡片操作",
              )}
            >
              <div className="form-grid">
                <Field label="显示接口数量" info={<ul><li><strong>显示数量。</strong>默认 2 个接口，可选择 1～4 个并指定名称。</li><li><strong>状态含义。</strong>主页已连接按本地接口运行状态判断，不等于每个 Peer 都可达。</li><li><strong>暂停监控。</strong>关闭模块监控后数值暂停，接口状态仍单独展示。</li></ul>}>
                  <select
                    aria-label="WireGuard显示接口数量"
                    value={Number(p.services_wireguard_count || 2)}
                    onChange={(e) =>
                      setDraft({
                        ...draft,
                        services_wireguard_count: Number(e.target.value),
                        services_wireguard_interfaces: (
                          (p.services_wireguard_interfaces as string[]) || []
                        ).slice(0, Number(e.target.value)),
                      })
                    }
                  >
                    {[1, 2, 3, 4].map((n) => (
                      <option key={n} value={n}>
                        {n}
                      </option>
                    ))}
                  </select>
                </Field>
              </div>
              <div className="form-grid">
                {Array.from(
                  { length: Number(p.services_wireguard_count || 2) },
                  (_, i) => i,
                ).map((index) => {
                  const names = list(wireguard.data?.items).map((row) =>
                      String(row.name),
                    ),
                    saved = p.services_wireguard_interfaces as
                      | string[]
                      | undefined,
                    chosen = [
                      ...(saved || []),
                      ...names.filter((n) => !(saved || []).includes(n)),
                    ].slice(0, Number(p.services_wireguard_count || 2)),
                    value = chosen[index] || "";
                  return (
                    <Field
                      key={index}
                      label={`显示接口 ${index + 1}`}
                      info={<ul><li><strong>接口列表。</strong>提供全部接口供选择。选择已分配的接口时，交换两个位置。</li><li><strong>排列顺序。</strong>接口 1～4 对应指标卡片从前到后的顺序。</li><li><strong>影响范围。</strong>只调整主页内容，不启停接口。</li></ul>}
                    >
                      <select
                        aria-label={`WireGuard主页接口${index + 1}`}
                        value={value}
                        disabled={!wireguard.data}
                        onChange={(event) => {
                          const next = [...chosen];
                          const selected = event.target.value;
                          const assigned = chosen.indexOf(selected);
                          if (selected && assigned >= 0 && assigned !== index)
                            next[assigned] = chosen[index] || "";
                          next[index] = selected;
                          set(
                            "services_wireguard_interfaces",
                            index === 0 && !event.target.value
                              ? []
                              : next.filter(Boolean),
                          );
                        }}
                      >
                        <option value="" disabled={index > 0}>
                          {index ? "暂无可用接口" : "自动选择接口"}
                        </option>
                        {[...new Set([...names, ...chosen])].map((name) => (
                          <option key={name} value={name}>
                            {name}
                            {names.includes(name) ? "" : "（接口已移除）"}
                          </option>
                        ))}
                      </select>
                    </Field>
                  );
                })}
              </div>
              <div className="form-grid">
                <Field label="圆形排列">
                  <select
                    aria-label="WireGuard圆形排列"
                    value={String(p.services_wireguard_axis || "auto")}
                    onChange={(e) =>
                      set("services_wireguard_axis", e.target.value)
                    }
                  >
                    <option value="auto">自动适配</option>
                    <option value="row">横排</option>
                    <option value="column">竖排</option>
                  </select>
                </Field>
                <Field
                  label="圆形直径（px）"
                  error={
                    !Number.isInteger(
                      Number(p.services_wireguard_diameter || 0),
                    ) ||
                    (Number(p.services_wireguard_diameter || 0) !== 0 &&
                      (Number(p.services_wireguard_diameter) < 40 ||
                        Number(p.services_wireguard_diameter) > 1200))
                      ? "须为0或40～1200的整数"
                      : ""
                  }
                  info={<ul><li><strong>自动适配。</strong>填写 0 时按可用空间计算，左侧大卡片默认圆径不超过 280px。</li><li><strong>指定尺寸。</strong>支持 40～1200px；空间不足时自动缩小。</li><li><strong>尺寸含义。</strong>圆径只计算圆形本身，不包含名称。</li></ul>}
                >
                  <CircleDiameterInput
                    value={Number(p.services_wireguard_diameter || 0)}
                    onChange={(value) =>
                      set("services_wireguard_diameter", value)
                    }
                  />
                </Field>
              </div>
              <div className="form-grid">
                <Field label="指标名称字号（px）" validateNumber>
                  <NumberInput
                    aria-label="WireGuard指标名称字号"
                    min={8}
                    max={24}
                    value={Number(p.services_wireguard_label_size || 14)}
                    onChange={(e) =>
                      set(
                        "services_wireguard_label_size",
                        Number(e.target.value),
                      )
                    }
                  />
                </Field>
                <Field label="指标数值字号（px）" validateNumber>
                  <NumberInput
                    aria-label="WireGuard指标数值字号"
                    min={12}
                    max={40}
                    value={Number(p.services_wireguard_value_size || 24)}
                    onChange={(e) =>
                      set(
                        "services_wireguard_value_size",
                        Number(e.target.value),
                      )
                    }
                  />
                </Field>
              </div>
              <div className="form-grid">
                <Field
                  label="圆环颜色"
                  info="默认使用主题中性色；只调整 WireGuard 接口圆环，不改变状态和指标配色。"
                >
                  <ColorInput
                    aria-label="WireGuard圆环颜色"
                    value={String(p.services_wireguard_ring_color || "#3b4451")}
                    onChange={(color) =>
                      set("services_wireguard_ring_color", color)
                    }
                  />
                </Field>
                <Field label="透明度（%）" validateNumber>
                  <NumberInput
                    aria-label="WireGuard圆环不透明度"
                    min={0}
                    max={100}
                    value={
                      100 - Number(p.services_wireguard_ring_opacity ?? 100)
                    }
                    onChange={(e) =>
                      set(
                        "services_wireguard_ring_opacity",
                        100 - Number(e.target.value),
                      )
                    }
                  />
                </Field>
                <Field label="阴影" validateNumber>
                  <NumberInput
                    aria-label="WireGuard圆环阴影"
                    min={0}
                    max={40}
                    value={Number(p.services_wireguard_ring_shadow || 0)}
                    onChange={(e) =>
                      set(
                        "services_wireguard_ring_shadow",
                        Number(e.target.value),
                      )
                    }
                  />
                </Field>
              </div>

              <ErrorLine error={wireguard.error} />
            </OverviewSettingsCard>
          )}
          {view === "dashboard" && (
            <OverviewSettingsCard
              title="默认时间范围"
              actions={actions(
                [
                  "overview_independent",
                  "overview_range_value",
                  "overview_range_unit",
                ],
                "默认时间范围操作",
              )}
            >
              <label className="check">
                <input
                  type="checkbox"
                  checked={!!p.overview_independent}
                  onChange={(e) =>
                    set("overview_independent", e.target.checked)
                  }
                />
                主页使用独立默认时间范围
              </label>
              <div className="form-grid">
                <Field label="默认时间范围">
                  <NumberInput
                    aria-label="主页默认时间范围"
                    min={1}
                    max={525600}
                    disabled={!p.overview_independent}
                    value={Number(
                      p.overview_independent
                        ? p.overview_range_value
                        : p.range_value,
                    )}
                    onChange={(e) =>
                      set("overview_range_value", Number(e.target.value))
                    }
                  />
                </Field>
                <Field label="单位">
                  <RangeUnit
                    disabled={!p.overview_independent}
                    value={String(
                      p.overview_independent
                        ? p.overview_range_unit
                        : p.range_unit,
                    )}
                    onChange={(value) => set("overview_range_unit", value)}
                  />
                </Field>
              </div>
            </OverviewSettingsCard>
          )}
        </div>
      </div>
      <div className="save-all-footer overview-sync-footer">
        <div className="overview-sync-copy">
          <h2 className="metric-heading tone-green">
            <SlidersHorizontal size={18} />
            配置与同步
            <SettingInfo
              label="配置与同步"
              text={<>在此统一保存本页配置；恢复默认与放弃保存仅修改草稿。</>}
            />
          </h2>
        </div>
        <ErrorLine error={r.error || sectionError} />
        <SaveCard
          dirty={hasChanges(draft, {
            ...defaults,
            ...themedAppearance(state?.manager_settings.presentation),
            ...themedAppearance(r.data),
          })}
          duplicateTarget={topSaveTarget}
          localActionsEnabled={!!r.data}
          title="配置与同步"
          defaultsLabel="全部恢复默认"
          saveLabel="全部保存并应用"
          disabled={!r.data || !!surfaceError || sectionBusy}
          defaults={() =>
            setDraft({
              ...draft,
              overview_show_clock: false,
              overview_show_data_status: false,
              overview_surfaces: {},
              overview_text: {},
              ...(configureDefault
                ? { default_overview_view: "services" }
                : {}),
              ...(view === "services"
                ? {
                    services_style: "metrics",
                    services_wireguard_interfaces: [],
                    services_host_order: ["cpu", "load", "memory", "root"],
                    services_wireguard_style: "center",
                    services_wireguard_ring_color: "",
                    services_wireguard_ring_opacity: 100,
                    services_wireguard_ring_shadow: 0,
                    services_wireguard_label_size: 14,
                    services_wireguard_count: 2,
                    services_wireguard_axis: "auto",
                    services_wireguard_diameter: 0,
                    services_wireguard_value_size: 24,
                    ...DEFAULT_SERVICE_THEME,
                    services_xray_order: SERVICE_METRICS,
                    services_warp_order: SERVICE_METRICS,
                  }
                : {}),
              [prefix + "layout"]:
                view === "services"
                  ? DEFAULT_SERVICE_THEME.services_layout
                  : "grid",
              [prefix + (view === "nodes" ? "order" : "cards")]:
                view === "services"
                  ? SERVICE_CARDS
                  : view === "nodes"
                    ? []
                    : EXIT_CARDS,
              ...(view === "nodes"
                ? {
                    nodes_limit: 0,
                    nodes_visibility_mode: "all",
                    nodes_visible_ids: [],
                  }
                : {}),
            })
          }
          discard={() => setDraft({})}
          save={async () => {
            setSectionBusy(true);
            try {
              const submitted = draft;
              if (
                Object.keys(submitted).length &&
                !(await act("/api/monitoring/presentation", submitted))
              )
                return false;
              await r.refresh();
              setDraft((current) =>
                Object.fromEntries(
                  Object.entries(current).filter(
                    ([key, value]) =>
                      !(key in submitted) ||
                      JSON.stringify(value) !== JSON.stringify(submitted[key]),
                  ),
                ),
              );
              return true;
            } finally {
              setSectionBusy(false);
            }
          }}
        />
      </div>
    </>
  );
}

function OverviewPreview({
  state,
  presentation,
  view,
  onChange,
  readonly = false,
}: {
  readonly?: boolean;
  state: State;
  presentation: Dict;
  view: OverviewView;
  onChange: (key: string, value: string[]) => void;
}) {
  const frame = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 1200, height: 680, scale: 0.6 });
  const policy = overviewPolicy(presentation, view);
  const root = useOverviewDrag(
    !readonly && view !== "nodes",
    policy.layout,
    (scope, from, to, ids) => {
      const key = ["host", "xray", "warp"].includes(scope)
        ? "services_" + scope + "_order"
        : scope === "wireguard"
          ? "services_wireguard_interfaces"
          : scope === "nodes"
            ? "nodes_order"
            : (view === "services" ? "services_" : "overview_") + "cards";
      const base = presentation[key] as string[] | undefined;
      const order = [...new Set([...(base || []), ...ids])],
        a = order.indexOf(from),
        b = order.indexOf(to);
      if (a < 0 || b < 0) return;
      [order[a], order[b]] = [order[b], order[a]];
      onChange(key, order);
    },
  );
  useLayoutEffect(() => {
    let pending = 0,
      observed: HTMLElement | null = null;
    const contentObserver = new ResizeObserver(() => schedule());
    const measure = () => {
      pending = 0;
      if (!frame.current) return;
      const actual = [
        ...document.querySelectorAll<HTMLElement>(
          "main>.overview-page>.overview-home>:is(.dashboard-current,.dashboard-nodes)",
        ),
      ].find((el) => !el.closest("dialog"));
      const box = actual?.getBoundingClientRect();
      const width =
        (readonly ? frame.current.clientWidth : box?.width) ||
        (document.body.clientWidth *
          Number(presentation.content_width_percent || 99)) /
          100;
      const nodes =
        root.current?.querySelector<HTMLElement>(
          view === "nodes" ? ".dashboard-nodes" : ".dashboard-current",
        ) || null;
      if (nodes !== observed) {
        contentObserver.disconnect();
        if (nodes) contentObserver.observe(nodes);
        observed = nodes;
      }
      const header =
        Number.parseFloat(
          getComputedStyle(document.documentElement).getPropertyValue(
            "--header-height",
          ),
        ) || 120;
      const contentHeight =
        nodes && (view === "nodes" || document.body.clientWidth < 1000)
          ? Math.max(nodes.offsetHeight, nodes.scrollHeight)
          : actual?.classList.contains("dashboard-current")
            ? box!.height
            : Math.max(300, innerHeight - header - (readonly ? 47 : 39));
      const height =
        contentHeight +
        (root.current?.querySelector<HTMLElement>(".overview-footer")
          ?.offsetHeight || 0) +
        (presentation.overview_show_clock ||
        presentation.overview_show_data_status
          ? 6
          : 0);
      const scale = Math.min(1, frame.current.clientWidth / width);
      setSize((old) =>
        Math.abs(old.width - width) < 0.5 &&
        Math.abs(old.height - height) < 0.5 &&
        Math.abs(old.scale - scale) < 0.0001
          ? old
          : { width, height, scale },
      );
    };
    const schedule = () => {
      if (!pending) pending = requestAnimationFrame(measure);
    };
    const observer = new ResizeObserver(schedule);
    if (frame.current) observer.observe(frame.current);
    const changes = new MutationObserver(schedule);
    if (root.current)
      changes.observe(root.current, { childList: true, subtree: true });
    measure();
    window.addEventListener("resize", schedule);
    return () => {
      cancelAnimationFrame(pending);
      observer.disconnect();
      contentObserver.disconnect();
      changes.disconnect();
      window.removeEventListener("resize", schedule);
    };
  }, [presentation.content_width_percent, view, readonly]);
  return (
    <div
      ref={frame}
      className="overview-preview-frame"
      style={{ height: size.height * size.scale }}
    >
      <div
        ref={root}
        className={
          "overview-preview-canvas" + (readonly ? "" : " overview-editing")
        }
        data-preview-scale={size.scale}
        style={{
          width: size.width,
          height: size.height,
          left: "50%",
          transform: `translateX(-50%) scale(${size.scale})`,
          transformOrigin: "top center",
        }}
      >
        <div
          className="overview-page"
          style={overviewSurfaceVariables(presentation)}
        >
          <Home
            state={{
              ...state,
              manager_settings: { ...state.manager_settings, presentation },
            }}
            page={view}
            preview
            navigation={<OverviewViewPicker value={view} onChange={() => {}} />}
          />
        </div>
        <OverviewFooter presentation={presentation} preview />
      </div>
    </div>
  );
}

function CircleDiameterInput({
  value,
  onChange,
  "aria-invalid": invalid,
  "aria-describedby": describedBy,
}: {
  value: number;
  onChange: (value: number) => void;
  "aria-invalid"?: boolean;
  "aria-describedby"?: string;
}) {
  const ref = useRef<HTMLInputElement>(null),
    latest = useRef({ value, onChange }),
    [text, setText] = useState(String(value));
  latest.current = { value, onChange };
  useEffect(() => setText(String(value)), [value]);
  const commit = (next: number) => {
    latest.current.value = next;
    setText(String(next));
    latest.current.onChange(next);
  };
  const step = (up: boolean) => {
    const n = latest.current.value;
    commit(up ? (n === 0 ? 40 : Math.min(1200, n + 1)) : n <= 40 ? 0 : n - 1);
  };
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const wheel = (e: WheelEvent) => {
      if (document.activeElement !== el || !e.deltaY) return;
      e.preventDefault();
      e.stopPropagation();
      step(e.deltaY < 0);
    };
    el.addEventListener("wheel", wheel, { passive: false, capture: true });
    return () => el.removeEventListener("wheel", wheel, true);
  }, []);
  return (
    <input
      ref={ref}
      aria-invalid={invalid}
      aria-describedby={describedBy}
      type="number"
      aria-label="WireGuard圆形直径"
      min={0}
      max={1200}
      step={1}
      value={text}
      onFocus={(e) => {
        if (latest.current.value === 0) e.currentTarget.select();
      }}
      onChange={(e) => {
        const raw = e.target.value;
        setText(raw);
        const n = raw === "" ? 0 : Number(raw);
        if (Number.isFinite(n)) {
          latest.current.value = n;
          latest.current.onChange(n);
        }
      }}
      onBlur={() => setText(String(latest.current.value))}
      onKeyDown={(e) => {
        if (e.key === "ArrowUp" || e.key === "ArrowDown") {
          e.preventDefault();
          e.stopPropagation();
          step(e.key === "ArrowUp");
        } else if (/^\d$/.test(e.key) && latest.current.value === 0) {
          e.preventDefault();
          commit(Number(e.key));
        }
      }}
    />
  );
}
