import { SettingInfo } from "./ui";
import { Globe } from "lucide-react";
import { NumberInput } from "./ui";
import { useEffect, useRef, useState } from "react";
import { Pagination, usePagination } from "./resource_controls";
import {
  Button,
  Field,
  Modal,
  Confirm,
  Table,
  ErrorLine,
  useResource,
} from "./ui";
import { api, errorMessage, list, timestamp, type Dict } from "./api";
import type { Act } from "./nodes";
function newSetID() {
  const values = new Uint32Array(4);
  globalThis.crypto.getRandomValues(values);
  return (
    "set_" + Array.from(values, (n) => n.toString(16).padStart(8, "0")).join("")
  );
}
const freshRule = (): Dict => ({
  name: "",
  enabled: true,
  action: "direct",
  type: "domain",
  targets: [],
  port: "",
  network: "tcp,udp",
});
export function CustomRulesEditor({
  value,
  onChange,
}: {
  value: Dict[];
  onChange: (v: Dict[]) => void;
}) {
  const [editing, setEditing] = useState<{ index: number; row: Dict } | null>(
      null,
    ),
    [deleting, setDeleting] = useState<number | null>(null),
    [targetsView, setTargetsView] = useState<{
      targets: string[];
      index: number;
      edit?: boolean;
      text?: string;
    } | null>(null);
  const [targetError, setTargetError] = useState("");
  const [query, setQuery] = useState("");
  const [searchStatus, setSearchStatus] = useState("");
  const [targetMatches, setTargetMatches] = useState<Record<string, string>>({});
  const targetCache = useRef(new Map<string, string[]>());
  const normalizedQuery = query.trim().toLowerCase();
  const targetPage = usePagination(targetsView?.targets ?? [], String(targetsView?.index ?? ""), 100, 100);
  const [position, setPosition] = useState<{
    index: number;
    value: number;
  } | null>(null);
  const loadTargets = async (row: Dict): Promise<string[]> => {
    if (!row.targets_ref)
      return Array.isArray(row.targets) ? (row.targets as string[]) : [];
    const data = await api<Dict>(
      "/api/rulesets/targets?ref=" +
        encodeURIComponent(String(row.targets_ref)),
    );
    if (data.error) throw new Error(String(data.error));
    return (data.items as string[]) || [];
  };
  const editRule = async (index: number, row: Dict) => {
    try {
      const targets = await loadTargets(row);
      const { targets_ref, targets_count, ...rest } = row;
      setEditing({ index, row: { ...rest, targets } });
      setTargetError("");
    } catch (error) {
      setTargetError(errorMessage(error));
    }
  };
  useEffect(() => {
    setTargetMatches({});
    if (!normalizedQuery) { setSearchStatus(""); return; }
    const refs = [...new Set(value.flatMap(row => row.targets_ref ? [String(row.targets_ref)] : []))];
    const controller = new AbortController();
    let cursor = 0;
    let incomplete = false;
    const matches: Record<string, string> = {};
    setSearchStatus(refs.length ? "正在搜索规则目标…" : "");
    const timer = window.setTimeout(async () => {
      const worker = async () => {
        while (!controller.signal.aborted && cursor < refs.length) {
          const ref = refs[cursor++];
          try {
            let targets = targetCache.current.get(ref);
            if (!targets) {
              const data = await api<Dict>("/api/rulesets/targets?ref=" + encodeURIComponent(ref), undefined, controller.signal);
              if (!Array.isArray(data.items)) throw new Error("无法读取规则目标");
              targets = data.items as string[];
              targetCache.current.set(ref, targets);
            }
            const match = targets.find(target => target.toLowerCase().includes(normalizedQuery));
            if (match) matches[ref] = match;
          } catch { incomplete = true; }
        }
      };
      await Promise.all(Array.from({ length: Math.min(4, refs.length) }, worker));
      if (!controller.signal.aborted) {
        setTargetMatches(matches);
        setSearchStatus(incomplete ? "部分规则目标无法读取，可重新搜索" : "");
      }
    }, 250);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [normalizedQuery, value]);
  const findTarget = (row: Dict) => (Array.isArray(row.targets) ? row.targets as string[] : [])
    .find(target => target.toLowerCase().includes(normalizedQuery)) ?? targetMatches[String(row.targets_ref ?? "")];
  const filteredRows = value.map((row, index) => ({ row, index })).filter(({row}) =>
    !normalizedQuery || !!findTarget(row) || String(row.targets_preview ?? "").toLowerCase().includes(normalizedQuery));
  const move = (i: number, step: number) => {
    const rows = [...value];
    [rows[i], rows[i + step]] = [rows[i + step], rows[i]];
    onChange(rows);
  };
  return (
    <div className="rules-table-editor">
      <ErrorLine error={targetError} />
      <div className="toolbar rules-search-toolbar">
        <Button onClick={() => setEditing({ index: -1, row: freshRule() })}>
          新增规则
        </Button>
        <input type="search" aria-label="搜索规则目标" placeholder="搜索域名或 IP" value={query} onChange={event => setQuery(event.target.value)} />
        {searchStatus && <span role="status" className="small muted">{searchStatus}</span>}
      </div>
      <Table
        rows={filteredRows.map(({row}) => row)}
        rowKey={(row) => String(value.indexOf(row))}
        paginationResetKey={normalizedQuery}
        keepHeaders
        columns={[
          { key: "order", label: "序号", render: (_, i) => filteredRows[i].index + 1 },
          {
            key: "enabled",
            label: "启用",
            render: (r, i) => (
              <input
                type="checkbox"
                checked={r.enabled !== false}
                onChange={(e) =>
                  onChange(
                    value.map((v, n) =>
                      n === filteredRows[i].index
                        ? { ...v, enabled: e.target.checked }
                        : v,
                    ),
                  )
                }
              />
            ),
          },
          {
            key: "name",
            label: "名称",
            render: (r) => String(r.name || "未命名规则"),
          },
          {
            key: "action",
            label: "动作",
            render: (r) =>
              ({ direct: "直连", proxy: "代理", block: "阻断" })[
                String(r.action)
              ] || "直连",
          },
          {
            key: "type",
            label: "类型",
            render: (r) => (r.type === "ip" ? "IP / CIDR" : "域名"),
          },
          {
            key: "targets",
            label: "目标",
            render: (r, i) => (
              <button
                className="rule-target-link"
                type="button"
                onClick={async () => {
                  try {
                    setTargetsView({
                      targets: await loadTargets(r),
                      index: filteredRows[i].index,
                    });
                    setTargetError("");
                  } catch (error) {
                    setTargetError(errorMessage(error));
                  }
                }}
              >
                <Globe size={14} />
                <span>
                  {String(
                    (normalizedQuery && findTarget(r)) || (Array.isArray(r.targets) && r.targets[0]) ||
                      r.targets_preview ||
                      "查看目标",
                  )}
                  {Number(
                    r.targets_count || (r.targets as unknown[])?.length || 0,
                  ) > 1
                    ? "…"
                    : ""}
                </span>
              </button>
            ),
          },
          {
            key: "port",
            label: "端口",
            render: (r) => String(r.port || "全部"),
          },
          {
            key: "network",
            label: "协议",
            render: (r) => String(r.network || "tcp,udp").toUpperCase(),
          },
          {
            key: "actions",
            label: "操作",
            render: (r, i) => (
              <div className="actions">
                <Button onClick={() => void editRule(filteredRows[i].index, r)}>
                  编辑
                </Button>
                <Button
                  disabled={filteredRows[i].index === 0}
                  onClick={() => move(filteredRows[i].index, -1)}
                >
                  上移
                </Button>
                <Button
                  disabled={filteredRows[i].index === value.length - 1}
                  onClick={() => move(filteredRows[i].index, 1)}
                >
                  下移
                </Button>
                <Button
                  onClick={() =>
                    setPosition({ index: filteredRows[i].index, value: filteredRows[i].index + 1 })
                  }
                >
                  优先级
                </Button>
                <Button onClick={() => setDeleting(filteredRows[i].index)}>删除</Button>
              </div>
            ),
          },
        ]}
      />
      {position && (
        <Modal title="设置规则优先级" onClose={() => setPosition(null)}>
          <Field label={`规则位置（1～${value.length}，数字越小优先）`}>
            <NumberInput
              min={1}
              max={value.length}
              value={position.value}
              onChange={(e) =>
                setPosition({ ...position, value: Number(e.target.value) })
              }
            />
          </Field>
          <div className="modal-actions"><Button
            variant="primary"
            disabled={
              !Number.isInteger(position.value) ||
              position.value < 1 ||
              position.value > value.length
            }
            onClick={() => {
              const rows = [...value];
              const [row] = rows.splice(position.index, 1);
              rows.splice(position.value - 1, 0, row);
              onChange(rows);
              setPosition(null);
            }}
          >
            保存到草稿
          </Button></div>
        </Modal>
      )}
      {targetsView && (
        <Modal
          title={`IP & 域名 · 共 ${targetsView.targets.length} 个`}
          onClose={() => setTargetsView(null)}
          wide
        >
          {targetsView.edit ? (
            <>
              <textarea
                className="rule-target-editor"
                aria-label="编辑IP与域名"
                rows={14}
                value={targetsView.text}
                onChange={(e) =>
                  setTargetsView({ ...targetsView, text: e.target.value })
                }
              />
              <div className="modal-actions">
                <Button
                  onClick={() =>
                    setTargetsView({
                      ...targetsView,
                      edit: false,
                      text: undefined,
                    })
                  }
                >
                  取消编辑
                </Button>
                <Button
                  variant="primary"
                  onClick={() => {
                    const targets = (targetsView.text || "")
                      .split(/\s+/)
                      .filter(Boolean);
                    if (!targets.length || targets.length > 500) {
                      setTargetError("每条规则须包含1～500个目标");
                      return;
                    }
                    const original = value[targetsView.index];
                    if (!original) {
                      setTargetError("规则已变更，请重新打开");
                      return;
                    }
                    const { targets_ref, targets_count, ...row } = original;
                    onChange(
                      value.map((r, i) =>
                        i === targetsView.index ? { ...row, targets } : r,
                      ),
                    );
                    setTargetsView({
                      ...targetsView,
                      targets,
                      edit: false,
                      text: undefined,
                    });
                    setTargetError("");
                  }}
                >
                  保存到草稿
                </Button>
              </div>
            </>
          ) : (
            <>
              <pre className="rule-target-full">{targetPage.items.join("\n")}</pre>
              <Pagination value={targetPage} />
              <div className="modal-actions">
                <Button onClick={() => setTargetsView({ ...targetsView, edit: true, text: targetsView.targets.join("\n") })}>
                  编辑 IP &amp; 域名
                </Button>
              </div>
            </>
          )}
          <ErrorLine error={targetError} />
        </Modal>
      )}
      {editing && (
        <Modal
          title={editing.index < 0 ? "新增规则" : "编辑规则"}
          onClose={() => setEditing(null)}
          wide
        >
          <div className="form-grid">
            {["name", "action", "type", "targets", "port", "network"].map(
              (k) => {
                const labels: Record<string, string> = {
                  name: "名称",
                  action: "动作",
                  type: "目标类型",
                  targets: "目标（每行一个）",
                  port: "端口（留空为全部）",
                  network: "传输协议",
                };
                const set = (v: unknown) =>
                  setEditing({ ...editing, row: { ...editing.row, [k]: v } });
                return (
                  <Field label={labels[k]} key={k}>
                    {k === "targets" ? (
                      <textarea
                        rows={5}
                        value={String(
                          editing.row.targets_text ??
                            listText(editing.row[k], "\n"),
                        )}
                        onChange={(e) =>
                          setEditing({
                            ...editing,
                            row: {
                              ...editing.row,
                              targets_text: e.target.value,
                            },
                          })
                        }
                      />
                    ) : k === "action" || k === "type" || k === "network" ? (
                      <select
                        value={String(editing.row[k])}
                        onChange={(e) => set(e.target.value)}
                      >
                        {(k === "action"
                          ? [
                              ["direct", "直连（白名单）"],
                              ["proxy", "代理"],
                              ["block", "阻断"],
                            ]
                          : k === "type"
                            ? [
                                ["domain", "域名及子域名"],
                                ["ip", "IP / CIDR"],
                              ]
                            : [
                                ["tcp,udp", "TCP + UDP"],
                                ["tcp", "TCP"],
                                ["udp", "UDP"],
                              ]
                        ).map(([v, l]) => (
                          <option key={v} value={v}>
                            {l}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <input
                        value={String(editing.row[k] || "")}
                        onChange={(e) => set(e.target.value)}
                      />
                    )}
                  </Field>
                );
              },
            )}
          </div>
          <div className="modal-actions">
            <Button onClick={() => setEditing(null)}>取消</Button>
            <Button
              variant="primary"
              disabled={
                !String(
                  editing.row.targets_text ?? listText(editing.row.targets),
                ).trim()
              }
              onClick={() => {
                const { targets_text, ...draft } = editing.row;
                const row = {
                  ...draft,
                  targets: String(targets_text ?? listText(draft.targets, "\n"))
                    .split(/\s+/)
                    .filter(Boolean),
                };
                onChange(
                  editing.index < 0
                    ? [...value, row]
                    : value.map((r, i) => (i === editing.index ? row : r)),
                );
                setEditing(null);
              }}
            >
              保存到草稿
            </Button>
          </div>
        </Modal>
      )}
      {deleting !== null && (
        <Confirm
          title="删除规则"
          message={String(value[deleting]?.name || "未命名规则")}
          onClose={() => setDeleting(null)}
          onConfirm={() => {
            onChange(value.filter((_, i) => i !== deleting));
            setDeleting(null);
          }}
        />
      )}
    </div>
  );
}
function listText(v: unknown, separator = "、") {
  return Array.isArray(v) ? v.join(separator) : "";
}
export function ruleSetPresets(): Dict[] {
  return [
    ["bypass_cn", "大陆白名单"],
    ["gfwlist", "GFWList 分流"],
    ["global", "全局代理"],
  ].map(([id, name]) => ({
    id: "preset_" + id,
    name,
    profile: id,
    predefined: true,
    source:
      id === "global"
        ? ""
        : `https://raw.githubusercontent.com/Loyalsoldier/v2ray-rules-dat/release/${id === "bypass_cn" ? "direct" : "proxy"}-list.txt`,
    source_format: "domain-list",
    auto_update: false,
    interval_hours: 24,
    rules:
      id === "bypass_cn"
        ? [{ ...freshRule(), name: "中国域名直连", targets: ["cn"] }]
        : id === "gfwlist"
          ? [
              {
                ...freshRule(),
                name: "常用境外服务",
                action: "proxy",
                targets: ["google.com", "youtube.com", "github.com"],
              },
            ]
          : [],
  }));
}
export function RuleSetsEditor({
  value,
  active,
  onChange,
  legacy,
  profile,
  act,
  saved,
}: {
  value: Dict[];
  active: string;
  onChange: (sets: Dict[], active: string) => void;
  legacy: Dict[];
  profile: string;
  act: Act;
  saved: Dict[];
}) {
  const defaults: Dict[] = [
    {
      id: "current",
      name: "当前规则",
      profile,
      rules: legacy,
      source: "",
      auto_update: false,
      interval_hours: 24,
    },
  ];
  const sets = value.length ? value : defaults,
    activeID = active || String(sets[0]?.id || "current");
  const [selected, setSelected] = useState(""),
    [edit, setEdit] = useState<Dict | null>(null),
    [remove, setRemove] = useState<string | null>(null),
    [updating, setUpdating] = useState<string | null>(null),
    [presets, setPresets] = useState(false);
  const chosen = sets.find((r) => r.id === selected);
  const change = (id: unknown, k: string, v: unknown) =>
    onChange(
      sets.map((r) => (r.id === id ? { ...r, [k]: v } : r)),
      activeID,
    );
  return (
    <details className="custom-routing settings-section">
      <summary>
        白名单与规则集 · 活动：
        {String(sets.find((r) => r.id === activeID)?.name || "当前规则")}
        <SettingInfo
          label="分流规则使用"
          text={
            <>
              规则从上到下匹配已接管的 TCP/UDP；VPN、私网、DNS
              与入口直连始终优先排除。修改先保存到草稿，再点击保存并应用。规则源支持公共域名列表和
              JSON；预定义 URL 由上游开源项目维护，FoXray
              仅使用。“新增规则集”创建空白自定义规则；“从内置模板添加”选择大陆白名单、GFWList
              或全局代理等模板，已存在的模板只定位查看，不重复添加，也不会立即激活。自动更新会替换该规则集中的规则，请将本地专用规则放入独立自定义规则集。
            </>
          }
        />
      </summary>

      <div className="management-toolbar">
        <Button
          onClick={() =>
            setEdit({
              id: newSetID(),
              name: "自定义规则集",
              profile: "bypass_cn",
              rules: [],
              source: "",
              auto_update: false,
              interval_hours: 24,
            })
          }
        >
          新增规则集
        </Button>
        <Button onClick={() => setPresets(true)}>从内置模板添加</Button>
        <span className="table-width-slot" data-table-width-slot="xray-rule-sets" />
      </div>
      <div className="rule-set-list">
        <Table
          rows={sets}
          columnSizingKey="xray-rule-sets"
          rowKey={(r) => String(r.id)}
          columns={[
            { key: "order", label: "序号", render: (_, i) => i + 1 },
            {
              key: "active",
              label: "活动",
              render: (r) => (
                <input
                  type="radio"
                  name="active-rule-set"
                  aria-label={"设为活动规则集 " + r.name}
                  checked={r.id === activeID}
                  onChange={() => onChange(sets, String(r.id))}
                />
              ),
            },
            {
              key: "name",
              label: "规则集",
              render: (r) => (
                <button
                  type="button"
                  className="rule-set-name"
                  title="查看规则明细"
                  onClick={() => setSelected(String(r.id))}
                >
                  {String(r.name)}
                </button>
              ),
            },
            {
              key: "kind",
              label: "类型",
              render: (r) => (r.predefined ? "预定义" : "自定义"),
            },
            {
              key: "count",
              label: "自定义规则数",
              render: (r) => list(r.rules).length,
            },
            {
              key: "mode",
              label: "基础模式",
              render: (r) =>
                ({
                  bypass_cn: "大陆白名单",
                  gfwlist: "GFWList",
                  global: "全局代理",
                })[String(r.profile)] || "大陆白名单",
            },
            {
              key: "auto",
              label: "更新方式",
              render: (r) =>
                r.auto_update ? `${r.interval_hours} 小时自动更新` : "手动更新",
            },
            {
              key: "updated",
              label: "上次更新",
              render: (r) => timestamp(r.updated),
            },
            {
              key: "actions",
              label: "操作",
              render: (r) => (
                <div className="actions">
                  <Button
                    onClick={() => {
                      setSelected(String(r.id));
                    }}
                  >
                    编辑
                  </Button>
                  <Button
                    disabled={
                      !r.source ||
                      !saved.some(
                        (s) => s.id === r.id && stableJSON(s) === stableJSON(r),
                      )
                    }
                    title={
                      !r.source
                        ? "请先配置规则源 URL"
                        : "有未保存修改时，请先保存并应用；更新会替换自定义优先规则"
                    }
                    onClick={() => setUpdating(String(r.id))}
                  >
                    更新
                  </Button>
                  <Button
                    disabled={r.id === activeID}
                    onClick={() => setRemove(String(r.id))}
                  >
                    删除
                  </Button>
                </div>
              ),
            },
          ]}
        />
      </div>
      {chosen && (
        <Modal
          description={
            <>
              <p>
                基础模式：
                {chosen.profile === "global"
                  ? "全局代理：已接管的公网 TCP/UDP 通过所选节点转发。"
                  : chosen.profile === "gfwlist"
                    ? "GFWList：命中代理域名走代理，直连域名及未匹配流量直连。"
                    : "大陆白名单：已标记为直连的流量优先排除，其余已接管的公网 TCP/UDP 走代理。"}
              </p>
              <p>
                VPN、私网、DNS和节点入口优先排除。下表列出可编辑的自定义优先规则；这些规则先于基础模式匹配，未匹配时使用基础模式。
              </p>
            </>
          }
          title={String(chosen.name) + " · 规则明细"}
          onClose={() => setSelected("")}
          wide
        >
          <div className="rule-set-description">
            <p className="small muted">
              {chosen.source
                ? "规则源：" + String(chosen.source)
                : "本地规则集：手动编辑，无远程规则源。"}{" "}
              ·{" "}
              {chosen.auto_update
                ? String(chosen.interval_hours) + " 小时自动更新"
                : "手动更新"}{" "}
              · 上次更新：{timestamp(chosen.updated)}
            </p>
            <div className="actions">
              <Button onClick={() => setEdit(structuredClone(chosen))}>
                配置规则集
              </Button>
              <span className="small muted">
                修改规则后先保存并应用，再使用更新按钮；远程更新替换下表规则，基础模式保持不变。
              </span>
            </div>
          </div>
          <BuiltinRuleTable
            key={String(chosen.id) + chosen.profile}
            profile={String(chosen.profile)}
          />
          <h4>自定义优先规则（可编辑）</h4>
          <CustomRulesEditor
            value={list(chosen.rules)}
            onChange={(v) => change(chosen.id, "rules", v)}
          />
        </Modal>
      )}
      {presets && (
        <Modal
          description={
            <>
              <p>选择模板后可查看、编辑规则及更新地址，保存并应用后生效。</p>
            </>
          }
          title="从内置模板添加"
          wide
          onClose={() => setPresets(false)}
        >
          <Table
            rows={ruleSetPresets()}
            rowKey={(r) => String(r.id)}
            columns={[
              { key: "name", label: "规则集", render: (r) => String(r.name) },
              {
                key: "description",
                label: "基础行为",
                render: (r) =>
                  r.profile === "global"
                    ? "公网流量代理"
                    : r.profile === "gfwlist"
                      ? "按 GFWList 域名分流"
                      : "直连标记优先排除，其余代理",
              },
              {
                key: "actions",
                label: "操作",
                render: (r) => (
                  <Button
                    onClick={() => {
                      if (!sets.some((v) => v.id === r.id))
                        onChange([...sets, r], activeID);
                      setSelected(String(r.id));
                      setPresets(false);
                    }}
                  >
                    {sets.some((v) => v.id === r.id)
                      ? "查看已添加规则"
                      : "添加并查看"}
                  </Button>
                ),
              },
            ]}
          />
        </Modal>
      )}
      {edit && (
        <Modal title="规则集配置" wide onClose={() => setEdit(null)}>
          <div className="form-grid">
            <Field label="名称">
              <input
                value={String(edit.name)}
                onChange={(e) => setEdit({ ...edit, name: e.target.value })}
              />
            </Field>
            <Field label="基础分流模式">
              <select
                value={String(edit.profile)}
                onChange={(e) => setEdit({ ...edit, profile: e.target.value })}
              >
                <option value="bypass_cn">大陆白名单</option>
                <option value="gfwlist">GFWList</option>
                <option value="global">全局代理</option>
              </select>
            </Field>
            <Field label="规则源 URL" hint="可选">
              <input
                type="url"
                value={String(edit.source || "")}
                onChange={(e) => setEdit({ ...edit, source: e.target.value })}
              />
            </Field>
            <Field label="规则源格式">
              <select
                value={String(edit.source_format || "json")}
                onChange={(e) =>
                  setEdit({ ...edit, source_format: e.target.value })
                }
              >
                <option value="json">JSON规则集</option>
                <option value="domain-list">公共域名列表</option>
              </select>
            </Field>
            <Field label="更新间隔（小时，1～168）">
              <NumberInput
                type="number"
                min={1}
                max={168}
                value={Number(edit.interval_hours || 24)}
                onChange={(e) =>
                  setEdit({ ...edit, interval_hours: Number(e.target.value) })
                }
              />
            </Field>
          </div>
          <label className="check">
            <input
              type="checkbox"
              checked={!!edit.auto_update}
              onChange={(e) =>
                setEdit({ ...edit, auto_update: e.target.checked })
              }
            />
            自动更新规则
          </label>
          <div className="modal-actions">
            <Button onClick={() => setEdit(null)}>取消</Button>
            <Button
              variant="primary"
              disabled={!edit.name}
              onClick={() => {
                onChange(
                  sets.some((r) => r.id === edit.id)
                    ? sets.map((r) => (r.id === edit.id ? edit : r))
                    : [...sets, edit],
                  activeID,
                );
                setSelected(String(edit.id));
                setEdit(null);
              }}
            >
              保存到草稿
            </Button>
          </div>
        </Modal>
      )}
      {remove && (
        <Confirm
          title="删除规则集"
          message="仅删除此规则集；活动规则集不能删除。"
          onClose={() => setRemove(null)}
          onConfirm={() => {
            onChange(
              sets.filter((r) => r.id !== remove),
              activeID,
            );
            setRemove(null);
          }}
        />
      )}
      {updating && (
        <Confirm
          title="更新规则集"
          message="下载并校验规则源，成功后替换此规则集的规则；如果它是活动规则集，将重新应用代理规则，已有连接可能短暂中断。"
          onClose={() => setUpdating(null)}
          onConfirm={() => {
            void act("/api/rulesets/update", { id: updating });
            setUpdating(null);
          }}
        />
      )}
    </details>
  );
}

function stableJSON(value: unknown): string {
  return JSON.stringify(value);
}

function BuiltinRuleTable({ profile }: { profile: string }) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [page, setPage] = useState(1),
    [pageSize, setPageSize] = useState(50);
  return (
    <details
      className="builtin-rules"
      open={open}
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary>
        查看基础模式规则（系统规则，只读）
        <SettingInfo
          label="基础分流匹配"
          text={
            <>
              基础模式规则在自定义优先规则之后匹配；VPN、私网、DNS和节点入口优先排除。需要改变某个目标的动作时，在自定义优先规则中添加覆盖。
            </>
          }
        />
      </summary>
      {open && (
        <BuiltinRuleRows
          profile={profile}
          query={query}
          setQuery={(v) => {
            setQuery(v);
            setPage(1);
          }}
          page={page}
          pageSize={pageSize}
          setPage={setPage}
          setPageSize={size => { setPageSize(size ?? 50); setPage(1); }}
        />
      )}
    </details>
  );
}
function BuiltinRuleRows({
  profile,
  query,
  setQuery,
  page,
  pageSize,
  setPage,
  setPageSize,
}: {
  profile: string;
  query: string;
  setQuery: (v: string) => void;
  page: number;
  pageSize: number;
  setPage: (v: number) => void;
  setPageSize: (v: number | null) => void;
}) {
  const offset = (page - 1) * pageSize;
  const r = useResource<Dict>(
    `/api/rulesets/builtin?profile=${encodeURIComponent(profile)}&query=${encodeURIComponent(query)}&offset=${offset}&limit=${pageSize}`,
    60000, true, true,
  );
  const total = Number(r.data?.total || 0);
  const displayedSize = Number(r.data?.limit || pageSize);
  const displayedOffset = Number(r.data?.offset || 0);
  const pageCount = Math.max(1, Math.ceil(total / displayedSize));
  const current = Math.min(Math.floor(displayedOffset / displayedSize) + 1, pageCount);
  return (
    <>
      <div className="toolbar rules-search-toolbar">
        <span className="table-width-slot" data-table-width-slot="builtin-rules" />
        <input type="search" aria-label="搜索基础规则" placeholder="搜索基础规则目标"
          value={query} onChange={event => setQuery(event.target.value)} />
      </div>
      <ErrorLine error={r.error || String(r.data?.error || "")} />
      <div className="builtin-rules-results" data-multipage={pageCount > 1 || undefined} aria-busy={r.loading}>
      <Table columnSizingKey="builtin-rules" paginate={false} keepHeaders
        rows={list(r.data?.items)} rowKey={row => String(row.type) + row.target}
        columns={[
          { key: "type", label: "匹配类型", render: row => String(row.type) },
          { key: "target", label: "目标", render: row => <span className="rules-builtin-target" title={String(row.target)}>{String(row.target)}</span> },
          { key: "action", label: "动作", render: row => row.action === "direct" ? "直连" : "代理" },
        ]}
      />
      </div>
      <Pagination value={{ automatic: false, autoPageSize: undefined, items: [], page: current, pageSize: displayedSize,
        pageCount, start: displayedOffset, end: Math.min(total, displayedOffset + displayedSize), count: total,
        setPage, setPageSize }} />
    </>
  );
}
