import { useEffect, useState } from "react";
export function ConnectedTime({
  since,
  connected,
  observed = false,
}: {
  since?: number;
  connected: boolean;
  observed?: boolean;
}) {
  const [clock, setClock] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const timer = setInterval(() => setClock(Date.now() / 1000), 1000);
    return () => clearInterval(timer);
  }, []);
  if (!connected) return <span className="xray-connected-time">未连接</span>;
  if (!since || !Number.isFinite(since))
    return <span className="xray-connected-time">连接时间未记录</span>;
  const seconds = Math.max(0, Math.floor(clock - since));
  return (
    <span
      className="xray-connected-time"
      title={
        observed
          ? "从管理服务首次确认WARP已连接时起计；实际连接可能更早，页面刷新不重置"
          : "从当前出口实际切换时刻计算；保存页面设置不会重置"
      }
    >
      已连接 {Math.floor(seconds / 86400)}天 {Math.floor(seconds / 3600) % 24}
      小时 {Math.floor(seconds / 60) % 60}分钟
    </span>
  );
}
