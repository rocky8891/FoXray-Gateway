import { SettingInfo } from "./ui";
import { ProductIcon } from "./product_icons";
import { SaveCard, SettingsSaveProvider } from "./settings_save";
import { useState } from "react";
import { Boxes, Monitor } from "lucide-react";
import { api, record, text, type Dict, type State, type Task } from "./api";
import { Button, Field, Panel, PageHeader, ErrorLine, useResource } from "./ui";
import type { Act } from "./nodes";

export function ModuleSettings({ state }: { state: State; act: Act }) {
  const resource = useResource<Dict>("/api/modules", 5000);
  const [url, setURL] = useState(state.components.komari_url || ""),
    [terms, setTerms] = useState(false),
    [check, setCheck] = useState(""),
    [error, setError] = useState("");
  const [desired, setDesired] = useState<Record<string, boolean>>({});
  const definitions = [
    {
      id: "xray",
      title: "Xray",
      icon: <ProductIcon product="xray" size={20} />,
    },
    {
      id: "warp",
      title: "WARP",
      icon: <ProductIcon product="warp" size={20} />,
    },
    {
      id: "wireguard",
      title: "WireGuard",
      icon: <ProductIcon product="wireguard" size={20} />,
    },
    { id: "komari", title: "Komari", icon: <Monitor size={20} /> },
  ];
  return (
    <SettingsSaveProvider expected={4} inline>
      {(footer) => (
      <Panel className="module-settings">
        <PageHeader
          title="功能模块"
          icon={<Boxes size={20} />}
          description={<ul><li><strong>启用模块。</strong>先检测已有服务，保留现有配置；缺失的组件按需安装。</li><li><strong>停用模块。</strong>隐藏该模块入口并暂停其自动任务。</li><li><strong>运行服务。</strong>模块开关不等于服务启停；已有服务和 VPN 连接保留。</li></ul>}
        />
        <ErrorLine error={error || resource.error} />
        <div className="module-settings-grid">
          {definitions.map((item) => {
            const info = record(record(resource.data?.modules)[item.id]);
            const enabled = !!state.components[item.id as "xray"];
            return (
              <article className="module-setting-card" key={item.id}>
                <h3 className="metric-heading">
                  {item.icon}
                  {item.title}
                  <small className={enabled ? "accent" : "muted"}>
                    {enabled ? "已启用" : "已停用"}
                  </small>
                  <SettingInfo
                    label="组件接管方式"
                    text={
                      <>
                        {item.id === "komari"
                          ? "仅连接独立部署的 Komari，不自动安装"
                          : info.installed
                            ? "检测到已安装服务，将保留现有文件和配置"
                            : "启用时自动检查并部署；不自动启动隧道或已有 VPN 接口"}
                      </>
                    }
                  />
                </h3>
                <label className="check module-enable">
                  <input
                    type="checkbox"
                    checked={desired[item.id] ?? enabled}
                    onChange={(e) =>
                      setDesired((v) => ({ ...v, [item.id]: e.target.checked }))
                    }
                  />
                  启用模块
                </label>
                {item.id === "komari" && (
                  <details className="module-connection-options">
                    <summary>连接地址与检测</summary>
                    <Field
                      label="Komari URL"
                      titleExtra={<small className="module-current-url">当前 URL：{state.components.komari_url || "未配置"}</small>}
                      hint="留空自动检测本机 25774 端口；不允许包含认证信息或 Token"
                    >
                      <input
                        value={url}
                        onChange={(e) => {
                          setURL(e.target.value);
                          setCheck("");
                        }}
                        placeholder="http://监控服务器:25774/"
                      />
                    </Field>
                    <Button
                      onClick={() => {
                        setError("");
                        void api<Dict>("/api/modules/komari-check", { url })
                          .then((r) => {
                            setURL(text(r.url));
                            setCheck(
                              r.recognized
                                ? "已识别 Komari，可连接"
                                : "地址可访问，但未识别为 Komari",
                            );
                          })
                          .catch((e) => setError(String(e.message || e)));
                      }}
                    >
                      自动检测 / 验证 URL
                    </Button>
                    <p className="small muted">{check}</p>
                  </details>
                )}
                {item.id === "warp" && (
                  <label className="check">
                    <input
                      type="checkbox"
                      checked={terms}
                      onChange={(e) => setTerms(e.target.checked)}
                    />
                    接受 Cloudflare WARP 服务条款（首次安装或注册时需要）
                  </label>
                )}
                <SaveCard
                  showActions={false}
 dirty={(desired[item.id] ?? enabled) !== enabled || (item.id === "komari" && url !== (state.components.komari_url || ""))}
                  title={item.title + " 功能"}
                  defaults={() => {
                    setDesired((v) => ({
                      ...v,
                      [item.id]: item.id === "xray",
                    }));
                    if (item.id === "komari") setURL("");
                    if (item.id === "warp") setTerms(false);
                  }}
                  discard={() => {
                    setDesired((v) => ({ ...v, [item.id]: enabled }));
                    if (item.id === "komari")
                      setURL(state.components.komari_url || "");
                    if (item.id === "warp") setTerms(false);
                  }}
                  disabled={state.read_only}
                  save={() => {
                    setError("");
                    const next = desired[item.id] ?? enabled;
                    if (
                      next === enabled &&
                      (item.id !== "komari" ||
                        url === (state.components.komari_url || "")) &&
                      (item.id !== "warp" || !terms)
                    )
                      return Promise.resolve(true);
                    return (async () => {
                      try {
                        const submitted = await api<Dict>("/api/modules", {
                          module: item.id,
                          enabled: next,
                          url,
                          accept_terms: terms,
                        });
                        if (!submitted.id) return !!submitted.ok;
                        for (let round = 0; round < 600; round++) {
                          const data = await api<{ tasks: Task[] }>(
                            "/api/tasks",
                          );
                          const task = data.tasks.find(
                            (t) => t.id === submitted.id,
                          );
                          if (task?.status === "success") {
                            void resource.refresh();
                            return true;
                          }
                          if (task?.status === "error")
                            throw new Error(task.message);
                          await new Promise((r) => setTimeout(r, 1000));
                        }
                        throw new Error("模块任务仍未完成，请查看任务日志");
                      } catch (e) {
                        setError(
                          e instanceof Error ? e.message : "模块设置失败",
                        );
                        return false;
                      }
                    })();
                  }}
                />
              </article>
            );
          })}
        </div>
        {footer}
      </Panel>
      )}
    </SettingsSaveProvider>
  );
}
