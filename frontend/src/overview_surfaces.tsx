import { ColorInput } from "./color_input";
import { OverviewSettingsCard } from "./overview_settings_card";
import { themedAppearance } from "./theme_appearance";
import type { CSSProperties, ReactNode } from "react";
import { record, type Dict } from "./api";
import { Field, NumberInput, SettingsSection } from "./ui";

const layers = [
  ["container", "页面容器"],
  ["card", "监控卡片"],
  ["detail", "指标卡片"],
] as const;

const defaultModuleBorder = (layer: string, module: string) =>
  layer === "card" || module !== "host";

export function overviewSurfaceError(value: unknown): string {
  if (value === undefined) return "";
  if (!value || typeof value !== "object" || Array.isArray(value))
    return "卡片外观格式无效";
  for (const [key, raw] of Object.entries(value)) {
    const s = record(raw);
    if (
      !layers.some(([id]) => id === key) ||
      Object.keys(s).some(
        (key) =>
          ![
            "enabled",
            "color",
            "opacity",
            "blur",
            "border_enabled",
            "border_visible",
            "module_borders",
            "border_color",
            "border_opacity",
            "shadow",
          ].includes(key),
      ) ||
      typeof s.enabled !== "boolean" ||
      !/^#[0-9a-f]{6}$/i.test(String(s.color))
    )
      return "卡片外观格式或颜色无效";
    if (
      !Number.isInteger(s.opacity) ||
      Number(s.opacity) < 0 ||
      Number(s.opacity) > 100
    )
      return "背景不透明度须为0～100的整数";
    if (!Number.isInteger(s.blur) || Number(s.blur) < 0 || Number(s.blur) > 40)
      return "毛玻璃模糊须为0～40的整数";
    if (s.border_enabled !== undefined && typeof s.border_enabled !== "boolean")
      return "边框开关无效";
    if (s.border_visible !== undefined && typeof s.border_visible !== "boolean")
      return "显示边框开关无效";
    if (s.module_borders !== undefined && (!s.module_borders || typeof s.module_borders !== "object" || Array.isArray(s.module_borders) || Object.entries(record(s.module_borders)).some(([key, value]) => !["host", "xray", "warp", "wireguard"].includes(key) || typeof value !== "boolean")))
      return "监控卡片边框设置无效";
    if (
      s.border_color !== undefined &&
      !/^#[0-9a-f]{6}$/i.test(String(s.border_color))
    )
      return "边框颜色无效";
    if (
      s.border_opacity !== undefined &&
      (!Number.isInteger(s.border_opacity) ||
        Number(s.border_opacity) < 0 ||
        Number(s.border_opacity) > 100)
    )
      return "边框不透明度须为0～100的整数";
    if (
      s.shadow !== undefined &&
      (!Number.isInteger(s.shadow) ||
        Number(s.shadow) < 0 ||
        Number(s.shadow) > 40)
    )
      return "阴影须为0～40的整数";
  }
  return "";
}

export function overviewTextError(value: unknown): string {
  if (value === undefined) return "";
  if (!value || typeof value !== "object" || Array.isArray(value))
    return "指标文字格式无效";
  for (const [key, n] of Object.entries(value))
    if (
      !["label_size", "value_size"].includes(key) ||
      !Number.isInteger(n) ||
      Number(n) < (key === "label_size" ? 8 : 12) ||
      Number(n) > (key === "label_size" ? 24 : 40)
    )
      return "指标名称字号须为8～24，数值字号须为12～40的整数";
  return "";
}

export function overviewSurfaceVariables(value: unknown): CSSProperties {
  const p = themedAppearance(value),
    surfaces = record(p.overview_surfaces),
    text = record(p.overview_text);
  const variables: Record<string, string> = {
    "--wg-ring-color": /^#[0-9a-f]{6}$/i.test(
      String(p.services_wireguard_ring_color),
    )
      ? String(p.services_wireguard_ring_color)
      : "var(--line)",
    "--wg-circle-axis": String(p.services_wireguard_axis || "auto"),
    "--wg-circle-size": String(Number(p.services_wireguard_diameter || 0)),
    "--wg-label-size": `${Number(p.services_wireguard_label_size || 14)}px`,
    "--wg-value-size": `${Number(p.services_wireguard_value_size || 24)}px`,
    "--overview-label-size": `${Math.min(24, Math.max(8, Number(text.label_size) || 14))}px`,
    "--overview-value-size": `${Math.min(40, Math.max(12, Number(text.value_size) || 24))}px`,
  };
  const rgba = (color: unknown, opacity: unknown) => {
    if (!/^#[0-9a-f]{6}$/i.test(String(color))) return undefined;
    const c = String(color),
      rgb = [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16));
    return `rgba(${rgb.join(",")},${Math.max(0, Math.min(100, Number(opacity))) / 100})`;
  };
  for (const [id] of layers) {
    const s = record(surfaces[id]);
    if (s.enabled === true) {
      const paint = rgba(s.color, s.opacity);
      if (paint) {
        variables[`--overview-${id}-paint`] = paint;
        variables[`--overview-${id}-blur`] = `${Number(s.blur) || 0}px`;
      }
    }
    if (s.enabled === true) {
      const edge = rgba(s.border_color, s.border_opacity);
      if (edge) {
        variables[`--overview-${id}-edge`] = edge;
        variables[`--overview-${id}-shadow`] =
          Number(s.shadow) > 0
            ? `0 4px ${Number(s.shadow)}px ${rgba(s.border_color, 22)}`
            : "none";
      }
    }
    if (id === "container" && s.border_visible === false) {
      variables[`--overview-${id}-edge`] = "transparent";
      variables[`--overview-${id}-shadow`] = "none";
      variables[`--overview-${id}-border-hidden`] = "1";
    }
  }
  for (const layer of ["card", "detail"]) {
    const surface = record(surfaces[layer]);
    const borders = record(surface.module_borders);
    for (const module of ["host", "xray", "warp", "wireguard"]) {
      if ((borders[module] ?? (surface.border_visible === false ? false : defaultModuleBorder(layer, module))) === false) {
        const prefix = `--overview-${module}${layer === "detail" ? "-detail" : ""}`;
        variables[`${prefix}-edge`] = "transparent";
        variables[`${prefix}-shadow`] = "none";
      }
    }
  }
  const details = record(surfaces.detail);
  variables["--overview-host-detail-width"] = record(details.module_borders).host === true ? "1px" : "0px";
  const ring = String(p.services_wireguard_ring_color || "#3b4451");
  if (
    p.services_wireguard_ring_color ||
    p.services_wireguard_ring_opacity !== undefined
  )
    variables["--wg-ring-color"] = rgba(
      ring,
      p.services_wireguard_ring_opacity ?? 100,
    )!;
  variables["--wg-ring-shadow"] =
    Number(p.services_wireguard_ring_shadow) > 0
      ? `0 0 ${Number(p.services_wireguard_ring_shadow)}px ${rgba(ring, 40)}`
      : "none";
  return variables as CSSProperties;
}

export function OverviewSurfaceSettings({
  presentation,
  onChange,
  onTextChange,
  actions,
}: {
  actions?: ReactNode;
  presentation: Dict;
  onChange: (value: Dict) => void;
  onTextChange: (value: Dict) => void;
}) {
  const surfaces = record(presentation.overview_surfaces);
  return (
    <OverviewSettingsCard
      title="卡片外观"
      actions={actions}
      info={<ul><li><strong>布局层。</strong>三种主页共用分层外观，每层可单独设置。</li><li><strong>边框。</strong>可独立关闭。背景、布局与服务器圆环进度继续显示。</li><li><strong>自定义配色。</strong>开启后生效。透明度只作用于背景，毛玻璃模糊背后的内容。</li></ul>}
    >
      <OverviewTextFields presentation={presentation} onChange={onTextChange} />
      {layers.map(([id, title]) => {
        const raw = record(surfaces[id]);
        const s = {
          enabled: false,
          color: "#171f2b",
          opacity: 80,
          blur: 12,
          border_enabled: false,
          border_visible: true,
          module_borders: {},
          border_color: "#40516a",
          border_opacity: 60,
          shadow: 12,
          ...raw,
          ...(id !== "container" ? {
            border_visible: true,
            module_borders: Object.fromEntries(["host", "xray", "warp", "wireguard"].map(module => [module, record(raw.module_borders)[module] ?? (raw.border_visible === false ? false : defaultModuleBorder(id, module))])),
          } : {}),
        };
        const set = (key: string, value: unknown) =>
          onChange({
            ...surfaces,
            [id]: {
              ...s,
              [key]: value,
            },
          });
        return (
          <div className="overview-surface-row" key={id}>
            <label className="check">
              <input
                type="checkbox"
                aria-label={`${title}自定义外观`}
                checked={s.enabled === true}
                onChange={(e) => set("enabled", e.target.checked)}
              />
              {title}
            </label>
            {id === "container" && <label className="check">
              <input
                type="checkbox"
                aria-label={`${title}显示边框`}
                checked={s.border_visible !== false}
                onChange={(e) => set("border_visible", e.target.checked)}
              />
              显示边框
            </label>}
            {id !== "container" && (
              <div className="monitor-items-options">
                {[["host", "服务器"], ["xray", "Xray"], ["warp", "WARP"], ["wireguard", "WireGuard"]].map(([module, label]) => (
                  <label className="check" key={module}>
                    <input type="checkbox" aria-label={`${label}${id === "detail" ? "指标" : "卡片"}显示边框`}
                      checked={(record(s.module_borders)[module] ?? defaultModuleBorder(id, module)) !== false}
                      disabled={!s.border_visible}
                      onChange={e => set("module_borders", {...record(s.module_borders), [module]: e.target.checked})} />
                    {label}显示边框
                  </label>
                ))}
              </div>
            )}
            <div className="form-grid">
              <Field label="背景颜色">
                <ColorInput
                  aria-label={`${title}背景颜色`}
                  value={String(s.color)}
                  disabled={!s.enabled}
                  onChange={(color) => set("color", color)}
                />
              </Field>
              <Field
                label="透明度（%）"
                validateNumber
                info={<ul><li><strong>透明度。</strong>0% 为完全不透明，100% 为完全透明。</li><li><strong>影响范围。</strong>只改变背景，文字保持原有透明度。</li></ul>}
              >
                <NumberInput
                  aria-label={`${title}不透明度`}
                  min={0}
                  max={100}
                  value={100 - Number(s.opacity)}
                  disabled={!s.enabled}
                  onChange={(e) => set("opacity", 100 - Number(e.target.value))}
                />
              </Field>
              <Field
                label="毛玻璃"
                validateNumber
                info="背景模糊强度，0关闭，最大40；背景存在透明度时更容易看到效果。"
              >
                <NumberInput
                  aria-label={`${title}毛玻璃模糊`}
                  min={0}
                  max={40}
                  value={Number(s.blur)}
                  disabled={!s.enabled}
                  onChange={(e) => set("blur", Number(e.target.value))}
                />
              </Field>
            </div>
            <div className="form-grid">
              <Field label="边框颜色">
                <ColorInput
                  aria-label={`${title}边框颜色`}
                  value={String(s.border_color)}
                  disabled={!s.enabled || !s.border_visible}
                  onChange={(color) => set("border_color", color)}
                />
              </Field>
              <Field
                label="透明度（%）"
                validateNumber
                info="边框透明程度：0%完全不透明，100%完全透明。"
              >
                <NumberInput
                  aria-label={`${title}边框不透明度`}
                  min={0}
                  max={100}
                  value={100 - Number(s.border_opacity)}
                  disabled={!s.enabled || !s.border_visible}
                  onChange={(e) =>
                    set("border_opacity", 100 - Number(e.target.value))
                  }
                />
              </Field>
              <Field
                label="阴影"
                validateNumber
                info="0关闭阴影，数值越大阴影越柔和。服务器指标卡片不应用自定义边框与阴影。"
              >
                <NumberInput
                  aria-label={`${title}阴影柔化`}
                  min={0}
                  max={40}
                  value={Number(s.shadow)}
                  disabled={!s.enabled || !s.border_visible}
                  onChange={(e) => set("shadow", Number(e.target.value))}
                />
              </Field>
            </div>
          </div>
        );
      })}
    </OverviewSettingsCard>
  );
}

function OverviewTextFields({
  presentation,
  onChange,
}: {
  presentation: Dict;
  onChange: (value: Dict) => void;
}) {
  const text = {
    label_size: 14,
    value_size: 24,
    ...record(presentation.overview_text),
  };
  return (
    <SettingsSection
      className="overview-text-card"
      title="指标文字"
      info={<ul><li><strong>适用范围。</strong>三种主页共用字号设置，名称与数值分别配置。</li><li><strong>自适应。</strong>同一卡片使用一致字号；空间不足时统一缩小，尽量保持单行完整。</li></ul>}
    >
      <div className="form-grid">
        <Field label="指标名称字号（px）" validateNumber>
          <NumberInput
            aria-label="指标名称字号"
            min={8}
            max={24}
            value={Number(text.label_size)}
            onChange={(e) =>
              onChange({ ...text, label_size: Number(e.target.value) })
            }
          />
        </Field>
        <Field label="指标数值字号（px）" validateNumber>
          <NumberInput
            aria-label="指标数值字号"
            min={12}
            max={40}
            value={Number(text.value_size)}
            onChange={(e) =>
              onChange({ ...text, value_size: Number(e.target.value) })
            }
          />
        </Field>
      </div>
    </SettingsSection>
  );
}
