import { type ReactNode } from "react";
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
