import { useContext, type ReactNode } from "react";
import { HomeHeaderContext } from "./home_header";
import { createPortal } from "react-dom";
export function HomeTools({
  fullWidth,
  view,
  children,
}: {
  fullWidth: boolean;
  view: string;
  children: ReactNode;
}) {
  const header = useContext(HomeHeaderContext);
  const tools = (
    <aside
      className="overview-side-tools"
      data-overview-view={view}
      data-auto-hide={fullWidth || undefined}
      aria-label="主页快捷工具"
    >
      {fullWidth && (
        <button
          type="button"
          className="home-tools-handle"
          aria-label="展开首页工具栏"
          title="悬停、点击或聚焦以展开首页工具栏"
          onKeyDown={(e) => {
            if (e.key === "Escape") e.currentTarget.blur();
          }}
        />
      )}
      {header.hidden && <button type="button" className="button home-header-toggle"
        aria-label={header.expanded ? "收起首页标题栏" : "展开首页标题栏"}
        title={header.expanded ? "收起首页标题栏" : "展开首页标题栏"}
        aria-expanded={header.expanded}
        onClick={e => header.toggle(e.currentTarget)}>
        <svg viewBox="105 83 292 349" aria-hidden="true"><image href="/foxray-icon.svg?v=0.9.4" width="512" height="512" /></svg>
      </button>}
      {children}
    </aside>
  );
  return fullWidth
    ? createPortal(
        <div className="home-tools-dock">{tools}</div>,
        document.body,
      )
    : tools;
}
