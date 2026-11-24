import { createContext } from "react";

export const HomeHeaderContext = createContext({
  hidden: false,
  expanded: false,
  collapseInNav: false,
  toggle: (_button: HTMLElement, _keyboard: boolean) => {},
});

export function HomeHeaderToggleIcon() {
  return <svg viewBox="105 83 292 349" aria-hidden="true">
    <image href="/foxray-icon.svg?v=0.9.4" width="512" height="512" />
  </svg>;
}
