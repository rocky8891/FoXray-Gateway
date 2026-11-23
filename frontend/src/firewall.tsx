import { CrossZoneRules } from "./cross_zone_rules";
import { ZoneRulesIcon } from "./workspace_icons";
import { FirewallServiceSelect } from "./firewall_service_select";
import { AdaptiveSearch } from "./adaptive_search";
import { ZoneSourceEditor,ZonePolicyEditor,RichRuleEditor,FirewallChangeDetails,FirewallRuleSummary } from "./firewall_editors";
import { FirewallWorkspace } from "./firewall_workspace";
import { firewallViewState } from "./firewall_view_state";
import { MultiSelect } from "./multi_select";
import { ResetFilters } from "./list_controls";
import { ExportButton } from "./resource_controls";
import { ActionMenu } from "./action_menu";
import { CompactFilters, ListFilter } from "./list_controls";
import { SortHeading } from "./list_controls";
import { NumberInput } from "./ui";
import { labelIcon } from "./ui_icons";
import { useEffect, useMemo, useState, useRef } from "react";
import {
  Plus,
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
  SecondaryNav,
  Button,
  ErrorLine,
  Field,
  Modal,
  Panel,
  Table,
  useResource,
  type Column,
} from "./ui";
import type { Act } from "./nodes";
const categories = [
  ["all", "全部规则"],
  ["ip", "IP 规则"],
  ["port", "端口规则"],
  ["service-rule", "服务放行"],
  ["masquerade", "SNAT"],
  ["forward", "端口转发"],
  ["rich", "高级规则"],
  ["cross-zone", "跨区域规则"],
  ["advanced", "未知规则（只读）"],
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
    [category, setCategory] = useState("all"),
    [query, setQuery] = useState(""),
    [protocol, setProtocol] = useState("all"),
    [status, setStatus] = useState("all"),
    [owner, setOwner] = useState("all"),
    [ruleAction, setRuleAction] = useState("all"),
    [configScope,setConfigScope]=useState("all");
  const resource = useResource<Dict>(
    "/api/network/firewall?driver=" +
      driver +
      "&zone=" +
      encodeURIComponent(zone),
    15000,
    category !== "cross-zone",
  );
  const serviceResource=useResource<Dict>("/api/network/firewall/service?driver="+encodeURIComponent(driver),5000,category!=="cross-zone");
  const [lastRead, setLastRead] = useState(0), [refreshing, setRefreshing] = useState(false);
  useEffect(()=>{if(resource.data)setLastRead(Date.now())},[resource.data]);
  const [selected, setSelected] = useState<Set<string>>(new Set()),
    [form, setForm] = useState<Dict | null>(null),
    [operation, setOperation] = useState("add"),
    [plan, setPlan] = useState<Plan | null>(null),
    [failure, setFailure] = useState(""),
    [detail, setDetail] = useState<unknown>(null),
    [ack, setAck] = useState(false),
    [sort, setSort] = useState({ key: "port", direction: 1 });
  const view = firewallViewState(resource.data, driver, zone, resource.error || "");
  const all = (view.current && !resource.error ? list(resource.data?.entries) : []).filter(r=>configScope==="runtime" ? !!(r.runtime||r.partial_runtime) : configScope==="permanent" ? !!(r.permanent||r.partial_permanent) : configScope==="difference" ? !!(r.drift||r.partial_runtime||r.partial_permanent||!!r.runtime!==!!r.permanent) : true),
    zones = Array.isArray(resource.data?.zones) ? resource.data.zones.map(String) : [zone],
    running = view.native ? view.canManage : view.running,
    native = view.native,
    writable = view.canManage;
  const ruleAnchor = useRef<HTMLDivElement>(null);
  const rows = useMemo(
    () =>
      all
        .filter(
          (r) =>
            (category === "all" || (category === "advanced"
              ? r.editable === false
              : category === "rich" ? (r.kind === "rich" || r.storage === "rich-rule") && r.editable !== false : r.kind === category && r.editable !== false)) &&
            (protocol === "all" || r.protocol === protocol) &&
            (status === "all" || !!r.enabled === (status === "enabled")) &&
            (owner === "all" || !!r.managed === (owner === "managed")) &&
            (ruleAction === "all" || (ruleAction === "blocked" ? ["drop","reject"].includes(String(r.action).toLowerCase()) : String(r.action).toLowerCase() === ruleAction)) &&
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
    [all, category, protocol, status, owner, ruleAction, query, sort],
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
  const zonePolicy = record(resource.data?.zone_policy);
  const targetDraftChanged = form?.kind === "zone-policy" && (
    String(form.target) !== String(record(zonePolicy.permanent).target) ||
    (form.target_apply === "immediate" && String(form.target) !== String(record(zonePolicy.runtime).target))
  );
  const preview = async (op: string, item: Dict, items?: Dict[]) => {
    if (item.kind === "service" ? !view.canServiceControl : !writable) {
      setFailure("当前状态未满足变更条件，请刷新并确认服务和规则读取状态");
      return;
    }
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
    kind: category === "all" ? "port" : category,
    ...(category === "rich" ? {rich_match:"port",family:"ipv4",storage:"rich-rule"} : {}),
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
    family: category === "rich" ? "ipv4" : "",
    priority: 0,
    logging: false,
    log_rate: "5/m",
    service: "",
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
      {r.managed ? (
        <>
          <Button
            variant="ghost"
            title="编辑"
            disabled={!writable}
            onClick={() => start("replace", r)}
          >
            <Settings2 size={14} />
          </Button>
          <Button
            variant="ghost"
            title={r.enabled ? "停用" : "启用"}
            disabled={!writable}
            onClick={() => toggle(r)}
          >
            <Power size={14} />
          </Button>
          <Button
            variant="danger"
            title="删除"
            disabled={!writable}
            onClick={() => void preview("delete", r)}
          >
            <Trash2 size={14} />
          </Button>
        </>
      ) : (
        <Button
          disabled={!writable || native || r.editable === false}
          onClick={() => void preview("adopt", r)}
        >
          纳管
        </Button>
      )}
    </div>
  );
  const filterControls = {
    action: <Field label="规则动作"><select aria-label="防火墙动作筛选" value={ruleAction} onChange={e=>setRuleAction(e.target.value)}><option value="all">全部动作</option><option value="accept">允许</option><option value="drop">丢弃</option><option value="reject">拒绝</option><option value="blocked">阻断（丢弃／拒绝）</option></select></Field>,
    protocol: (
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
    ),
    state: (
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
    ),
    owner: (
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
    ),
  };
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
  if (category === "all") columns.push(
    {key:"name",label:"名称／类型",heading:heading("kind","名称／类型"),render:r=><><strong>{text(r.service || r.port || r.source || (r.kind === "masquerade" ? "地址转换" : "规则"))}</strong><small>{r.editable===false ? "未知规则（只读）" : r.storage==="rich-rule"||r.kind==="rich" ? "高级规则" : categories.find(([id])=>id===r.kind)?.[1] || String(r.kind || "系统规则")}</small></>},
    {key:"protocol",label:"协议／端口",heading:heading("protocol","协议／端口"),render:r=>r.kind==="service-rule" ? "服务定义" : [r.protocol ? String(r.protocol).toUpperCase() : "全部",r.port].filter(Boolean).join(" / ")},
  );
  if (category === "service-rule")
    columns.push({
      key: "service",
      label: "服务",
      heading: heading("service", "服务"),
      render: (r) => <strong>{text(r.service)}</strong>,
    });
  if (category === "port" || category === "forward" || category==="rich")
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
        <span className={r.action === "accept" ? "accent" : ["drop","reject"].includes(String(r.action)) ? "poor" : "muted"}>
          {r.editable === false
            ? "高级策略"
            : strategies[String(r.action)] || ({masquerade:"地址伪装",snat:"源地址转换"} as Record<string,string>)[String(r.action)] || text(r.action)}
        </span>
      ),
    });
  if (!native && category !== "service-rule" && category !== "all" && category!=="advanced")
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
        <SecondaryNav label="防火墙规则分类">
            <div className="module-tabs firewall-secondary-tabs" role="tablist" aria-label="防火墙规则分类">
              {categories
                .filter(([id]) => !native || !["service-rule","rich","cross-zone"].includes(id))
                .map(([id, label]) => (
                  <button
                    key={id}
                    role="tab"
                    aria-selected={category === id}
                    onClick={() => {
                      setCategory(id);
                      setSelected(new Set());
                      setProtocol("all");setRuleAction("all");
                    }}
                  >
                    {labelIcon(label, 14)}
                    {label}
                  </button>
                ))}
            </div>
        </SecondaryNav>
        {category === "cross-zone" && <CrossZoneRules act={act}/>}
        <div hidden={category === "cross-zone"}>
        <FirewallWorkspace serviceData={serviceResource.data} serviceError={serviceResource.error || ""} rulesLoading={resource.loading} data={resource.data ? {...resource.data,entries:all} : null} error={resource.error || ""} driver={driver} zone={zone} lastRead={lastRead} refreshing={refreshing}
          refresh={()=>{setRefreshing(true);void Promise.all([resource.refresh(),serviceResource.refresh()]).finally(()=>setRefreshing(false))}}
          changeDriver={value=>{setDriver(value);setCategory("all");localStorage.setItem("foxray.firewall.driver",value);setZone(value==="firewalld"?"public":"host");setSelected(new Set());setQuery("");setProtocol("all");setStatus("all");setOwner("all");setRuleAction("all");setConfigScope("all");setForm(null);setPlan(null);setFailure("")}}
          changeZone={value=>{setZone(value);setSelected(new Set());setQuery("");setProtocol("all");setStatus("all");setOwner("all");setRuleAction("all");setConfigScope("all");setForm(null);setPlan(null);setFailure("")}}
          manageInterfaces={()=>start("add",{kind:"zone-interface",zone,interfaces:[],driver})}
          serviceAction={action=>start(action,{kind:"service",driver})}
          selectedFilter={category === "all" ? ruleAction === "blocked" ? "blocked" : owner === "managed" ? "managed" : owner === "external" ? "external" : "all" : ""}
          filterRules={filter=>{setCategory("all");setOwner(filter==="managed"?"managed":filter==="external"?"external":"all");setRuleAction(filter==="blocked"?"blocked":"all");setQuery("");setProtocol("all");setStatus("all");setSelected(new Set());ruleAnchor.current?.scrollIntoView({behavior:"smooth",block:"start"})}}
          viewRules={kind=>{setCategory(kind);setOwner("all");setRuleAction("all");setQuery("");setProtocol("all");setStatus("all");setSelected(new Set());ruleAnchor.current?.scrollIntoView({behavior:"smooth",block:"start"})}}
          addService={()=>start("add",{...defaultRule(),kind:"service-rule",service:""})}
          configScope={configScope} changeScope={value=>{setConfigScope(value);setSelected(new Set());setQuery("")}}
          manageSources={()=>start("add",{kind:"zone-source",driver,zone,source:"",binding_action:"bind"})}
          editPolicy={()=>{const policy=record(resource.data?.zone_policy),runtime=record(policy.runtime),permanent=record(policy.permanent);start("add",{kind:"zone-policy",driver,zone,target:String(permanent.target||runtime.target||"default"),target_apply:"permanent",target_ack:false,reload_ack:false,masquerade:runtime.masquerade===true,icmp_blocks:Array.isArray(runtime.icmp_blocks)?runtime.icmp_blocks:[],icmp_inversion:runtime.icmp_inversion===true})}}
        />
      <Panel className="firewall-main-panel management-panel">
        <ErrorLine error={resource.error || failure} />
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
        <div className="firewall-rules-heading" ref={ruleAnchor}><h3><ZoneRulesIcon size={20} className="firewall-heading-icon"/>{native ? "系统规则" : "区域规则"} <span>{native ? driver : zone}</span></h3><span className="muted">{native ? "当前工具规则快照" : ({all:"运行时与永久配置 · 合并展示",runtime:"运行时配置",permanent:"永久配置",difference:"存在差异的配置"} as Record<string,string>)[configScope]}</span></div>
        <CompactFilters
          before={
            <>
              {category !== "advanced" && (
                <Button
                  disabled={!running || !writable}
                  onClick={() => start("add", defaultRule())}
                >
                  <Plus size={15} />
                  添加
                </Button>
              )}
              <ResetFilters
                active={
                  query !== "" ||
                  protocol !== "all" ||
                  status !== "all" ||
                  owner !== "all" || ruleAction !== "all"
                }
                selectedCount={chosen.length}
                summary={[
                  query && `搜索：${query}`,
                  protocol !== "all" && `协议：${protocol}`,
                  status !== "all" && `状态：${status}`,
                  owner !== "all" && `归属：${owner}`,
                  ruleAction !== "all" && `动作：${ruleAction}`,
                  `${rows.length} 项`,
                ]
                  .filter(Boolean)
                  .join(" · ")}
                onClick={() => {
                  setSelected(new Set());
                  setQuery("");
                  setProtocol("all");
                  setStatus("all");
                  setOwner("all");setRuleAction("all");
                }}
              />
              <ExportButton
                name={"防火墙-" + category}
                rows={chosen.length ? chosen : rows}
              />
              <Button
                disabled={
                  !managedChosen.length ||
                  managedChosen.length > 50 ||
                  externalChosen.length > 0 ||
                  !writable
                }
                onClick={() =>
                  void preview(
                    "enable",
                    {},
                    managedChosen.map((r) => ({ id: r.id })),
                  )
                }
              >
                启用
              </Button>
              <Button
                disabled={
                  !managedChosen.length ||
                  managedChosen.length > 50 ||
                  externalChosen.length > 0 ||
                  !writable
                }
                onClick={() =>
                  void preview(
                    "disable",
                    {},
                    managedChosen.map((r) => ({ id: r.id })),
                  )
                }
              >
                停用
              </Button>
              <ActionMenu
                label="所选规则"
                triggerLabel="批量操作"
                items={[
                  ...(!native
                    ? [
                        {
                          label: "纳管",
                          disabled:
                            !externalChosen.length ||
                            externalChosen.length > 50 ||
                            !writable,
                          onClick: () =>
                            void preview("adopt", {}, externalChosen),
                        },
                      ]
                    : []),
                  ...[["delete", "删除"]].map(([op, label]) => ({
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
            </>
          }
          label="防火墙筛选"
          active={protocol !== "all" || status !== "all" || owner !== "all" || ruleAction !== "all"}
        >
          <AdaptiveSearch>
            <Search size={15} />
            <input
              placeholder="查询端口、IP、网段或协议"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </AdaptiveSearch>
        </CompactFilters>

        <Table
          rows={rows}
          total={all.length}
          selectedCount={chosen.length}
          keepHeaders
          columns={columns.map((c) =>
            ["protocol", "state", "owner", "action"].includes(c.key)
              ? {
                  ...c,
                  heading: (
                    <span className="table-heading-tools">
                      {c.heading ||
                        heading(
                          c.key === "state"
                            ? "enabled"
                            : c.key === "owner"
                              ? "managed"
                              : c.key,
                          c.label,
                        )}
                      <ListFilter
                        label={"防火墙" + c.label + "筛选"}
                        column
                        active={
                          {
                            protocol: protocol !== "all",
                            state: status !== "all",
                            owner: owner !== "all",
                            action: ruleAction !== "all",
                          }[c.key as "protocol" | "state" | "owner" | "action"]
                        }
                      >
                        {
                          filterControls[
                            c.key as "protocol" | "state" | "owner" | "action"
                          ]
                        }
                        <Button
                          onClick={() => {
                            if (c.key === "protocol") setProtocol("all");
                            else if (c.key === "state") setStatus("all");
                            else if (c.key === "action") setRuleAction("all");
                            else setOwner("all");
                          }}
                        >
                          重置
                        </Button>
                      </ListFilter>
                    </span>
                  ),
                }
              : c,
          )}
          rowKey={(r) => String(r.id)}
        />
      </Panel>
      {form && (
        <Modal className="firewall-dialog"
          description={
            <>
              <p>
                运行规则和永久声明同时处理；不全局
                reload。允许规则仍可能受其他防火墙的拒绝规则限制。
              </p>
            </>
          }
          title={
            form.kind === "zone-source" ? "管理来源地址归属" : form.kind === "zone-policy" ? "编辑区域策略" : form.kind === "rich" ? (operation==="add"?"添加高级规则":"编辑高级规则") : form.kind === "zone-interface"
              ? "调整接口所属区域"
              : form.kind === "service"
                ? "防火墙服务操作"
                : operation === "add"
                  ? "添加防火墙规则"
                  : "编辑防火墙规则"
          }
          onClose={() => setForm(null)}
          wide
        >
          {form.kind === "zone-source" ? <ZoneSourceEditor value={form} onChange={update} zones={zones} sources={String(record(resource.data?.zone_info).sources||" ").trim().split(/\s+/).filter(Boolean)}/> : form.kind === "zone-policy" ? <ZonePolicyEditor value={form} onChange={update} policy={record(resource.data?.zone_policy)}/> : form.kind==="rich" ? <RichRuleEditor value={form} onChange={update} services={Array.isArray(resource.data?.available_services)?resource.data.available_services.map(String):[]}/> : form.kind === "zone-interface" ? (
            <div className="form-grid">
              <Field label="目标区域">
                <select
                  aria-label="目标区域"
                  value={String(form.zone)}
                  onChange={(e) => update("zone", e.target.value)}
                >
                  {zones.map((v) => (
                    <option key={v}>{v}</option>
                  ))}
                </select>
              </Field>
              <Field
                label="接口名称"
                hint="可多选，最多50个接口；生成预览后逐项核对原区域及回退命令"
              >
                <MultiSelect
                  label="防火墙区域接口"
                  options={interfaces}
                  value={
                    Array.isArray(form.interfaces)
                      ? (form.interfaces as string[])
                      : []
                  }
                  onChange={(value) => update("interfaces", value)}
                />
              </Field>
            </div>
          ) : form.kind === "service" ? (
            <p>
                将{({start:"启动防火墙服务",stop:"停止防火墙服务",enable:"开启开机启动",disable:"关闭开机启动"} as Record<string,string>)[operation] || "调整防火墙服务"}。
                请核对管理连接的影响，确认后生成变更预览；未完成网络确认时将按预览方案回退。
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
                ((form.kind === "service-rule" || (form.kind === "rich" && form.rich_match === "service")) && !String(form.service || "").trim()) ||
                (form.kind === "zone-interface" && (!Array.isArray(form.interfaces) || !form.interfaces.length)) ||
                (["service", "zone-interface", "zone-source", "zone-policy", "masquerade", "rich"].includes(String(form.kind)) && !ack) ||
                (targetDraftChanged && (form.target_ack !== true || (form.target_apply === "immediate" && form.reload_ack !== true)))
              }
              onClick={() => void preview(operation, form)}
            >
              生成变更预览
            </Button>
          </div>
        </Modal>
      )}
      {plan && (
        <Modal className="firewall-dialog" title="防火墙变更预览" onClose={() => setPlan(null)} wide>
          <h3>{plan.summary}</h3>
          {plan.preview_only===true&&<p className="hint">只读预览：可以核对命令与回退方案，未应用变更。</p>}
          <FirewallChangeDetails plan={plan}/>
          {plan.warnings.map((v, i) => (
            <p className="hint" key={i}>
              {v}
            </p>
          ))}
          {!!plan.zone_changes?.length && (
            <div className="zone-change-preview">
              {plan.zone_changes.map((change) => (
                <section key={change.interface}>
                  <h4>
                    {change.interface} → {change.target_zone}
                  </h4>
                  <p className="small muted">
                    运行区域：{change.runtime_zone || "未绑定"} · 永久区域：
                    {change.permanent_zone || "未绑定"}
                  </p>
                  <strong>变更命令</strong>
                  <pre className="command-preview">
                    {change.commands.length
                      ? change.commands.join("\n")
                      : "已在目标区域，无需变更"}
                  </pre>
                  <strong>回退信息</strong>
                  <pre className="command-preview">
                    {change.rollback.length
                      ? change.rollback.join("\n")
                      : "无需回退"}
                  </pre>
                </section>
              ))}
            </div>
          )}
          {!plan.zone_changes?.length && (
            <>
              <pre className="command-preview">
                {plan.preview.length
                  ? plan.preview.join("\n")
                  : "仅变更管理归属，不重复添加系统规则。"}
              </pre>
            </>
          )}
          <div className="modal-actions">
            <Button onClick={() => setPlan(null)}>取消</Button>
            <Button
              variant="primary"
              disabled={plan.preview_only===true}
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
        <Modal className="firewall-dialog"
          title="防火墙规则详情"
          copyText={JSON.stringify(detail, null, 2)}
          onClose={() => setDetail(null)}
          wide
        >
          <FirewallRuleSummary value={record(detail)} />
        </Modal>
      )}
        </div>
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
            <FirewallServiceSelect value={String(value.service || "")} services={services} onChange={name => onChange("service", name)}/>
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
