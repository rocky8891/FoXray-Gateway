import { Clock } from "lucide-react";
import { useId } from "react";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useMemo,
  useState,
} from "react";
import { Button, Field } from "./ui";
export const RangeDefaultsContext = createContext(24);
export const TIME_RANGES = [
  [String(1 / 60), "近1分钟"],
  [String(5 / 60), "近5分钟"],
  ["0.25", "近15分钟"],
  ["today", "今天"],
  ["yesterday", "昨天"],
  ["1", "近1小时"],
  ["6", "近6小时"],
  ["24", "近24小时"],
  ["72", "近3天"],
  ["168", "近7天"],
  ["720", "近30天"],
  ["2160", "近3个月"],
  ["4320", "近半年"],
  ["8760", "近1年"],
  ["custom", "自定义"],
];
export function durationLabel(hours: number) {
  const minutes = Math.round(hours * 60);
  return (
    "近" +
    (minutes < 60
      ? minutes + "分钟"
      : hours < 24
        ? hours + "小时"
        : hours % 24 === 0
          ? hours / 24 + "天"
          : hours + "小时")
  );
}
function localTime(ms: number) {
  const d = new Date(ms);
  return new Date(ms - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
export function useTimeRange(override?: number) {
  const inherited = useContext(RangeDefaultsContext);
  const initial = String(override || inherited);
  const [value, setValue] = useState(initial),
    [start, setStart] = useState(() => localTime(Date.now() - 86400000)),
    [end, setEnd] = useState(() => localTime(Date.now())),
    [custom, setCustom] = useState<{ start: number; end: number } | null>(null),
    [error, setError] = useState("");
  const touched = useRef(false);
  useEffect(() => {
    if (!touched.current) setValue(initial);
  }, [initial]);
  const choose = (next: string) => {
    touched.current = true;
    setValue(next);
  };
  const query = useMemo(() => {
    if (value === "custom")
      return custom
        ? `start=${custom.start}&end=${custom.end}`
        : `hours=${initial}`;
    if (value === "today" || value === "yesterday") {
      const d = new Date();
      d.setHours(0, 0, 0, 0);
      const midnight = d.getTime() / 1000;
      if (value === "today") return `start=${midnight}`;
      d.setDate(d.getDate() - 1);
      return `start=${d.getTime() / 1000}&end=${midnight}`;
    }
    return `hours=${value}`;
  }, [value, custom, initial]);
  const label =
    value === "custom"
      ? "自定义范围"
      : (
          TIME_RANGES.find(([v]) => v === value)?.[1] ||
          durationLabel(Number(value))
        ).replace(/^近/, "");
  const apply = () => {
    const a = new Date(start).getTime() / 1000,
      b = new Date(end).getTime() / 1000;
    if (
      !Number.isFinite(a) ||
      !Number.isFinite(b) ||
      b <= a ||
      b - a > 365 * 86400
    ) {
      setError("请选择有效起止时间，最长365天");
      return;
    }
    setError("");
    setCustom({ start: a, end: b });
  };
  return {
    value,
    isChanged: value !== initial,
    setValue: choose,
    start,
    setStart,
    end,
    setEnd,
    query,
    label,
    error,
    apply,
    reset: () => {
      touched.current = false;
      setValue(initial);
      setCustom(null);
      setError("");
    },
  };
}
export type TimeRange = ReturnType<typeof useTimeRange>;
export function TimeRangeControl({
  range,
  label = "时间范围",
  ariaLabel,
}: {
  range: TimeRange;
  label?: string;
  ariaLabel?: string;
}) {
  return (
    <div className="time-range-control time-range-unlabelled">
      <Field label={label}>
        <select
          aria-label={ariaLabel || label}
          value={range.value}
          onChange={(e) => range.setValue(e.target.value)}
        >
          {!TIME_RANGES.some(([v]) => v === range.value) && (
            <option value={range.value}>
              {durationLabel(Number(range.value))}
            </option>
          )}
          {TIME_RANGES.map(([v, l]) => (
            <option key={v} value={v}>
              {l}
            </option>
          ))}
        </select>
      </Field>
      {range.value === "custom" && (
        <div className="time-range-custom">
          <Field label="开始时间">
            <input
              aria-label="开始时间"
              type="datetime-local"
              value={range.start}
              onChange={(e) => range.setStart(e.target.value)}
            />
          </Field>
          <Field label="结束时间">
            <input
              aria-label="结束时间"
              type="datetime-local"
              value={range.end}
              onChange={(e) => range.setEnd(e.target.value)}
            />
          </Field>
          <Button onClick={range.apply}>查询</Button>
          {range.error && <small role="alert">{range.error}</small>}
        </div>
      )}
    </div>
  );
}

export function OverviewTimeControl({ range }: { range: TimeRange }) {
  const id = useId();
  const [position, setPosition] = useState({ top: 0, left: 0 });
  return (
    <div className="overview-time-tool">
      <button
        type="button"
        className="button secondary"
        title={`时间范围：${range.label}`}
        aria-label="主页时间范围"
        popoverTarget={id}
        onClick={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          setPosition({
            top: Math.max(12, Math.min(innerHeight - 300, r.top)),
            left: Math.max(12, Math.min(innerWidth - 300, r.left - 292)),
          });
        }}
      >
        <Clock size={18} />
      </button>
      <div
        id={id}
        popover="auto"
        className="overview-tool-popover"
        style={position}
      >
        <h3>时间范围</h3>
        <TimeRangeControl range={range} ariaLabel="主页监控时间范围" />
      </div>
    </div>
  );
}
