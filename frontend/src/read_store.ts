const storageKey = "foxray.results.read.v1";
export const readEventName = "foxray:results-read";
let memory = new Set<string>();

export function loadReadState() {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) || "[]");
    if (Array.isArray(saved)) memory = new Set(saved.filter(v => typeof v === "string"));
  } catch { /* Keep the in-memory state when storage is unavailable. */ }
  return new Set(memory);
}

export function markResultsRead(keys: string[]) {
  if (!keys.length) return;
  const next = loadReadState();
  keys.forEach(key => next.add(key));
  memory = new Set([...next].slice(-10000));
  try { localStorage.setItem(storageKey, JSON.stringify([...memory])); } catch { /* Session-only fallback. */ }
  window.dispatchEvent(new Event(readEventName));
}
