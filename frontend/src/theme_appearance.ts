import { useState, useEffect } from "react";
import { record, type Dict } from "./api";
export const APPEARANCE_KEYS = [
  "overview_surfaces",
  "services_wireguard_ring_color",
  "services_wireguard_ring_opacity",
  "services_wireguard_ring_shadow",
];
export function appearanceTheme() {
  return document.documentElement.dataset.theme === "light" ? "light" : "dark";
}
export function themedAppearance(
  raw: unknown,
  theme = appearanceTheme(),
): Dict {
  const p = record(raw);
  if (p.__appearance_resolved) return p;
  const scheme = record(record(p.overview_color_schemes)[theme]);
  const base = { ...p };
  for (const key of APPEARANCE_KEYS) {
    delete base[key];
    if (p[key] !== undefined) base[key] = p[key];
  }
  return { ...base, ...scheme, __appearance_resolved: true };
}
export function appearancePatch(
  input: Dict,
  latest: Dict,
  theme = appearanceTheme(),
): Dict {
  const patch = { ...input },
    colors: Dict = {};
  for (const key of APPEARANCE_KEYS)
    if (key in patch) {
      colors[key] = patch[key];
      delete patch[key];
    }
  if (Object.keys(colors).length)
    patch.overview_color_schemes = {
      ...record(latest.overview_color_schemes),
      [theme]: {
        ...Object.fromEntries(
          APPEARANCE_KEYS.filter(
            (k) => k in themedAppearance(latest, theme),
          ).map((k) => [k, themedAppearance(latest, theme)[k]]),
        ),
        ...colors,
      },
    };
  return patch;
}

export function useAppearanceTheme() {
  const [t, set] = useState(appearanceTheme);
  useEffect(() => {
    const o = new MutationObserver(() => set(appearanceTheme()));
    o.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => o.disconnect();
  }, []);
  return t;
}
