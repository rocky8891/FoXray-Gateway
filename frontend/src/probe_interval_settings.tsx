import { hasChanges } from "./settings_save";
import { useState } from "react";
import { type Dict } from "./api";
import {
  Button,
  Field,
  NumberInput,
  Modal,
  ErrorLine,
  useResource,
} from "./ui";
import { SaveCard } from "./settings_save";
import type { Act } from "./nodes";

export function ProbeIntervalField({
  module,
  act,
}: {
  module: string;
  act: Act;
}) {
  const r = useResource<Dict>(
    "/api/monitoring/probe-intervals?module=" + module,
    15000,
  );
  const [draft, setDraft] = useState<Dict | null>(null),
    [open, setOpen] = useState(false);
  const p = { ...r.data, ...draft },
    xray = module === "xray";
  const display = r.data
    ? xray
      ? `真连接 ${r.data.interval}秒 / ICMP ${r.data.icmp_interval}秒`
      : `${r.data.interval}秒`
    : "正在读取…";
  return (
    <>
      <Field
        label="主动探测间隔（秒）"
        info="关联本服务的主动检测策略，独立于全局采集和历史写入间隔。修改间隔不会开启检测，也不会重启转发服务。"
      >
        <div className="field-with-action">
          <input readOnly value={display} aria-label="当前检测间隔" />
          <Button
            disabled={!r.data}
            onClick={() => {
              setDraft(null);
              setOpen(true);
            }}
          >
            修改
          </Button>
        </div>
      </Field>
      <ErrorLine error={r.error} />
      {open && (
        <Modal
          title={
            (xray ? "Xray" : module === "warp" ? "WARP" : "WireGuard") +
            "检测间隔"
          }
          onClose={() => setOpen(false)}
        >
          <div className="form-grid">
            <Field
              label={xray ? "真连接检测间隔（秒）" : "检测间隔（秒）"}
              info={
                xray
                  ? "0～3600秒；0暂停周期检测，也会暂停依赖周期检测的自动选优。独立于监控采集间隔。"
                  : "10～3600秒，使用本服务原有检测策略。检测关闭时修改间隔不会将它开启。"
              }
            >
              <NumberInput
                aria-label={xray ? "真连接检测间隔（秒）" : "检测间隔（秒）"}
                min={xray ? 0 : 10}
                max={3600}
                value={Number(p.interval)}
                onChange={(e) =>
                  setDraft({ ...p, interval: Number(e.target.value) })
                }
              />
            </Field>
            {xray && (
              <Field
                label="ICMP检测间隔（秒）"
                info="0～3600秒；0暂停周期ICMP检测。仅修改间隔，保留原检测开关、包数、大小和目标。"
              >
                <NumberInput
                  aria-label="ICMP检测间隔（秒）"
                  min={0}
                  max={3600}
                  value={Number(p.icmp_interval)}
                  onChange={(e) =>
                    setDraft({ ...p, icmp_interval: Number(e.target.value) })
                  }
                />
              </Field>
            )}
          </div>
          <p className="small muted">
            {xray
              ? `真连接检测${p.enabled ? "已开启" : "已关闭"}；ICMP检测${p.icmp_enabled ? "已开启" : "已关闭"}`
              : `主动检测${p.enabled ? "已开启" : "已关闭"}`}
          </p>
          <SaveCard
 dirty={hasChanges(draft,r.data)}
            title="检测间隔"
            defaults={() =>
              setDraft({
                ...p,
                interval: xray ? 30 : 60,
                ...(xray ? { icmp_interval: 300 } : {}),
              })
            }
            discard={() => setDraft(null)}
            save={async () => {
              const ok = await act(
                "/api/monitoring/probe-intervals?module=" + module,
                {
                  interval: p.interval,
                  ...(xray ? { icmp_interval: p.icmp_interval } : {}),
                },
              );
              if (ok) {
                await r.refresh();
                setDraft(null);
                setOpen(false);
              }
              return ok;
            }}
          />
        </Modal>
      )}
    </>
  );
}
