import { SettingsIcon } from "./requirement_icons";
import { ThemedSelects } from "./themed_selects";
import { useOverviewRandomTheme } from "./overview_random_theme";
import { HomeTools } from "./home_tools";
import { HomeHeaderContext } from "./home_header";
import { OverviewFooter } from "./overview_footer";
import { labelIcon } from "./ui_icons";
import type { OverviewView } from "./overview_policy";
import { useCompactSelects } from "./compact_selects";
import { Tasks, type OperationNotice } from "./task_center";
import { TrafficWindowContext } from "./traffic_window";
import { RangeDefaultsContext } from "./time_range";
import { rangeHours } from "./presentation_settings";
import { UpdatesContext, UpdateBadge, versionLabel } from "./version_panel";
import { WireGuardHub } from "./wg_monitor";
import { MonitoringHub, XrayHub, LogsHub, GlobalSettingsHub } from "./hubs";
import { OverviewPage } from "./host";
import { TrafficPage } from "./traffic";
import { NetworkHub } from "./network_hub";
import { useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Moon,
  Sun,
  LoaderCircle,
} from "lucide-react";
import {
  api,
  errorMessage,
  record,
  type Page,
  type State,
  type Dict,
} from "./api";
import {
  Button,
  ChartRefreshContext,
  LiveRefreshContext,
  DataFreshness,
  ErrorLine,
  PageHeader,
  Panel,
  useResource,
} from "./ui";
import { Nodes, Subscriptions, type Act } from "./nodes";
import { Diagnostics, History, Operations, Switches } from "./views";
import { Komari, WarpPage } from "./warp";
import { NetworkPending } from "./network";
import { FirewallPage } from "./firewall";
import "./style.css";
import "./button_system.css";
import "./toolbar_layout.css";
import "./menu_colors.css";
import "./warp_detection_control.css";
import "./popup_system.css";
import "./preview_refinements.css";
import "./network_interaction.css";
import "./select_menus.css";
import "./release_098.css";
const navigation: [Page, string][] = [
  ["overview", "主页"],
  ["monitoring", "监控"],
  ["xray", "Xray"],
  ["warp", "WARP"],
  ["wireguard", "WireGuard"],
  ["network", "网络"],
  ["firewall", "防火墙"],
  ["diagnostics", "诊断"],
  ["logs", "日志"],
  ["monitor", "Komari"],
];
const pageHeadings: Partial<Record<Page, [string, string]>> = {
  overview: ["主页", "当前出口状态、实时流量与延迟一览"],
  monitoring: ["监控", "服务器资源、出口真实流量与节点质量统一查看"],
  xray: ["Xray", "代理模式、核心服务、节点与订阅集中管理"],
  warp: ["WARP", "隧道、候选端点与设置统一管理"],
  wireguard: ["WireGuard", "管理多个 VPN 接口、服务与 Peer"],
  network: ["网络", "接口、静态路由与策略路由管理"],
  firewall: ["防火墙", "主机防火墙、访问规则与端口转发管理"],
  logs: ["日志", "操作、检测与出口切换日志统一查看"],
  diagnostics: ["诊断", "检查目标转发路径与连接，查看历史诊断"],
  monitor: ["Komari", "嵌入式服务器监控与状态查看"],
  settings: ["设置", "应用设置、备份、服务与版本维护"],
};
function App() {
  useCompactSelects();
  useEffect(() => {
    const handler = (event: Event) => {
      if ((event as CustomEvent).detail === "wireguard") {
        setPage("wireguard");
        localStorage.setItem("foxray.page", "wireguard");
        window.scrollTo({ top: 0 });
      }
    };
    window.addEventListener("foxray:navigate", handler);
    return () => window.removeEventListener("foxray:navigate", handler);
  }, []);
  const [page, setPage] = useState<Page>(() => {
      let saved = localStorage.getItem("foxray.page");
      if (saved === "nodes" || saved === "subscriptions") {
        localStorage.setItem("foxray.xray.tab", saved);
        saved = "xray";
      }
      if (saved === "history" || saved === "traffic") {
        localStorage.setItem(
          "foxray.monitor.tab",
          saved === "history" ? "quality" : "traffic",
        );
        saved = "monitoring";
      }
      if (saved === "switches" || saved === "events") {
        localStorage.setItem("foxray.logs.tab", saved);
        saved = "logs";
      }
      if (["routes", "rules"].includes(saved || "")) {
        localStorage.setItem("foxray.network.tab", saved!);
        saved = "network";
      }
      if (saved) localStorage.setItem("foxray.page", saved);
      return saved === "settings" || navigation.some(([id]) => id === saved)
        ? (saved as Page)
        : "overview";
    }),
    [homeKey, setHomeKey] = useState(0),
    [overviewEditor, setOverviewEditor] = useState<OverviewView | undefined>(),
    [theme, setTheme] = useState(() =>
      localStorage.getItem("vpm.theme") === "light" ? "light" : "dark",
    ),
    [notice, setNotice] = useState<OperationNotice | null>(null),
    [statusRefresh, setStatusRefresh] = useState(5000),
    [taskWarning, setTaskWarning] = useState("");
  const {
    data: state,
    error,
    refresh,
  } = useResource<State>("/api/state", statusRefresh);
  useEffect(() => {
    setStatusRefresh(
      Math.max(1, Number(state?.manager_settings.status_refresh_seconds || 5)) *
        1000,
    );
  }, [state?.manager_settings.status_refresh_seconds]);
  const [monitorHeaderHidden, setMonitorHeaderHidden] = useState(() => sessionStorage.getItem("foxray.monitor.header-hidden") === "true");
  const hideHomeHeader =
    (page === "overview" || page === "monitoring" && monitorHeaderHidden) &&
    record(state?.manager_settings.presentation).home_header_auto_hide === true;
  const [headerExpanded, setHeaderExpanded] = useState(false);
  const [headerOrigin, setHeaderOrigin] = useState("calc(100vw - 30px) 150px");
  const headerCollapsed = hideHomeHeader && !headerExpanded;
  useEffect(() => { setHeaderExpanded(false); }, [page, hideHomeHeader]);
  const updates = useResource<Dict>("/api/updates", 60000);
  const [updateData, setUpdateData] = useState<Dict>({});
  useEffect(() => {
    if (updates.data) setUpdateData(updates.data);
  }, [updates.data]);
  const header = useRef<HTMLElement>(null),
    noticeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("vpm.theme", theme);
  }, [theme]);
  useEffect(() => {
    const element = header.current;
    if (!element) return;
    const update = () => {
      document.documentElement.style.setProperty(
        "--layout-width",
        document.body.clientWidth + "px",
      );
      document.documentElement.style.setProperty(
        "--header-edge",
        (headerCollapsed ? 8 : element.offsetHeight) + "px",
      );
      document.documentElement.style.setProperty(
        "--header-height",
        (headerCollapsed ? 0 : element.offsetHeight) + "px",
      );
    };
    const observer = new ResizeObserver(update);
    observer.observe(element);
    update();
    window.addEventListener("resize", update);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [headerCollapsed]);
  const navigate = (next: Page, keepHeaderHidden = false) => {
    setMonitorHeaderHidden(keepHeaderHidden);
    sessionStorage.setItem("foxray.monitor.header-hidden", String(keepHeaderHidden));
    setOverviewEditor(undefined);
    setPage(next);
    localStorage.setItem("foxray.page", next);
    window.scrollTo({ top: 0 });
  };
  useEffect(()=>{
    const openMonitor=(event:Event)=>{
      const module=String((event as CustomEvent).detail?.module || "");
      if(!["host","xray","warp","wireguard"].includes(module))return;
      localStorage.setItem("foxray.monitor.module.v322",module);
      navigate("monitoring",hideHomeHeader);
    };
    window.addEventListener("foxray:open-monitor",openMonitor);
    return ()=>window.removeEventListener("foxray:open-monitor",openMonitor);
  },[hideHomeHeader]);
  const randomRequest = useRef<unknown>(null),
    cancelRandom = useRef<() => Promise<void>>(() => Promise.resolve());
  const act: Act = useCallback(
    async (path, data, receive) => {
      const route = path.split("?")[0];
      const operation = route.includes("diagnostic")
        ? "诊断任务"
        : route.includes("probe") && !route.includes("interval")
          ? "节点检测"
          : /policy|settings|config|monitoring/.test(route)
            ? "保存设置"
            : route.includes("subscription")
              ? "订阅操作"
              : route.includes("updates")
                ? "版本检查或更新"
                : "本次操作";
      if (noticeTimer.current) clearTimeout(noticeTimer.current);
      setNotice({ kind: "info", title: "正在执行", operation, message: "" });
      try {
        if (
          route === "/api/monitoring/presentation" &&
          data !== randomRequest.current &&
          Object.keys(record(data)).some(
            (key) =>
              key.startsWith("services_") ||
              [
                "overview_text",
                "overview_surfaces",
                "overview_color_schemes",
              ].includes(key),
          )
        )
          await cancelRandom.current();
        const result = await api<Dict>(path, data);
        if (result.ok === false || result.error)
          throw new Error(String(result.error || "操作未完成"));
        receive?.(result);
        const messages: Record<string, string> = {
          "/api/monitoring/probe-intervals":
            "检测间隔已保存，检测开关保持原状态。",
          "/api/monitoring/modules":
            "监控总开关已保存并生效，单项设置与主动探测策略保持不变。",
          "/api/monitoring/display": "显示设置已保存并生效。",
          "/api/monitoring/presentation": "页面时间范围与卡片布局已保存。",
          "/api/traffic/history-policy":
            "流量历史记录间隔已保存，将用于后续记录。",
          "/api/config": "Xray 配置已保存，评分与检测设置已生效。",
          "/api/settings": "应用设置已保存并生效。",
          "/api/traffic/policy": "流量采集设置已保存并生效。",
          "/api/network/confirm": "网络变更已确认并保存。",
        };
        setNotice(
          result.id
            ? {
                kind: "info",
                operation,
                title: "任务已提交",
                message: "执行结果将在任务卡片中更新。",
                taskId: String(result.id),
              }
            : {
                kind: "success",
                operation,
                title: "操作成功",
                message:
                  typeof result.message === "string"
                    ? result.message
                    : messages[path.split("?")[0]] || "操作已完成。",
              },
        );
        if (noticeTimer.current) clearTimeout(noticeTimer.current);
        noticeTimer.current = setTimeout(() => setNotice(null), 10000);
        void refresh();
        return true;
      } catch (e) {
        setNotice({
          kind: "error",
          operation,
          title: "操作失败",
          message: errorMessage(e),
        });
        if (noticeTimer.current) clearTimeout(noticeTimer.current);
        noticeTimer.current = setTimeout(() => setNotice(null), 10000);
        return false;
      }
    },
    [refresh],
  );
  const randomTheme = useOverviewRandomTheme(
    state?.manager_settings.presentation,
    async (patch) => {
      randomRequest.current = patch;
      try {
        return await act("/api/monitoring/presentation", patch);
      } finally {
        if (randomRequest.current === patch) randomRequest.current = null;
      }
    },
    state?.read_only === true,
  );
  cancelRandom.current = randomTheme.cancel;
  const available = navigation.filter(
    ([id]) =>
      (id !== "xray" || state?.components.xray) &&
      (id !== "wireguard" || state?.components.wireguard) &&
      (id !== "warp" || state?.components.warp) &&
      (id !== "monitor" || state?.components.komari),
  );
  useEffect(() => {
    if (state && !available.some(([id]) => id === page) && page !== "settings")
      setPage("overview");
  }, [state?.components, page]);
  let content: React.ReactNode = null;
  if (state) {
    switch (page) {
      case "monitoring":
        content = (
          <MonitoringHub
            state={state}
            act={act}
            initialOverviewView={overviewEditor}
          />
        );
        break;
      case "logs":
        content = <LogsHub state={state} act={act} />;
        break;
      case "overview":
        content = (
          <OverviewPage
            key={homeKey}
            randomTheme={randomTheme}
            state={state}
            act={act}
            onMonitoring={() => navigate("monitoring", hideHomeHeader)}
            onLayout={(view) => {
              navigate("monitoring");
              setOverviewEditor(view);
            }}
          />
        );
        break;
      case "nodes":
        content = <Nodes state={state} act={act} refresh={refresh} />;
        break;
      case "subscriptions":
        content = <Subscriptions state={state} act={act} refresh={refresh} />;
        break;
      case "history":
        content = <History state={state} act={act} />;
        break;
      case "switches":
        content = <Switches />;
        break;
      case "events":
        content = <Operations />;
        break;
      case "xray":
        content = <XrayHub state={state} act={act} refresh={refresh} />;
        break;
      case "warp":
        content = state.components.warp ? (
          <WarpPage act={act} />
        ) : (
          <Panel>WARP 组件未部署。</Panel>
        );
        break;
      case "wireguard":
        content = <WireGuardHub act={act} />;
        break;
      case "network":
      case "routes":
      case "rules":
        content = <NetworkHub act={act} />;
        break;
      case "traffic":
        content = <TrafficPage state={state} act={act} />;
        break;
      case "firewall":
        content = <FirewallPage act={act} />;
        break;
      case "diagnostics":
        content = <Diagnostics act={act} />;
        break;
      case "monitor":
        content = state.components.komari ? (
          <Komari url={state.components.komari_url || ""} />
        ) : null;
        break;
      case "settings":
        content = <GlobalSettingsHub state={state} act={act} />;
        break;
    }
  }
  const managerWarning = [
    state?.state.recovery_issue,
    taskWarning,
    notice?.kind === "error" ? notice.message || notice.title : "",
  ]
    .filter(Boolean)
    .join("；");
  const managerStatus = error
    ? "offline"
    : !state
      ? "connecting"
      : managerWarning
        ? "warning"
        : "normal";
  const managerStatusText = error
    ? "管理服务失联，正在重试"
    : !state
      ? "正在连接管理服务"
      : managerWarning
        ? `管理服务在线，有告警：${managerWarning}`
        : "管理服务正常";
  return (
    <HomeHeaderContext.Provider value={{hidden: hideHomeHeader, expanded: headerExpanded, toggle: button => {
      const box = button.getBoundingClientRect();
      setHeaderOrigin(`${box.x + box.width / 2}px ${box.y + box.height / 2}px`);
      setHeaderExpanded(value => !value);
    }}}>
    <UpdatesContext.Provider
      value={{ data: updateData, receive: setUpdateData }}
    >
      {import.meta.env.VITE_PREVIEW_LABEL && (
        <div className="preview-banner" role="status">
          预览版 · {import.meta.env.VITE_PREVIEW_LABEL} ·
          独立预览环境，未发布到正式服务
        </div>
      )}
      <div
        className="home-header-shell"
        data-auto-hide={hideHomeHeader || undefined}
        data-expanded={headerExpanded || undefined}
        style={{"--home-header-origin": headerOrigin} as React.CSSProperties}
      >
        <header ref={header} inert={headerCollapsed}>
          <a
            className="brand"
            href="#"
            title={managerStatusText}
            aria-label={`返回主页；${managerStatusText}`}
            onClick={(e) => {
              e.preventDefault();
              localStorage.setItem("foxray.overview.tab", "dashboard");
              setHomeKey((value) => value + 1);
              navigate("overview");
            }}
          >
            <svg
              className="brand-logo"
              viewBox="105 83 292 349"
              role="img"
              aria-label="FoXray"
            >
              <image href="/foxray-icon.svg?v=0.9.4" width="512" height="512" />
            </svg>
            <div>
              <div className="brand-title-row">
                <h1 aria-label="FoXray Gateway">Fo<span className="brand-name-x">X</span>ray Gateway</h1>
                <span
                  className="manager-status-dot"
                  data-status={managerStatus}
                  role="img"
                  aria-label={managerStatusText}
                />
              </div>
            </div>
          </a>
          <nav aria-label="功能模块">
            {available.map(([id, label]) => (
              <button
                key={id}
                className={"nav-tab " + (id === "monitor" ? "integrated" : "")}
                aria-current={page === id ? "page" : undefined}
                onClick={() => navigate(id)}
              >
                <span className="nav-icon" aria-hidden="true">
                  {labelIcon(label, 18)}
                </span>
                <span>{label}</span>
              </button>
            ))}
          </nav>
          <div className="header-status">
            <div className="header-tools">
              <Tasks
                notice={notice}
                onClear={() => setNotice(null)}
                onWarningChange={setTaskWarning}
              />
              <Button
                variant="ghost"
                title={theme === "dark" ? "切换浅色主题" : "切换深色主题"}
                onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
              >
                {theme === "dark" ? <Moon size={20} /> : <Sun size={20} />}
              </Button>
              <Button
                variant="ghost"
                title="设置"
                onClick={() => {
                  navigate("settings");
                }}
              >
                <SettingsIcon size={20} />
                <UpdateBadge />
              </Button>
            </div>
          </div>
        </header>
      </div>
      <TrafficWindowContext.Provider
        value={Number(
          state?.manager_settings.traffic_chart_window_seconds || 30,
        )}
      >
        <RangeDefaultsContext.Provider
          value={rangeHours(state?.manager_settings.presentation)}
        >
          <LiveRefreshContext.Provider
            value={
              Math.max(
                1,
                Number(state?.manager_settings.live_refresh_seconds || 2),
              ) * 1000
            }
          >
            <ChartRefreshContext.Provider
              value={
                Math.max(
                  1,
                  Number(state?.manager_settings.chart_refresh_seconds || 10),
                ) * 1000
              }
            >
              <main
                data-home-fullwidth={page === "overview" && Number(record(state?.manager_settings.presentation).content_width_percent || 80) === 100 || undefined}
                className={page === "monitoring" && hideHomeHeader ? "monitoring-shortcut-page" : undefined}
                style={
                  {
                    "--overview-label-size": `${Number(record(record(state?.manager_settings.presentation).overview_text).label_size || 14)}px`,
                    "--overview-value-size": `${Number(record(record(state?.manager_settings.presentation).overview_text).value_size || 24)}px`,
                    "--content-width": `${Math.min(100, Math.max(80, Number(record(state?.manager_settings.presentation).content_width_percent || 80)))}%`,
                  } as React.CSSProperties
                }
              >
                {page !== "overview" && (
                  <section className="module-heading main-page-heading">
                    <PageHeader
                      title={pageHeadings[page]?.[0] || "FoXray Gateway"}
                      description={pageHeadings[page]?.[1]}
                    />
                    <div id="secondary-navigation">
                      {page === "monitoring" && hideHomeHeader && <HomeTools view="monitoring">
                        <Button title="返回主页" aria-label="返回主页" onClick={() => navigate("overview")}>{labelIcon("主页", 18)}</Button>
                      </HomeTools>}
                    </div>
                  </section>
                )}
                {state?.read_only && (
                  <div className="read-only-banner">
                    只读验证实例 ·
                    可预览配置和临时密钥，不会修改接口、路由或服务
                  </div>
                )}
                {state && ["network","routes","rules","firewall"].includes(page) && <NetworkPending act={act} inline readOnly={state.read_only}/>}
                {!state ? (
                  <Panel>
                    <div className="loading">
                      <LoaderCircle className="spin" />
                      正在连接管理服务…
                    </div>
                    <ErrorLine error={error} />
                  </Panel>
                ) : (
                  content
                )}
                {state && page === "overview" && (
                  <OverviewFooter
                    presentation={state.manager_settings.presentation}
                    statusRefresh={statusRefresh}
                  />
                )}
                {page === "monitoring" && (
                  <DataFreshness statusRefresh={statusRefresh} />
                )}
                <footer>
                  {versionLabel(state?.version || "0.9.4")} · Go / React ·
                  网络变更采用预览、确认与回退
                </footer>
              </main>
            </ChartRefreshContext.Provider>
          </LiveRefreshContext.Provider>
        </RangeDefaultsContext.Provider>
      </TrafficWindowContext.Provider>
      {state && !state.read_only && !["network","routes","rules","firewall"].includes(page) && <NetworkPending act={act} />}
      <ThemedSelects />
    </UpdatesContext.Provider>
    </HomeHeaderContext.Provider>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
