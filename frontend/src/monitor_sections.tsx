import {
  ScaledMonitorPreview,
  MockMonitorChart,
  type MonitorPreviewSpec,
} from "./scaled_monitor_preview";
import { Fragment, useEffect, useState, type ReactNode } from "react";
import { LayoutGrid } from "lucide-react";
import { record, type Dict } from "./api";
import type { Act } from "./nodes";
import { Button, Empty, ErrorLine, Modal, useResource } from "./ui";
import { LayoutEditor } from "./layout_editor";

type View = "traffic" | "latency" | "connections";
export const MONITOR_SECTION_OPTIONS: Record<View, string[][]> = {
  traffic: [
    ["trend", "流量趋势"],
    ["statistics", "流量统计"],
  ],
  latency: [
    ["trend", "延迟趋势"],
    ["recent", "最近检测"],
  ],
  connections: [
    ["trend", "活动连接"],
    ["details", "连接详情"],
  ],
};
export function useMonitorSections(
  module: string,
  view: View,
  act: Act,
  previews: Record<string, MonitorPreviewSpec> = {},
) {
  const resource = useResource<Dict>("/api/monitoring/presentation", 15000);
  const key = `monitor_${module}_${view}_sections`;
  const layoutKey = `monitor_${module}_${view}_layout`;
  const [draftLayout, setDraftLayout] = useState("single");
  const [draft, setDraft] = useState<string[] | null>(null);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState("");
  const [applied, setApplied] = useState<{
    key: string;
    value: string[];
    layout: string;
  } | null>(null);
  const choices = MONITOR_SECTION_OPTIONS[view];
  const all = choices.map(([id]) => id);
  const saved = record(resource.data)[key];
  useEffect(() => {
    if (
      applied?.key === key &&
      JSON.stringify(saved) === JSON.stringify(applied.value) &&
      String(resource.data?.[layoutKey] || "single") === applied.layout
    )
      setApplied(null);
  }, [key, saved, applied]);
  const layout =
    applied?.key === key
      ? applied.layout
      : String(resource.data?.[layoutKey] || "single");
  const value =
    applied?.key === key
      ? applied.value
      : Array.isArray(saved)
        ? [
            ...new Set(
              saved.filter(
                (id): id is string =>
                  typeof id === "string" && all.includes(id),
              ),
            ),
          ]
        : all;
  const close = () => {
    if (!saving) {
      setDraft(null);
      setFailure("");
    }
  };
  const control = (
    <>
      <Button
        onClick={() => {
          setDraft([...value]);
          setDraftLayout(layout);
          setFailure("");
        }}
      >
        <LayoutGrid size={16} />
        布局
      </Button>
      {draft !== null && (
        <Modal
          description={
            <>
              <p>
                选择显示分区，拖动预览调整上下顺序；也可聚焦分区后用方向键调整，保存并应用后生效。
              </p>
            </>
          }
          className="monitor-layout-modal"
          title="监控分区布局"
          onClose={close}
        >
          <div className="monitor-section-editor">
            <ErrorLine error={resource.error || failure} />

            <div className="inline monitor-section-choices">
              {choices.map(([id, label]) => (
                <label className="check" key={id}>
                  <input
                    type="checkbox"
                    checked={draft.includes(id)}
                    disabled={saving}
                    onChange={(e) =>
                      setDraft(
                        e.target.checked
                          ? [...draft, id]
                          : draft.filter((v) => v !== id),
                      )
                    }
                  />
                  {label}
                </label>
              ))}
            </div>
            <label className="field">
              <span className="field-title">分区布局</span>
              <select
                aria-label="监控分区布局"
                value={draftLayout}
                onChange={(e) => setDraftLayout(e.target.value)}
              >
                <option value="single">单排布局</option>
                <option value="double">双排布局</option>
              </select>
            </label>
            {!draft.length ? (
              <Empty>所有分区均已隐藏，可随时重新勾选。</Empty>
            ) : (
              <div inert={saving}>
                <ScaledMonitorPreview>
                  <LayoutEditor
                    value={draft}
                    options={choices}
                    layout={draftLayout}
                    onChange={setDraft}
                    renderCard={(id, label) => (
                      <MockMonitorChart
                        kind={
                          previews[id]?.kind ||
                          (id !== "trend"
                            ? "table"
                            : view === "latency"
                              ? "line"
                              : "area")
                        }
                        series={
                          previews[id]?.series ||
                          (view === "traffic"
                            ? ["下载", "上传"]
                            : view === "connections"
                              ? ["活动连接"]
                              : ["延迟"])
                        }
                        colors={previews[id]?.colors}
                        message={previews[id]?.message}
                        title={previews[id]?.title || label}
                      />
                    )}
                  />
                </ScaledMonitorPreview>
              </div>
            )}
            <div className="actions">
              <Button
                disabled={saving}
                onClick={() => {
                  setDraft([...all]);
                  setDraftLayout("single");
                }}
              >
                恢复默认
              </Button>
              <Button disabled={saving} onClick={close}>
                取消
              </Button>
              <Button
                variant="primary"
                disabled={saving || resource.loading || !!resource.error}
                onClick={async () => {
                  setSaving(true);
                  setFailure("");
                  try {
                    const next = [...draft];
                    if (
                      await act("/api/monitoring/presentation", {
                        [key]: next,
                        [layoutKey]: draftLayout,
                      })
                    ) {
                      setApplied({ key, value: next, layout: draftLayout });
                      setDraft(null);
                      await resource.refresh();
                    } else setFailure("保存失败，请重试。");
                  } catch {
                    setFailure("保存失败，请重试。");
                  } finally {
                    setSaving(false);
                  }
                }}
              >
                {saving ? "保存中…" : "保存并应用"}
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </>
  );
  return {
    control,
    render: (sections: Record<string, ReactNode>) =>
      value.length ? (
        <div
          className={`monitor-view-layout monitor-view-layout-${layout}`}
          data-module={module}
          data-view={view}
        >
          {value.map((id) => (
            <Fragment key={id}>{sections[id]}</Fragment>
          ))}
        </div>
      ) : (
        <Empty>当前视图的分区均已隐藏，可通过“布局”重新显示。</Empty>
      ),
  };
}
