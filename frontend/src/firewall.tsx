import { ExportButton } from "./resource_controls";
import { ActionMenu } from "./action_menu";
import { CompactFilters } from "./list_controls";
import { SortHeading } from "./list_controls";
import { NumberInput } from "./ui";
import { labelIcon } from "./ui_icons";
import { useEffect, useMemo, useState } from "react";
import {
  Plus,
  RefreshCw,
  ShieldCheck,
  MapPin,
  ListChecks,
  ShieldEllipsis,
  Settings2,
  Trash2,
  Power,
  Search,
} from "lucide-react";
import {
  api,
  errorMessage,
  list,
  record,
  text,
  type Dict,
  type Plan,
} from "./api";
import {
  Button,
  ErrorLine,
  Field,
  JsonDetails,
  Modal,
  PageHeader,
  SecondaryNav,
  Panel,
  Table,
  useResource,
  type Column,
} from "./ui";
import type { Act } from "./nodes";
const categories = [
  ["ip", "IP 规则"],
  ["port", "端口规则"],
  ["service-rule", "服务放行"],
  ["masquerade", "SNAT"],
  ["forward", "端口转发"],
  ["advanced", "高级规则（只读）"],
];
const strategies: Record<string, string> = {
  accept: "允许",
  drop: "丢弃",
  reject: "拒绝",
};
export function FirewallPage({ act }: { act: Act }) {
  const [driver, setDriver] = useState(
    () => localStorage.getItem("foxray.firewall.driver") || "firewalld",
  );
  const [zone, setZone] = useState("public"),
    [category, setCategory] = useState("ip"),
    [query, setQuery] = useState(""),
    [protocol, setProtocol] = useState("all"),
    [status, setStatus] = useState("all"),
    [owner, setOwner] = useState("all");
  const resource = useResource<Dict>(
    "/api/network/firewall?driver=" +
      driver +
      "&zone=" +
      encodeURIComponent(zone),
    15000,
  );
  const [selected, setSelected] = useState<Set<string>>(new Set()),
    [form, setForm] = useState<Dict | null>(null),
    [operation, setOperation] = useState("add"),
    [plan, setPlan] = useState<Plan | null>(null),
    [failure, setFailure] = useState(""),
    [detail, setDetail] = useState<unknown>(null),
    [ack, setAck] = useState(false),
    [sort, setSort] = useState({ key: "port", direction: 1 });
  const state = record(resource.data?.firewalld),
    all = list(resource.data?.entries),
    zones = Array.isArray(resource.data?.zones)
      ? (resource.data.zones as string[])
      : [zone],
    running = state.state === "active",
    native = driver !== "firewalld",
    writable = driver !== "ufw" && resource.data?.installed !== false,
    types = list(resource.data?.types);
  const rows = useMemo(
    () =>
      all
        .filter(
          (r) =>
            (category === "advanced"
              ? r.editable === false
              : r.kind === category && r.editable !== false) &&
            (protocol === "all" || r.protocol === protocol) &&
            (status === "all" || !!r.enabled === (status === "enabled")) &&
            (owner === "all" || !!r.managed === (owner === "managed")) &&
            JSON.stringify(r).toLowerCase().includes(query.toLowerCase()),
        )
        .sort((a, b) => {
          const x = a[sort.key],
            y = b[sort.key];
          if (sort.key === "port")
            return (
              ((parseInt(String(x)) || 0) - (parseInt(String(y)) || 0)) *
              sort.direction
            );
          return text(x).localeCompare(text(y), "zh-CN") * sort.direction;
        }),
    [all, category, protocol, status, owner, query, sort],
  );
  const selectable = rows.filter(
    (r) => r.managed || (!native && r.editable !== false),
  );
  const chosen = rows.filter((r) => selected.has(String(r.id)));
  const managedChosen = chosen.filter((r) => r.managed),
    externalChosen = chosen.filter((r) => !r.managed);
  const [interfaces, setInterfaces] = useState<string[]>([]);
  useEffect(() => {
    if (form?.kind !== "zone-interface") return;
    let live = true;
    void api<Dict>("/api/network/interfaces")
      .then((data) => {
        if (live)
          setInterfaces(
            list(data.items)
              .map((r) => String(r.ifname || r.name))
              .filter(Boolean),
          );
      })
      .catch(() => {
        if (live) setFailure("接口列表读取失败，请刷新重试");
      });
    return () => {
      live = false;
    };
  }, [form?.kind]);
  const update = (key: string, value: unknown) =>
    setForm((old) => ({ ...old, [key]: value }));
  const preview = async (op: string, item: Dict, items?: Dict[]) => {
    try {
      const next = await api<Plan>("/api/network/plan", {
        kind: "firewall",
        operation: op,
        item: { ...item, driver },
        items,
        allow_protected: ack,
      });
      setPlan(next);
      setForm(null);
      setFailure("");
    } catch (e) {
      setFailure(errorMessage(e));
    }
  };
  const start = (op: string, item: Dict) => {
    setOperation(op);
    setForm({ ...item });
    setAck(false);
    setFailure("");
  };
  const defaultRule = () => ({
    kind: category,
    driver,
    zone: native ? "host" : zone,
    protocol: "tcp",
    port: "",
    source: "",
    action: category === "masquerade" ? "masquerade" : "accept",
    to_source: "",
    out_interface: "",
    enabled: true,
    target_ip: "",
    target_port: "",
    family: "",
    priority: 0,
    logging: false,
    log_rate: "5/m",
    service: "ssh",
  });
  const heading = (key: string, label: string) => (
    <SortHeading
      label={label}
      direction={sort.key === key ? sort.direction : undefined}
      onClick={() =>
        setSort({ key, direction: sort.key === key ? -sort.direction : 1 })
      }
    />
  );
  const toggle = (r: Dict) => {
    void preview("replace", { ...r, enabled: !r.enabled });
  };
  const actions = (r: Dict) => (
    <div className="actions firewall-row-actions">
      <Button variant="ghost" title="查看规则详情" onClick={() => setDetail(r)}>
        详情
      </Button>
      {r.managed && writable ? (
        <>
          <Button
            variant="ghost"
            title="编辑规则"
            onClick={() => start("replace", r)}
          >
            <Settings2 size={14} />
          </Button>
          <Button
            variant="ghost"
            title={r.enabled ? "停用规则" : "启用规则"}
            onClick={() => toggle(r)}
          >
            <Power size={14} />
          </Button>
          <Button
            variant="danger"
            title="删除规则"
            onClick={() => void preview("delete", r)}
          >
            <Trash2 size={14} />
          </Button>
        </>
      ) : (
        <Button
          disabled={native || r.editable === false}
          onClick={() => void preview("adopt", r)}
        >
          纳入管理
        </Button>
      )}
    </div>
  );
  const columns: Column<Dict>[] = [
    {
      key: "select",
      label: "选择",
      heading: (
        <input
          type="checkbox"
          aria-label="全选规则"
          title="选择当前筛选范围的全部可操作规则"
          checked={selectable.length > 0 && chosen.length === selectable.length}
          ref={(el) => {
            if (el)
              el.indeterminate =
                chosen.length > 0 && chosen.length < selectable.length;
          }}
          onChange={(e) =>
            setSelected(
              e.target.checked
                ? new Set(selectable.map((r) => String(r.id)))
                : new Set(),
            )
          }
        />
      ),
      render: (r) => (
        <input
          type="checkbox"
          aria-label={"选择规则 " + text(r.port || r.source)}
          disabled={!r.managed && (native || r.editable === false)}
          checked={selected.has(String(r.id))}
          onChange={(e) =>
            setSelected((old) => {
              const next = new Set(old);
              e.target.checked
                ? next.add(String(r.id))
                : next.delete(String(r.id));
              return next;
            })
          }
        />
      ),
    },
    { key: "serial", label: "序号", render: (_, i) => i + 1 },
  ];
  if (category === "service-rule")
    columns.push({
      key: "service",
      label: "服务",
      heading: heading("service", "服务"),
      render: (r) => <strong>{text(r.service)}</strong>,
    });
  if (category === "port" || category === "forward")
    columns.push(
      {
        key: "port",
        label: "端口",
        heading: heading("port", "端口"),
        render: (r) => <strong>{text(r.port)}</strong>,
      },
      {
        key: "protocol",
        label: "协议",
        heading: heading("protocol", "协议"),
        render: (r) => text(r.protocol).toUpperCase(),
      },
    );
  columns.push({
    key: "source",
    label: "来源 IP／网段",
    heading: heading("source", "来源 IP／网段"),
    render: (r) => (
      <span className="ellipsis" title={text(r.source || "全部来源")}>
        {text(r.source || "全部来源")}
      </span>
    ),
  });
  if (category === "forward")
    columns.push({
      key: "target",
      label: "转发目标",
      render: (r) => (
        <>
          {text(r.target_ip || "本机")}
          <small>端口 {text(r.target_port)}</small>
        </>
      ),
    });
  else if (category === "masquerade")
    columns.push(
      {
        key: "action",
        label: "动作",
        render: (r) => String(r.action === "snat" ? "SNAT" : "MASQUERADE"),
      },
      {
        key: "to_source",
        label: "转换后源地址",
        render: (r) =>
          r.action === "snat" ? text(r.to_source) : "自动使用出接口地址",
      },
      {
        key: "out_interface",
        label: "出接口",
        render: (r) => text(r.out_interface || "区域出接口"),
      },
    );
  else if (category !== "service-rule")
    columns.push({
      key: "action",
      label: "策略",
      heading: heading("action", "策略"),
      render: (r) => (
        <span className={r.action === "accept" ? "accent" : "poor"}>
          {r.editable === false
            ? "高级策略"
            : strategies[String(r.action)] || text(r.action)}
        </span>
      ),
    });
  if (!native && category !== "service-rule")
    columns.push({
      key: "priority",
      label: "优先级",
      heading: heading("priority", "优先级"),
      render: (r) => Number(r.priority || 0),
    });
  if (native)
    columns.push({
      key: "packets",
      label: "命中包数",
      heading: heading("packets", "命中包数"),
      render: (r) =>
        r.packets == null ? (
          <span title="当前后端或规则未提供计数">—</span>
        ) : (
          text(r.packets)
        ),
    });
  columns.push(
    {
      key: "state",
      label: "状态",
      render: (r) => (
        <>
          {r.enabled ? (
            <span className={r.runtime ? "accent" : "fair"}>
              {r.runtime ? "已生效" : "待生效／状态偏差"}
            </span>
          ) : (
            <span className="muted">已停用</span>
          )}
          <small>
            {r.permanent ? "已持久化" : "仅运行时"}
            {r.drift ? " · 与运行状态不一致" : ""}
          </small>
        </>
      ),
    },
    {
      key: "owner",
      label: "归属",
      heading: heading("managed", "归属"),
      render: (r) => (r.managed ? "FoXray 管理" : "系统／外部配置"),
    },
    {
      key: "actions",
      label: "操作",
      className: "firewall-actions-column",
      render: actions,
    },
  );
  return (
    <>
      <Panel className="firewall-main-panel management-panel">
        <PageHeader
          title="防火墙"
          description={
            <>
              {"端口访问控制、IP 策略与端口转发 · 变更预览、独立回退与操作日志"}
              <p>
                {
                  "外部规则须先明确纳入管理，才可编辑或删除。每批最多50条，未确认的变更自动回退。"
                }
              </p>
            </>
          }
          actions={
            <>
              <Button onClick={() => void resource.refresh()}>
                <RefreshCw size={15} />
                刷新
              </Button>
              <Button
                disabled={native}
                onClick={() =>
                  start(running ? "stop" : "start", { kind: "service", driver })
                }
              >
                {running ? "关闭防火墙" : "开启防火墙"}
              </Button>
              <Button
                disabled={native}
                onClick={() =>
                  start(state.autostart ? "disable" : "enable", {
                    kind: "service",
                    driver,
                  })
                }
              >
                {state.autostart ? "关闭自启" : "开启自启"}
              </Button>
            </>
          }
        />
        <div className="toolbar firewall-driver-picker">
          <Field
            label="主机防火墙类型"
            info="选择规则管理工具，不自动启停或迁移系统防火墙。iptables／nftables 已安装不代表多个防火墙正在独立运行。"
          >
            <select
              value={driver}
              onChange={(e) => {
                setDriver(e.target.value);
                setCategory("port");
                localStorage.setItem("foxray.firewall.driver", e.target.value);
                setSelected(new Set());
                setZone(e.target.value === "firewalld" ? "public" : "host");
              }}
            >
              {(types.length ? types : [{ id: driver, installed: true }]).map(
                (t) => (
                  <option
                    key={String(t.id)}
                    value={String(t.id)}
                    disabled={!t.installed}
                  >
                    {String(t.id)} · {t.installed ? "已安装" : "未安装"}
                    {t.id === "ufw" ? "（规则只读）" : ""}
                  </option>
                ),
              )}
            </select>
          </Field>
        </div>
        <div className="firewall-overview">
          <div>
            <ShieldCheck size={21} />
            <span>{native ? "管理工具" : "运行状态"}</span>
            <strong className={running ? "accent" : "fair"}>
              {native
                ? resource.data?.installed
                  ? "已安装"
                  : "未安装"
                : running
                  ? "运行中"
                  : "已停止／不可用"}
            </strong>
          </div>
          <div>
            <MapPin size={21} className="metric-blue" />
            <span>当前区域</span>
            <strong className="metric-blue">{zone}</strong>
          </div>
          <div>
            <ListChecks size={21} className="metric-orange" />
            <span>当前区域规则</span>
            <strong className="metric-orange">{all.length}</strong>
          </div>
          <div>
            <ShieldEllipsis size={21} className="metric-purple" />
            <span>受管理规则</span>
            <strong className="metric-purple">
              {all.filter((r) => r.managed).length}
            </strong>
          </div>
        </div>
        <div className="toolbar">
          <Field label="防火墙区域">
            <select
              value={zone}
              onChange={(e) => {
                setZone(e.target.value);
                setSelected(new Set());
              }}
            >
              {(zones.length ? zones : [zone]).map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </Field>
          <p className="small muted">
            已允许的系统服务：
            {Array.isArray(resource.data?.allowed_services)
              ? resource.data.allowed_services.join("、")
              : "—"}
          </p>
          <p className="small muted">
            区域地址伪装：{resource.data?.masquerade ? "已开启" : "已关闭"}
          </p>
        </div>
        {!native && (
          <div className="zone-summary">
            <span>
              区域目标：
              <strong>{text(record(resource.data?.zone_info).target)}</strong>
            </span>
            <span>
              绑定接口：
              <strong>
                {text(record(resource.data?.zone_info).interfaces || "无")}
              </strong>
            </span>
            <span>
              绑定来源：
              <strong>
                {text(record(resource.data?.zone_info).sources || "无")}
              </strong>
            </span>
            <Button
              onClick={() =>
                start("add", {
                  kind: "zone-interface",
                  zone,
                  interface: "",
                  driver,
                })
              }
            >
              调整接口所属区域
            </Button>
          </div>
        )}
        <ErrorLine error={resource.error || failure} />
        {!!resource.data?.note && (
          <p className="hint">{text(resource.data.note)}</p>
        )}
        {!!resource.data?.ruleset_raw && (
          <details className="advanced-form">
            <summary>完整系统规则（只读）</summary>
            <pre className="command-preview">
              {text(resource.data.ruleset_raw)}
            </pre>
          </details>
        )}
        {!!resource.data?.error && (
          <p className="hint">{text(resource.data.error)}</p>
        )}
        <div className="page-heading firewall-category-heading">
          <SecondaryNav label="防火墙">
            <div className="subtabs" role="tablist">
              {categories
                .filter(([id]) => !native || id !== "service-rule")
                .map(([id, label]) => (
                  <button
                    key={id}
                    role="tab"
                    aria-selected={category === id}
                    onClick={() => {
                      setCategory(id);
                      setSelected(new Set());
                      setProtocol("all");
                    }}
                  >
                    {labelIcon(label, 14)}
                    {label}
                  </button>
                ))}
            </div>
          </SecondaryNav>
        </div>
        <CompactFilters
          label="防火墙筛选"
          active={protocol !== "all" || status !== "all" || owner !== "all"}
        >
          <label className="search">
            <Search size={15} />
            <input
              placeholder="查询端口、IP、网段或协议"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
          {category !== "ip" && category !== "masquerade" && (
            <Field label="协议">
              <select
                aria-label="防火墙协议筛选"
                value={protocol}
                onChange={(e) => setProtocol(e.target.value)}
              >
                <option value="all">全部协议</option>
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </select>
            </Field>
          )}
          <Field label="状态">
            <select
              aria-label="防火墙状态筛选"
              value={status}
              onChange={(e) => setStatus(e.target.value)}
            >
              <option value="all">全部状态</option>
              <option value="enabled">已启用</option>
              <option value="disabled">已停用</option>
            </select>
          </Field>
          <Field label="归属">
            <select
              aria-label="防火墙归属筛选"
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
            >
              <option value="all">全部来源</option>
              <option value="managed">FoXray 管理</option>
              <option value="external">系统／外部配置</option>
            </select>
          </Field>
        </CompactFilters>
        <div className="management-toolbar">
          {category !== "advanced" && (
            <Button
              disabled={!running || !writable}
              onClick={() => start("add", defaultRule())}
            >
              <Plus size={15} />
              添加
            </Button>
          )}
          <ExportButton
            name={"防火墙-" + category}
            rows={chosen.length ? chosen : rows}
          />
          <ActionMenu
            label="规则操作"
            items={[
              ...(!native
                ? [
                    {
                      label: "纳管",
                      disabled:
                        !externalChosen.length ||
                        externalChosen.length > 50 ||
                        !writable,
                      onClick: () => void preview("adopt", {}, externalChosen),
                    },
                  ]
                : []),
              ...[
                ["enable", "启用"],
                ["disable", "停用"],
                ["delete", "删除"],
              ].map(([op, label]) => ({
                label,
                disabled:
                  !managedChosen.length ||
                  managedChosen.length > 50 ||
                  externalChosen.length > 0 ||
                  !writable,
                onClick: () =>
                  void preview(
                    op,
                    {},
                    managedChosen.map((r) => ({ id: r.id })),
                  ),
              })),
            ]}
          />
        </div>
        <Table
          rows={rows}
          total={all.length}
          selectedCount={chosen.length}
          columns={columns}
          rowKey={(r) => String(r.id)}
        />
      </Panel>
      {form && (
        <Modal
          description={
            <>
              <p>
                运行规则和永久声明同时处理；不全局
                reload。允许规则仍可能受其他防火墙的拒绝规则限制。
              </p>
            </>
          }
          title={
            form.kind === "service"
              ? "防火墙服务操作"
              : operation === "add"
                ? "添加防火墙规则"
                : "编辑防火墙规则"
          }
          onClose={() => setForm(null)}
          wide
        >
          {form.kind === "zone-interface" ? (
            <div className="form-grid">
              <Field label="目标区域">
                <select
                  value={String(form.zone)}
                  onChange={(e) => update("zone", e.target.value)}
                >
                  {zones.map((v) => (
                    <option key={v}>{v}</option>
                  ))}
                </select>
              </Field>
              <Field label="接口名称">
                <select
                  aria-label="防火墙区域接口"
                  value={String(form.interface || "")}
                  onChange={(e) => update("interface", e.target.value)}
                >
                  <option value="">选择接口</option>
                  {interfaces.map((name) => (
                    <option key={name}>{name}</option>
                  ))}
                </select>
              </Field>
            </div>
          ) : form.kind === "service" ? (
            <p>
              将执行 firewalld 的 {operation}{" "}
              操作。请确认管理端口已经放行；没有确认时会恢复原服务状态。
            </p>
          ) : (
            <FirewallForm
              value={form}
              onChange={update}
              services={
                Array.isArray(resource.data?.available_services)
                  ? (resource.data.available_services as string[])
                  : []
              }
            />
          )}
          <label className="check risk">
            <input
              type="checkbox"
              checked={ack}
              onChange={(e) => setAck(e.target.checked)}
            />
            我已确认规则和管理连接的影响，允许本次变更；必要时使用独立回退
          </label>
          <ErrorLine error={failure} />
          <div className="modal-actions">
            <Button onClick={() => setForm(null)}>取消</Button>
            <Button
              variant="primary"
              disabled={
                ["service", "zone-interface", "masquerade"].includes(
                  String(form.kind),
                ) && !ack
              }
              onClick={() => void preview(operation, form)}
            >
              生成变更预览
            </Button>
          </div>
        </Modal>
      )}
      {plan && (
        <Modal title="防火墙变更预览" onClose={() => setPlan(null)} wide>
          <h3>{plan.summary}</h3>
          {plan.warnings.map((v, i) => (
            <p className="hint" key={i}>
              {v}
            </p>
          ))}
          <pre className="command-preview">
            {plan.preview.length
              ? plan.preview.join("\n")
              : "仅变更管理归属，不重复添加系统规则。"}
          </pre>
          <div className="modal-actions">
            <Button onClick={() => setPlan(null)}>取消</Button>
            <Button
              variant="primary"
              onClick={() => {
                const id = plan.id;
                setPlan(null);
                void act("/api/network/apply", { id });
              }}
            >
              应用并等待网络确认
            </Button>
          </div>
        </Modal>
      )}
      {detail != null && (
        <Modal title="防火墙规则详情" onClose={() => setDetail(null)} wide>
          <JsonDetails data={detail} />
        </Modal>
      )}
    </>
  );
}
function FirewallForm({
  value,
  onChange,
  services,
}: {
  value: Dict;
  onChange: (key: string, v: unknown) => void;
  services: string[];
}) {
  const kind = String(value.kind),
    native = value.driver && value.driver !== "firewalld";
  return (
    <>
      <div className="form-grid">
        <Field label="备注名称">
          <input
            value={String(value.name || "")}
            onChange={(e) => onChange("name", e.target.value)}
            placeholder="方便辨认此规则"
          />
        </Field>
        <Field label="区域">
          <input value={String(value.zone)} readOnly />
        </Field>
        <Field label="规则状态">
          <select
            value={value.enabled === false ? "disabled" : "enabled"}
            onChange={(e) => onChange("enabled", e.target.value === "enabled")}
          >
            <option value="enabled">启用</option>
            <option value="disabled">停用（保留配置）</option>
          </select>
        </Field>
        {kind === "service-rule" ? (
          <Field label="预定义服务">
            <input
              list="firewall-services"
              value={String(value.service || "")}
              onChange={(e) => onChange("service", e.target.value)}
            />
            <datalist id="firewall-services">
              {services.map((name) => (
                <option key={name}>{name}</option>
              ))}
            </datalist>
          </Field>
        ) : (
          <>
            <Field label="地址族">
              <select
                value={String(value.family || "")}
                onChange={(e) => onChange("family", e.target.value)}
              >
                <option value="">根据来源自动判断／双栈</option>
                <option value="ipv4">IPv4</option>
                {kind !== "forward" && kind !== "masquerade" && (
                  <option value="ipv6">IPv6</option>
                )}
              </select>
            </Field>
            {kind !== "ip" && kind !== "masquerade" && (
              <>
                <Field label="端口／端口范围" hint="443 或 8000-8100">
                  <input
                    value={String(value.port || "")}
                    onChange={(e) => onChange("port", e.target.value)}
                  />
                </Field>
                <Field label="协议">
                  <select
                    value={String(value.protocol || "tcp")}
                    onChange={(e) => onChange("protocol", e.target.value)}
                  >
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                    <option value="tcpudp">TCP + UDP</option>
                  </select>
                </Field>
              </>
            )}
            <Field
              label={
                kind === "ip"
                  ? "来源 IP／CIDR"
                  : "来源 IP／CIDR（留空为整个区域）"
              }
              hint={
                kind === "masquerade"
                  ? "SNAT 动作或原生防火墙模式必须指定来源 IPv4／网段"
                  : "支持 IPv4 或 IPv6 地址／网段"
              }
            >
              <input
                value={String(value.source || "")}
                onChange={(e) => onChange("source", e.target.value)}
                placeholder="192.168.1.0/24"
              />
            </Field>
            {kind !== "forward" && kind !== "masquerade" && (
              <Field label="访问策略">
                <select
                  value={String(value.action || "accept")}
                  onChange={(e) => onChange("action", e.target.value)}
                >
                  <option value="accept">允许（accept）</option>
                  <option value="drop">丢弃（drop）</option>
                  <option value="reject">拒绝（reject）</option>
                </select>
              </Field>
            )}
            {kind === "masquerade" && (
              <>
                <Field label="SNAT 动作">
                  <select
                    value={value.action === "snat" ? "snat" : "masquerade"}
                    onChange={(e) => onChange("action", e.target.value)}
                  >
                    <option value="masquerade">
                      MASQUERADE（自动使用出接口地址）
                    </option>
                    <option value="snat">SNAT（指定转换后源地址）</option>
                  </select>
                </Field>
                {value.action === "snat" && (
                  <Field
                    label="转换后源 IPv4 地址"
                    hint="填写此主机出接口上可用的源地址"
                  >
                    <input
                      value={String(value.to_source || "")}
                      onChange={(e) => onChange("to_source", e.target.value)}
                      placeholder="203.0.113.10"
                    />
                  </Field>
                )}
                {(value.action === "snat" || native) && (
                  <Field
                    label="出接口"
                    hint={
                      native
                        ? "仅匹配指定出接口的转发流量"
                        : "接口必须属于当前选择的 firewalld 区域"
                    }
                  >
                    <input
                      value={String(value.out_interface || "")}
                      onChange={(e) =>
                        onChange("out_interface", e.target.value)
                      }
                      placeholder="eth0"
                    />
                  </Field>
                )}
              </>
            )}
            {kind === "forward" && (
              <>
                <Field label="目标 IPv4" hint="留空为本机">
                  <input
                    value={String(value.target_ip || "")}
                    onChange={(e) => onChange("target_ip", e.target.value)}
                    placeholder="192.168.1.20"
                  />
                </Field>
                <Field label="目标端口／范围">
                  <input
                    value={String(value.target_port || "")}
                    onChange={(e) => onChange("target_port", e.target.value)}
                    placeholder="8080"
                  />
                </Field>
              </>
            )}
            {!native && (
              <Field
                label="优先级（越小越先匹配）"
                hint="-32768 至 32767；0 使用 firewalld 默认顺序"
              >
                <NumberInput
                  type="number"
                  min={-32768}
                  max={32767}
                  value={Number(value.priority || 0)}
                  onChange={(e) => onChange("priority", Number(e.target.value))}
                />
              </Field>
            )}
          </>
        )}
      </div>
      {!native && ["port", "ip"].includes(kind) && (
        <div className="checks">
          <label className="check">
            <input
              type="checkbox"
              checked={!!value.logging}
              onChange={(e) => onChange("logging", e.target.checked)}
            />
            记录匹配日志
          </label>
          {!!value.logging && (
            <Field label="日志速率限制">
              <input
                value={String(value.log_rate || "5/m")}
                onChange={(e) => onChange("log_rate", e.target.value)}
                placeholder="5/m"
              />
            </Field>
          )}
        </div>
      )}
    </>
  );
}
