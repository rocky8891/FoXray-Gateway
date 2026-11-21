import { routeTableId, routeTableLabel } from "./network_display";
import "./network_interaction.css";
import { NetworkBatchDialog } from "./network_batch_dialog";
import { AdaptiveSearch } from "./adaptive_search";
import { Layers, ShieldCheck, ListChecks, Globe } from "lucide-react";
import { ResetFilters } from "./list_controls";
import { ExportButton, useRowSelection } from "./resource_controls";
import { SelectionActions } from "./selection_actions";
import { CompactFilters, ListFilter } from "./list_controls";
import { SortHeading } from "./list_controls";
import { useMemo, useState } from "react";
import { AlertTriangle, Plus, RefreshCw, Search } from "lucide-react";
import { api, errorMessage, list, text, type Dict, type Plan } from "./api";
import {
  Button,
  ErrorLine,
  Field,
  JsonDetails,
  Modal,
  PageHeader,
  Panel,
  Table,
  useResource,
  type Column,
} from "./ui";
import type { Act } from "./nodes";
type Kind = "routes" | "rules";
const enabled = (r: Dict) => r.enabled !== false;
const fieldValue = (r: Dict, key: string) =>
  key === "table" ? routeTableId(r.table) : key === "from"
    ? r.from || r.src || "all"
    : key === "to"
      ? r.to || r.dst || "all"
      : r[key];
export function RoutingPage({
  kind,
  act,
  initialFilter,
  onTable,
}: {
  kind: Kind;
  act: Act;
  initialFilter?: { table: string; family: string };
  onTable?: (table: string, family: string) => void;
}) {
  const route = kind === "routes",
    title = route ? "路由" : "策略路由";
  const resource = useResource<Dict>("/api/network/" + kind, 10000);
  const [family, setFamily] = useState(initialFilter?.family || "4"),
    [owner, setOwner] = useState("all"),
    [status, setStatus] = useState("all"),
    [table, setTable] = useState(initialFilter?.table || "all"),
    [query, setQuery] = useState(""),
    [sort, setSort] = useState({
      key: route ? "dst" : "priority",
      direction: 1,
    });
  const [form, setForm] = useState<Dict | null>(null),
    [op, setOp] = useState("add"),
    [ack, setAck] = useState(false),
    [failure, setFailure] = useState(""),
    [plan, setPlan] = useState<Plan | null>(null),
    [details, setDetails] = useState<unknown>(null);
  const managed = list(resource.data?.managed);
  const all = useMemo(() => {
    const entries: Dict[] = [
      ...list(resource.data?.ipv4).map((r) => ({ ...r, family: 4 })),
      ...list(resource.data?.ipv6).map((r) => ({ ...r, family: 6 })),
    ];
    const used = new Set<string>();
    const result: Dict[] = entries
      .map((r) =>
        route
          ? {
              ...r,
              dst: r.dst || "default",
              table: r.table ?? 254,
              metric: r.metric ?? 0,
              type: r.type || "unicast",
            }
          : r,
      )
      .filter(() => true)
      .map((r) => {
        const match = managed.find(
          (m) =>
            enabled(m) &&
            Number(m.family || 4) === r.family &&
            (route
              ? String(m.dst || "default") === String(r.dst || "default") &&
                String(m.dev || "") === String(r.dev || "") &&
                String(m.gateway || "") === String(r.gateway || "") &&
                Number(m.metric || 0) === Number(r.metric || 0) &&
                routeTableId(m.table) === routeTableId(r.table)
              : Number(m.priority) === Number(r.priority)),
        );
        if (match) {
          used.add(String(match.id));
          return { ...r, ...match, managed: true, runtime: true };
        }
        return { ...r, managed: false, runtime: true, enabled: true };
      });
    for (const m of managed) {
      if (!used.has(String(m.id)))
        result.push({
          ...m,
          managed: true,
          runtime: false,
          enabled: enabled(m),
        });
    }
    return result;
  }, [resource.data, managed, route]);
  const tables = [
    ...new Set(
      all
        .filter((r) => String(r.family || 4) === family)
        .map((r) => routeTableId(r.table)),
    ),
  ].sort((a, b) => a.localeCompare(b, undefined, { numeric: true }));
  const scopeRows = all.filter(r => String(r.family || 4) === family &&
    (table === "all" || routeTableId(r.table) === routeTableId(table)) &&
    [JSON.stringify(r),routeTableLabel(r.table)].join(" ").toLowerCase().includes(query.toLowerCase()));
  const ready = !!resource.data && !resource.error && resource.data.complete !== false;
  const rows = scopeRows.filter(r => (owner === "all" || !!r.managed === (owner === "managed")) &&
    (status === "all" || enabled(r) === (status === "enabled")))
    .sort((a, b) => {
      const x = fieldValue(a, sort.key),
        y = fieldValue(b, sort.key);
      return (
        (typeof x === "number" && typeof y === "number"
          ? x - y
          : text(x).localeCompare(text(y), "zh-CN", { numeric: true })) *
        sort.direction
      );
    });
  const [bulk, setBulk] = useState<{ operation: string; rows: Dict[] } | null>(
    null,
  );
  const selection = useRowSelection(rows, (r) =>
    String(r.id || JSON.stringify(r)),
  );
  const changeFamily = (next: string) => { setFamily(next); setTable("all"); selection.setSelected(new Set()); };
  const chooseStat = (nextOwner: string, nextStatus: string) => {
    const same = owner === nextOwner && status === nextStatus;
    setOwner(same ? "all" : nextOwner); setStatus(same ? "all" : nextStatus);
    selection.setSelected(new Set());
  };
  const begin = (operation: string, item: Dict) => {
    setOp(operation);
    setForm({ ...item });
    setAck(false);
    setFailure("");
  };
  const preview = async (operation: string, item: Dict) => {
    try {
      const p = await api<Plan>("/api/network/plan", {
        kind,
        operation,
        item,
        allow_protected: ack,
      });
      setPlan(p);
      setForm(null);
      setFailure("");
    } catch (e) {
      setFailure(errorMessage(e));
    }
  };
  const filterControls = {
    family: (
      <Field label="地址族">
        <select
          value={family}
          onChange={(e) => {
            changeFamily(e.target.value);
          }}
        >
          <option value="4">IPv4</option>
          <option value="6">IPv6</option>
        </select>
      </Field>
    ),
    owner: (
      <Field label="归属">
        <select value={owner} onChange={(e) => setOwner(e.target.value)}>
          <option value="all">全部来源</option>
          <option value="managed">FoXray 管理</option>
          <option value="external">系统／外部</option>
        </select>
      </Field>
    ),
    table: (
      <Field label="路由表">
        <select value={table} onChange={(e) => setTable(e.target.value)}>
          <option value="all">全部路由表</option>
          {tables.map((t) => (
            <option key={t} value={t}>{routeTableLabel(t)}</option>
          ))}
        </select>
      </Field>
    ),
    status: (
      <Field label="状态">
        <select value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="all">全部状态</option>
          <option value="enabled">已启用</option>
          <option value="disabled">已停用</option>
        </select>
      </Field>
    ),
  };
  const headerFilter = (key: keyof typeof filterControls) => (
    <ListFilter
      label={
        { family: "地址族", owner: "归属", table: "路由表", status: "状态" }[
          key
        ] + "筛选"
      }
      column
      active={
        {
          family: family !== "4",
          owner: owner !== "all",
          table: table !== "all",
          status: status !== "all",
        }[key]
      }
    >
      {filterControls[key]}
      <Button
        onClick={() => {
          if (key === "family") {
            setFamily("4");
            setTable("all");
          } else if (key === "owner") setOwner("all");
          else if (key === "table") setTable("all");
          else setStatus("all");
        }}
      >
        重置
      </Button>
    </ListFilter>
  );
  const sortHeading = (key: string, label: string) => (
    <SortHeading
      label={label}
      direction={sort.key === key ? sort.direction : undefined}
      onClick={() =>
        setSort({ key, direction: sort.key === key ? -sort.direction : 1 })
      }
    />
  );
  const col = (key: string, label: string): Column<Dict> => ({
    key,
    label,
    heading: (
      <span className="table-heading-tools">
        {sortHeading(key, label)}
        {key === "table"
          ? headerFilter("table")
          : ["dst", "priority"].includes(key)
            ? headerFilter("family")
            : null}
      </span>
    ),
    render: (r) => key === "table" ? (
      onTable ? <button className="table-link" title={"查看路由表 " + routeTableLabel(r.table)} onClick={() => onTable(routeTableId(r.table), String(r.family || family))}>{routeTableLabel(r.table)}</button>
      : <span>{routeTableLabel(r.table)}</span>
    ) : <span className="ellipsis" title={text(fieldValue(r,key))}>{text(fieldValue(r,key))}</span>,
  });
  const columns: Column<Dict>[] = [
    selection.column,
    { key: "serial", label: "序号", render: (_, i) => i + 1 },
    ...(route
      ? [
          col("dst", "目标网段"),
          col("gateway", "下一跳"),
          col("dev", "接口"),
          col("table", "路由表"),
          col("metric", "Metric"),
          col("type", "类型"),
        ]
      : [
          col("priority", "优先级"),
          col("from", "来源"),
          col("to", "目标"),
          col("fwmark", "标记／掩码"),
          col("table", "路由表"),
        ]),
    {
      key: "state",
      label: "状态",
      heading: (
        <span className="table-heading-tools">
          {sortHeading("enabled", "状态")}
          {headerFilter("status")}
        </span>
      ),
      render: (r) =>
        r.managed ? (
          <span
            className={!enabled(r) ? "muted" : r.runtime ? "accent" : "fair"}
          >
            {!enabled(r) ? "已停用" : r.runtime ? "已生效" : "状态偏差"}
          </span>
        ) : (
          <span className="muted">系统生效</span>
        ),
    },
    {
      key: "owner",
      label: "归属",
      heading: (
        <span className="table-heading-tools">
          {sortHeading("managed", "归属")}
          {headerFilter("owner")}
        </span>
      ),
      render: (r) => (
        <>
          {r.managed ? "FoXray" : "系统／外部"}
          <small>{text(r.protocol || "")}</small>
        </>
      ),
    },
    {
      key: "actions",
      label: "操作",
      render: (r) => (
        <div className="actions">
          <Button onClick={() => setDetails(r)}>详情</Button>
          <Button
            disabled={!r.managed}
            title={
              !r.managed
                ? "先纳管独立规则；系统与 VPN 保留条目不可编辑"
                : undefined
            }
            onClick={() => begin("replace", r)}
          >
            编辑
          </Button>
          <Button
            disabled={!r.managed}
            onClick={() => begin("replace", { ...r, enabled: !enabled(r) })}
          >
            {enabled(r) ? "停用" : "启用"}
          </Button>
          <Button
            variant="danger"
            disabled={!r.managed}
            title={!r.managed ? "系统与外部条目不可直接删除" : undefined}
            onClick={() => begin("delete", r)}
          >
            删除
          </Button>
          <Button
            disabled={
              !!r.managed ||
              !(route
                ? ["static", "boot"].includes(String(r.protocol)) &&
                  !String(r.dev || "").startsWith("wg-") &&
                  !["vone0", "CloudflareWARP", "vpm-tun"].includes(
                    String(r.dev),
                  )
                : Number(r.priority) > 0 &&
                  Number(r.priority) < 32766 &&
                  !(Number(r.priority) >= 16060 && Number(r.priority) <= 16100))
            }
            title="仅纳管独立静态条目；系统、VPN 和保留标记不可接管"
            onClick={() => begin("adopt", r)}
          >
            纳管
          </Button>
        </div>
      ),
    },
  ];
  const initial: Dict = route
    ? {
        family: Number(family),
        dst: "",
        gateway: "",
        dev: "",
        table: 254,
        metric: 0,
        type: "unicast",
        enabled: true,
      }
    : {
        family: Number(family),
        priority: 2000,
        from: "all",
        to: "all",
        fwmark: "",
        table: 2000,
        enabled: true,
      };
  return (
    <>
      <Panel className="routing-panel management-panel">
        <PageHeader
          title={route && table !== "all" ? "路由表 · " + routeTableLabel(table) : title}
          description={
            <>
              {route
                ? "查看完整路由与来源 · 支持 IPv4／IPv6、持久化与单条回退"
                : "按优先级匹配来源、目标、标记和接口 · 保留系统与 VPN 策略"}
              <p>
                {route
                  ? "内核、DHCP 和 VPN 自动路由保持原归属；可明确接管独立静态路由。"
                  : "系统策略只读；新建策略检查优先级冲突，并保护代理与 VPN 保留标记。"}{" "}
                更改先预览，应用后 120 秒内确认，否则独立回退。
              </p>
              <p>点击列标题排序。</p>
            </>
          }
        />
        <div className="network-stat-grid" aria-label={title+"统计与地址族"}>
          {[
            {id:"external",label:"系统／外部条目",count:scopeRows.filter(r=>!r.managed).length,owner:"external",status:"all",hint:"只读 · 点击筛选",icon:<Layers size={19}/>},
            {id:"managed",label:"FoXray 管理",count:scopeRows.filter(r=>r.managed).length,owner:"managed",status:"all",hint:"点击筛选管理条目",icon:<ShieldCheck size={19}/>},
            {id:"enabled",label:"已启用管理条目",count:scopeRows.filter(r=>r.managed&&enabled(r)).length,owner:"managed",status:"enabled",hint:"按管理开关统计",icon:<ListChecks size={19}/>},
          ].map(stat=><button type="button" key={stat.id} className={"network-stat-card "+stat.id} disabled={!ready} aria-label={"筛选"+title+"的"+stat.label} aria-pressed={owner===stat.owner&&status===stat.status} onClick={()=>chooseStat(stat.owner,stat.status)}>{stat.icon}<strong>{ready?stat.count:"—"}</strong><span>{stat.label}</span><small>{stat.hint} · IPv{family}</small></button>)}
          <div className="network-stat-card network-family-card"><Globe size={19}/><strong>IPv{family}</strong><span>地址族</span><div className="network-family-switch" role="group" aria-label={title+"地址族切换"}>{["4","6"].map(value=><button type="button" key={value} aria-pressed={family===value} onClick={()=>changeFamily(value)}>IPv{value}</button>)}</div></div>
        </div>

        <CompactFilters
          before={
            <>
              <Button onClick={() => begin("add", initial)}>
                <Plus size={15} />
                添加
              </Button>
              <Button onClick={() => void resource.refresh()}>
                <RefreshCw size={15} />
                刷新
              </Button>

              <SelectionActions
                bulk={[
                  {
                    label: "启用",
                    disabled:
                      !selection.chosen.length ||
                      selection.chosen.length > 50 ||
                      selection.chosen.some((r) => !r.managed) ||
                      selection.chosen.every(enabled),
                    title: "仅支持 FoXray 管理的条目；系统与外部条目需先纳管",
                    onClick: () =>
                      setBulk({ operation: "start", rows: selection.chosen }),
                  },
                  {
                    label: "停用",
                    disabled:
                      !selection.chosen.length ||
                      selection.chosen.length > 50 ||
                      selection.chosen.some((r) => !r.managed) ||
                      !selection.chosen.some(enabled),
                    title: "仅支持 FoXray 管理的条目；系统与外部条目需先纳管",
                    onClick: () =>
                      setBulk({ operation: "stop", rows: selection.chosen }),
                  },
                ]}
                count={selection.chosen.length}
                content={
                  selection.chosen.length === 1
                    ? columns
                        .find((c) => c.label === "操作")
                        ?.render(selection.chosen[0], 0)
                    : null
                }
              />
              <ResetFilters
                active={
                  query !== "" ||
                  owner !== "all" ||
                  status !== "all" ||
                  table !== "all" ||
                  family !== "4"
                }
                selectedCount={selection.chosen.length}
                summary={[
                  `IPv${family}`,
                  query && `搜索：${query}`,
                  owner !== "all" && `归属：${owner}`,
                  status !== "all" && `状态：${status}`,
                  table !== "all" && `路由表：${table}`,
                  `${rows.length} 项`,
                ]
                  .filter(Boolean)
                  .join(" · ")}
                onClick={() => {
                  selection.setSelected(new Set());
                  setQuery("");
                  setOwner("all");
                  setStatus("all");
                  setTable("all");
                  setFamily("4");
                }}
              />
              <ExportButton
                rows={selection.chosen.length ? selection.chosen : rows}
                name={title}
              />
            </>
          }
          label="路由与策略筛选"
          active={
            owner !== "all" ||
            status !== "all" ||
            table !== "all" ||
            family !== "4"
          }
        >
          <AdaptiveSearch>
            <Search size={15} />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="查询网段、接口、路由表、标记"
            />
          </AdaptiveSearch>
        </CompactFilters>

        <ErrorLine error={failure || resource.error} />
        {resource.data?.complete === false && (
          <p className="hint">系统规则读取不完整，请刷新后核对。</p>
        )}
        <Table
          keepHeaders
          rows={rows}
          total={scopeRows.length}
          selectedCount={selection.chosen.length}
          columns={columns}
          rowKey={(r) => String(r.id || JSON.stringify(r))}
        />
      </Panel>
      {form && (
        <Modal
          title={
            title +
            " · " +
            ({
              add: "新增",
              replace: "修改",
              delete: "删除",
              adopt: "纳入管理",
            }[op] || op)
          }
          onClose={() => setForm(null)}
          wide
        >
          {op === "add" || op === "replace" ? (
            <RoutingForm route={route} value={form} onChange={setForm} />
          ) : (
            <p>
              {op === "delete" ? "删除" : "接管"}{" "}
              {text(form.dst || form.priority)}，不会清空其他规则。
            </p>
          )}
          <label className="check risk">
            <input
              type="checkbox"
              checked={ack}
              onChange={(e) => setAck(e.target.checked)}
            />
            本次涉及默认出口或受保护 VPN 时，允许修改；我会检查连接后确认
          </label>
          <ErrorLine error={failure} />
          <div className="modal-actions">
            <Button onClick={() => setForm(null)}>取消</Button>
            <Button variant="primary" onClick={() => void preview(op, form)}>
              生成变更预览
            </Button>
          </div>
        </Modal>
      )}
      {plan && (
        <Modal title="网络变更预览" onClose={() => setPlan(null)} wide>
          <h3>{plan.summary}</h3>
          {plan.warnings.map((w, i) => (
            <p className="hint" key={i}>
              <AlertTriangle size={15} />
              {w}
            </p>
          ))}
          <pre className="command-preview">
            {plan.preview.length
              ? plan.preview.join("\n")
              : "仅更新管理归属，不改动现有内核规则"}
          </pre>
          <p>120 秒内确认网络正常，否则恢复本次修改。</p>
          <div className="modal-actions">
            <Button onClick={() => setPlan(null)}>取消</Button>
            <Button
              variant="primary"
              onClick={() => {
                const id = plan.id;
                setPlan(null);
                void act("/api/network/apply", { id }).then(() =>
                  resource.refresh(),
                );
              }}
            >
              应用本次变更
            </Button>
          </div>
        </Modal>
      )}
      {bulk && (
        <NetworkBatchDialog
          kind={kind}
          operation={bulk.operation}
          rows={bulk.rows}
          act={act}
          onClose={() => setBulk(null)}
        />
      )}
      {details != null && (
        <Modal
          title={title + " · 详细信息"}
          copyText={JSON.stringify(details, null, 2)}
          onClose={() => setDetails(null)}
          wide
        >
          <JsonDetails data={details} />
        </Modal>
      )}
    </>
  );
}
function RoutingForm({
  route,
  value,
  onChange,
}: {
  route: boolean;
  value: Dict;
  onChange: (v: Dict) => void;
}) {
  const set = (key: string, v: unknown) => onChange({ ...value, [key]: v });
  const input = (
    key: string,
    label: string,
    placeholder = "",
    numeric = false,
  ) => (
    <Field label={label}>
      <input
        type={numeric ? "number" : "text"}
        value={String(value[key] ?? "")}
        placeholder={placeholder}
        onChange={(e) =>
          set(key, numeric ? Number(e.target.value) : e.target.value)
        }
      />
    </Field>
  );
  return (
    <>
      <div className="form-grid">
        <Field label="地址族">
          <select
            disabled={!!value.id}
            value={Number(value.family || 4)}
            onChange={(e) => set("family", Number(e.target.value))}
          >
            <option value={4}>IPv4</option>
            <option value={6}>IPv6</option>
          </select>
        </Field>
        {route ? (
          <>
            <Field label="路由类型">
              <select
                value={String(value.type || "unicast")}
                onChange={(e) => set("type", e.target.value)}
              >
                <option value="unicast">单播路由</option>
                <option value="blackhole">丢弃（blackhole）</option>
                <option value="unreachable">目标不可达</option>
                <option value="prohibit">禁止访问</option>
              </select>
            </Field>
            {input("dst", "目标网段", "10.20.0.0/16 或 default")}
            {input("gateway", "下一跳", "可留空")}
            {input("dev", "出口接口", "wg-work")}
            {input("table", "路由表", "254 或 main")}
            {input("metric", "Metric", "0", true)}
          </>
        ) : (
          <>
            {input("priority", "优先级", "数值越小越优先", true)}
            {input("from", "来源 IP／CIDR", "all")}
            {input("to", "目标 IP／CIDR", "all")}
            {input("table", "查询路由表", "2000")}
            {input("fwmark", "标记／掩码", "0x200/0xfff")}
          </>
        )}
      </div>
      <details className="advanced-form">
        <summary>高级匹配与选项</summary>
        <div className="form-grid">
          {route ? (
            <>
              {input("prefsrc", "首选源地址")}
              {input("mtu", "MTU（留空使用接口值）", "", true)}
              <Field label="作用域">
                <select
                  value={String(value.scope || "")}
                  onChange={(e) => set("scope", e.target.value)}
                >
                  <option value="">自动</option>
                  <option value="global">global</option>
                  <option value="link">link</option>
                  <option value="host">host</option>
                </select>
              </Field>
              <label className="check">
                <input
                  type="checkbox"
                  checked={!!value.onlink}
                  onChange={(e) => set("onlink", e.target.checked)}
                />
                下一跳位于直连链路（onlink）
              </label>
            </>
          ) : (
            <>
              {input("iif", "入接口（可选）", "ens16")}
              {input("oif", "出接口（可选）", "wg-work")}
              {input("uidrange", "本机进程 UID 范围", "1000-1999")}
              <label className="check">
                <input
                  type="checkbox"
                  checked={!!value.invert}
                  onChange={(e) => set("invert", e.target.checked)}
                />
                反向匹配（not）
              </label>
            </>
          )}
        </div>
      </details>
      <label className="check">
        <input
          type="checkbox"
          checked={enabled(value)}
          onChange={(e) => set("enabled", e.target.checked)}
        />
        启用此{route ? "路由" : "策略"}并在服务启动时恢复
      </label>
    </>
  );
}
