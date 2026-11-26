import { FirewallDiagnosis } from "./firewall_diagnosis";
import { CrossZoneRules } from "./cross_zone_rules";
import { FirewallServiceSelect } from "./firewall_service_select";
import { AdaptiveSearch } from "./adaptive_search";
import { FirewallAuditFields,FirewallDestinationField,ZoneSourceEditor,ZonePolicyEditor,RichRuleEditor,FirewallChangeDetails,FirewallRuleSummary } from "./firewall_editors";
import { FirewallWorkspace } from "./firewall_workspace";
import { ALL_FIREWALL_ZONES, firewallBindings, firewallRuleOrigin, firewallRuleOriginLabel, firewallViewState } from "./firewall_view_state";
import { MultiSelect } from "./multi_select";
import { ResetFilters } from "./list_controls";
import { ExportButton, useRowSelection } from "./resource_controls";
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
const categories = [["all","域内规则"],["cross-zone","域间规则"],["masquerade","SNAT"],["forward","DNAT"],["diagnosis","规则诊断"]];
const zoneRuleTypes = [["ip","IP 规则"],["port","端口规则"],["rich","高级规则"],["service-rule","服务放行"]];
const ruleCategory = (row: Dict) => row.kind === "system" ? (["dnat","redirect"].includes(String(row.action)) ? "forward" : ["snat","masquerade"].includes(String(row.action)) ? "masquerade" : "all") : row.kind;
const strategies: Record<string, string> = {
  accept: "允许",
  drop: "丢弃",
  reject: "拒绝",
};
export function FirewallPage({ act }: { act: Act }) {
  const link = new URLSearchParams(location.search);
  const linkedRule = link.get("firewallRule") || "";
  const linkedDriver = linkedRule ? link.get("ruleDriver") : null;
  const linkedZone = linkedRule ? link.get("ruleZone") : null;
  const linkedCategory = linkedRule ? link.get("ruleCategory") : null;
  const [driver, setDriver] = useState(
    () => linkedDriver && ["firewalld", "nftables", "iptables"].includes(linkedDriver) ? linkedDriver : localStorage.getItem("foxray.firewall.driver") || "firewalld",
  );
  const [zone, setZone] = useState(linkedZone || "public"),
    [category, setCategory] = useState(categories.some(([id]) => id === linkedCategory && id !== "diagnosis") ? linkedCategory! : "all"),
    [ruleType, setRuleType] = useState("all"),
    [query, setQuery] = useState(linkedCategory === "cross-zone" ? "" : linkedRule),
    [protocol, setProtocol] = useState("all"),
    [status, setStatus] = useState("all"),
    [owner, setOwner] = useState("all"),
    [ruleAction, setRuleAction] = useState("all"),
    [configScope,setConfigScope]=useState("all"),
    [origin,setOrigin]=useState("all");
  const diagnosing = category === "diagnosis";
  const resource = useResource<Dict>(
    "/api/network/firewall?driver=" +
      driver +
      "&zone=" +
      encodeURIComponent(zone),
    15000,
    category !== "cross-zone" && !diagnosing,
  );
  const serviceResource=useResource<Dict>("/api/network/firewall/service?driver="+encodeURIComponent(driver),5000,!diagnosing);
  const crossZone = category === "cross-zone";
  const crossResource = useResource<Dict>("/api/network/firewall/cross-zone", 30000, crossZone);
  const originResource = useResource<Dict>("/api/network/firewall/origins", 30000, driver === "firewalld" && !diagnosing && origin !== "firewalld");
  const [crossFilter, setCrossFilter] = useState("all");
  const [lastRead, setLastRead] = useState(0), [refreshing, setRefreshing] = useState(false);
  useEffect(()=>{if(resource.data)setLastRead(Date.now())},[resource.data]);
  useEffect(()=>{if(crossResource.data)setLastRead(Date.now())},[crossResource.data]);
  const [form, setForm] = useState<Dict | null>(null),
    [operation, setOperation] = useState("add"),
    [plan, setPlan] = useState<Plan | null>(null),
    [failure, setFailure] = useState(""),
    [detail, setDetail] = useState<unknown>(null),
    [ack, setAck] = useState(false),
    [sort, setSort] = useState({ key: "priority", direction: 1 });
  const allZones = driver === "firewalld" && zone === ALL_FIREWALL_ZONES;
  const view = firewallViewState(resource.data, driver, zone, resource.error || "");
  const moduleRows = driver === "firewalld" && origin !== "firewalld"
    ? list(originResource.data?.entries).filter(row => origin === "all" || firewallRuleOrigin(row, driver) === origin)
    : [];
  const crossItems = [
    ...(origin === "all" || origin === "firewalld" ? list(crossResource.data?.items) : []),
    ...moduleRows.filter(row => row.kind === "cross-zone"),
  ];
  const originsNotice = driver === "firewalld" && origin !== "firewalld"
    ? originResource.error || (originResource.data?.complete === false ? "部分模块规则未读到，列表可能不完整" : "")
    : "";
  const crossDisplay = crossResource.data ? {...crossResource.data, items: crossItems} : null;
  const scopedModules = moduleRows.filter(row => row.kind !== "cross-zone" && (allZones || row.zone === zone));
  const zoneRows = view.current && !resource.error && (origin === "all" || origin === "firewalld") ? list(resource.data?.entries) : [];
  const all = [...zoneRows, ...scopedModules].filter(r=>configScope==="runtime" ? !!(r.runtime||r.partial_runtime) : configScope==="permanent" ? !!(r.permanent||r.partial_permanent) : configScope==="difference" ? !!(r.drift||r.partial_runtime||r.partial_permanent||!!r.runtime!==!!r.permanent) : true),
    zones = Array.isArray(resource.data?.zones) ? resource.data.zones.map(String) : allZones ? [] : [zone],
    running = view.native ? view.canManage : view.running,
    native = view.native,
    writable = view.canManage;
  const rowKey = (r: Dict) => allZones ? `${String(r.zone || "")}::${String(r.id)}` : String(r.id);
  const renderZoneBinding = (r: Dict) => {
    const name = text(r.zone || "区域未确认");
    const device = String((category === "masquerade" ? r.out_interface : r.in_interface) || "");
    const binding = allZones
      ? record(record(resource.data?.zone_bindings)[String(r.zone || "")])
      : view.current && r.zone === zone ? record(resource.data?.zone_info) : {};
    const interfaces = firewallBindings(binding.interfaces);
    const sources = firewallBindings(binding.sources);
    return <div className="firewall-row-zone">
      <strong>{name}</strong>
      {device ? <small>{device}（规则指定）</small> : interfaces.length > 1 ? (
        <details><summary>{interfaces.length} 个接口（区域绑定）</summary><small>{interfaces.join("、")}</small></details>
      ) : interfaces.length === 1 ? <small>{interfaces[0]}（区域绑定）</small> : sources.length === 0 && <small>接口未确认</small>}
      {!device && sources.length > 0 && <small>来源归属 {sources.join("、")}</small>}
    </div>;
  };
  const categoryLabel = categories.find(([key]) => key === category)?.[1] ?? "域内规则";
  const ruleHeading = category === "masquerade" || category === "forward" ? `${categoryLabel} 规则` : categoryLabel;
  const ruleAnchor = useRef<HTMLDivElement>(null);
  const rows = useMemo(
    () =>
      all
        .filter(
          (r) =>
            (category === "all" ? !["forward", "masquerade"].includes(String(ruleCategory(r))) && (ruleType === "all" || (ruleType === "system" ? r.editable === false : r.kind === ruleType)) : ruleCategory(r) === category) &&
            (protocol === "all" || r.protocol === protocol) &&
            (status === "all" || !!r.enabled === (status === "enabled")) &&
            (owner === "all" || !!r.managed === (owner === "managed")) &&
            (ruleAction === "all" || (ruleAction === "blocked" ? ["drop","reject"].includes(String(r.action).toLowerCase()) : (category === "masquerade" ? (r.action === "snat" ? "snat" : "masquerade") : String(r.action).toLowerCase()) === ruleAction)) &&
            (linkedRule && query === linkedRule ? String(r.id) === linkedRule : JSON.stringify(r).toLowerCase().includes(query.toLowerCase())),
        )
        .sort((a, b) => {
          const x = a[sort.key],
            y = b[sort.key];
          if (sort.key === "priority") {
            if (x == null || y == null) return x == null ? (y == null ? 0 : 1) : -1;
            return (Number(x) - Number(y)) * sort.direction;
          }
          if (sort.key === "port")
            return (
              ((parseInt(String(x)) || 0) - (parseInt(String(y)) || 0)) *
              sort.direction
            );
          return text(x).localeCompare(text(y), "zh-CN") * sort.direction;
        }),
    [all, category, ruleType, native, protocol, status, owner, ruleAction, query, sort, linkedRule],
  );
  const { chosen, setSelected, column: selectionColumn } = useRowSelection(rows, rowKey);
  const managedChosen = chosen.filter((row) => row.managed);
  const externalChosen = chosen.filter((row) => !row.managed);
  const canAdoptSelection = externalChosen.length > 0 &&
    externalChosen.length <= 50 && externalChosen.every((row) => row.editable !== false);
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
    if (item.kind === "service" ? !firewallViewState(serviceResource.data || resource.data, driver, zone, serviceResource.error).canServiceControl : !writable) {
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
  const adopting = operation === "adopt";
  const adoptionItems = adopting ? list(form?.adoptionItems) : [];
  const defaultRule = (kind = category === "all" ? "port" : category) => ({
    kind,
    ...(kind === "rich" ? {rich_match:"port",family:"ipv4",storage:"rich-rule"} : {}),
    driver,
    zone: native ? "host" : allZones ? String(record(resource.data?.zone_policy).default_zone || zones[0] || "") : zone,
    protocol: "tcp",
    port: "",
    source: "",
    action: category === "masquerade" ? "masquerade" : "accept",
    to_source: "",
    out_interface: "",
    enabled: true,
    target_ip: "",
    target_port: "",
    family: kind === "rich" ? "ipv4" : "",
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
  const showPriority = category === "rich" || all.some(row => row.priority != null && (native || row.storage === "rich-rule" || row.storage === "direct-rule"));
  const canPrioritize = (row: Dict) => driver !== "ufw" && row.kind !== "service-rule" && (native || row.storage === "rich-rule" || row.storage === "direct-rule" || row.kind === "ip" || row.kind === "rich");
  const movePriority = (row: Dict, offset: number) => {
    const field = driver === "nftables" ? "hook_priority" : "priority";
    const current = Number(row[field] ?? 0);
    const scope = rows.filter(item => rowKey(item) !== rowKey(row) && (!allZones || item.zone === row.zone) && item[field] != null && (!native || item.kind === row.kind));
    const neighbor = scope.map(item => Number(item[field])).filter(value => offset < 0 ? value <= current : value >= current)
      .sort((a,b) => offset < 0 ? b-a : a-b)[0];
    const priority = (neighbor ?? current) + offset;
    if (priority < -32768 || priority > 32767) { setFailure("已达到优先级范围边界"); return; }
    start("replace", {...row, [field]:priority});
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
          {showPriority && canPrioritize(r) && <>
            <Button variant="ghost" disabled={!writable} onClick={()=>movePriority(r,-1)}>提高优先级</Button>
            <Button variant="ghost" disabled={!writable} onClick={()=>movePriority(r,1)}>降低优先级</Button>
          </>}
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
          onClick={() => start("adopt", r)}
        >
          纳管
        </Button>
      )}
    </div>
  );
  const changeRuleType = (value: string) => {
    setRuleType(value);
    setSelected(new Set());
  };
  const filterControls = {
    name: (
      <Field label="类型">
        <select aria-label="规则类型" value={ruleType} onChange={event => changeRuleType(event.target.value)}>
          <option value="all">全部类型</option>
          {zoneRuleTypes.filter(([kind]) => !native || kind === "ip" || kind === "port")
            .map(([kind, label]) => <option key={kind} value={kind}>{label}</option>)}
          <option value="system">系统规则</option>
        </select>
      </Field>
    ),
    action: <Field label="规则动作"><select aria-label="防火墙动作筛选" value={ruleAction} onChange={e=>setRuleAction(e.target.value)}><option value="all">全部动作</option>{category === "masquerade" ? <><option value="snat">SNAT</option><option value="masquerade">MASQUERADE</option></> : <><option value="accept">允许</option><option value="drop">丢弃</option><option value="reject">拒绝</option><option value="blocked">阻断（丢弃／拒绝）</option></>}</select></Field>,
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
    selectionColumn,
    showPriority ? {
      key: "priority", label: "优先级", heading: heading("priority","优先级"),
      render: row => row.priority != null && (native || canPrioritize(row)) ? text(row.priority) : "—",
    } : { key: "serial", label: "序号", render: (_, i) => i + 1 },
  ];
  const showZoneBinding = !native;
  const zoneBindingColumn: Column<Dict> = {
    key: "zone",
    label: category === "masquerade" ? "出口区域 / 出接口" : category === "forward" ? "入口区域 / 入接口" : "入口区域 / 接口",
    render: renderZoneBinding,
  };
  if (category === "all") columns.push(
    {key:"name",label:"名称／类型",heading:heading("kind","名称／类型"),render:r=><><strong>{text(r.service || (r.kind === "system" ? [r.family, r.table, r.chain].filter(Boolean).join(" / ") : "") || r.port || r.source || (r.kind === "masquerade" ? "地址转换" : "规则"))}</strong><small>{r.kind === "system" ? "系统规则（只读）" : r.editable===false ? "只读规则" : [...zoneRuleTypes,...categories].find(([id])=>id===r.kind)?.[1] || String(r.kind || "系统规则")}</small></>},
    {key:"protocol",label:"协议／端口",heading:heading("protocol","协议／端口"),render:r=>r.service ? (
      <div>
        {Array.isArray(r.service_ports) && r.service_ports.length ? r.service_ports.map(String).join("、") : "端口未列出"}
        <small className="muted firewall-service-definition-label">服务定义</small>
      </div>
    ) : [r.protocol ? String(r.protocol).toUpperCase() : "全部",r.port].filter(Boolean).join(" / ")},
  );
  if (category === "service-rule")
    columns.push({
      key: "service",
      label: "服务",
      heading: heading("service", "服务"),
      render: (r) => <strong>{text(r.service)}</strong>,
    });
  if (category === "forward") columns.push({
    key: "protocol", label: "协议 / 端口", heading: heading("protocol", "协议 / 端口"),
    render: r => [r.protocol && String(r.protocol).toUpperCase(), r.port].filter(Boolean).join(" / ") || "—",
  });
  if (category === "port" || category === "rich")
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
    label: "源 IP",
    heading: heading("source", "源 IP"),
    render: (r) => (
      <span className="ellipsis" title={text(r.source || "全部来源")}>
        {text(r.source || "全部来源")}
      </span>
    ),
  });
  if ((category === "all" || category === "forward" || all.some(r => r.destination)) && category !== "service-rule")
    columns.push({
      key: "destination",
      label: "目标 IP",
      heading: heading("destination", "目标 IP"),
      render: r => <span className="ellipsis" title={String(r.destination || "")}>{text(r.destination || "任意目标")}</span>,
    });
  if (category === "forward")
    columns.push({
      key: "target",
      label: "转换目标 / 端口",
      render: (r) => (
        <>
          {text(r.target_ip || "本机")}
          <small>{r.target_port ? "转换后端口 " + text(r.target_port) : "保留原端口"}</small>
        </>
      ),
    }, ...(showZoneBinding ? [zoneBindingColumn] : []));
  else if (category === "masquerade")
    columns.push(
      {
        key: "action",
        label: "动作",
        render: (r) => String(r.action === "snat" ? "SNAT" : "MASQUERADE"),
      },
      {
        key: "to_source",
        label: "转换源",
        render: (r) =>
          r.action === "snat" ? text(r.to_source) : "自动使用出接口地址",
      },
      ...(showZoneBinding ? [zoneBindingColumn] : [{
        key: "out_interface",
        label: "出接口",
        render: (r: Dict) => text(r.out_interface || "区域出接口"),
      }]),
    );
  else if (category !== "service-rule")
    columns.push(...(showZoneBinding ? [zoneBindingColumn] : []), {
      key: "action",
      label: "动作",
      heading: heading("action", "动作"),
      render: (r) => (
        <span className={r.action === "accept" ? "accent" : ["drop","reject"].includes(String(r.action)) ? "poor" : "muted"}>
          {r.editable === false && r.kind !== "system" && !r.rule_origin
            ? "查看规则详情"
            : strategies[String(r.action)] || ({masquerade:"地址伪装",snat:"源地址转换"} as Record<string,string>)[String(r.action)] || text(r.action)}
        </span>
      ),
    });
  const reorderedKeys = category === "forward" ? ["source", "destination", "protocol", "zone", "target"]
    : category === "all" ? ["source", "destination", "protocol", "zone", "action"]
    : category === "masquerade" ? ["to_source", "zone", "out_interface", "action"] : [];
  if (reorderedKeys.length) {
    const first = columns.findIndex(column => reorderedKeys.includes(column.key));
    if (first >= 0) {
      const group = reorderedKeys.flatMap(key => columns.filter(column => column.key === key));
      const rest = columns.filter(column => !reorderedKeys.includes(column.key));
      rest.splice(first, 0, ...group);
      columns.splice(0, columns.length, ...rest);
    }
  }
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
  if (!native) columns.push({
    key: "rule_origin",
    label: "规则来源",
    render: r => <><span>{firewallRuleOriginLabel(r)}</span><small>{r.editable === false ? "只读" : r.managed ? "可编辑" : "纳管后可编辑"}</small></>,
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
            {r.permanent ? "已持久化" : r.runtime ? "仅运行时" : "未载入"}
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
                      setRuleType("all");
                      setSort({key:"priority",direction:1});
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
        {!diagnosing && <FirewallWorkspace crossZone={crossZone} serviceData={serviceResource.data} serviceError={serviceResource.error || ""} rulesLoading={crossZone ? crossResource.loading : resource.loading} data={crossZone ? (crossResource.data ? {...serviceResource.data, ...crossDisplay, zone, driver, entries:crossItems} : null) : resource.data ? {...resource.data,entries:all} : null} error={(crossZone ? crossResource.error : resource.error) || ""} driver={driver} zone={zone} lastRead={lastRead} refreshing={refreshing}
          refresh={()=>{setRefreshing(true);void Promise.all([(crossZone ? crossResource : resource).refresh(),serviceResource.refresh(),...(driver === "firewalld" && origin !== "firewalld" ? [originResource.refresh()] : [])]).finally(()=>setRefreshing(false))}}
          changeDriver={value=>{setDriver(value);setOrigin("all");setCategory("all");setRuleType("all");localStorage.setItem("foxray.firewall.driver",value);setZone(value==="firewalld"?"public":"host");setSelected(new Set());setQuery("");setProtocol("all");setStatus("all");setOwner("all");setRuleAction("all");setConfigScope("all");setForm(null);setPlan(null);setFailure("")}}
          changeZone={value=>{setZone(value);setSelected(new Set());setQuery("");setProtocol("all");setStatus("all");setOwner("all");setRuleAction("all");setConfigScope("all");setForm(null);setPlan(null);setFailure("")}}
          manageInterfaces={()=>start("add",{kind:"zone-interface",zone,interfaces:[],driver})}
          serviceAction={action=>start(action,{kind:"service",driver})}
          selectedFilter={crossZone ? crossFilter : category === "all" ? ruleAction === "blocked" ? "blocked" : owner === "managed" ? "managed" : owner === "external" ? "external" : "all" : ""}
          filterRules={filter=>{if(crossZone){setCrossFilter(filter);document.getElementById("cross-zone-rules")?.scrollIntoView({behavior:"smooth",block:"start"});return}setCategory("all");setOwner(filter==="managed"?"managed":filter==="external"?"external":"all");setRuleAction(filter==="blocked"?"blocked":"all");setQuery("");setProtocol("all");setStatus("all");setSelected(new Set());ruleAnchor.current?.scrollIntoView({behavior:"smooth",block:"start"})}}
          viewRules={kind=>{setCategory(kind);setOwner("all");setRuleAction("all");setQuery("");setProtocol("all");setStatus("all");setSelected(new Set());ruleAnchor.current?.scrollIntoView({behavior:"smooth",block:"start"})}}
          addService={()=>start("add",{...defaultRule(),kind:"service-rule",service:""})}
          origin={origin} changeOrigin={value=>{setOrigin(value);setSelected(new Set())}}
          configScope={configScope} changeScope={value=>{setConfigScope(value);setSelected(new Set());setQuery("")}}
          manageSources={()=>start("add",{kind:"zone-source",driver,zone,source:"",binding_action:"bind"})}
          editDefault={()=>start("add",{kind:"zone-default",driver,zone:String(record(resource.data?.zone_policy).default_zone||zone)})}
          editPolicy={()=>{const policy=record(resource.data?.zone_policy),runtime=record(policy.runtime),permanent=record(policy.permanent);start("add",{kind:"zone-policy",driver,zone,target:String(permanent.target||runtime.target||"default"),target_apply:"permanent",target_ack:false,reload_ack:false,masquerade:runtime.masquerade===true,icmp_blocks:Array.isArray(runtime.icmp_blocks)?runtime.icmp_blocks:[],icmp_inversion:runtime.icmp_inversion===true,forward:typeof runtime.forward==="boolean"?runtime.forward:undefined})}}
        />}
      {originsNotice && !diagnosing && <ErrorLine error={originsNotice} />}
      {diagnosing ? <FirewallDiagnosis act={act} /> : crossZone ? <CrossZoneRules linkedRule={linkedCategory === "cross-zone" ? linkedRule : ""} act={act} resource={{...crossResource,data:crossDisplay}} filter={crossFilter} resetFilter={()=>setCrossFilter("all")} /> : <Panel className="firewall-main-panel management-panel">
        <ErrorLine error={resource.error || failure} />
        {!!resource.data?.error && (
          <p className="hint">{text(resource.data.error)}</p>
        )}
        <div className="firewall-rules-heading" ref={ruleAnchor}><h3>{labelIcon(categoryLabel, 20)}{ruleHeading} <span>{native ? driver : allZones ? "所有区域" : zone}</span></h3><span className="muted">{native ? "当前工具规则快照" : ({all:"运行时与永久配置 · 合并展示",runtime:"运行时配置",permanent:"永久配置",difference:"存在差异的配置"} as Record<string,string>)[configScope]}</span></div>
        <CompactFilters
          reset={<ResetFilters
                active={
                  query !== "" || ruleType !== "all" ||
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
                  setRuleType("all");
                  setProtocol("all");
                  setStatus("all");
                  setOwner("all");setRuleAction("all");
                }}
              />}
          before={
            <>
              {category === "all" ? <ActionMenu label="新增规则" triggerLabel="新增规则" showLabel items={zoneRuleTypes.filter(([kind])=>!native||kind==="ip"||kind==="port").map(([kind,label])=>({label,disabled:!running||!writable,onClick:()=>start("add",defaultRule(kind))}))} /> :
                <Button disabled={!running||!writable} onClick={()=>start("add",defaultRule())}><Plus size={15}/>添加</Button>}

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
                            !canAdoptSelection ||
                            !writable,
                          onClick: () => start("adopt", { adoptionItems: externalChosen }),
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
          <AdaptiveSearch tableWidthKey="firewall-rules">
            <Search size={15} />
            <input
              placeholder="查询端口、IP、网段或协议"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </AdaptiveSearch>
        </CompactFilters>

        <Table columnSizingKey="firewall-rules"
          rows={rows}
          total={all.filter(row=>category === "all" ? !["forward","masquerade"].includes(String(ruleCategory(row))) : ruleCategory(row) === category).length}
          selectedCount={chosen.length}
          keepHeaders
          columns={columns.map((c) =>
            (c.key === "name" || ["protocol", "state", "owner", "action"].includes(c.key))
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
                            name: ruleType !== "all",
                            protocol: protocol !== "all",
                            state: status !== "all",
                            owner: owner !== "all",
                            action: ruleAction !== "all",
                          }[c.key as keyof typeof filterControls]
                        }
                      >
                        {
                          filterControls[
                            c.key as keyof typeof filterControls
                          ]
                        }
                        <Button
                          onClick={() => {
                            if (c.key === "name") changeRuleType("all");
                            else if (c.key === "protocol") setProtocol("all");
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
          rowKey={rowKey}
        />
      </Panel>}
      {form && (
        <Modal className="firewall-dialog"
          description={adopting ? undefined : (
            <p>运行规则和永久声明同时处理；不全局 reload。允许规则仍可能受其他防火墙的拒绝规则限制。</p>
          )}
          title={adopting ? "纳管防火墙规则" :
            form.kind === "zone-default" ? "设置默认区域" : form.kind === "zone-source" ? "管理来源地址归属" : form.kind === "zone-policy" ? "编辑区域策略" : form.kind === "rich" ? (operation==="add"?"添加高级规则":"编辑高级规则") : form.kind === "zone-interface"
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
            {allZones && operation === "add" && !String(form.kind).startsWith("zone-") && <Field label="所属区域">
              <select value={String(form.zone || "")} onChange={event => update("zone", event.target.value)}>
                {zones.map(candidate => <option key={candidate}>{candidate}</option>)}
              </select>
            </Field>}
          {adopting ? (
            <>
              <p>将 {adoptionItems.length || 1} 条现有规则纳入 FoXray 管理。纳管只登记归属，现有规则保持生效。</p>
              <ul>
                {(adoptionItems.length ? adoptionItems : [form]).map(rule => (
                  <li key={rowKey(rule)}>
                    {text(rule.zone)} · {text(rule.raw || rule.name || rule.id)}
                  </li>
                ))}
              </ul>
            </>
          ) : form.kind === "zone-default" ? <Field label="默认区域"><select value={String(form.zone)} onChange={event=>update("zone",event.target.value)}>{zones.map(item=><option key={item}>{item}</option>)}</select></Field> : form.kind === "zone-source" ? <ZoneSourceEditor value={form} onChange={update} zones={zones} sources={String(record(resource.data?.zone_info).sources||" ").trim().split(/\s+/).filter(Boolean)}/> : form.kind === "zone-policy" ? <ZonePolicyEditor value={form} onChange={update} policy={record(resource.data?.zone_policy)}/> : form.kind==="rich" ? <RichRuleEditor value={form} onChange={update} services={Array.isArray(resource.data?.available_services)?resource.data.available_services.map(String):[]}/> : form.kind === "zone-interface" ? (
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
            <>
            <FirewallForm
              value={form}
              onChange={update}
              services={
                Array.isArray(resource.data?.available_services)
                  ? (resource.data.available_services as string[])
                  : []
              }
            />
            </>
          )}
          <label className="check risk">
            <input
              type="checkbox"
              checked={ack}
              onChange={(e) => setAck(e.target.checked)}
            />
            {adopting ? "我确认将上述规则纳入 FoXray 管理" : "我已确认规则和管理连接的影响，允许本次变更；必要时使用独立回退"}
          </label>
          <ErrorLine error={failure} />
          <div className="modal-actions">
            <Button onClick={() => setForm(null)}>取消</Button>
            <Button
              variant="primary"
              disabled={adopting ? !ack : (
                ((form.kind === "service-rule" || (form.kind === "rich" && form.rich_match === "service")) && !String(form.service || "").trim()) ||
                (form.kind === "zone-interface" && (!Array.isArray(form.interfaces) || !form.interfaces.length)) ||
                (["service", "zone-interface", "zone-source", "zone-policy", "zone-default", "masquerade", "rich"].includes(String(form.kind)) && !ack) ||
                (targetDraftChanged && (form.target_ack !== true || (form.target_apply === "immediate" && form.reload_ack !== true)))
              )}
              onClick={() => void (adoptionItems.length
                ? preview(operation, {}, adoptionItems)
                : preview(operation, form))}
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
                void act("/api/network/apply", { id }).then(ok=>{if(ok)void Promise.all([serviceResource.refresh(),(crossZone ? crossResource : resource).refresh()])});
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
          {!!record(detail).native_summary && <p>{text(record(detail).native_summary)}</p>}
          {record(detail).editable === false && !!record(detail).table && !!record(detail).raw && (
            <details>
              <summary>原始规则</summary>
              <pre style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{text(record(detail).raw)}</pre>
            </details>
          )}
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
    addressOnly = kind === "forward" && value.protocol === "all",
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
                <option value="">按匹配地址自动判断／双栈</option>
                <option value="ipv4">IPv4</option>
                {kind !== "forward" && kind !== "masquerade" && (
                  <option value="ipv6">IPv6</option>
                )}
              </select>
            </Field>
            {kind !== "ip" && kind !== "masquerade" && (
              <>
                <Field label="端口／端口范围" hint="443 或 8000-8100">
                  <input disabled={addressOnly}
                    value={String(value.port || "")}
                    onChange={(e) => onChange("port", e.target.value)}
                  />
                </Field>
                <Field label="协议">
                  <select
                    value={String(value.protocol || "tcp")}
                    onChange={(e) => {onChange("protocol", e.target.value); if(e.target.value==="all"){onChange("port", "");onChange("target_port", "")}}}
                  >
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                    <option value="tcpudp">TCP + UDP</option>
                    {!!native && kind === "forward" && <option value="all">整地址转换（不限协议和端口）</option>}
                  </select>
                </Field>
              </>
            )}
            <Field
              label="源 IP"
              hint={
                kind === "masquerade"
                  ? "支持多个 IPv4 地址或 CIDR，用英文逗号分隔；任一来源匹配即生效"
                  : "支持 IPv4 或 IPv6 地址／网段"
              }
            >
              <input
                value={String(value.source || "")}
                onChange={(e) => onChange("source", e.target.value)}
                placeholder="192.168.1.0/24"
              />
            </Field>
            {(!native || kind === "forward") && !(kind === "masquerade" && value.action === "snat") && (
              <FirewallDestinationField value={value} onChange={destination => onChange("destination", destination)} />
            )}
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
                <Field label="转换目标" hint="填写 IPv4 地址；留空为本机">
                  <input
                    value={String(value.target_ip || "")}
                    onChange={(e) => onChange("target_ip", e.target.value)}
                    placeholder="192.168.1.20"
                  />
                </Field>
                <Field label="转换后端口／范围" hint="填写转换后 IP 时可留空，保留原端口">
                  <input
                    disabled={addressOnly} value={String(value.target_port || "")}
                    onChange={(e) => onChange("target_port", e.target.value)}
                    placeholder="8080"
                  />
                </Field>
              </>
            )}
          </>
        )}
      </div>
      {kind !== "service-rule" && value.driver !== "ufw" && <details className="advanced-form">
        <summary>高级选项{(value.priority || value.hook_priority != null || value.logging || value.audit || value.action_rate) ? " · 已设置" : ""}</summary>
            {value.driver !== "ufw" && (
              <Field
                label="优先级（越小越先匹配）"
                hint={value.driver === "iptables" ? "仅在同一 FoXray 管理链组内排序" : value.driver === "nftables" ? "同一 hook 中数值越小越早执行" : "范围为 -32768 至 32767"}
              >
                <NumberInput
                  type="number"
                  min={-32768}
                  max={32767}
                  value={Number(value.driver === "nftables" ? value.hook_priority ?? (kind === "forward" ? -110 : kind === "masquerade" ? 100 : -10) : value.priority ?? 0)}
                  onChange={(e) => onChange(value.driver === "nftables" ? "hook_priority" : "priority", Number(e.target.value))}
                />
              </Field>
            )}
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
        {!native && ["port","ip"].includes(kind) && <FirewallAuditFields value={value} onChange={onChange} />}
      </details>}
    </>
  );
}
