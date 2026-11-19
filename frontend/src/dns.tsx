import { DnsIcon } from "./note_icons";
import { useState } from "react";
import { RefreshCw } from "lucide-react";
import { api, record, errorMessage, type Dict, type Plan } from "./api";
import {
  Button,
  Field,
  Panel,
  PageHeader,
  ErrorLine,
  Modal,
  useResource,
} from "./ui";
import type { Act } from "./nodes";
export function DNSPage({
  act,
  initialScope = "host",
}: {
  act: Act;
  initialScope?: string;
}) {
  const [scope, setScope] = useState(initialScope),
    [iface, setIface] = useState(""),
    [servers, setServers] = useState(""),
    [plan, setPlan] = useState<Plan | null>(null),
    [error, setError] = useState(""),
    [ack, setAck] = useState(false);
  const r = useResource<Dict>(
    "/api/network/dns?scope=" +
      scope +
      "&interface=" +
      encodeURIComponent(iface),
    15000,
  );
  const profile = record(r.data?.profile);
  const effective =
    ((scope === "host" && iface
      ? profile.runtime_servers || r.data?.servers
      : r.data?.servers) as string[]) || [];
  return (
    <Panel className="dns-panel">
      <PageHeader
        title="DNS"
        icon={<DnsIcon size={20} />}
        description={
          <>
            {
              "查看系统与WARP隔离解析配置，预览后应用，120秒内确认；不启停网络连接。"
            }
            <p>
              {String(r.data?.note || "WARP仅更改独立解析文件，不改主机DNS。")}
            </p>
          </>
        }
        actions={
          <Button onClick={() => void r.refresh()}>
            <RefreshCw size={14} />
            刷新
          </Button>
        }
      />
      <ErrorLine error={error || r.error || String(r.data?.error || "")} />
      <div className="form-grid">
        <Field label="DNS范围">
          <select
            value={scope}
            onChange={(e) => {
              setScope(e.target.value);
              setServers("");
              setError("");
            }}
          >
            <option value="host">系统 / Xray使用的系统DNS</option>
            <option value="warp">WARP独立DNS</option>
          </select>
        </Field>
        {scope === "host" && (
          <Field label="接口">
            <select
              value={iface}
              onChange={(e) => {
                setIface(e.target.value);
                setServers("");
              }}
            >
              <option value="">选择要管理的接口</option>
              {((r.data?.interfaces as string[]) || []).map((name) => (
                <option key={name}>{name}</option>
              ))}
            </select>
          </Field>
        )}
      </div>
      <div className="dns-server-grid">
        <Field label="当前生效DNS">
          <textarea readOnly value={effective.join("\n")} />
        </Field>
        <Field label="新DNS服务器" hint="每行一个IP，1～6个">
          <textarea
            value={servers}
            onChange={(e) => setServers(e.target.value)}
            placeholder="1.1.1.1&#10;223.5.5.5"
          />
        </Field>
      </div>

      <label className="check">
        <input
          type="checkbox"
          checked={ack}
          onChange={(e) => setAck(e.target.checked)}
        />
        我已确认DNS变更范围；未确认变更自动回退。
      </label>
      <div className="actions">
        <Button
          disabled={!ack || !r.data?.editable}
          onClick={async () => {
            try {
              setPlan(
                await api<Plan>("/api/network/plan", {
                  kind: "dns",
                  operation: "replace",
                  allow_protected: ack,
                  item: {
                    scope,
                    interface: iface,
                    servers: servers.split(/[\s,]+/).filter(Boolean),
                  },
                }),
              );
              setError("");
            } catch (e) {
              setError(errorMessage(e));
            }
          }}
        >
          生成变更预览
        </Button>
      </div>
      {plan && (
        <Modal title="DNS变更预览" onClose={() => setPlan(null)} wide>
          <h3>{plan.summary}</h3>
          {plan.warnings.map((v, i) => (
            <p className="hint" key={i}>
              {v}
            </p>
          ))}
          <pre className="command-preview">{plan.preview.join("\n")}</pre>
          <Button
            onClick={async () => {
              if (await act("/api/network/apply", { id: plan.id })) {
                setPlan(null);
                void r.refresh();
              }
            }}
          >
            应用并等待网络确认
          </Button>
        </Modal>
      )}
    </Panel>
  );
}
