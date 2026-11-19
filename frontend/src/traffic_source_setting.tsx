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
          text="控制Xray核心是否提供出口流量计数。启用时可能重载Xray，短暂影响代理；停用后不再读取该计数，统计监听在核心下次正常更新时移除。与页面显示开关、监控总开关和历史保留分开管理。"
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
