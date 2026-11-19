import { hasChanges } from "./settings_save";
import { HomeClockIcon } from "./requirement_icons";
import { useEffect, useState, useRef } from "react";
import { record, type Dict } from "./api";
import { useResource, Panel, PageHeader, SettingsSection, Field } from "./ui";
import { SaveCard } from "./settings_save";
import type { Act } from "./nodes";
export function OverviewClock({ presentation }: { presentation: unknown }) {
  const p = record(presentation),
    source = String(p.clock_source || "internet"),
    zone = String(p.clock_timezone || "Asia/Shanghai"),
    server = String(p.clock_server || "");
  const r = useResource<Dict>(
      "/api/time?source=" +
        encodeURIComponent(source) +
        "&server=" +
        encodeURIComponent(server),
      60000,
    ),
    [tick, setTick] = useState(0),
    anchor = useRef({ seconds: 0, at: 0 });
  useEffect(() => {
    const t = setInterval(() => setTick((x) => x + 1), 1000);
    return () => clearInterval(t);
  }, []);
  useEffect(() => {
    const seconds = Number(
      source === "server" ? r.data?.server_time : r.data?.internet_time,
    );
    anchor.current = { seconds, at: performance.now() };
  }, [r.data, source, server]);
  void tick;
  const seconds =
    source === "browser"
      ? Date.now() / 1000
      : anchor.current.seconds + (performance.now() - anchor.current.at) / 1000;
  let text = "时间获取中";
  if (
    source !== "browser" &&
    (!Number.isFinite(seconds) || !anchor.current.seconds)
  ) {
    if (r.error || r.data) text = "时间获取失败";
  } else {
    try {
      const parts = new Intl.DateTimeFormat("en-GB", {
        timeZone: zone,
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hourCycle: "h23",
      }).formatToParts(new Date(seconds * 1000));
      const get = (t: string) => parts.find((p) => p.type === t)?.value;
      text = `${get("year")}年${get("month")}月${get("day")}日 ${get("hour")}:${get("minute")}:${get("second")}`;
    } catch {
      text = "时区无效";
    }
  }
  return (
    <div
      className="overview-clock"
      title={String(
        r.data?.error ||
          r.error ||
          r.data?.time_server ||
          "只显示时间，不修改系统时钟",
      )}
    >
      <time>{text}</time>
      <span>
        {zone} ·{" "}
        {source === "internet"
          ? "互联网"
          : source === "server"
            ? "服务器"
            : "浏览器"}
      </span>
    </div>
  );
}
function validClockServer(value: string) {
  if (!value) return true;
  try {
    const url = new URL(value);
    return (
      value.length <= 2048 &&
      value.trim() === value &&
      url.protocol === "https:" &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash
    );
  } catch {
    return false;
  }
}
export function ClockSettings({ act }: { act: Act }) {
  const r = useResource<Dict>("/api/monitoring/presentation");
  const [d, setD] = useState<Dict>({});
  const p = { ...r.data, ...d };
  const invalidServer = !validClockServer(String(p.clock_server || ""));
  return (
    <Panel>
      <PageHeader
        title="主页时钟"
        icon={<HomeClockIcon size={20} />}
        description="互联网时间通过HTTPS响应时间校准；服务器时间由管理服务提供，浏览器时间读取本机。只显示时间，不修改系统时钟。"
      />
      <SettingsSection>
        <div className="form-grid">
          <Field
            label="时间来源"
            info="互联网从所选HTTPS服务器的Date响应头校准；服务器读取网关时间；浏览器读取当前设备时间。都不会修改系统时钟。"
          >
            <select
              aria-label="时钟来源"
              value={String(p.clock_source || "internet")}
              onChange={(e) => setD({ ...d, clock_source: e.target.value })}
            >
              <option value="internet">互联网时间</option>
              <option value="server">服务器时间</option>
              <option value="browser">浏览器时间</option>
            </select>
          </Field>
          <Field
            label="时区"
            info="只影响显示时区，不影响取时来源或网关系统时区。"
          >
            <select
              aria-label="时钟时区"
              value={String(p.clock_timezone || "Asia/Shanghai")}
              onChange={(e) => setD({ ...d, clock_timezone: e.target.value })}
            >
              {[
                "Asia/Shanghai",
                "UTC",
                "Asia/Tokyo",
                "Asia/Singapore",
                "Europe/London",
                "Europe/Berlin",
                "America/New_York",
                "America/Los_Angeles",
              ].map((z) => (
                <option key={z}>{z}</option>
              ))}
            </select>
          </Field>
          <Field
            label="互联网时间服务器"
            error={
              invalidServer ? "请输入不含账号、查询参数或片段的HTTPS地址" : ""
            }
            info="填写提供有效Date响应头的HTTPS地址。留空自动尝试Cloudflare、Microsoft、百度，每分钟校准一次；指定地址时仅使用该服务器。"
          >
            <input
              aria-label="互联网时间服务器"
              type="text"
              placeholder="自动选择（可填写HTTPS地址）"
              disabled={String(p.clock_source || "internet") !== "internet"}
              value={String(p.clock_server || "")}
              onChange={(e) => setD({ ...d, clock_server: e.target.value })}
            />
          </Field>
        </div>
      </SettingsSection>
      <SaveCard
 dirty={hasChanges(d,r.data)}
        disabled={invalidServer}
        title="主页时钟"
        defaults={() =>
          setD({
            clock_source: "internet",
            clock_timezone: "Asia/Shanghai",
            clock_server: "",
          })
        }
        discard={() => setD({})}
        save={async () => {
          if (!(await act("/api/monitoring/presentation", d))) return false;
          await r.refresh();
          setD({});
          return true;
        }}
      />
    </Panel>
  );
}
