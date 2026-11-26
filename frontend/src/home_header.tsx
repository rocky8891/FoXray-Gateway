import { createContext, type SVGProps } from "react";
import emblem from "../public/foxray-icon.svg?raw";

const homeMotionKey = "foxray.home.motion";

export function homeMotionEnabled() {
  return document.documentElement.dataset.homeMotion === "on";
}

export function setHomeMotionEnabled(enabled: boolean) {
  if (enabled) {
    localStorage.setItem(homeMotionKey, "on");
    document.documentElement.dataset.homeMotion = "on";
  } else {
    localStorage.removeItem(homeMotionKey);
    delete document.documentElement.dataset.homeMotion;
  }
}

export function reduceHomeMotion() {
  return !homeMotionEnabled() && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

if (localStorage.getItem(homeMotionKey) === "on") document.documentElement.dataset.homeMotion = "on";

export const HomeHeaderContext = createContext({
  hidden: false,
  expanded: false,
  collapseInNav: false,
  closing: false,
  toggle: (_button: HTMLElement, _keyboard: boolean) => {},
});

// The checked-in SVG is bundled so each navigation surface can paint the logo immediately.
const emblemMarkup = emblem.slice(emblem.indexOf("<g"), emblem.lastIndexOf("</svg>"));

export function HomeHeaderToggleIcon(props: SVGProps<SVGSVGElement>) {
  return <svg viewBox="105 83 292 349" aria-hidden="true" {...props} dangerouslySetInnerHTML={{ __html: emblemMarkup }} />;
}
