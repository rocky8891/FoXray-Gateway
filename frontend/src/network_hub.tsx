import { LinkReadyIcon } from "./workspace_icons";
import { ActionMenu } from "./action_menu";
import { interfaceLinkState, interfaceLinkLabels, interfaceOwner, interfaceOwnerLabels, interfaceEditReason } from "./interface_display";
import "./interface_refinements.css";
import { NetworkBatchDialog } from "./network_batch_dialog";
import { AdaptiveSearch } from "./adaptive_search";
import { ResetFilters } from "./list_controls";
import { ExportButton, useRowSelection } from "./resource_controls";
import { SelectionActions } from "./selection_actions";
import { CompactFilters, ListFilter, SortHeading } from "./list_controls";
import { DNSPage } from "./dns";
import { ModuleHeading } from "./hubs";
import { useState } from "react";
import { AlertTriangle, RefreshCw, Search, Network, Power, Layers } from "lucide-react";
import { api, errorMessage, list, text, type Dict, type Plan } from "./api";
import {
  Button,
  Field,
  SettingInfo,
  NumberInput,
  ErrorLine,
  JsonDetails,
  Modal,
  PageHeader,
  Panel,
  Table,
  useResource,
  type Column,
} from "./ui";
import { RoutingPage } from "./routing";
import type { Act } from "./nodes";
export function NetworkHub({ act }: { act: Act }) {
  const [view, setView] = useState<"interfaces" | "routes" | "rules" | "dns">(
      () =>
        (["interfaces", "routes", "rules", "dns"].includes(
          localStorage.getItem("foxray.network.tab") || "",
        )
          ? localStorage.getItem("foxray.network.tab")
          : "interfaces") as "interfaces",
    ),
    [filter, setFilter] = useState<
      { table: string; family: string } | undefined
    >();
  return (
    <>
      <ModuleHeading
        title="网络管理"
        description="接口、静态路由与策略路由管理"
        tabs={[
          ["interfaces", "接口"],
          ["routes", "路由"],
          ["rules", "策略路由"],
          ["dns", "DNS"],
        ]}
        value={view}
        onChange={(v) => {
          setView(v as typeof view);
          localStorage.setItem("foxray.network.tab", v);
          setFilter(undefined);
        }}
      />
      {view === "dns" ? (
        <DNSPage act={act} />
      ) : view === "interfaces" ? (
        <Interfaces act={act} />
      ) : (
        <RoutingPage
          key={view + JSON.stringify(filter)}
          kind={view}
          act={act}
          initialFilter={filter}
          onTable={(table, family) => {
            setFilter({ table, family });
            setView("routes");
            localStorage.setItem("foxray.network.tab", "routes");
          }}
        />
      )}
    </>
  );
}
function InterfaceAddressFields({
  value,
  onChange,
  gateways = [],
  existing = false,
}: {
  value: Dict;
  onChange: (value: Dict) => void;
  gateways?: Dict[];
  existing?: boolean;
}) {
  const set = (key: string, next: unknown) =>
    onChange({ ...value, [key]: next });
  return (
    <>
      <Field
        label="IP地址"
        info="静态IPv4或IPv6地址。添加可留空。已有接口只替换选中的地址，其他地址保留。"
      >
        <input
          aria-label="接口IP地址"
          value={String(value.address || "")}
          placeholder="192.0.2.10 或 2001:db8::10"
          onChange={(e) => set("address", e.target.value.trim())}
        />
      </Field>
      <Field
        label="掩码／前缀"
        info="IPv4可用24或255.255.255.0；IPv6填写0～128的前缀长度。"
      >
        <input
          aria-label="接口掩码或前缀"
          value={String(value.prefix ?? 24)}
          onChange={(e) => set("prefix", e.target.value.trim())}
        />
      </Field>
      <Field label="默认网关操作">
        <select
          aria-label="默认网关操作"
          value={String(value.gateway_action || "keep")}
          onChange={(e) => set("gateway_action", e.target.value)}
        >
          <option value="keep">保持现有网关</option>
          <option value="set">设置网关</option>
          {existing && <option value="remove">删除所选网关</option>}
        </select>
      </Field>
      {value.gateway_action !== "keep" && (
        <>
          {existing && (
            <Field label="要修改的默认路由">
              <select
                aria-label="要修改的默认路由"
                value={String(value.gateway_key || "")}
                onChange={(e) => {
                  const row = gateways.find((r) => r.key === e.target.value);
                  onChange({
                    ...value,
                    gateway_key: e.target.value,
                    gateway: row?.gateway || "",
                    family: row?.family || 4,
                  });
                }}
              >
                <option value="">新增独立默认路由</option>
                {gateways.map((r) => (
                  <option key={String(r.key)} value={String(r.key)}>
                    {String(r.gateway || "直连")} · IPv{String(r.family)} ·
                    优先级{String(r.metric || 0)}
                  </option>
                ))}
              </select>
            </Field>
          )}
          {value.gateway_action === "set" && (
            <>
              <Field
                label="网关地址"
                info="只操作main表。网关必须在当前接口可达网段内；不改其他接口默认路由。"
              >
                <input
                  aria-label="接口网关地址"
                  value={String(value.gateway || "")}
                  onChange={(e) => set("gateway", e.target.value.trim())}
                />
              </Field>
              {!value.gateway_key && (
                <Field
                  label="网关优先级"
                  info="新增路由默认600，数值越小优先级越高；不能与已有默认路由优先级冲突。"
                >
                  <NumberInput
                    aria-label="网关优先级"
                    type="number"
                    min={0}
                    max={2147483647}
                    value={Number(value.gateway_metric ?? 600)}
                    onChange={(e) =>
                      set("gateway_metric", Number(e.target.value))
                    }
                  />
                </Field>
              )}
            </>
          )}
        </>
      )}
      <label className="check">
        <input
          type="checkbox"
          checked={!!value.activate}
          onChange={(e) => set("activate", e.target.checked)}
        />
        {existing ? "如接口已停用，应用时启用" : "创建后启用接口"}
      </label>
    </>
  );
}
function Interfaces({ act }: { act: Act }) {
  const resource = useResource<Dict>("/api/network/interfaces", 10000);
  const [creating, setCreating] = useState(false);
  const [createItem, setCreateItem] = useState<Dict>({
    type: "veth",
    name: "",
    peer: "",
    parent: "",
    vlan_id: 1,
    address: "",
    prefix: 24,
    gateway_action: "keep",
    gateway: "",
    gateway_metric: 600,
    activate: false,
  });
  const [previewing, setPreviewing] = useState(false);
  const [editing, setEditing] = useState<Dict | null>(null);
  const [editItem, setEditItem] = useState<Dict>({});
  const [query, setQuery] = useState(""),
    [columnFilters, setColumnFilters] = useState<Record<string, string>>({}),
    [sort, setSort] = useState({ key: "name", direction: 1 }),
    [details, setDetails] = useState<unknown>(null),
    [target, setTarget] = useState<Dict | null>(null),
    [ack, setAck] = useState(false),
    [failure, setFailure] = useState(""),
    [plan, setPlan] = useState<Plan | null>(null);
  const columnValue = (r: Dict, key: string): string | number =>
    key === "name"
      ? text(r.ifname)
      : key === "ip"
        ? list(r.addr_info)
            .map((ip) => text(ip.local) + "/" + text(ip.prefixlen))
            .join(", ") || "—"
        : key === "mac"
          ? text(r.address)
          : key === "type" ? String(r.type||"未上报")
          : key === "link" ? interfaceLinkLabels[interfaceLinkState(r)]
          : key === "owner" ? interfaceOwnerLabels[interfaceOwner(r)]
          : key === "state"
            ? r.admin_up
              ? "已启用"
              : "已停用"
            : key === "mtu"
              ? Number(r.mtu || 0)
              : text(r[key]);
  const allRows = list(resource.data?.items);
  const searchedRows=allRows.filter(r=>[JSON.stringify(r),interfaceOwnerLabels[interfaceOwner(r)],interfaceLinkLabels[interfaceLinkState(r)]].join(" ").toLowerCase().includes(query.toLowerCase()));
  const ready=!!resource.data&&!resource.error&&resource.data.complete!==false;
  const types=[...new Set(searchedRows.map(r=>String(r.type||"未上报")))].sort();
  const rows = searchedRows
    .filter(
      (r) =>
        Object.entries(columnFilters).every(
          ([key, v]) => !v || String(columnValue(r, key)) === v,
        ),
    )
    .sort((a, b) => {
      const x = columnValue(a, sort.key),
        y = columnValue(b, sort.key);
      return (
        (typeof x === "number" && typeof y === "number"
          ? x - y
          : String(x).localeCompare(String(y), undefined, { numeric: true })) *
        sort.direction
      );
    });
  const columnHeading = (key: string, label: string) => (
    <span className="table-heading-tools">
      <SortHeading
        label={label}
        direction={sort.key === key ? sort.direction : undefined}
        onClick={() =>
          setSort({ key, direction: sort.key === key ? -sort.direction : 1 })
        }
      />
      <ListFilter label={label + "筛选"} column active={!!columnFilters[key]}>
        <Field label={label}>
          <select
            aria-label={label + "筛选值"}
            value={columnFilters[key] || ""}
            onChange={(e) =>
              setColumnFilters({ ...columnFilters, [key]: e.target.value })
            }
          >
            <option value="">全部</option>
            {[...new Set(allRows.map((r) => String(columnValue(r, key))))]
              .sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))
              .map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
          </select>
        </Field>
        <Button
          onClick={() => setColumnFilters({ ...columnFilters, [key]: "" })}
        >
          重置
        </Button>
      </ListFilter>
    </span>
  );
  const [bulk, setBulk] = useState<{ operation: string; rows: Dict[] } | null>(
    null,
  );
  const selection = useRowSelection(rows, (r) => String(r.ifindex));
  const chooseFilter=(key:string,value:string)=>{setColumnFilters(old=>({...old,[key]:old[key]===value?"":value}));selection.setSelected(new Set())};
  const columns: Column<Dict>[] = [
    selection.column,
    { key: "serial", label: "序号", render: (_, i) => i + 1 },
    {
      key: "name",
      label: "接口",
      width:"250px",
      className:"interface-name-column",
      render: (r) => (
        <><strong className="ellipsis" title={text(r.ifname)}>{text(r.ifname)}</strong><div className="interface-badges"><span>{interfaceOwnerLabels[interfaceOwner(r)]}</span>{list(r.gateways).length>0&&<span className="default-route" title="系统main表中有默认路由使用此接口，不代表某次连接实际经过它">默认路由</span>}{!!r.protected&&!r.managed_by_vpn&&<span title={interfaceEditReason(r)}>受保护</span>}</div></>
      ),
    },
    {
      key: "ip",
      label: "IP／前缀",
      width:"190px",
      className:"interface-ip-column",
      render: (r) =>
        list(r.addr_info).map((ip, i) => (
          <span className="interface-address" title={text(ip.local)+"/"+text(ip.prefixlen)} key={i}>
            {text(ip.local)}/{text(ip.prefixlen)}
          </span>
        )),
    },
    {
      key: "mac",
      label: "MAC",
      width:"170px",
      className:"interface-mac-column",
      render: (r) => (
        <span className="ellipsis" title={text(r.address)}>
          {text(r.address)}
        </span>
      ),
    },
    { key: "type", label: "类型", render: (r) => text(r.type) },
    { key: "mtu", label: "MTU", render: (r) => text(r.mtu) },
    {key:"state",label:"管理状态",render:r=><span className={r.admin_up?"accent":"muted"}>{r.admin_up?"已启用":"已停用"}</span>},
    {key:"link",label:"链路状态",render:r=><><span className={interfaceLinkState(r)==="ready"?"accent":"muted"} title={"内核operstate="+String(r.operstate||"未上报")+"；链路就绪不等于对端或业务连通"}>{interfaceLinkLabels[interfaceLinkState(r)]}</span>{interfaceLinkState(r)==="down"&&<small>不等于业务故障</small>}{interfaceLinkState(r)==="unknown"&&<small>需实测对端连通</small>}</>},
    {
      key: "action",
      label: "操作",
      width:"68px",
      className:"interface-actions-column",
      render: (r) => (
        <ActionMenu label={text(r.ifname)} items={[
          {label:"详情",onClick:()=>setDetails(r)},
          {label:"编辑",disabled:!!r.managed_by_vpn||r.ifname==="lo",title:interfaceEditReason(r),onClick:()=>{
            const addresses=list(r.addr_info).filter(ip=>ip.scope==="global"&&!ip.dynamic&&!ip.temporary),first=addresses[0];
            setEditing(r);setAck(false);setFailure("");
            setEditItem({name:r.ifname,original_address:first?String(first.local)+"/"+String(first.prefixlen):"",address:first?.local||"",prefix:first?.prefixlen??24,gateway_action:"keep",gateway:"",gateway_metric:600,activate:false});
          }},
          {label:r.admin_up?"停用":"启用",disabled:!!r.managed_by_vpn||r.ifname==="lo",title:interfaceEditReason(r),onClick:()=>{setTarget(r);setAck(false);setFailure("")}},
          {label:"删除",disabled:!r.can_delete,title:"仅删除FoXray创建、已停用且无地址及引用的虚拟接口",onClick:()=>{setTarget({...r,delete_request:true});setAck(false);setFailure("")}},
        ]}/>
      ),
    },
  ];
  const preview = async () => {
    try {
      const p = await api<Plan>("/api/network/plan", {
        kind: "interfaces",
        operation: target?.delete_request
          ? "delete"
          : target?.admin_up
            ? "stop"
            : "start",
        item: { name: target?.ifname },
        allow_protected: ack,
      });
      setPlan(p);
      setTarget(null);
    } catch (e) {
      setFailure(errorMessage(e));
    }
  };
  return (
    <>
      <Panel className="interface-management management-panel">
        <PageHeader
          title="接口"
          description={
            <>
              {
                "查看物理、虚拟与 VPN 接口；支持运行期静态 IP、掩码与默认网关配置"
              }
              <p>
                VPN 和代理接口由对应模块维护。虚拟接口按 UP／LOWER_UP
                标志显示就绪，对端连通性以握手与探测为准。
              </p>
            </>
          }
        />
        <div className="network-stat-grid interface-stat-grid" aria-label="接口统计与筛选">
          <button type="button" className="network-stat-card" disabled={!ready} aria-label="显示全部接口" onClick={()=>{setQuery("");setColumnFilters({});selection.setSelected(new Set())}}><Network size={19}/><strong>{ready?allRows.length:"—"}</strong><span>接口总数</span><small>全部接口 · 点击清除搜索与列筛选</small></button>
          <div className="network-stat-card managed" data-filter-active={!!columnFilters.state}><Power size={19}/><strong>{ready?searchedRows.filter(r=>r.admin_up).length:"—"} / {ready?searchedRows.filter(r=>!r.admin_up).length:"—"}</strong><span>管理状态 · 启用／停用</span><div className="interface-stat-switch">{["已启用","已停用"].map(value=><button type="button" key={value} disabled={!ready} aria-label={"筛选"+value+"接口"} aria-pressed={columnFilters.state===value} onClick={()=>chooseFilter("state",value)}>{value}</button>)}</div></div>
          <div className="network-stat-card enabled" data-filter-active={!!columnFilters.link}><LinkReadyIcon size={19}/><strong>{ready?searchedRows.filter(r=>interfaceLinkState(r)==="ready").length:"—"}</strong><span>链路就绪</span><div className="interface-stat-switch">{["ready","down","unknown"].map(key=><button type="button" key={key} disabled={!ready} aria-label={"筛选"+interfaceLinkLabels[key as keyof typeof interfaceLinkLabels]+"接口"} aria-pressed={columnFilters.link===interfaceLinkLabels[key as keyof typeof interfaceLinkLabels]} onClick={()=>chooseFilter("link",interfaceLinkLabels[key as keyof typeof interfaceLinkLabels])}>{({ready:"就绪",down:"未就绪",unknown:"未确认"} as Record<string,string>)[key]} {ready?searchedRows.filter(r=>interfaceLinkState(r)===key).length:"—"}</button>)}</div></div>
          <div className="network-stat-card interface-kind-card" data-filter-active={!!(columnFilters.type||columnFilters.owner)}><Layers size={19}/><span className="interface-kind-title">类型与来源<SettingInfo label="类型与来源" text="按内核接口类型与后端明确管理归属筛选。来源为系统接口不等于只读；模块维护接口在对应模块操作，其他修改仍需预览和确认。"/></span><Field label="类型"><select aria-label="接口类型筛选" value={columnFilters.type||""} onChange={e=>{setColumnFilters(old=>({...old,type:e.target.value}));selection.setSelected(new Set())}}><option value="">全部类型</option>{types.map(type=><option key={type} value={type}>{type}（{searchedRows.filter(r=>String(r.type||"未上报")===type).length}）</option>)}</select></Field><Field label="来源"><select aria-label="接口来源筛选" value={columnFilters.owner||""} onChange={e=>{setColumnFilters(old=>({...old,owner:e.target.value}));selection.setSelected(new Set())}}><option value="">全部来源</option>{Object.values(interfaceOwnerLabels).map(label=><option key={label} value={label}>{label}（{searchedRows.filter(r=>interfaceOwnerLabels[interfaceOwner(r)]===label).length}）</option>)}</select></Field></div>
        </div>
        <CompactFilters
          before={
            <>
              <Button
                onClick={() => {
                  setCreating(true);
                  setFailure("");
                }}
              >
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
                      !selection.chosen.some(
                        (r) =>
                          !r.admin_up && r.ifname !== "lo" && !r.managed_by_vpn,
                      ),
                    title: "合并生成预览；回环及 VPN 接口由对应模块维护",
                    onClick: () =>
                      setBulk({ operation: "start", rows: selection.chosen }),
                  },
                  {
                    label: "停用",
                    disabled:
                      !selection.chosen.length ||
                      selection.chosen.length > 50 ||
                      !selection.chosen.some(
                        (r) =>
                          r.admin_up && r.ifname !== "lo" && !r.managed_by_vpn,
                      ),
                    title: "合并生成预览；回环及 VPN 接口由对应模块维护",
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
                  Object.values(columnFilters).some((v) => v && v !== "all")
                }
                selectedCount={selection.chosen.length}
                summary={[
                  query && `搜索：${query}`,
                  ...Object.entries(columnFilters)
                    .filter(([, v]) => v && v !== "all")
                    .map(([k, v]) => `${k}：${v}`),
                  `${rows.length} 项`,
                ]
                  .filter(Boolean)
                  .join(" · ")}
                onClick={() => {
                  selection.setSelected(new Set());
                  setQuery("");
                  setColumnFilters({});
                }}
              />
              <ExportButton
                name="网络接口"
                rows={selection.chosen.length ? selection.chosen : rows}
              />
            </>
          }
          label="接口筛选"
        >
          <AdaptiveSearch>
            <Search size={18} />
            <input
              placeholder="查询接口、IP、MAC、类型或来源"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </AdaptiveSearch>
        </CompactFilters>

        <ErrorLine error={resource.error || failure} />
        {resource.data?.complete===false&&<p className="hint">接口快照未完整读取，统计暂不可用，请刷新核对。</p>}
        <Table
          keepHeaders
          rows={rows}
          total={list(resource.data?.items).length}
          selectedCount={selection.chosen.length}
          columns={columns.map((c) =>
            ["serial", "selected", "action", "actions"].includes(c.key)
              ? c
              : {
                  ...c,
                  heading: columnHeading(c.key, c.label),
                  sortDirection:
                    sort.key === c.key
                      ? sort.direction === 1
                        ? "ascending"
                        : "descending"
                      : undefined,
                },
          )}
          rowKey={(r) => String(r.ifindex)}
        />
      </Panel>
      {creating && (
        <Modal
          description={
            <>
              <p>
                未选择启用时保持
                DOWN，仅本次运行期有效。可配置静态IP和默认网关；不改动已有网桥成员。
              </p>
            </>
          }
          title="新增网络接口"
          onClose={() => {
            if (!previewing) setCreating(false);
          }}
        >
          <div className="form-grid">
            <Field
              info={
                createItem.type === "dummy" ? (
                  <>
                    <p>
                      系统原生 lo 不能重复创建，此选项提供 Dummy 虚拟回环设备。
                    </p>
                  </>
                ) : undefined
              }
              label="接口类型"
            >
              <select
                aria-label="添加类型"
                value={String(createItem.type)}
                onChange={(e) =>
                  setCreateItem({ ...createItem, type: e.target.value })
                }
              >
                <option value="veth">veth（虚拟网卡对）</option>
                <option value="dummy">虚拟回环（Dummy）</option>
                <option value="vlan">VLAN 子接口</option>
                <option value="bridge">网桥</option>
              </select>
            </Field>
            <Field
              label="接口名称"
              info="1～15位字母、数字、短横线、下划线或点，首位为字母或数字；不能与现有接口重名。"
            >
              <input
                aria-label="添加名称"
                maxLength={15}
                value={String(createItem.name)}
                onChange={(e) =>
                  setCreateItem({ ...createItem, name: e.target.value })
                }
              />
            </Field>
            {createItem.type === "veth" && (
              <Field label="对端接口名称">
                <input
                  aria-label="veth对端名称"
                  maxLength={15}
                  value={String(createItem.peer)}
                  onChange={(e) =>
                    setCreateItem({ ...createItem, peer: e.target.value })
                  }
                />
              </Field>
            )}
            {createItem.type === "vlan" && (
              <>
                <Field label="父接口">
                  <select
                    aria-label="VLAN父接口"
                    value={String(createItem.parent)}
                    onChange={(e) =>
                      setCreateItem({ ...createItem, parent: e.target.value })
                    }
                  >
                    <option value="">选择父接口</option>
                    {list(resource.data?.items)
                      .filter((r) => r.ifname !== "lo" && !r.managed_by_vpn)
                      .map((r) => (
                        <option key={String(r.ifname)} value={String(r.ifname)}>
                          {String(r.ifname)}
                        </option>
                      ))}
                  </select>
                </Field>
                <Field label="VLAN ID">
                  <NumberInput
                    aria-label="VLAN ID"
                    type="number"
                    min={1}
                    max={4094}
                    value={Number(createItem.vlan_id)}
                    onChange={(e) =>
                      setCreateItem({
                        ...createItem,
                        vlan_id: Number(e.target.value),
                      })
                    }
                  />
                </Field>
              </>
            )}
            <InterfaceAddressFields
              value={createItem}
              onChange={setCreateItem}
            />
          </div>

          <ErrorLine error={failure} />
          <div className="modal-actions">
            <Button disabled={previewing} onClick={() => setCreating(false)}>
              取消
            </Button>
            <Button
              disabled={
                previewing ||
                !String(createItem.name).trim() ||
                (createItem.type === "veth" &&
                  !String(createItem.peer).trim()) ||
                (createItem.type === "vlan" && !createItem.parent)
              }
              onClick={async () => {
                setPreviewing(true);
                setFailure("");
                try {
                  const next = await api<Plan>("/api/network/plan", {
                    kind: "interfaces",
                    operation: "add",
                    item: createItem,
                  });
                  setPlan(next);
                  setCreating(false);
                } catch (e) {
                  setFailure(errorMessage(e));
                } finally {
                  setPreviewing(false);
                }
              }}
            >
              {previewing ? "生成中…" : "生成变更预览"}
            </Button>
          </div>
        </Modal>
      )}
      {editing && (
        <Modal
          description={
            <>
              <p>
                本页编辑运行期静态配置；自动获取地址由网络管理器维护。应用后120秒内未确认会回退。
              </p>
            </>
          }
          title="编辑接口IP与网关"
          onClose={() => {
            if (!previewing) setEditing(null);
          }}
          wide
        >
          <h3>{String(editing.ifname)}</h3>
          <div className="form-grid">
            <Field label="原地址">
              <select
                aria-label="选择原地址"
                value={String(editItem.original_address || "")}
                onChange={(e) => {
                  const selected = list(editing.addr_info).find(
                    (ip) =>
                      String(ip.local) + "/" + String(ip.prefixlen) ===
                      e.target.value,
                  );
                  setEditItem({
                    ...editItem,
                    original_address: e.target.value,
                    address: selected?.local || "",
                    prefix: selected?.prefixlen ?? 24,
                  });
                }}
              >
                <option value="">新增独立静态地址</option>
                {list(editing.addr_info)
                  .filter(
                    (ip) =>
                      ip.scope === "global" && !ip.dynamic && !ip.temporary,
                  )
                  .map((ip) => (
                    <option
                      key={String(ip.local)}
                      value={String(ip.local) + "/" + String(ip.prefixlen)}
                    >
                      {String(ip.local)}/{String(ip.prefixlen)}
                    </option>
                  ))}
              </select>
            </Field>
            <InterfaceAddressFields
              value={editItem}
              onChange={setEditItem}
              existing
              gateways={list(editing.gateways)}
            />
          </div>

          <label className="check risk">
            <input
              type="checkbox"
              checked={ack}
              onChange={(e) => setAck(e.target.checked)}
            />
            我了解修改IP或网关可能中断当前连接
          </label>
          <ErrorLine error={failure} />
          <div className="modal-actions">
            <Button disabled={previewing} onClick={() => setEditing(null)}>
              取消
            </Button>
            <Button
              disabled={previewing || !ack}
              onClick={async () => {
                setPreviewing(true);
                setFailure("");
                try {
                  const next = await api<Plan>("/api/network/plan", {
                    kind: "interfaces",
                    operation: "replace",
                    item: editItem,
                    allow_protected: ack,
                  });
                  setPlan(next);
                  setEditing(null);
                } catch (e) {
                  setFailure(errorMessage(e));
                } finally {
                  setPreviewing(false);
                }
              }}
            >
              {previewing ? "生成中…" : "生成变更预览"}
            </Button>
          </div>
        </Modal>
      )}
      {bulk && (
        <NetworkBatchDialog
          kind={"interfaces"}
          operation={bulk.operation}
          rows={bulk.rows}
          act={act}
          onClose={() => setBulk(null)}
        />
      )}
      {details != null && (
        <Modal
          title="接口详细信息"
          copyText={JSON.stringify(details, null, 2)}
          onClose={() => setDetails(null)}
          wide
        >
          <JsonDetails data={details} />
        </Modal>
      )}
      {target && (
        <Modal title="确认接口状态变更" onClose={() => setTarget(null)}>
          <p>
            将
            {target.delete_request ? "删除" : target.admin_up ? "停用" : "启用"}
            接口 <strong>{text(target.ifname)}</strong>。
          </p>
          <p className="hint">
            <AlertTriangle size={16} />
            该接口承载的连接可能中断。未在 120 秒内确认，自动恢复原状态。
          </p>
          <label className="check risk">
            <input
              type="checkbox"
              checked={ack}
              onChange={(e) => setAck(e.target.checked)}
            />
            我了解本次接口状态变更的影响
          </label>
          <ErrorLine error={failure} />
          <div className="modal-actions">
            <Button onClick={() => setTarget(null)}>取消</Button>
            <Button disabled={!ack} onClick={() => void preview()}>
              生成变更预览
            </Button>
          </div>
        </Modal>
      )}
      {plan && (
        <Modal title="接口变更预览" onClose={() => setPlan(null)}>
          <h3>{plan.summary}</h3>
          <pre className="command-preview">{plan.preview.join("\n")}</pre>
          {plan.warnings.map((warning, i) => (
            <p className="small muted" key={i}>
              {warning}
            </p>
          ))}
          {plan.preview_only && (
            <p className="hint">
              独立预览只展示变更命令，正式确认升级后才能应用。
            </p>
          )}
          <div className="modal-actions">
            <Button onClick={() => setPlan(null)}>取消</Button>
            <Button
              variant="primary"
              disabled={plan.preview_only}
              onClick={() => {
                const id = plan.id;
                setPlan(null);
                void act("/api/network/apply", { id }).then(() =>
                  resource.refresh(),
                );
              }}
            >
              应用变更
            </Button>
          </div>
        </Modal>
      )}
    </>
  );
}
