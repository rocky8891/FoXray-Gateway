import { ExportButton, useRowSelection } from "./resource_controls";
import { SelectionActions } from "./selection_actions";
import { CompactFilters } from "./list_controls";
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
  key === "from"
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
              table: r.table || 254,
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
                String(m.table || 254) ===
                  String(r.table === "main" ? 254 : r.table || 254)
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
        .map((r) => String(r.table || 254)),
    ),
  ].sort((a, b) => a.localeCompare(b, undefined, { numeric: true }));
  const rows = all
    .filter(
      (r) =>
        String(r.family || 4) === family &&
        (owner === "all" || !!r.managed === (owner === "managed")) &&
        (status === "all" || enabled(r) === (status === "enabled")) &&
        (table === "all" || String(r.table || 254) === table) &&
        JSON.stringify(r).toLowerCase().includes(query.toLowerCase()),
    )
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
  const selection = useRowSelection(rows, (r) =>
    String(r.id || JSON.stringify(r)),
  );
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
    heading: sortHeading(key, label),
    render: (r) =>
      key === "table" && onTable ? (
        <button
          className="table-link"
          onClick={() =>
            onTable(String(r.table || 254), String(r.family || family))
          }
        >
          {text(r.table || "main (254)")}
        </button>
      ) : (
        <span className="ellipsis" title={text(fieldValue(r, key))}>
          {text(fieldValue(r, key))}
        </span>
      ),
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
          {r.managed ? (
            <>
              <Button onClick={() => begin("replace", r)}>编辑</Button>
              <Button
                onClick={() => begin("replace", { ...r, enabled: !enabled(r) })}
              >
                {enabled(r) ? "停用" : "启用"}
              </Button>
              <Button variant="danger" onClick={() => begin("delete", r)}>
                删除
              </Button>
            </>
          ) : (
              route
                ? ["static", "boot"].includes(String(r.protocol)) &&
                  !String(r.dev || "").startsWith("wg-") &&
                  !["vone0", "CloudflareWARP", "vpm-tun"].includes(
                    String(r.dev),
                  )
                : Number(r.priority) > 0 &&
                  Number(r.priority) < 32766 &&
                  !(Number(r.priority) >= 16060 && Number(r.priority) <= 16100)
            ) ? (
            <Button onClick={() => begin("adopt", r)}>纳管</Button>
          ) : null}
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
          title={route && table !== "all" ? "路由表 · " + table : title}
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
          actions={
            <>
              <Button onClick={() => begin("add", initial)}>
                <Plus size={15} />
                添加
              </Button>
              <Button onClick={() => void resource.refresh()}>
                <RefreshCw size={15} />
                刷新
              </Button>
            </>
          }
        />
        <div className="firewall-overview">
          <div>
            <span>系统与管理条目</span>
            <strong>{all.length}</strong>
          </div>
          <div>
            <span>FoXray 管理</span>
            <strong>{managed.length}</strong>
          </div>
          <div>
            <span>已启用管理条目</span>
            <strong>{managed.filter(enabled).length}</strong>
          </div>
          <div>
            <span>地址族</span>
            <strong>IPv{family}</strong>
          </div>
        </div>

        <CompactFilters
          label="路由与策略筛选"
          active={
            owner !== "all" ||
            status !== "all" ||
            table !== "all" ||
            family !== "4"
          }
        >
          <label className="search">
            <Search size={15} />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="查询网段、接口、路由表、标记"
            />
          </label>
          <Field label="地址族">
            <select
              value={family}
              onChange={(e) => {
                setFamily(e.target.value);
                setTable("all");
              }}
            >
              <option value="4">IPv4</option>
              <option value="6">IPv6</option>
            </select>
          </Field>
          <Field label="归属">
            <select value={owner} onChange={(e) => setOwner(e.target.value)}>
              <option value="all">全部来源</option>
              <option value="managed">FoXray 管理</option>
              <option value="external">系统／外部</option>
            </select>
          </Field>
          <Field label="路由表">
            <select value={table} onChange={(e) => setTable(e.target.value)}>
              <option value="all">全部路由表</option>
              {tables.map((t) => (
                <option key={t}>{t}</option>
              ))}
            </select>
          </Field>
          <Field label="状态">
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="all">全部状态</option>
              <option value="enabled">已启用</option>
              <option value="disabled">已停用</option>
            </select>
          </Field>
          <Button
            onClick={() => {
              setQuery("");
              setOwner("all");
              setStatus("all");
              setTable("all");
            }}
          >
            重置筛选
          </Button>
        </CompactFilters>
        <div className="management-toolbar">
          <SelectionActions
            count={selection.chosen.length}
            content={
              selection.chosen.length === 1
                ? columns
                    .find((c) => c.label === "操作")
                    ?.render(selection.chosen[0], 0)
                : null
            }
          />
          <ExportButton
            rows={selection.chosen.length ? selection.chosen : rows}
            name={title}
          />
        </div>
        <ErrorLine error={failure || resource.error} />
        {resource.data?.complete === false && (
          <p className="hint">系统规则读取不完整，请刷新后核对。</p>
        )}
        <Table
          rows={rows}
          total={all.length}
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
      {details != null && (
        <Modal
          title={title + " · 详细信息"}
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
