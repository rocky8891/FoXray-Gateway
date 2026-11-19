import { IconSelect } from "./icon_select";
import { Clock3 } from "lucide-react";
import { SettingInfo } from "./ui";
import type { TimeRange } from "./time_range";
import { createContext, useContext } from "react";
export const TrafficWindowContext = createContext(30);
export function useTrafficWindow() {
  return useContext(TrafficWindowContext);
}
export function trafficWindow(_range: TimeRange, value = "auto", minimum = 30) {
  if (value !== "auto") return Math.min(3600, Math.max(1, Number(value) || 30));
  return Math.min(3600, Math.max(1, Number(minimum) || 30));
}
export function TrafficWindowControl({
  value,
  onChange,
  compact = false,
}: {
  value: string;
  compact?: boolean;
  onChange: (value: string) => void;
}) {
  const seconds = useTrafficWindow(),
    presets = ["auto", "1", "5", "15", "30", "60", "300"],
    custom = !presets.includes(value);
  const labels: Record<string, string> = {
    auto: `跟随全局（${seconds}秒）`,
    "1": "精细（1秒）",
    "5": "5秒",
    "15": "15秒",
    "30": "30秒",
    "60": "1分钟",
    "300": "5分钟",
    custom: "自定义",
  };
  return (
    <div
      className={
        "traffic-window-control chart-inline-control" +
        (compact ? " compact" : "")
      }
    >
      <IconSelect
        label="图表汇总窗口"
        showIcon={false}
        triggerText={(value === "auto" ? seconds : value) + "秒"}
        value={custom ? "custom" : value}
        options={[...presets, "custom"].map((v) => ({
          value: v,
          label: labels[v],
          icon: <Clock3 size={16} />,
          tone: v === "auto" ? "green" : "blue",
        }))}
        onChange={(v) => onChange(v === "custom" ? "120" : v)}
      />
      <SettingInfo
        label="图表汇总窗口"
        text={`当前${custom ? value + "秒" : labels[value]}。数值为最近一次真实采样速率；曲线按真实字节和采样时长计算区间均速，只影响曲线。跟随全局默认30秒，可设1～3600秒；页面内选择为临时覆盖，采集、历史写入与累计量不变。长范围可能进一步合并，缺测保持断点。`}
      />
      {custom && (
        <input
          aria-label="自定义图表汇总秒数"
          type="number"
          min={1}
          max={3600}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onBlur={() =>
            onChange(
              String(
                Math.min(3600, Math.max(1, Math.round(Number(value) || 30))),
              ),
            )
          }
        />
      )}
    </div>
  );
}
