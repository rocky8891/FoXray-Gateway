import { record, type Dict } from "./api";
export type OverviewView = "services" | "dashboard" | "nodes";
export const SERVICE_CARDS = [
  "service-host",
  "service-xray",
  "service-warp",
  "service-wireguard",
];
export const DEFAULT_SERVICE_THEME: Dict = {
  services_layout: "top",
  services_cards: SERVICE_CARDS,
  services_host_style: "radial",
  services_xray_style: "metrics",
  services_warp_style: "radial-diamond-compact",
  services_wireguard_layout: "metrics",
  services_wireguard_style: "above",
};
export const EXIT_CARDS = ["info", "traffic", "https", "connections"];
export function defaultOverviewView(value: unknown): OverviewView {
  const v = record(value).default_overview_view;
  return v === "dashboard" || v === "nodes" ? v : "services";
}
export function servicePreset(cards: unknown): cards is string[] {
  return (
    Array.isArray(cards) &&
    cards.length > 0 &&
    cards.every((v) => typeof v === "string" && v.startsWith("service-"))
  );
}
export function overviewPolicy(
  value: unknown,
  view: OverviewView,
): { cards: string[]; layout: string } {
  const p = record(value) as Dict,
    legacy = servicePreset(p.overview_cards);
  if (view === "nodes")
    return {
      cards: Array.isArray(p.nodes_order) ? (p.nodes_order as string[]) : [],
      layout: "grid",
    };
  return view === "services"
    ? {
        cards:
          (p.services_cards as string[] | undefined) ||
          (legacy ? (p.overview_cards as string[]) : SERVICE_CARDS),
        layout: String(
          p.services_layout ||
            (legacy
              ? p.overview_layout
              : DEFAULT_SERVICE_THEME.services_layout) ||
            "top",
        ),
      }
    : {
        cards:
          !legacy && Array.isArray(p.overview_cards)
            ? (p.overview_cards as string[])
            : EXIT_CARDS,
        layout: String(p.overview_layout || "grid"),
      };
}

export const SERVICE_STYLE_FIELDS: Record<string, string> = {
  host: "services_host_style",
  xray: "services_xray_style",
  warp: "services_warp_style",
  wireguard: "services_wireguard_layout",
};
export const SERVICE_METRICS = ["upload", "download", "latency", "connections"];
export function splitWireGuardCards(value: unknown): boolean {
  const { cards } = overviewPolicy(value, "services");
  return cards.includes("service-wireguard") &&
    (Number(record(value).services_wireguard_count) === 1 ||
      SERVICE_CARDS.some(card => !cards.includes(card)));
}

export function serviceStyle(value: unknown, module: string): string {
  const p = record(value);
  // Older versions saved the circle choice independently of the global style.
  if (
    module === "wireguard" &&
    !p.services_wireguard_layout &&
    ["classic", "corner", "corner-left", "above"].includes(
      String(p.services_wireguard_style),
    )
  )
    return "radial";

  const style = String(
    p[SERVICE_STYLE_FIELDS[module]] ||
      p.services_style ||
      DEFAULT_SERVICE_THEME[SERVICE_STYLE_FIELDS[module]] ||
      "metrics",
  );
  return serviceStyleAvailable(p, module, style)
    ? style
    : style.startsWith("radial")
      ? "radial"
      : "metrics";
}

export const SERVICE_CIRCLE_STYLES = [
  "radial",
  "radial-square",
  "radial-diamond",
  "radial-diamond-compact",
  "radial-wave",
  "radial-lightning",
  "radial-reverse-lightning",
  "radial-mixed",
  "radial-vertical",
];
// Wide, shallow cards use the horizontal default; the other shapes belong
// to balanced cards and the tall anchor. Stored preferences remain intact.
export function serviceStyleAvailable(
  value: unknown,
  module: string,
  style: string,
): boolean {
  const { layout, cards } = overviewPolicy(value, "services");
  const anchor = cards[0] === "service-" + module;
  if (module === "wireguard")
    return splitWireGuardCards(value)
      ? style === "metrics" || SERVICE_CIRCLE_STYLES.includes(style)
      : ["metrics", "radial"].includes(style);
  if (style === "radial-vertical") return layout === "sidebar" && anchor;
  if (style === "metrics" || style === "radial" || style === "radial-lightning" || style === "radial-reverse-lightning") return true;
  if (!style.startsWith("radial")) return false;
  if (layout === "sidebar" && !anchor) return false;
  if (
    layout === "top" &&
    anchor &&
    ["radial-square", "radial-diamond", "radial-diamond-compact"].includes(
      style,
    )
  )
    return false;
  return SERVICE_CIRCLE_STYLES.includes(style);
}
export function wireguardNameStyle(value: unknown): string {
  void value;
  return "above";
}
export function normalizeServiceLayoutChange(p: Dict, layout: string): Dict {
  const patch: Dict = { services_layout: layout };
  if (layout !== "sidebar")
    for (const module of ["host", "xray", "warp"]) {
      const key = SERVICE_STYLE_FIELDS[module];
      if (p[key] === "radial-vertical") patch[key] = "radial";
    }
  return patch;
}
