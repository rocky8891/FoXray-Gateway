import { useCallback, useEffect, useState } from "react";
import type { Operation, Task } from "./api";

const storageKey = "foxray.results.read.v1";
const eventName = "foxray:results-read";
let memory = new Set<string>();
function load() {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) || "[]");
    if (Array.isArray(saved)) memory = new Set(saved.filter(v => typeof v === "string"));
  } catch { /* Keep the in-memory state when storage is unavailable. */ }
  return new Set(memory);
}
export const taskReadKeys = (task: Pick<Task, "id" | "log_id">) =>
  ["task:" + task.id, ...(task.log_id ? ["log:" + task.log_id] : [])];
export const logReadKeys = (row: Pick<Operation, "id" | "details">) =>
  ["log:" + row.id, ...(row.details?.job_id ? ["task:" + String(row.details.job_id)] : [])];
export function useReadState() {
  const [read, setRead] = useState(load);
  useEffect(() => {
    const sync = () => setRead(load());
    window.addEventListener(eventName, sync);
    window.addEventListener("storage", sync);
    return () => { window.removeEventListener(eventName, sync); window.removeEventListener("storage", sync); };
  }, []);
  const markRead = useCallback((keys: string[]) => {
    if (!keys.length) return;
    const next = load();
    keys.forEach(key => next.add(key));
    memory = new Set([...next].slice(-10000));
    try { localStorage.setItem(storageKey, JSON.stringify([...memory])); } catch { /* Session-only fallback. */ }
    setRead(new Set(memory));
    window.dispatchEvent(new Event(eventName));
  }, []);
  return { isRead: (keys: string[]) => keys.some(key => read.has(key)), markRead };
}
export function ReadBadge({ read }: { read: boolean }) {
  return <span className={"read-badge " + (read ? "is-read" : "is-unread")}>
    {!read && <span aria-hidden="true">●</span>}{read ? "已读" : "未读"}
  </span>;
}
