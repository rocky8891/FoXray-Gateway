import { useState } from "react";

// Module memory survives navigation, while a browser reload restores the page default.
const views = new Map<string, string>();
export function useView(key: string, initial = "grid") {
  const [view, update] = useState(() => views.get(key) || initial);
  return [
    view,
    (value: string) => {
      views.set(key, value);
      update(value);
    },
  ] as const;
}
