import { NetworkPage } from "./network";
import { ProductIcon } from "./product_icons";
import { createContext, useContext, useEffect, useRef, useState } from "react";
import { BookOpen, RefreshCw } from "lucide-react";
import { record, timestamp, type Dict } from "./api";
import {
  Button,
  SettingInfo,
  Confirm,
  ErrorLine,
  Modal,
  PageHeader,
  Panel,
  useResource,
} from "./ui";
import type { Act } from "./nodes";
export const UpdatesContext = createContext<{
  data: Dict;
  receive?: (value: Dict) => void;
}>({ data: {} });
export function UpdateBadge({ service }: { service?: string }) {
  const { data } = useContext(UpdatesContext),
    services = record(data.services);
  const available = service
    ? !!record(services[service]).update_available
    : Object.values(services).some((v) => !!record(v).update_available);
  return available ? (
    <span
      className="update-dot"
      role="img"
      aria-label="有新版本"
      title="有新版本"
    />
  ) : null;
}
const names: Record<string, string> = {
  foxray: "FoXray",
  xray: "Xray-core",
  warp: "WARP",
  wireguard: "WireGuard 工具包",
};
export function VersionPanel({
  service,
  act,
  current,
  rollback,
}: {
  service: string;
  act: Act;
  current?: string;
  rollback?: React.ReactNode;
}) {
  const r = useResource<Dict>("/api/updates", 5000),
    [notes, setNotes] = useState(false),
    [confirm, setConfirm] = useState(false),
    [started, setStarted] = useState(0),
    [policyDraft, setPolicyDraft] = useState<{
      enabled: boolean;
      interval_hours: number;
    } | null>(null),
    [policyBusy, setPolicyBusy] = useState(false),
    [policyMessage, setPolicyMessage] = useState("");
  const storedInfo = record(record(r.data?.services)[service]);
  const info: Dict =
    service === "foxray" &&
    current &&
    storedInfo.current &&
    current !== storedInfo.current
      ? {
          ...storedInfo,
          current,
          status: "unchecked",
          message:
            "当前运行版本：" + current + "；版本检查缓存已过期，请重新检查",
          update_available: false,
          checked_at: undefined,
          latest: undefined,
        }
      : {
          ...storedInfo,
          current:
            service === "foxray"
              ? current || storedInfo.current
              : storedInfo.current || current,
        };
  const savedPolicy = record(info.check_policy);
  const policy = policyDraft || {
    enabled: savedPolicy.enabled !== false,
    interval_hours: Number(savedPolicy.interval_hours || 24),
  };
  useEffect(() => {
    setPolicyDraft(null);
    setPolicyMessage("");
  }, [service]);
  const { receive } = useContext(UpdatesContext);
  useEffect(() => {
    if (r.data) receive?.(r.data);
  }, [r.data, receive]);
  const previousCheck = useRef<unknown>(undefined);
  const checking = !r.error && (info.status === "checking" || started > 0);
  useEffect(() => {
    if (
      started &&
      info.checked_at !== previousCheck.current &&
      info.status !== "checking"
    )
      setStarted(0);
  }, [started, info.checked_at, info.status]);
  const labels: Record<string, string> = {
    unchecked: "尚未检查",
    checking: "正在检查",
    current: "已是最新版本",
    available: "发现新版本",
    error: "检查或更新失败",
  };
  const summary = String(
    info.message || labels[String(info.status)] || "尚未检查",
  );
  return (
    <section
      className={"version-panel " + (info.update_available ? "has-update" : "")}
      aria-label={names[service] + "版本管理"}
    >
      <div className="version-facts">
        <span>
          <span className="version-fact-label">当前版本</span>{" "}
          <b
            className={
              info.status === "current" && !info.update_available
                ? "good"
                : undefined
            }
          >
            {String(info.current || current || "待检查")}
          </b>
        </span>
        <span>
          <span className="version-fact-label">最新版本</span>{" "}
          <b>{String(info.latest || "待检查")}</b>
        </span>
        <span>
          <span className="version-fact-label">最近检查</span>{" "}
          <b>{timestamp(info.checked_at)}</b>
        </span>
        <span>
          <span className="version-fact-label">自动检查</span>{" "}
          <b>
            {savedPolicy.enabled === false
              ? "已关闭"
              : `每 ${Number(savedPolicy.interval_hours || 24)} 小时`}
          </b>
        </span>
      </div>
      <div className="actions version-actions">
        <span className="version-check-control">
          <Button
            disabled={checking || r.data?.read_only === true}
            onClick={() => {
              previousCheck.current = info.checked_at;
              setStarted(Date.now() / 1000);
              void act("/api/updates/check", { service }).then((ok) => {
                if (!ok) setStarted(0);
                void r.refresh();
              });
            }}
          >
            <RefreshCw size={14} />
            {checking ? "检查中…" : "检查更新"}
          </Button>
          <SettingInfo
            label="检查更新"
            text={
              <>
                <p>{summary}</p>
                <p>版本来源：{String(info.source || "等待版本源检查")}</p>
                {Object.entries(info)
                  .filter(
                    ([key, value]) =>
                      /url|^source_url$/.test(key) &&
                      typeof value === "string" &&
                      !!value,
                  )
                  .map(([key, value]) => (
                    <p key={key}>{String(value)}</p>
                  ))}
              </>
            }
          />
        </span>
        <Button
          disabled={
            !info.update_available ||
            checking ||
            info.status === "error" ||
            r.data?.read_only === true
          }
          onClick={() => setConfirm(true)}
        >
          一键更新
        </Button>
        {rollback}
        <span className="version-release-notes">
          <Button disabled={!info.checked_at} onClick={() => setNotes(true)}>
            <BookOpen size={14} />
            更新日志
          </Button>
        </span>
      </div>
      <div
        className="version-check-policy"
        aria-label={names[service] + "检查设置"}
      >
        <label className="check">
          <input
            type="checkbox"
            checked={policy.enabled}
            disabled={policyBusy || r.data?.read_only === true}
            onChange={(e) => {
              setPolicyDraft({ ...policy, enabled: e.target.checked });
              setPolicyMessage("");
            }}
          />
          自动检查新版本
          <SettingInfo
            label="自动检查新版本"
            text="只控制版本信息检查；手动检查随时可用，不自动安装更新。"
          />
        </label>
        <label className="check-interval-label">
          检查间隔（小时）
          <input
            aria-label={names[service] + "检查间隔（小时）"}
            type="number"
            min={1}
            max={168}
            value={policy.interval_hours}
            disabled={
              !policy.enabled || policyBusy || r.data?.read_only === true
            }
            onChange={(e) => {
              setPolicyDraft({
                ...policy,
                interval_hours: Number(e.target.value),
              });
              setPolicyMessage("");
            }}
          />
        </label>
        <Button
          variant="primary"
          highlighted={
            !!policyDraft &&
            (policy.enabled !== savedPolicy.enabled ||
              policy.interval_hours !== savedPolicy.interval_hours)
          }
          disabled={
            !policyDraft ||
            policyBusy ||
            r.data?.read_only === true ||
            !Number.isInteger(policy.interval_hours) ||
            policy.interval_hours < 1 ||
            policy.interval_hours > 168
          }
          onClick={() => {
            setPolicyBusy(true);
            void act("/api/updates/policy", { service, ...policy })
              .then(async (ok) => {
                if (ok) {
                  await r.refresh();
                  setPolicyDraft(null);
                  setPolicyMessage("检查设置已保存");
                }
              })
              .finally(() => setPolicyBusy(false));
          }}
        >
          保存
        </Button>
        {!!policyMessage && (
          <span className="small muted" role="status">
            {policyMessage}
          </span>
        )}
      </div>
      <ErrorLine error={r.error} />
      {notes && (
        <Modal
          title={names[service] + " 更新日志"}
          copyText={`${String(info.current || current || "—")} → ${String(info.latest || "—")}\n\n${String(info.release_notes || "版本源未提供更新日志。")}`}
          wide
          onClose={() => setNotes(false)}
        >
          <p className="small">
            {String(info.current || current || "—")} →{" "}
            {String(info.latest || "—")}
          </p>
          <pre className="release-notes">
            {String(info.release_notes || "版本源未提供更新日志。")}
          </pre>
        </Modal>
      )}
      {confirm && (
        <Confirm
          title={"更新 " + names[service]}
          message={
            <>
              <p>
                确认从 {String(info.current || current)} 更新到{" "}
                {String(info.latest)}。
              </p>
              <p>
                {service === "foxray"
                  ? "只更新管理程序与前端，页面短暂重连；校验或健康检查失败恢复旧版本。"
                  : service === "wireguard"
                    ? "只升级 wireguard-tools 软件包，不升级内核，不重启现有 VPN 接口。"
                    : "会重启本模块服务，已有代理或隧道连接可能短暂中断；其他模块保持独立。"}
              </p>
            </>
          }
          onClose={() => setConfirm(false)}
          onConfirm={() => {
            setConfirm(false);
            void act("/api/updates/upgrade", {
              service,
              version: info.latest,
              confirmed: true,
            });
          }}
        />
      )}
    </section>
  );
}
export function WireGuardServicePanel({ act }: { act: Act }) {
  return (
    <>
      <Panel className="component-service-panel">
        <PageHeader
          title="版本信息"
          icon={<ProductIcon product="wireguard" size={20} />}
          description="WireGuard 按接口管理服务；工具包可以更新，接口启停和自启仍在接口管理中设置。"
        />
        <VersionPanel service="wireguard" act={act} />
      </Panel>
      <NetworkPage kind="wireguard" serviceOnly act={act} />
    </>
  );
}
