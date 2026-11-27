import { useState, type ReactNode } from "react";
import { GripVertical } from "lucide-react";

export function cardNeighbor(
  index: number,
  direction: string,
  count: number,
  layout: string,
) {
  const slots = Array.from({ length: count }, (_, i) =>
    layout === "equal-rows" ? { r: i, c: 0 }
    : layout === "equal-columns" ? { r: 0, c: i }
    : layout === "diamond"
      ? [
          { r: 1, c: 0 },
          { r: 0, c: 1 },
          { r: 2, c: 1 },
          { r: 1, c: 2 },
        ][i]
      : layout === "mixed"
        ? [
            { r: 1, c: 0 },
            { r: 0, c: 1 },
            { r: 2, c: 2 },
            { r: 1, c: 3 },
          ][i]
        : layout === "reverse-lightning-row"
          ? { r: i % 2, c: i }
        : layout === "lightning-row"
          ? { r: 1 - (i % 2), c: i }
        : layout === "wave-row"
          ? { r: [1, 0, 2, 1][i], c: i }
          : layout === "wave-column"
            ? { r: i, c: i % 2 }
            : layout === "ring-row"
              ? { r: 0, c: i }
              : layout === "collection-top"
                ? i === 0
                  ? { r: 0, c: 0 }
                  : { r: 1 + Math.floor((i - 1) / 2), c: (i - 1) % 2 }
                : layout === "collection-sidebar"
                  ? i === 0
                    ? { r: 0, c: 0 }
                    : { r: i - 1, c: 1 }
                  : layout === "top"
                    ? i === 0
                      ? { r: 0, c: 1 }
                      : { r: 1, c: i - 1 }
                    : layout === "sidebar"
                      ? i === 0
                        ? { r: 1, c: 0 }
                        : { r: i - 1, c: 1 }
                      : layout === "single" || layout === "collection-single"
                        ? { r: i, c: 0 }
                        : { r: Math.floor(i / 2), c: i % 2 },
  );
  const a = slots[index];
  if (!a) return -1;
  const choices = slots
    .map((b, i) => ({ i, dr: b.r - a.r, dc: b.c - a.c }))
    .filter((v) =>
      direction === "up"
        ? v.dr < 0
        : direction === "down"
          ? v.dr > 0
          : direction === "left"
            ? v.dc < 0
            : v.dc > 0,
    );
  choices.sort(
    (x, y) =>
      Math.abs(direction === "up" || direction === "down" ? x.dc : x.dr) * 10 +
      Math.abs(x.dr) +
      Math.abs(x.dc) -
      (Math.abs(direction === "up" || direction === "down" ? y.dc : y.dr) * 10 +
        Math.abs(y.dr) +
        Math.abs(y.dc)),
  );
  return choices[0]?.i ?? -1;
}
export function LayoutEditor({
  value,
  options,
  layout,
  onChange,
  collection = false,
  renderCard,
}: {
  value: string[];
  options: string[][];
  layout: string;
  onChange: (value: string[]) => void;
  collection?: boolean;
  renderCard?: (id: string, label: string) => ReactNode;
}) {
  const [dragging, setDragging] = useState("");
  const label = (id: string) => options.find(([key]) => key === id)?.[1] || id;
  const swap = (from: number, to: number) => {
    if (from === to || from < 0 || to < 0) return;
    const next = [...value];
    [next[from], next[to]] = [next[to], next[from]];
    onChange(next);
  };
  const dragProps = (id: string) => ({
    draggable: true,
    onDragStart: (e: React.DragEvent) => {
      e.dataTransfer.setData("text/x-foxray-card", id);
      e.dataTransfer.effectAllowed = "move";
      setDragging(id);
    },
    onDragEnd: () => setDragging(""),
    onDragOver: (e: React.DragEvent) => {
      if (dragging || e.dataTransfer.types.includes("text/x-foxray-card")) {
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
      }
    },
    onDrop: (e: React.DragEvent) => {
      e.preventDefault();
      const from = e.dataTransfer.getData("text/x-foxray-card");
      if (value.includes(from)) swap(value.indexOf(from), value.indexOf(id));
      setDragging("");
    },
  });
  return (
    <div className="layout-editor">
      <div>
        <div
          className={`layout-preview layout-preview-${layout}${collection ? " layout-preview-collection" : ""}`}
          aria-label="可拖动布局预览"
          data-anchor={value[0]}
          data-card-count={value.length}
          style={
            {
              "--layout-secondary-count": Math.max(1, value.length - 1),
            } as React.CSSProperties
          }
        >
          {value.map((id, i) => (
            <div
              data-monitor={id}
              data-layout-anchor={i === 0}
              className={dragging === id ? "dragging" : ""}
              key={id}
              role="button"
              tabIndex={0}
              aria-label={"拖动" + label(id)}
              {...dragProps(id)}
              onKeyDown={(e) => {
                const direction = (
                  {
                    ArrowUp: "up",
                    ArrowDown: "down",
                    ArrowLeft: "left",
                    ArrowRight: "right",
                  } as Record<string, string>
                )[e.key];
                if (direction) {
                  e.preventDefault();
                  swap(
                    i,
                    cardNeighbor(
                      i,
                      direction,
                      value.length,
                      collection ? "collection-" + layout : layout,
                    ),
                  );
                }
              }}
            >
              {renderCard ? (
                renderCard(id, label(id))
              ) : (
                <span className="layout-preview-label">
                  <GripVertical size={16} />
                  <span>{label(id)}</span>
                </span>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function layoutEditorHelp(
  layout: string,
  collection = false,
  renderCard = false,
) {
  return renderCard
    ? "拖动示意卡片交换位置；也可聚焦后用方向键调整，保存后应用。"
    : collection
      ? "拖动预览交换节点卡片，保存后应用；不会启停节点。"
      : layout === "single" || layout === "double"
        ? "拖动预览交换历史卡片；运行状态固定在顶部，保存后应用。"
        : "拖动预览中的卡片交换位置；保存后应用。横排／侧栏布局的第一个卡片占据上方／左侧大区域。";
}
