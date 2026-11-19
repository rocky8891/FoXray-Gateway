import { record } from "./api";
import { OverviewClock } from "./overview_clock";
import { DataFreshness } from "./ui";

export function OverviewFooter({
  presentation,
  statusRefresh,
  preview = false,
}: {
  presentation: unknown;
  statusRefresh?: number;
  preview?: boolean;
}) {
  const p = record(presentation),
    clock = p.overview_show_clock === true,
    status = p.overview_show_data_status === true;
  if (!clock && !status) return null;
  return (
    <div
      className={
        "overview-footer" + (preview ? " overview-footer-preview" : "")
      }
    >
      {clock &&
        (preview ? (
          <div className="overview-clock">
            <time>2026年11月18日 17:06:57</time>
            <span>
              {String(p.clock_timezone || "Asia/Shanghai")} ·{" "}
              {p.clock_source === "server"
                ? "服务器"
                : p.clock_source === "browser"
                  ? "浏览器"
                  : "互联网"}
            </span>
          </div>
        ) : (
          <OverviewClock presentation={presentation} />
        ))}
      {clock && status && (
        <span className="overview-footer-divider" aria-hidden="true" />
      )}
      {status &&
        (preview ? (
          <div className="data-freshness">
            页面更新：状态 1 秒 · 实时指标 1 秒 · 历史图 1 秒 |{" "}
            <span className="accent">● 数据获取正常（预览）</span>
          </div>
        ) : (
          <DataFreshness statusRefresh={statusRefresh} />
        ))}
    </div>
  );
}
