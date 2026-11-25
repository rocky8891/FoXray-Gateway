import { createContext, type SVGProps } from "react";
import emblem from "../public/foxray-icon.svg?raw";

export const HomeHeaderContext = createContext({
  hidden: false,
  expanded: false,
  collapseInNav: false,
  toggle: (_button: HTMLElement, _keyboard: boolean) => {},
});

// The checked-in SVG is bundled so each navigation surface can paint the logo immediately.
const emblemMarkup = emblem.slice(emblem.indexOf("<g"), emblem.lastIndexOf("</svg>"));

export function HomeHeaderToggleIcon(props: SVGProps<SVGSVGElement>) {
  return <svg viewBox="105 83 292 349" aria-hidden="true" {...props} dangerouslySetInnerHTML={{ __html: emblemMarkup }} />;
}
