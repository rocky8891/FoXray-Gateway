import { MultiSelect } from "./multi_select";
import { remainingPlanSeconds } from "./network_display";
import { AdaptiveSearch } from "./adaptive_search";
import { ResetFilters } from "./list_controls";
import { ExportButton, useRowSelection } from "./resource_controls";
import { ListFilter, SortHeading } from "./list_controls";
import { SelectionActions } from "./selection_actions";
import { NetworkBatchDialog } from "./network_batch_dialog";
import { SettingInfo } from "./ui";
import { NumberInput } from "./ui";
import { ActionMenu } from "./action_menu";
import { formatCapacity } from "./host";
import { useEffect, useState, useRef } from "react";
import {
  AlertTriangle,
  ArrowRight,
  Eye,
  EyeOff,
  ShieldCheck,
  Plus,
  RefreshCw,
  Search,
} from "lucide-react";
import {
  api,
  errorMessage,
  list,
  record,
  text,
  timestamp,
  type Dict,
  type NetSnapshot,
  type Plan,
} from "./api";
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
export type NetworkKind = "wireguard" | "routes" | "rules" | "firewall";
const names: Record<NetworkKind, string> = {
  wireguard: "接口管理",
  routes: "路由",
  rules: "策略路由",
  firewall: "防火墙管理",
};
const descriptions: Record<NetworkKind, string> = {
  wireguard: "独立管理多个 VPN 接口",
  routes: "查看 IPv4 / IPv6 路由 · 管理 FoXray 创建的持久路由",
  rules: "按优先级查看策略路由 · 保护代理和 VPN 专用规则",
  firewall: "识别 firewalld 与 nftables 规则来源 · 变更采用预览与自动回退",
};
export function NetworkPage({
  kind,
  act,
}: {
  kind: NetworkKind;
  act: Act;
}) {
  const { data, error, refresh } = useResource<NetSnapshot>(
    "/api/network/" + kind,
    10000,
  );
  const [form, setForm] = useState<Dict | null>(null),
    [operation, setOperation] = useState("add"),
    [allowProtected, setAllowProtected] = useState(false),
    [plan, setPlan] = useState<Plan | null>(null),
    [failure, setFailure] = useState(""),
    [details, setDetails] = useState<unknown>(null),
    [peerDetails, setPeerDetails] = useState<{ name: string; key: string; label: string } | null>(null),
    [query, setQuery] = useState(""),
    [family, setFamily] = useState("4"),
    [section, setSection] = useState("managed");
  const originalWireGuardForm = useRef<Dict | null>(null);
  const peerEditDraft = useRef<{ form: Dict; original: Dict; allowProtected: boolean } | null>(null);
  const resumePeerEditor = () => {
    const draft = peerEditDraft.current;
    peerEditDraft.current = null;
    setPlan(null);
    setFailure("");
    setForm(draft?.form ?? null);
    if (draft) {
      originalWireGuardForm.current = draft.original;
      setOperation("replace");
      setAllowProtected(draft.allowProtected);
    }
  };
  const onlyWireGuardRemarkChanged = (() => {
    const original = originalWireGuardForm.current;
    if (kind !== "wireguard" || operation !== "replace" || !form || !original) return false;
    const { remark, ...config } = form;
    const { remark: originalRemark, ...originalConfig } = original;
    return String(remark ?? "").trim() !== String(originalRemark ?? "").trim()
      && JSON.stringify(config) === JSON.stringify(originalConfig);
  })();
  useEffect(() => {
    peerEditDraft.current = null;
    setForm(null);
    setPlan(null);
    setDetails(null);
    setFailure("");
    setQuery("");
  }, [kind]);
  const begin = (op: string, item: Dict) => {
    originalWireGuardForm.current = kind === "wireguard" && op === "replace" ? item : null;
    setOperation(op);
    setForm(item);
    setAllowProtected(false);
    setFailure("");
  };
  const preview = async (op: string, item: Dict, allow = false) => {
    try {
      const result = await api<Plan>("/api/network/plan", {
        kind,
        operation: op,
        item,
        allow_protected: allow,
      });
      setPlan(result);
      setForm(null);
      setFailure("");
    } catch (e) {
      setFailure(errorMessage(e));
    }
  };
  const [filterState, setFilterState] = useState("all"),
    [sort, setSort] = useState({ key: "name", direction: 1 }),
    [bulk, setBulk] = useState<{ operation: string; rows: Dict[] } | null>(
      null,
    );
  const sourceRows =
    kind === "wireguard"
      ? data?.items || []
      : section === "managed"
        ? data?.managed || []
        : family === "4"
          ? data?.ipv4 || []
          : data?.ipv6 || [];
  const rows = sourceRows
    .filter(
      (row) =>
        JSON.stringify(row).toLowerCase().includes(query.toLowerCase()) &&
        (filterState === "all" || !!row.active === (filterState === "active")),
    )
    .sort(
      (a, b) =>
        String(
          (sort.key === "state"
            ? a.active
            : sort.key === "autostart"
              ? record(a.service).autostart
              : sort.key === "port"
                ? a.listen_port
                : sort.key === "peers"
                  ? list(a.peers).length
                  : a[sort.key]) ?? "",
        ).localeCompare(
          String(
            (sort.key === "state"
              ? b.active
              : sort.key === "autostart"
                ? record(b.service).autostart
                : sort.key === "port"
                  ? b.listen_port
                  : sort.key === "peers"
                    ? list(b.peers).length
                    : b[sort.key]) ?? "",
          ),
          undefined,
          { numeric: true },
        ) * sort.direction,
    );
  const selection = useRowSelection(rows, (r) =>
    String(r.id || r.name || JSON.stringify(r)),
  );
  const editInterface = async (row: Dict) => {
    try {
      const profile = await api<Dict>(
        "/api/network/wireguard/profile?name=" +
          encodeURIComponent(String(row.name)),
      );
      begin("replace", {
        ...profile,
        addresses: Array.isArray(profile.addresses)
          ? profile.addresses.join(", ")
          : "",
        dns: Array.isArray(profile.dns) ? profile.dns.join(", ") : "",
      });
    } catch (e) {
      setFailure(errorMessage(e));
    }
  };

  const add = () =>
    begin(
      "add",
      kind === "wireguard"
        ? {
            name: "",
            remark: "",
            addresses: "",
            dns: "",
            listen_port: 0,
            mtu: 1420,
            table: "auto",
            network: {
              role: "client",
              route_mode: "auto",
              snat: false,
              lan_interface: "",
              lan_sources: [],
              routes: [],
              advertised_routes: [],
            },
            peers: [],
          }
        : kind === "routes"
          ? {
              family: Number(family),
              dst: "",
              gateway: "",
              dev: "",
              table: 254,
              metric: 0,
            }
          : kind === "rules"
            ? {
                family: Number(family),
                priority: 2000,
                from: "",
                to: "",
                fwmark: "",
                table: 2000,
              }
            : { zone: "public", protocol: "tcp", port: 8080 },
    );
  const interfaceStateControl = (
    <Field label="状态">
      <select
        value={filterState}
        onChange={(e) => setFilterState(e.target.value)}
      >
        <option value="all">全部状态</option>
        <option value="active">已启用</option>
        <option value="inactive">已停用</option>
      </select>
    </Field>
  );
  const baseColumns: Column<Dict>[] = [
    { key: "serial", label: "序号", render: (_, i) => i + 1 },
  ];
  let columns: Column<Dict>[] = baseColumns;
  if (kind === "wireguard")
    columns = [
      ...baseColumns,
      {
        key: "name",
        label: "接口",
        render: (r) => (
          <>
            <button
              type="button"
              className="resource-name"
              onClick={() => void editInterface(r)}
            >
              {text(r.name)}
            </button>
            {!!r.remark && <small className="wg-interface-remark" title={String(r.remark)}>{String(r.remark)}</small>}
            {r.protected && <small className="accent wg-protection-label">受保护<SettingInfo label="接口保护" text={<ul><li><strong>保护状态。</strong>系统保护 wg-home 接口。修改或启停需要额外确认。</li><li><strong>用途备注。</strong>在「操作 → 编辑」修改。</li><li><strong>接口角色。</strong>在「操作 → 编辑 → 角色、路由与 LAN 转发」设置。修改角色不会取消接口保护。</li></ul>} /></small>}
          </>
        ),
      },
      {
        key: "state",
        fitContent: true,
        label: "状态",
        render: (r) => (
          <>
            <strong>{text(record(r.service).name || `wg-quick@${r.name}`)}</strong>
            <small>{r.configured ? "wg-quick 配置已就绪" : "未配置 wg-quick"}</small>
            <small className={r.active ? "accent" : "muted"}>
              {r.active ? "接口已启动" : "接口已停止"}
            </small>
          </>
        ),
      },
      {
        key: "autostart",
        fitContent: true,
        label: "开机自启",
        render: (r) => (
          <span
            className={record(r.service).autostart ? "accent" : "metric-orange"}
          >
            {record(r.service).autostart ? "已开启" : "已关闭"}
          </span>
        ),
      },
      { key: "addresses", label: "接口 IP", fitContent: true, render: (r) => {
        const current = Array.isArray(r.addresses) ? r.addresses.flatMap(a => Array.isArray(record(a).addr_info) ? (record(a).addr_info as Dict[]).map(ip => String(ip.local || "") + "/" + String(ip.prefixlen ?? "")) : []) : [];
        const addresses = current.length ? current : Array.isArray(r.configured_addresses) ? r.configured_addresses.map(String) : [];
        return addresses.length ? addresses.map(ip=><div key={ip}>{ip}</div>) : "—";
      } },
      { key: "port", label: "监听端口", fitContent: true, render: (r) => text(r.listen_port) },
      { key: "endpoint", label: "Endpoint", render: r => list(r.peers).map((p,i)=><div key={i}><small>Peer {i+1}</small>{p.endpoint && p.endpoint!=="(none)" ? String(p.endpoint) : "未配置／等待对端"}</div>) },
      { key: "peer_addresses", label: "AllowedIPs", render: r => list(r.peers).map((p,i)=><div key={i}><small>Peer {i+1}{p.disabled ? " · 已停用" : ""}</small>{(Array.isArray(p.allowed_ips) ? p.allowed_ips.map(String) : String(p.allowed_ips||"").split(/[\s,]+/)).filter(Boolean).map(ip=><div key={ip}>{ip}</div>)}</div>) },
      {
        key: "peers",
        label: "Peer / 隧道状态",
        render: (r) => {
          const peers = list(r.peers);
          const healthy = peers.filter(peer => ["隧道正常", "目标可达"].includes(String(record(peer.health).status))).length;
          const disconnected = peers.filter(peer => ["隧道不可达", "目标不可达"].includes(String(record(peer.health).status))).length;
          return <>
            <small>{peers.length} 个 Peer{healthy > 0 && ` · ${healthy} 正常`}{disconnected > 0 && ` · ${disconnected} 不可达`}</small>
            {peers.map((peer, index) => {
              const health = record(peer.health);
              const status = String(health.status || (peer.disabled ? "已停用" : Number(peer.handshake) ? "握手成功" : "未握手"));
              const label = [`Peer ${index + 1}`, health.tunnel, status].filter(Boolean).join(" · ");
              return <div className="wg-peer-status-entry" key={String(peer.public_key || index)}>
                <button type="button" className="wg-peer-status-line"
                  onClick={() => setPeerDetails({ name: String(r.name), key: String(peer.public_key), label })}>
                  {label}
                </button>
                <div className="wg-peer-status-times">
                  <span>最近握手 {timestamp(peer.handshake)}</span>
                  {record(peer.probe).enabled === true && <span>最近探测 {timestamp(health.last_probe)}</span>}
                </div>
              </div>;
            })}
          </>;
        },
      },
      {
        key: "transfer",
        label: "累计接收 / 发送",
        render: (r) =>
          list(r.peers).map((p, i) => {
            const values = Array.isArray(p.transfer)
              ? p.transfer
              : String(p.transfer ?? "")
                  .trim()
                  .split(/\s+/);
            const receive =
              values[0] !== "" &&
              values[0] != null &&
              Number.isFinite(Number(values[0]))
                ? Number(values[0])
                : null;
            const send =
              values[1] !== "" &&
              values[1] != null &&
              Number.isFinite(Number(values[1]))
                ? Number(values[1])
                : null;
            return (
              <div key={i} className="wireguard-transfer">
                <span>
                  接收 {receive == null ? "—" : formatCapacity(receive)}
                </span>
                <span>发送 {send == null ? "—" : formatCapacity(send)}</span>
              </div>
            );
          }),
      },
      {
        key: "actions",
        label: "操作",
        render: (r) => (
          <ActionMenu
            label={String(r.name)}
            items={[
              { label: "详情", onClick: () => setDetails(r) },
              { label: "编辑", disabled: !r.configured, onClick: () => void editInterface(r) },
              {
                label: r.active ? "停止" : "启动",
                title: "只操作此接口",
                onClick: () =>
                  begin(r.active ? "stop" : "start", { name: r.name }),
              },
              {
                label: record(r.service).autostart ? "关闭自启" : "开启自启",
                onClick: () =>
                  begin(record(r.service).autostart ? "disable" : "enable", {
                    name: r.name,
                  }),
              },
              { label: "管理 Peer", disabled: !r.configured, onClick: () => begin("peers", r) },
              {
                label: "删除",
                disabled: !!r.active,
                onClick: () => begin("delete", { name: r.name }),
              },
            ]}
          />
        ),
      },
    ];
  if (kind === "routes")
    columns = [
      ...baseColumns,
      {
        key: "family",
        label: "地址族",
        render: (r) => "IPv" + text(r.family || family),
      },
      {
        key: "dst",
        label: "目标网段",
        render: (r) => <strong>{text(r.dst || "default")}</strong>,
      },
      { key: "via", label: "下一跳", render: (r) => text(r.gateway) },
      { key: "dev", label: "出口接口", render: (r) => text(r.dev) },
      { key: "table", label: "路由表", render: (r) => text(r.table || "main") },
      { key: "metric", label: "Metric", render: (r) => text(r.metric || 0) },
      {
        key: "owner",
        label: "来源",
        render: (r) =>
          section === "managed"
            ? "FoXray"
            : text(r.protocol || "内核 / 外部服务"),
      },
      {
        key: "actions",
        label: "操作",
        render: (r) =>
          section === "managed" ? (
            <div className="actions">
              <Button onClick={() => begin("replace", r)}>编辑</Button>
              <Button variant="danger" onClick={() => preview("delete", r)}>
                删除
              </Button>
            </div>
          ) : (
            <Button onClick={() => setDetails(r)}>查看详情</Button>
          ),
      },
    ];
  if (kind === "rules")
    columns = [
      ...baseColumns,
      {
        key: "priority",
        label: "优先级",
        render: (r) => <strong>{text(r.priority)}</strong>,
      },
      {
        key: "from",
        label: "来源",
        render: (r) => text(r.from || r.src || "all"),
      },
      { key: "to", label: "目标", render: (r) => text(r.to || r.dst || "all") },
      { key: "mark", label: "fwmark", render: (r) => text(r.fwmark) },
      { key: "table", label: "路由表", render: (r) => text(r.table) },
      {
        key: "owner",
        label: "来源",
        render: (r) =>
          section === "managed"
            ? "FoXray"
            : Number(r.priority) >= 16060 && Number(r.priority) <= 16100
              ? "代理 / VPN 保留"
              : "内核 / 外部服务",
      },
      {
        key: "actions",
        label: "操作",
        render: (r) =>
          section === "managed" ? (
            <div className="actions">
              <Button onClick={() => begin("replace", r)}>编辑</Button>
              <Button variant="danger" onClick={() => preview("delete", r)}>
                删除
              </Button>
            </div>
          ) : (
            <Button onClick={() => setDetails(r)}>查看详情</Button>
          ),
      },
    ];
  if (kind === "firewall")
    columns = [
      ...baseColumns,
      { key: "zone", label: "区域", render: (r) => text(r.zone) },
      { key: "protocol", label: "协议", render: (r) => text(r.protocol) },
      { key: "port", label: "放行端口", render: (r) => text(r.port) },
      { key: "owner", label: "归属", render: () => "FoXray · firewalld" },
      {
        key: "actions",
        label: "操作",
        render: (r) => (
          <Button variant="danger" onClick={() => preview("delete", r)}>
            删除
          </Button>
        ),
      },
    ];
  columns = [
    selection.column,
    ...columns.map((c) =>
      c.label === "操作" || c.label === "序号"
        ? c
        : {
            ...c,
            heading: (
              <span className="table-heading-tools">
                <SortHeading
                  label={c.label}
                  direction={sort.key === c.key ? sort.direction : undefined}
                  onClick={() =>
                    setSort({
                      key: c.key,
                      direction: sort.key === c.key ? -sort.direction : 1,
                    })
                  }
                />

                {c.key === "state" && (
                  <ListFilter
                    label="接口状态筛选"
                    column
                    active={filterState !== "all"}
                  >
                    {interfaceStateControl}
                    <Button onClick={() => setFilterState("all")}>重置</Button>
                  </ListFilter>
                )}
              </span>
            ),
          },
    ),
  ];
  return (
    <>
      <Panel className="network-management management-panel">
        <PageHeader
          title={names[kind]}
          description={
            <>
              {descriptions[kind]}
              {kind === "wireguard" && (
                <p>
                  启动／停止对应 systemctl start／stop
                  wg-quick@接口名；开启／关闭自启对应 systemctl enable／disable
                  wg-quick@接口名。批量操作逐项生成预览；不改变其他接口。编辑与删除在行菜单中，运行中的接口需先停止。
                </p>
              )}
              {kind !== "wireguard" && (
                <p>
                  系统和其他组件的现有规则保留原归属；新增规则只由 FoXray
                  管理，不清空其他表或全局 reload。
                </p>
              )}
            </>
          }
        />
        <ErrorLine error={error || failure} />
        {kind !== "wireguard" && kind !== "firewall" && (
          <div className="toolbar">
            <div className="subtabs">
              {[
                ["managed", "FoXray 管理"],
                ["all", "全部系统规则（只读）"],
              ].map(([id, label]) => (
                <button
                  key={id}
                  aria-selected={section === id}
                  onClick={() => setSection(id)}
                >
                  {label}
                </button>
              ))}
            </div>
            <Field label="地址族">
              <select
                value={family}
                onChange={(e) => setFamily(e.target.value)}
              >
                <option value="4">IPv4</option>
                <option value="6">IPv6</option>
              </select>
            </Field>
          </div>
        )}
        <div className="management-toolbar">
          <Button onClick={() => void refresh()}>
            <RefreshCw size={15} /> 刷新
          </Button>
          <Button onClick={add}><Plus size={15} /> 添加</Button>

          {kind === "wireguard" ? (
            <>
              <Button
                disabled={
                  !selection.chosen.length || selection.chosen.length > 50
                }
                onClick={() =>
                  setBulk({ operation: "start", rows: selection.chosen })
                }
              >
                启动
              </Button>
              <Button
                disabled={
                  !selection.chosen.length || selection.chosen.length > 50
                }
                onClick={() =>
                  setBulk({ operation: "stop", rows: selection.chosen })
                }
              >
                停止
              </Button>
              <ActionMenu
                label="所选接口"
                triggerLabel="批量操作"
                items={[
                  ["enable", "开启自启"],
                  ["disable", "关闭自启"],
                  ["delete", "删除"],
                ].map(([operation, label]) => ({
                  label,
                  disabled:
                    !selection.chosen.length || selection.chosen.length > 50,
                  onClick: () => setBulk({ operation, rows: selection.chosen }),
                }))}
              />
            </>
          ) : (
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
          )}

          <div className="management-query-tools">
            <AdaptiveSearch tableWidthKey={"network-" + kind}>
              <Search size={16} />
              <input
                placeholder={kind === "wireguard" ? "查询接口或服务单元" : "查询接口、网段、路由表或规则"}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />{" "}
            </AdaptiveSearch>
          </div>
          <ResetFilters
            active={query !== "" || filterState !== "all"}
            selectedCount={selection.chosen.length}
            summary={[
              query && `搜索：${query}`,
              filterState !== "all" && `状态：${filterState}`,
              `${rows.length} 项`,
            ]
              .filter(Boolean)
              .join(" · ")}
            onClick={() => {
              selection.setSelected(new Set());
              setQuery("");
              setFilterState("all");
            }}
          />
          <ExportButton
            name={names[kind]}
            rows={selection.chosen.length ? selection.chosen : rows}
          />
        </div>

        {bulk && (
          <NetworkBatchDialog
            kind={kind}
            operation={bulk.operation}
            rows={bulk.rows}
            act={act}
            onClose={() => setBulk(null)}
          />
        )}

        {kind === "firewall" && (
          <div className="firewall-summary">
            <p>
              <ShieldCheck size={16} /> firewalld：
              {text(record(data?.firewalld).state)}
            </p>
            <pre>{text(data?.zones)}</pre>
            <Button onClick={() => setDetails(data?.ruleset)}>
              查看全部 nftables 表与规则（只读）
            </Button>
          </div>
        )}
        <Table
          keepHeaders
          columnSizingKey={"network-" + kind}
          rows={rows}
          total={sourceRows.length}
          selectedCount={selection.chosen.length}
          columns={columns}
          rowKey={(r) => String(r.id || r.name || JSON.stringify(r))}
        />
      </Panel>
      {peerDetails && <WireGuardPeerStatusDetail
        name={peerDetails.name}
        label={peerDetails.label}
        peer={list(list(data?.items).find(row => row.name === peerDetails.name)?.peers).find(candidate => candidate.public_key === peerDetails.key)}
        onClose={() => setPeerDetails(null)}
      />}
      {form && operation === "peers" && (
        <WireGuardPeerManager
          name={String(form.name)}
          peers={list(form.peers)}
          onClose={() => setForm(null)}
          onToggle={peer => begin(peer.disabled ? "peer-enable" : "peer-disable", { name: form.name, public_key: peer.public_key })}
        />
      )}
      {form && operation !== "peers" && (
        <Modal
          description={
            kind === "wireguard" ? (
              <>
                私钥不通过查询接口返回，不进入操作日志或
                GitHub。运行中编辑地址或路由会重启此接口；单独启停 Peer 不重启接口。
              </>
            ) : undefined
          }
          title={
            names[kind] +
            " · " +
            ({
              add: "新增",
              remark: "编辑备注",
              replace: "编辑",
              delete: "删除",
              start: "启动",
              stop: "停止",
              restart: "重启",
              enable: "开启自启",
              disable: "关闭自启",
              "peer-enable": "启用 Peer",
              "peer-disable": "停用 Peer",
            }[operation] || operation)
          }
          onClose={resumePeerEditor}
          wide
        >
          <NetworkForm
            kind={kind}
            operation={operation}
            value={form}
            onChange={setForm}
            onTogglePeer={kind === "wireguard" && operation === "replace" ? peer => {
              const original = originalWireGuardForm.current;
              const saved = list(original?.peers).find(item => item.public_key === peer.public_key);
              if (!original || !saved) return;
              peerEditDraft.current = { form, original, allowProtected };
              begin(saved.disabled ? "peer-enable" : "peer-disable", { name: original.name, public_key: saved.public_key });
            } : undefined}
            savedPeers={list(originalWireGuardForm.current?.peers)}
          />
          {operation !== "remark" && !onlyWireGuardRemarkChanged && (form.name === "wg-home" || form.new_name === "wg-home" || (kind === "wireguard" && ["add","replace"].includes(operation)) ||
            operation === "delete" ||
            (kind === "routes" && form.dst === "default") ||
            kind === "wireguard" ||
            (kind === "rules" && Number(form.priority) < 16070)) && (
            <label className="check risk">
              <input
                type="checkbox"
                checked={allowProtected}
                onChange={(e) => setAllowProtected(e.target.checked)}
              />
              我了解本次操作可能影响该接口或路由承载的连接；允许本次修改受保护对象
            </label>
          )}
          <ErrorLine error={failure} />
          <div className="modal-actions">
            <Button onClick={resumePeerEditor}>取消</Button>
            <Button
              variant="primary"
              disabled={
                kind === "wireguard" && operation === "add" && !form.key_id
              }
              onClick={() => {
                if (onlyWireGuardRemarkChanged) {
                  void preview("remark", { name: form.name, remark: form.remark });
                  return;
                }
                let item = { ...form };
                if (
                  kind === "wireguard" &&
                  (operation === "add" || operation === "replace")
                )
                  item = {
                    ...item,
                    addresses: String(item.addresses || "")
                      .split(/[\n,]+/)
                      .map((s) => s.trim())
                      .filter(Boolean),
                    dns: String(item.dns || "")
                      .split(/[\n,]+/)
                      .map((s) => s.trim())
                      .filter(Boolean),
                  };
                void preview(operation, item, allowProtected);
              }}
            >
              生成变更预览 <ArrowRight size={15} />
            </Button>
          </div>
        </Modal>
      )}
      {plan && (
        <Modal title="网络变更预览" onClose={resumePeerEditor} wide>
          <h3>{plan.summary}</h3>
          {plan.warnings.map((v, i) => (
            <p className="hint" key={i}>
              <AlertTriangle size={15} />
              {v}
            </p>
          ))}
          <pre className="command-preview">{plan.preview.join("\n")}</pre>
          <p>应用后需要在 120 秒内确认网络正常，否则自动恢复本次修改。</p>
          {peerEditDraft.current && <p className="hint">应用只处理此 Peer 并关闭编辑器，其他未保存修改会丢弃。取消可返回原草稿。</p>}
          <div className="modal-actions">
            <Button onClick={resumePeerEditor}>取消</Button>
            <Button
              variant="primary"
              disabled={plan.preview_only === true}
              onClick={() => {
                const id = plan.id;
                setPlan(null);
                void act("/api/network/apply", { id }).then(ok => {
                  if (!ok && peerEditDraft.current) resumePeerEditor();
                  else peerEditDraft.current = null;
                  return refresh();
                });
              }}
            >
              应用本次变更
            </Button>
          </div>
        </Modal>
      )}
      {details != null && (
        <Modal
          title="规则与状态详情"
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
function WireGuardPeerManager({ name, peers, onClose, onToggle }: {
  name: string;
  peers: Dict[];
  onClose: () => void;
  onToggle: (peer: Dict) => void;
}) {
  return (
    <Modal title={name + " · Peer 管理"} onClose={onClose} wide>
      <p className="hint">单独启停 Peer，保留配置；不停止接口或其他 Peer。已停止的接口仅保存状态，下次启动时生效。</p>
      {!peers.length && <p className="muted">此接口尚未配置 Peer。</p>}
      {peers.map((peer, i) => (
        <section className="peer-card" key={String(peer.public_key)}>
          <strong>Peer {i + 1} · {peer.disabled ? "已停用" : "已启用"}</strong>
          <p>AllowedIPs：{Array.isArray(peer.allowed_ips) ? peer.allowed_ips.join(", ") : String(peer.allowed_ips || "—")}</p>
          <p>Endpoint：{String(peer.endpoint || "未配置／等待对端")}</p>
          {peer.configured === false && <p className="hint">仅运行时存在，尚未写入此接口配置，暂不支持保存启停状态。</p>}
          <Button size="regular" variant={peer.disabled ? "primary" : "danger"} disabled={peer.configured === false} onClick={() => onToggle(peer)}>
            {peer.disabled ? "启用此 Peer" : "停用此 Peer"}
          </Button>
        </section>
      ))}
    </Modal>
  );
}

function WireGuardPublicKey({ value, onChange }: { value: string; onChange?: (value: string) => void }) {
  const [visible, setVisible] = useState(false);
  return <div className="wg-public-key-field">
    <input type={visible ? "text" : "password"} autoComplete="off" value={value}
      readOnly={!onChange} onChange={event => onChange?.(event.target.value)} />
    <Button type="button" variant="ghost" iconOnly aria-pressed={visible}
      ariaLabel={visible ? "隐藏公钥" : "显示公钥"} title={visible ? "隐藏公钥" : "显示公钥"}
      onClick={() => setVisible(!visible)}>
      {visible ? <EyeOff size={16} /> : <Eye size={16} />}
    </Button>
  </div>;
}

function WireGuardRemarkField({value,onChange}:{value:string;onChange:(value:string)=>void}) {
  return <Field label="备注" hint="可选，最多200字；用于说明接口用途，不影响接口名称、角色或路由。"><input aria-label="WireGuard接口备注" value={value} maxLength={200} placeholder="例如：回家 VPN、办公网络" onChange={e=>onChange(e.target.value)}/></Field>;
}
function NetworkForm({
  kind,
  operation,
  value,
  onChange,
  onTogglePeer,
  savedPeers,
}: {
  kind: NetworkKind;
  operation: string;
  value: Dict;
  onChange: (v: Dict) => void;
  onTogglePeer?: (peer: Dict) => void;
  savedPeers: Dict[];
}) {
  const latest = useRef(value);
  latest.current = value;
  const [keyError, setKeyError] = useState(""),
    [keyBusy, setKeyBusy] = useState(false);
  const set = (k: string, v: unknown) =>
    onChange(
      k === "name"
        ? { ...value, name: v, key_id: undefined, public_key: undefined }
        : { ...value, [k]: v },
    );
  if (kind === "wireguard" && ["peer-enable","peer-disable"].includes(operation)) return <p>{operation === "peer-disable" ? "停用" : "启用"}接口 <strong>{String(value.name)}</strong> 中选定的 Peer；保留配置，接口和其他 Peer 保持运行。</p>;
  if (kind === "wireguard" && operation === "peers") return <p>请在接口表格的 Peer 管理中操作。</p>;
  if (kind === "wireguard" && operation === "remark") return <><p>接口：<strong>{String(value.name)}</strong></p><WireGuardRemarkField value={String(value.remark||"")} onChange={v=>set("remark",v)}/><p className="hint">仅修改用途备注，无需停止或重启接口，不改变受保护状态。</p></>;
  if (!["add", "replace"].includes(operation))
    return (
      <p>
        仅对{" "}
        <strong>
          {text(value.name || value.dst || value.priority || value.port)}
        </strong>{" "}
        执行本次操作。
      </p>
    );
  if (kind === "wireguard")
    return (
      <>
        <div className="form-grid">
          <Field label="接口名称" hint="例如 wg-home、wg-work、wg-work2">
            <input
              value={String(value.new_name ?? value.name ?? "")}
              onChange={(e) => set(operation === "replace" ? "new_name" : "name", e.target.value)}
              maxLength={15}
            />
          </Field>
          <WireGuardRemarkField value={String(value.remark||"")} onChange={v=>set("remark",v)}/>
          <Field label="监听端口" hint="0 为自动">
            <NumberInput
              type="number"
              min={0}
              max={65535}
              value={Number(value.listen_port || 0)}
              onChange={(e) => set("listen_port", Number(e.target.value))}
            />
          </Field>
          <Field label="接口地址" hint="多个 CIDR 用逗号分隔">
            <input
              value={String(value.addresses || "")}
              placeholder="10.24.255.1/24"
              onChange={(e) => set("addresses", e.target.value)}
            />
          </Field>
          <Field label="DNS" hint="多个地址用逗号分隔。留空保持原解析">
            <input
              value={String(value.dns || "")}
              onChange={(e) => set("dns", e.target.value)}
            />
          </Field>
          <Field label="MTU" hint="0 为自动">
            <NumberInput
              type="number"
              value={Number(value.mtu ?? 1420)}
              onChange={(e) => set("mtu", Number(e.target.value))}
            />
          </Field>
          {operation === "add" ? (
            <div className="key-generation">
              <Field
                label="独立密钥对"
                hint="每个接口单独生成；私钥不会返回浏览器"
              >
                <Button
                  disabled={!value.name || keyBusy}
                  onClick={() => {
                    const name = String(value.name);
                    setKeyBusy(true);
                    setKeyError("");
                    void api<Dict>("/api/network/wireguard/keys", { name })
                      .then((keys) => {
                        if (latest.current.name !== name)
                          throw Error("接口名已改变，请重新生成");
                        onChange({
                          ...latest.current,
                          key_id: keys.key_id,
                          public_key: keys.public_key,
                          key_expires: keys.expires,
                        });
                      })
                      .catch((e) => setKeyError(errorMessage(e)))
                      .finally(() => setKeyBusy(false));
                  }}
                >
                  {keyBusy
                    ? "正在生成…"
                    : value.key_id
                      ? "重新生成密钥对"
                      : "生成独立公私钥"}
                </Button>
              </Field>
              {!!value.public_key && (
                <Field label="本接口公钥" hint="可复制给对端">
                  <WireGuardPublicKey value={String(value.public_key)} />
                </Field>
              )}
              <ErrorLine error={keyError} />
            </div>
          ) : (
            <Field label="本接口公钥" hint="私钥保留在服务器，不通过接口回显">
              <WireGuardPublicKey value={String(value.public_key || "")} />
            </Field>
          )}
        </div>
        <WGNetworkEditor
          value={record(value.network)}
          routeTable={<Field label="路由表">
                      <input
                        value={String(value.table || "auto")}
                        onChange={(e) => set("table", e.target.value)}
                      />
                    </Field>}
          onChange={(network) => set("network", network)}
        />
        <PeerEditor
          name={String(value.name || "")}
          routing={record(value.network)}
          table={String(value.table || "auto")}
          peers={list(value.peers)}
          onTogglePeer={onTogglePeer}
          savedPeers={savedPeers}
          onChange={(peers) => set("peers", peers)}
        />
        {operation === "replace" && record(value.network).role === "server" && (
          <WGClientTemplates
            name={String(value.name)}
            peers={list(value.peers)}
          />
        )}
      </>
    );
  return (
    <div className="form-grid">
      {kind !== "firewall" && (
        <Field label="地址族">
          <select
            value={Number(value.family || 4)}
            onChange={(e) => set("family", Number(e.target.value))}
          >
            <option value={4}>IPv4</option>
            <option value={6}>IPv6</option>
          </select>
        </Field>
      )}
      {kind === "routes" ? (
        <>
          <Field label="目标网段">
            <input
              value={String(value.dst || "")}
              onChange={(e) => set("dst", e.target.value)}
              placeholder="10.20.0.0/16 或 default"
            />
          </Field>
          <Field label="下一跳" hint="可留空">
            <input
              value={String(value.gateway || "")}
              onChange={(e) => set("gateway", e.target.value)}
            />
          </Field>
          <Field label="出口接口">
            <input
              value={String(value.dev || "")}
              onChange={(e) => set("dev", e.target.value)}
              placeholder="wg-work"
            />
          </Field>
          <Field label="路由表">
            <NumberInput
              type="number"
              value={Number(value.table || 254)}
              onChange={(e) => set("table", Number(e.target.value))}
            />
          </Field>
          <Field label="Metric">
            <NumberInput
              type="number"
              value={Number(value.metric || 0)}
              onChange={(e) => set("metric", Number(e.target.value))}
            />
          </Field>
        </>
      ) : kind === "rules" ? (
        <>
          <Field label="优先级">
            <NumberInput
              type="number"
              value={Number(value.priority || 2000)}
              onChange={(e) => set("priority", Number(e.target.value))}
            />
          </Field>
          <Field label="来源 IP / CIDR">
            <input
              value={String(value.from || "")}
              onChange={(e) => set("from", e.target.value)}
              placeholder="all"
            />
          </Field>
          <Field label="目标 IP / CIDR">
            <input
              value={String(value.to || "")}
              onChange={(e) => set("to", e.target.value)}
              placeholder="all"
            />
          </Field>
          <Field label="fwmark" hint="可留空">
            <input
              value={String(value.fwmark || "")}
              onChange={(e) => set("fwmark", e.target.value)}
              placeholder="0x200"
            />
          </Field>
          <Field label="查询路由表">
            <NumberInput
              type="number"
              value={Number(value.table || 2000)}
              onChange={(e) => set("table", Number(e.target.value))}
            />
          </Field>
        </>
      ) : (
        <>
          <Field label="firewalld 区域">
            <input
              value={String(value.zone || "public")}
              onChange={(e) => set("zone", e.target.value)}
            />
          </Field>
          <Field label="协议">
            <select
              value={String(value.protocol || "tcp")}
              onChange={(e) => set("protocol", e.target.value)}
            >
              <option>tcp</option>
              <option>udp</option>
            </select>
          </Field>
          <Field label="端口">
            <NumberInput
              type="number"
              min={1}
              max={65535}
              value={Number(value.port || 0)}
              onChange={(e) => set("port", Number(e.target.value))}
            />
          </Field>
        </>
      )}
    </div>
  );
}
function WGNetworkEditor({
  value,
  onChange,
  routeTable,
}: {
  value: Dict;
  routeTable: React.ReactNode;
  onChange: (v: Dict) => void;
}) {
  const [lanSources, setLanSources] = useState(() => Array.isArray(value.lan_sources) ? value.lan_sources.join(", ") : "");
  const role = String(value.role || "client"),
    mode = String(value.route_mode || "auto");
  const change = (key: string, v: unknown) => onChange({ ...value, [key]: v });
  const prefixes = (key: string, label: string, hint: string) => (
    <Field label={label} hint={hint}>
      <textarea
        rows={3}
        value={
          Array.isArray(value[key]) ? (value[key] as string[]).join("\n") : ""
        }
        onChange={(e) =>
          change(
            key,
            e.target.value
              .split(/[\n,]+/)
              .map((v) => v.trim())
              .filter(Boolean),
          )
        }
      />
    </Field>
  );
  return (
    <section className="peer-editor">
      <h3>
        角色、路由与 LAN 转发
        <SettingInfo
          label="SNAT与路由"
          text={
            <>
              SNAT 不改变全局默认路由；IPv4 forwarding 需已开启。服务端 Peer
              AllowedIPs 应填写客户端地址或其实际远端网段。
            </>
          }
        />
      </h3>
      <div className="form-grid">
        <Field label="接口角色">
          <select value={role} onChange={(e) => change("role", e.target.value)}>
            <option value="client">客户端 · 连接远端网络</option>
            <option value="server">服务端 · 接受客户端接入</option>
          </select>
        </Field>
        <Field
          label="接口路由模式"
          hint="自动使用 Peer AllowedIPs；WireGuard 不从对端动态协商路由"
        >
          <select
            value={mode}
            onChange={(e) => change("route_mode", e.target.value)}
          >
            <option value="auto">自动：从 Peer AllowedIPs 生成</option>
            <option value="manual">手动：仅安装填写的路由</option>
            <option value="off">不安装路由：由策略路由单独控制</option>
          </select>
        </Field>
        {routeTable}
        {mode === "manual" && (
          <>
            {prefixes(
              "routes",
              "手动路由（每行一个 CIDR）",
              "必须被 Peer AllowedIPs 覆盖；默认路由必须使用独立路由表",
            )}
            <Field label="手动路由使用的表">
              <input
                value={String(value.route_table || "254")}
                onChange={(e) => change("route_table", e.target.value)}
              />
            </Field>
          </>
        )}
        {role === "server" && (
          <>
            <Field
              label="服务端公网入口"
              hint="用于生成客户端配置，例如 vpn.example.com:51820"
            >
              <input
                value={String(value.server_endpoint || "")}
                onChange={(e) => change("server_endpoint", e.target.value)}
              />
            </Field>
            {prefixes(
              "advertised_routes",
              "客户端访问网段（写入导出配置）",
              "例如LAN 网段；修改后需要更新客户端配置，不能动态推送",
            )}
          </>
        )}
      </div>
      <div className="wg-forward-controls">
      <label className="check">
        <input type="checkbox" checked={value.lan_forward == null ? !!value.snat : value.lan_forward === true}
          onChange={event => change("lan_forward", event.target.checked)} />
        {role === "client" ? "允许 LAN 设备经此 VPN 访问远端" : "允许 VPN 客户端访问 LAN"}
      </label>
      <label className="check">
        <input
          type="checkbox"
          checked={!!value.snat}
          onChange={event => onChange({...value, lan_forward: value.lan_forward ?? !!value.snat, snat: event.target.checked})}
        />
        启用 SNAT（使用出接口地址）
      </label>
      </div>
      {(!!value.snat || value.lan_forward === true) && (
        <div className="form-grid">
          <Field label="LAN 转发／SNAT 来源网段" hint="多个网段用逗号分隔">
            <textarea rows={1} className="wg-resizable-text" value={lanSources} onChange={event => {
              const draft = event.target.value;
              setLanSources(draft);
              change("lan_sources", draft.split(/[,，\n]+/).map(prefix => prefix.trim()).filter(Boolean));
            }} />
          </Field>
          <Field label="LAN 接口" hint="用于限定转发范围，填写连接测试 LAN 的实际接口">
            <input
              value={String(value.lan_interface || "")}
              onChange={(e) => change("lan_interface", e.target.value)}
            />
          </Field>
        </div>
      )}
    </section>
  );
}
function WGClientTemplates({ name, peers }: { name: string; peers: Dict[] }) {
  const [template, setTemplate] = useState(""),
    [error, setError] = useState("");
  return (
    <section className="peer-editor">
      <h3>
        客户端配置模板
        <SettingInfo
          label="客户端配置模板"
          text={
            <>
              读取服务器上已保存的配置；客户端私钥在客户端生成，此处仅包含公钥与路由。
            </>
          }
        />
      </h3>

      <div className="actions">
        {peers.map((_, index) => (
          <Button
            key={index}
            onClick={() =>
              void api<Dict>(
                "/api/network/wireguard/client-template?name=" +
                  encodeURIComponent(name) +
                  "&peer=" +
                  index,
              )
                .then((d) => {
                  setTemplate(String(d.template));
                  setError("");
                })
                .catch((e) => setError(errorMessage(e)))
            }
          >
            Peer {index + 1} 配置模板
          </Button>
        ))}
      </div>
      <ErrorLine error={error} />
      {template && (
        <textarea
          className="client-template"
          readOnly
          rows={12}
          value={template}
          onFocus={(e) => e.target.select()}
        />
      )}
    </section>
  );
}
function wireGuardPrefixes(value: unknown): string[] {
  return Array.isArray(value) ? value.map(String) : String(value || "").split(/[\s,，;；]+/).filter(Boolean);
}

function WireGuardPeerProbeEditor({ name, peer, routing, table, saved, onChange }: {
  name: string;
  peer: Dict;
  routing: Dict;
  table: string;
  saved: boolean;
  onChange: (value: Dict) => void;
}) {
  const probe = record(peer.probe);
  const [result, setResult] = useState<{ target: string; key: string; ok: boolean; message: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const target = String(probe.target || "");
  const key = String(peer.public_key || "");
  const applicable = result?.target === target && result?.key === key ? result : null;
  const selected = wireGuardPrefixes(probe.routes);
  const manual = routing.route_mode === "manual";
  const candidates = (manual ? wireGuardPrefixes(routing.routes) : wireGuardPrefixes(peer.allowed_ips))
    .filter(prefix => !prefix.endsWith("/0") && prefix !== target && prefix !== `${target}/32` && prefix !== `${target}/128`);
  const supported = manual ? ["254", "auto", "main"].includes(String(routing.route_table || "254")) : routing.route_mode !== "off" && ["254", "auto", ""].includes(table);
  async function test() {
    setTesting(true);
    try {
      const r = await api<Dict>("/api/network/wireguard/test-peer-probe", { name, public_key: key, target });
      const latency = Number(r.latency);
      setResult({ target, key, ok: r.ok === true, message: r.ok === true ? (latency >= 0 ? `目标可达 · ${latency.toFixed(1)} ms` : "目标可达") : "未收到 ICMP 回复" });
    } catch (error) {
      setResult({ target, key, ok: false, message: errorMessage(error) });
    } finally {
      setTesting(false);
    }
  }
  return <details className="wg-peer-probe-editor disclosure-stack">
    <summary>状态探测{probe.enabled === true ? " · 已启用" : ""}</summary>
    <label className="check"><input type="checkbox" checked={probe.enabled === true}
      onChange={event => onChange({ ...probe, enabled: event.target.checked, auto_withdraw: event.target.checked && probe.auto_withdraw === true })}/>
      启用主动探测
    </label>
    <div className="wg-peer-probe-controls">
      <Field label="探测目标 IP">
        <input value={target} onChange={event => onChange({ ...probe, target: event.target.value.trim(), auto_withdraw: false })}/>
      </Field>
      <Button size="regular" disabled={!saved || !target || testing} onClick={() => void test()}>{testing ? "测试中…" : "测试"}</Button>
    </div>
    {!saved && <p className="hint">保存并启用 Peer 后可测试当前路径。</p>}
    {applicable && <p role="status">{applicable.message}</p>}
    <label className="check"><input type="checkbox" checked={probe.auto_withdraw === true}
      disabled={probe.auto_withdraw !== true && (probe.enabled !== true || applicable?.ok !== true || !supported)}
      onChange={event => onChange({ ...probe, auto_withdraw: event.target.checked })}/>
      失联时自动撤销路由
    </label>
    {probe.auto_withdraw === true && <>
      <p className="hint">连续超时 10 次后阻断选定的业务前缀，连续成功 3 次后恢复。探测目标落在撤销网段内时，会先保留经此接口的主机路由。</p>
      <MultiSelect label="待撤销的托管路由" itemName="路由" options={[...new Set([...candidates, ...selected])]} value={selected}
        onChange={routes => onChange({ ...probe, routes })} />
    </>}
  </details>;
}

function WireGuardPeerStatusDetail({ name, label, peer, onClose }: {
  name: string;
  label: string;
  peer?: Dict;
  onClose: () => void;
}) {
  const health = record(peer?.health);
  const probe = record(peer?.probe);
  const transfer = String(peer?.transfer || "").trim().split(/\s+/).filter(Boolean);
  return <Modal title={`${name} · ${label}`} onClose={onClose} wide>
    {!peer ? <p>此 Peer 当前不可用。</p> : <>
      <dl>
        <dt>状态</dt><dd>{text(health.status)}{health.reason ? ` · ${text(health.reason)}` : ""}</dd>
        <dt>最近握手</dt><dd>{timestamp(peer.handshake)}</dd>
        <dt>累计接收／发送</dt><dd>{transfer.length === 2 ? `${formatCapacity(Number(transfer[0]))} / ${formatCapacity(Number(transfer[1]))}` : "—"}</dd>
        {probe.enabled ? <>
          <dt>探测目标 IP</dt><dd>{text(health.target)}</dd>
          <dt>最近探测</dt><dd>{timestamp(health.last_probe)}</dd>
          <dt>最近成功</dt><dd>{timestamp(health.last_success)}</dd>
          <dt>最近成功延迟</dt><dd>{health.latency == null ? "—" : `${Number(health.latency).toFixed(1)} ms`}</dd>
          <dt>连续超时</dt><dd>{Number(health.failures || 0)}/10</dd>
          <dt>连续成功</dt><dd>{Number(health.recoveries || 0)}/3</dd>
        </> : <><dt>探测模式</dt><dd>主动探测未开启</dd></>}
      </dl>
      <h3>路由状态</h3>
      {health.auto_withdraw !== true && <p>自动撤销未开启</p>}
      {list(health.routes).length ? <ul>{list(health.routes).map(route => <li key={String(route.prefix)}>
        {text(route.prefix)} · {text(route.status)}{route.covering_prefix ? `（${text(route.covering_prefix)}）` : ""}{route.changed_at ? ` · ${timestamp(route.changed_at)}` : ""}{route.error ? ` · ${text(route.error)}` : ""}
      </li>)}</ul> : <p>暂无已核实的业务路由。</p>}
    </>}
  </Modal>;
}

function PeerEditor({
  name,
  routing,
  table,
  peers,
  onChange,
  onTogglePeer,
  savedPeers,
}: {
  name: string;
  routing: Dict;
  table: string;
  peers: Dict[];
  onTogglePeer?: (peer: Dict) => void;
  savedPeers: Dict[];
  onChange: (peers: Dict[]) => void;
}) {
  const change = (index: number, key: string, value: unknown) =>
    onChange(peers.map((p, i) => (i === index ? { ...p, [key]: value } : p)));
  return (
    <div className="peer-editor">
      <div className="page-heading">
        <h3>Peer 对端 · {peers.length}</h3>
        <Button size="regular"
          onClick={() =>
            onChange([
              ...peers,
              { public_key: "", allowed_ips: [], endpoint: "", keepalive: 25 },
            ])
          }
        >
          新增 Peer
        </Button>
      </div>
      {peers.map((peer, i) => (
        <section className="peer-card" key={i}>
          <p className="hint">Peer {i+1} · {peer.disabled ? "已停用（保留配置）" : "已启用"}</p>
          <div className="form-grid">
            <Field label={"Peer " + (i + 1) + " 公钥"}>
              <WireGuardPublicKey value={String(peer.public_key || "")} onChange={key => change(i, "public_key", key)} />
            </Field>
            <Field label="AllowedIPs" hint="支持多个 IP/CIDR，用逗号、空格或换行分隔">
              <textarea rows={1} className="wg-resizable-text"
                value={
                  typeof peer.allowed_ips === "string" ? peer.allowed_ips : Array.isArray(peer.allowed_ips) ? peer.allowed_ips.join(", ") : ""
                }
                onChange={(e) =>
                  change(
                    i,
                    "allowed_ips",
                    e.target.value,
                  )
                }
              />
            </Field>
            <Field label="Endpoint" hint="可留空">
              <input
                value={String(peer.endpoint || "")}
                onChange={(e) => change(i, "endpoint", e.target.value)}
              />
            </Field>
            <Field label="PersistentKeepalive（秒）">
              <NumberInput
                type="number"
                min={0}
                max={65535}
                value={Number(peer.keepalive || 0)}
                onChange={(e) => change(i, "keepalive", Number(e.target.value))}
              />
            </Field>
            <Field
              label={
                peer.has_preshared_key
                  ? "预共享密钥（留空保留）"
                  : "预共享密钥（可选）"
              }
            >
              <input
                type="password"
                autoComplete="new-password"
                value={String(peer.preshared_key || "")}
                onChange={(e) => change(i, "preshared_key", e.target.value)}
              />
            </Field>
          </div>
          <WireGuardPeerProbeEditor
            name={name}
            peer={peer}
            routing={routing}
            table={table}
            saved={savedPeers.some(saved => saved.public_key === peer.public_key && !saved.disabled)}
            onChange={probe => change(i, "probe", probe)}
          />
          <div className="actions wg-peer-actions">
            {onTogglePeer && <Button size="regular"
              disabled={!savedPeers.some(saved => saved.public_key === peer.public_key)}
              onClick={() => onTogglePeer(peer)}
            >{peer.disabled ? "启用此 Peer" : "停用此 Peer"}</Button>}
            <Button size="regular" variant="danger"
              onClick={() => onChange(peers.filter((_, index) => index !== i))}>
              删除此 Peer
            </Button>
          </div>
        </section>
      ))}
    </div>
  );
}
export function NetworkPending({ act, inline = false, readOnly = false }: { act: Act; inline?: boolean; readOnly?: boolean }) {
  const { data, error: resourceError, refresh } = useResource<{ plans: Plan[]; server_time?: number }>("/api/network/plans", 2000);
  const [, tick] = useState(0), [offset, setOffset] = useState(0), [busy,setBusy] = useState(""), [failure,setFailure]=useState("");
  useEffect(() => { const t=setInterval(()=>tick(n=>n+1),1000); return ()=>clearInterval(t); }, []);
  useEffect(() => { if (typeof data?.server_time === "number" && Number.isFinite(data.server_time)) setOffset(Date.now()/1000-data.server_time); },[data]);
  const pending = data?.plans.filter(p=>p.status === "awaiting_confirmation") || [];
  const [dismissed,setDismissed]=useState("");
  const notice=pending.find(p=>p.id!==dismissed);
  const run = async (p:Plan, operation:string) => {
    if (readOnly || busy) return;
    setBusy(p.id+operation); setFailure("");
    try { const ok = await act("/api/network/"+operation,{id:p.id}); if(!ok) setFailure("操作未完成，请核对状态后重试；变更仍按服务端期限自动回退。"); await refresh(); }
    catch(e) {setFailure(errorMessage(e));} finally {setBusy("");}
  };
  return pending.length ? <>{notice&&<Modal title="网络变更待确认" onClose={()=>setDismissed(notice.id)}><div className="network-confirm-content"><p>{notice.summary}</p><p className="network-confirm-countdown" role="timer">{remainingPlanSeconds(notice.expires,Date.now()/1000-offset)} 秒后自动回退</p><p>请确认管理连接、VPN 和业务访问正常，再保留本次变更。</p><ErrorLine error={failure||resourceError||""}/><div className="modal-actions"><Button variant="danger" disabled={readOnly||!!busy} onClick={()=>void run(notice,"rollback")}>立即回退</Button><Button variant="primary" disabled={readOnly||!!busy||remainingPlanSeconds(notice.expires,Date.now()/1000-offset)===0||!!resourceError} onClick={()=>void run(notice,"confirm")}>确认保留</Button></div></div></Modal>}{!notice&&<section className={inline?"network-pending-banner":"network-pending"} aria-label="网络变更待确认" role="region">
    {pending.map(p=>{const remaining=remainingPlanSeconds(p.expires,Date.now()/1000-offset);return <div className="network-pending-item" key={p.id}>
      <div className="network-pending-description"><strong><AlertTriangle size={17}/>有 {pending.length} 项网络变更待确认</strong><p>{p.summary}</p><small role="timer" aria-live="off">{remaining>0?remaining+"秒后自动回退":"确认期限已到，正在核对自动回退状态"}</small>{readOnly&&<small>只读预览：请在正式服务确认或回退。</small>}</div>
      <div className="actions"><Button variant="primary" disabled={readOnly||!!busy||remaining===0||!!resourceError} onClick={()=>void run(p,"confirm")}>{busy===p.id+"confirm"?"正在确认…":"确认保留"}</Button><Button variant="danger" disabled={readOnly||!!busy} onClick={()=>void run(p,"rollback")}>{busy===p.id+"rollback"?"正在回退…":"立即回退"}</Button></div>
    </div>})}<ErrorLine error={failure||resourceError||""}/>
  </section>}</> : null;
}
