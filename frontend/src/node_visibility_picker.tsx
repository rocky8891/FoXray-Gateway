import { useRef, useState } from "react";
import { type Node } from "./api";
import { NodeTable, sortOverviewNodes } from "./nodes";
import { Button, Modal } from "./ui";
import { AdaptiveSearch } from "./adaptive_search";
import { ResetFilters } from "./list_controls";
import { Search } from "lucide-react";
export function NodeVisibilityPicker({
  nodes,
  mode,
  selected,
  onModeChange,
  onChange,
}: {
  nodes: Node[];
  mode: string;
  selected: string[];
  onModeChange: (v: string) => void;
  onChange: (v: string[]) => void;
}) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [sort, setSort] = useState({ key: "name", direction: 1 });
  const chosen = new Set(selected),
    latest = useRef(chosen);
  latest.current = chosen;
  const rows = sortOverviewNodes(
    nodes.filter((n) =>
      [n.name, n.protocol, n.subscription ? "订阅节点" : "独立节点"]
        .join(" ")
        .toLocaleLowerCase()
        .includes(query.trim().toLocaleLowerCase()),
    ),
    sort,
  );
  const toggle = (id: string, on: boolean) => {
    const next = new Set(latest.current);
    on ? next.add(id) : next.delete(id);
    latest.current = next;
    onChange([...next]);
  };
  return (
    <div className="node-visibility-control">
      <select
        aria-label="节点显示范围"
        value={mode}
        onChange={(e) => {
          onModeChange(e.target.value);
          if (e.target.value === "selected") setOpen(true);
        }}
      >
        <option value="all">全部节点</option>
        <option value="selected">指定节点</option>
      </select>
      {mode === "selected" && (
        <Button onClick={() => setOpen(true)}>
          选择节点 · {selected.length} 项
        </Button>
      )}
      {open && (
        <Modal
          title="选择显示节点"
          className="node-visibility-modal"
          wide
          onClose={() => setOpen(false)}
          description="勾选即时更新配置草稿；搜索和排序不丢失选择。保存并应用后改变主页显示，不启停节点。"
        >
          <div className="management-toolbar node-picker-toolbar">
            <Button
              onClick={() => {
                const all =
                  rows.length > 0 && rows.every((n) => chosen.has(n.id));
                const next = new Set(chosen);
                rows.forEach((n) => (all ? next.delete(n.id) : next.add(n.id)));
                latest.current = next;
                onChange([...next]);
              }}
            >
              全选
            </Button>
            <div className="management-query-tools">
              <AdaptiveSearch>
                <Search size={18} />
                <input
                  aria-label="搜索显示节点"
                  placeholder="查询节点名称、分组或协议"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </AdaptiveSearch>
            </div>
            <ResetFilters
              active={!!query}
              selectedCount={selected.length}
              summary={query && `搜索：${query}`}
              onClick={() => {
                setQuery("");
                setSort({ key: "name", direction: 1 });
                onChange([]);
              }}
            />
          </div>
          <NodeTable
            nodes={rows}
            actions={() => null}
            showActions={false}
            selection={chosen}
            selectedCount={selected.length}
            total={nodes.length}
            onSelect={toggle}
            sort={sort}
            onSort={(key) =>
              setSort({
                key,
                direction: sort.key === key ? -sort.direction : 1,
              })
            }
          />
          <div className="modal-actions">
            <Button onClick={() => setOpen(false)}>完成</Button>
          </div>
        </Modal>
      )}
    </div>
  );
}
