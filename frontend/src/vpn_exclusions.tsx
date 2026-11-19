import { useEffect, useRef, useState } from "react";
import { api, list, record, type Dict } from "./api";
import { ErrorLine, Field } from "./ui";

function ExclusionLines({
  value,
  onChange,
  disabled,
  label,
  placeholder,
}: {
  value: unknown;
  onChange: (value: string[]) => void;
  disabled: boolean;
  label: string;
  placeholder?: string;
}) {
  const format = (v: unknown) => (Array.isArray(v) ? v.join("\n") : "");
  const [text, setText] = useState(format(value)),
    editing = useRef(false);
  useEffect(() => {
    if (!editing.current) setText(format(value));
  }, [value]);
  return (
    <textarea
      aria-label={label}
      rows={4}
      disabled={disabled}
      placeholder={placeholder}
      value={text}
      onFocus={() => (editing.current = true)}
      onBlur={() => {
        editing.current = false;
        setText(format(value));
      }}
      onChange={(e) => {
        setText(e.target.value);
        onChange(
          e.target.value
            .split(/[\n,]+/)
            .map((s) => s.trim())
            .filter(Boolean),
        );
      }}
    />
  );
}
export function VPNExclusionsEditor({
  value,
  onChange,
  disabled,
}: {
  value: unknown;
  onChange: (value: Dict) => void;
  disabled: boolean;
}) {
  const p = {
    mode: "default",
    interfaces: [],
    cidrs: [],
    domains: [],
    ...record(value),
  };
  const [interfaces, setInterfaces] = useState<string[]>([]),
    [error, setError] = useState("");
  useEffect(() => {
    if (p.mode !== "custom") return;
    let live = true;
    void api<Dict>("/api/network/interfaces")
      .then((data) => {
        if (live) {
          setInterfaces(
            list(data.items)
              .map((v) => String(v.ifname || v.name))
              .filter(Boolean),
          );
          setError("");
        }
      })
      .catch(() => {
        if (live) setError("接口列表读取失败，可手动填写有效接口名称。");
      });
    return () => {
      live = false;
    };
  }, [p.mode]);
  const set = (key: string, v: unknown) => onChange({ ...p, [key]: v });
  return (
    <div className="vpn-exclusions-editor settings-section">
      <Field
        label="VPN排除模式"
        info="默认保护vone、现有WireGuard接口、私网、节点入口和IPv6原路径；自定义模式在此基础上增加排除。"
      >
        <select
          aria-label="VPN排除模式"
          value={p.mode}
          disabled={disabled}
          onChange={(e) => set("mode", e.target.value)}
        >
          <option value="default">默认排除</option>
          <option value="custom">自定义排除</option>
        </select>
      </Field>

      {p.mode === "custom" && (
        <>
          <div className="form-grid">
            <div className="field vpn-exclusion-interfaces">
              <div className="vpn-interface-heading">
                <span className="field-title">排除接口（每行一个）</span>
                <select
                  aria-label="添加排除接口"
                  disabled={disabled}
                  value=""
                  onChange={(e) => {
                    if (e.target.value)
                      set("interfaces", [
                        ...new Set([
                          ...(p.interfaces as string[]),
                          e.target.value,
                        ]),
                      ]);
                  }}
                >
                  <option value="">从现有接口添加…</option>
                  {interfaces.map((name) => (
                    <option key={name}>{name}</option>
                  ))}
                </select>
              </div>
              <ExclusionLines
                label="VPN排除接口"
                disabled={disabled}
                value={p.interfaces}
                onChange={(v) => set("interfaces", v)}
              />
            </div>
            <Field label="排除目标CIDR" hint="每行一个">
              <ExclusionLines
                label="VPN排除CIDR"
                disabled={disabled}
                placeholder="198.51.100.0/24"
                value={p.cidrs}
                onChange={(v) => set("cidrs", v)}
              />
            </Field>
            <Field
              label="排除目标域名"
              hint="每行一个"
              info="example.com包含子域名；full:example.com仅精确匹配。域名按Xray可识别的HTTP/TLS/QUIC信息直连，无法识别的加密域名不猜测。"
            >
              <ExclusionLines
                label="VPN排除域名"
                disabled={disabled}
                placeholder="example.com"
                value={p.domains}
                onChange={(v) => set("domains", v)}
              />
            </Field>
          </div>
          <ErrorLine error={error} />
        </>
      )}
    </div>
  );
}
