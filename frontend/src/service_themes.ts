import { appearancePatch } from "./theme_appearance";
import { record, type Dict } from "./api";
import {
  DEFAULT_SERVICE_THEME,
  overviewPolicy,
  serviceStyle,
  serviceStyleAvailable,
  SERVICE_CIRCLE_STYLES,
  SERVICE_STYLE_FIELDS,
  wireguardNameStyle,
} from "./overview_policy";

const arrangements = [
  ["grid", "宫格"],
  ["top", "上下"],
  ["sidebar", "左右"],
];
const names = [["above", "上方名称"]];
const ARRANGED_THEMES = arrangements.flatMap(([layout, arrangement]) => [
  ...[["metrics", "对称矩形"]].map(([style, align]) => ({
    id: `${layout}-${style}`,
    label: `${arrangement}-${align}`,
    group: arrangement,
    shape: "对称矩形",
    leaf: align,
    values: {
      services_layout: layout,
      services_host_style: style,
      services_xray_style: style,
      services_warp_style: style,
      services_wireguard_layout: style,
      services_wireguard_style: "center",
    } as Dict,
  })),
  ...[
    ["radial", "四圆横排"],
    ["radial-square", "四圆方形"],
    ["radial-diamond", "四圆菱形"],
    ["radial-diamond-compact", "四圆心形"],
    ["radial-wave", "四圆波浪"],
    ["radial-mixed", "四圆混合"],
    ...(layout === "sidebar" ? [["radial-vertical", "四圆竖排"]] : []),
  ].flatMap(([style, shape]) =>
    names.map(([name, label]) => ({
      id: `${layout}-${style}-${name}`,
      label: `${arrangement}-${shape}-${label}`,
      group: arrangement,
      shape,
      leaf: label,
      values: {
        services_layout: layout,
        services_host_style: style,
        services_xray_style: style,
        services_warp_style: style,
        services_wireguard_layout: "radial",
        services_wireguard_style: name,
      } as Dict,
    })),
  ),
]);

export const SERVICE_THEMES = [
  {
    id: "default",
    label: "默认主题",
    group: "默认",
    shape: "组合",
    leaf: "默认主题",
    values: DEFAULT_SERVICE_THEME,
  },
  ...ARRANGED_THEMES,
];

export function currentServiceTheme(value: unknown) {
  const p = record(value);
  return (
    SERVICE_THEMES.find(
      (theme) =>
        theme.values.services_layout === overviewPolicy(p, "services").layout &&
        ["host", "xray", "warp", "wireguard"].every(
          (module) =>
            serviceStyle(p, module) ===
            serviceStyle({ ...p, ...theme.values }, module),
        ) &&
        (!String(theme.values.services_host_style).startsWith("radial") ||
          wireguardNameStyle(p) === theme.values.services_wireguard_style),
    )?.id || "custom"
  );
}

export const CUSTOM_THEME_KEYS = [
  "services_layout",
  "services_cards",
  "services_style",
  "services_host_style",
  "services_xray_style",
  "services_warp_style",
  "services_wireguard_layout",
  "services_wireguard_style",
  "services_wireguard_count",
  "services_wireguard_axis",
  "services_wireguard_diameter",
  "services_wireguard_label_size",
  "services_wireguard_value_size",
  "services_wireguard_ring_color",
  "services_wireguard_ring_opacity",
  "services_wireguard_ring_shadow",
  "services_host_order",
  "services_xray_order",
  "services_warp_order",
  "services_wireguard_interfaces",
  "overview_text",
  "overview_surfaces",
];
export function serviceThemeSnapshot(input: unknown) {
  const value = record(input);
  return appearancePatch(
    Object.fromEntries(
      CUSTOM_THEME_KEYS.filter((key) => value[key] !== undefined).map((key) => [
        key,
        value[key] === "metrics-left" &&
        (key === "services_style" ||
          Object.values(SERVICE_STYLE_FIELDS).includes(key))
          ? "metrics"
          : structuredClone(value[key]),
      ]),
    ),
    value,
  );
}
function stable(value: unknown): string {
  if (Array.isArray(value)) return "[" + value.map(stable).join(",") + "]";
  if (value && typeof value === "object")
    return (
      "{" +
      Object.keys(value)
        .sort()
        .map((key) => JSON.stringify(key) + ":" + stable(record(value)[key]))
        .join(",") +
      "}"
    );
  return JSON.stringify(value);
}
export function customServiceTheme(value: unknown) {
  return listThemes(value).find(
    (theme) => stable(theme.values) === stable(serviceThemeSnapshot(value)),
  );
}
export function listThemes(
  input: unknown,
): { id: string; name: string; values: Dict }[] {
  const value = record(input);
  return Array.isArray(value.service_custom_themes)
    ? (value.service_custom_themes as {
        id: string;
        name: string;
        values: Dict;
      }[])
    : [];
}

export function randomServiceTheme(
  input: unknown,
  random = Math.random,
): { source: "builtin" | "custom" | "mixed"; label: string; values: Dict } {
  const p = record(input);
  const pick = <T>(items: T[]): T => items[Math.floor(random() * items.length)];
  const current = currentServiceTheme(p),
    selectedCustom = customServiceTheme(p);
  const builtins = SERVICE_THEMES.filter(
    (theme) =>
      theme.id !== current &&
      !(
        theme.id.includes("radial-vertical") &&
        overviewPolicy(p, "services").cards[0] === "service-wireguard"
      ),
  );
  const custom = listThemes(p).filter(
    (theme) => theme.id !== selectedCustom?.id,
  );
  const sources: ("builtin" | "custom" | "mixed")[] = [
    ...(builtins.length ? ["builtin" as const] : []),
    ...(custom.length ? ["custom" as const] : []),
    "mixed",
  ];
  const source = pick(sources);
  if (source === "builtin") {
    const theme = pick(builtins);
    return {
      source,
      label: theme.label,
      values: structuredClone(theme.values),
    };
  }
  if (source === "custom") {
    const theme = pick(custom);
    return {
      source,
      label: theme.name,
      values: serviceThemeSnapshot(theme.values),
    };
  }
  const values: Dict = {
    services_layout: pick(["grid", "top", "sidebar"]),
    services_wireguard_style: "above",
  };
  for (const module of ["host", "xray", "warp", "wireguard"]) {
    const options =
      module === "wireguard"
        ? ["metrics", "radial"]
        : ["metrics", ...SERVICE_CIRCLE_STYLES];
    values[SERVICE_STYLE_FIELDS[module]] = pick(
      options.filter((style) =>
        serviceStyleAvailable({ ...p, ...values }, module, style),
      ),
    );
  }
  return { source, label: "随机混搭", values };
}

export function randomHomeServiceTheme(input: unknown, random = Math.random) {
  const theme = randomServiceTheme(input, random);
  const values: Dict = {
    ...theme.values,
    services_layout: ["grid", "top", "sidebar"][Math.floor(random() * 3)],
  };
  const current = { ...record(input), ...values };
  for (const module of ["host", "xray", "warp", "wireguard"]) {
    const key = SERVICE_STYLE_FIELDS[module],
      style = String(current[key] || serviceStyle(current, module));
    if (!serviceStyleAvailable(current, module, style)) {
      const options = (
        module === "wireguard"
          ? ["metrics", "radial"]
          : ["metrics", ...SERVICE_CIRCLE_STYLES]
      ).filter((s) => serviceStyleAvailable(current, module, s));
      values[key] = options[Math.floor(random() * options.length)];
    }
  }
  return { ...theme, values };
}
