import { ChartNoAxesCombined } from "lucide-react";
import { useState } from "react";
import { type Dict } from "./api";
import { Confirm, ErrorLine, SettingInfo, useResource } from "./ui";
import type { Act } from "./nodes";
export function TrafficSourceSetting({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/traffic", 5000),
    [confirm, setConfirm] = useState(false);
  const enabled = !!r.data?.enabled;
  return (
    <div className="monitor-source-setting settings-section">
      <div className="monitor-items-options traffic-source-options">
        <label className="check">
          <input
            type="checkbox"
            aria-label="Xray核心流量统计"
            checked={enabled}
            disabled={!r.data}
            onChange={() => setConfirm(true)}
          />
          <ChartNoAxesCombined size={16} />
          Xray核心流量统计
        </label>
        <SettingInfo
          label="Xray核心流量统计"
          text={<ul><li><strong>统计来源：</strong>控制 Xray 核心是否提供各出口的字节计数。</li><li><strong>开启影响：</strong>可能重新加载 Xray，短暂影响代理连接。</li><li><strong>关闭影响：</strong>不再读取计数；统计监听在核心下次正常更新时移除。</li><li><strong>独立开关：</strong>页面显示、模块监控和历史保留仍分别设置。</li></ul>}
        />
      </div>
      <ErrorLine error={r.error} />
      {confirm && (
        <Confirm
          title={enabled ? "停用Xray核心流量统计" : "启用Xray核心流量统计"}
          message={
            enabled
              ? "停用后停止读取Xray出口流量，保留已有历史；核心统计监听在下次正常更新时移除。"
              : "首次启用可能重载Xray核心并短暂中断代理；不会改变路由策略。"
          }
          onClose={() => setConfirm(false)}
          onConfirm={() => {
            setConfirm(false);
            void act("/api/traffic/enable", { enabled: !enabled }).then(() =>
              r.refresh(),
            );
          }}
        />
      )}
    </div>
  );
}
