import { useEffect, useState } from "react";
import { HomeHeaderToggleIcon } from "./home_header";
import { formatRate } from "./host";
import { Button, PageHeader, Panel } from "./ui";

type MonitorSnapshot = {
  host: Record<string, number | null>;
  modules: Record<string, Record<string, number | null>>;
  time: number;
};
const modules = [["xray", "Xray"], ["warp", "WARP"], ["wireguard", "WireGuard"]];
const display = (value: number | null | undefined, suffix = "", digits = 1) =>
  typeof value === "number" && Number.isFinite(value) ? value.toFixed(digits) + suffix : "—";

export function HomeMonitorScreen({ onLogin }: { onLogin: () => void }) {
  const [data, setData] = useState<MonitorSnapshot>();
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    let pending = false;
    const read = async () => {
      if (document.hidden || pending) return;
      pending = true;
      try {
        const response = await fetch("/api/management/home-monitor", { cache: "no-store", signal: controller.signal });
        if (response.status === 401) { onLogin(); return; }
        if (!response.ok) throw new Error();
        setData(await response.json());
        setError("");
      } catch {
        if (!controller.signal.aborted) setError("暂时无法读取监控数据，正在重试");
      } finally { pending = false; }
    };
    void read();
    const timer = window.setInterval(read, 5000);
    document.addEventListener("visibilitychange", read);
    return () => {
      controller.abort();
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", read);
    };
  }, [onLogin]);
  const host = data?.host;
  const cards = (values: Array<[string, string]>) => <dl className="form-grid">{values.map(([label, value]) => <div key={label}><dt className="muted">{label}</dt><dd className="metric-heading">{value}</dd></div>)}</dl>;
  return <>
    <header>
      <div className="brand"><HomeHeaderToggleIcon className="brand-logo" /><h1>FoXray Gateway</h1></div>
      <div className="header-tools"><Button variant="primary" onClick={onLogin}>登录管理</Button></div>
    </header>
    <main>
      <Panel>
        <PageHeader title="首页常驻监控" description={error || (data ? `只读 · 最近读取 ${new Date(data.time * 1000).toLocaleTimeString()}` : "正在读取监控数据…")} />
      </Panel>
      <Panel><PageHeader title="服务器" />{cards([["平均负载", display(host?.load, "", 2)], ["CPU 使用率", display(host?.cpu, "%")], ["内存使用率", display(host?.memory, "%")], ["分区使用率", display(host?.disk, "%")]])}</Panel>
      {modules.map(([key, name]) => {
        const sample = data?.modules[key];
        return sample && <Panel key={key}><PageHeader title={name} />{cards([["上传", formatRate(sample.upload)], ["下载", formatRate(sample.download)], ["活动连接数", display(sample.connections, "", 0)]])}</Panel>;
      })}
    </main>
  </>;
}
