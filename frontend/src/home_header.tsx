import { createContext } from "react";

export const HomeHeaderContext = createContext({
  hidden: false,
  expanded: false,
  toggle: (_button: HTMLElement) => {},
});
