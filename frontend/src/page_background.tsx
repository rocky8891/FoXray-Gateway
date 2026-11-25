import { useEffect, useState } from "react";
import { ErrorLine, Field, PageHeader, Panel } from "./ui";
import { SaveCard } from "./settings_save";
import type { BackgroundFiles } from "./background_codec";

const storageKey = "foxray.page-background";
const presets = {
  gradient: "深色背景-渐变",
  grid: "深色背景-网格",
  tide: "深色背景-青潮",
  orbit: "深色背景-紫轨",
};
type Preset = keyof typeof presets;
type Mode = Preset | "custom";
const presetUrl = (name: Preset, preview = false) => `/backgrounds/${name}${preview ? "-preview" : ""}.webp`;
const isLegacy = (value: string) => /^data:image\/webp;base64,[A-Za-z0-9+/]+={0,2}$/.test(value);

function loadChoice(): { mode: Mode; legacy?: string } {
  try {
    const value = localStorage.getItem(storageKey) ?? "gradient";
    if (value === "custom" || Object.hasOwn(presets, value)) return { mode: value as Mode };
    if (isLegacy(value)) return { mode: "custom", legacy: value };
  } catch { /* Use the default when browser storage is unavailable. */ }
  return { mode: "gradient" };
}

function customBackground(files?: BackgroundFiles) {
  return new Promise<BackgroundFiles | undefined>((resolve, reject) => {
    const request = indexedDB.open("foxray-appearance", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("background");
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const db = request.result;
      const transaction = db.transaction("background", files ? "readwrite" : "readonly");
      const store = transaction.objectStore("background");
      const operation = files ? store.put(files, "custom") : store.get("custom");
      transaction.oncomplete = () => { db.close(); resolve(files ?? operation.result); };
      transaction.onabort = () => { db.close(); reject(transaction.error); };
    };
  });
}

let generation = 0;
let activeUrls: string[] = [];
function setBackground(url: string, urls: string[] = []) {
  document.documentElement.style.setProperty("--page-background-image", `url("${url}")`);
  for (const old of activeUrls) if (!urls.includes(old)) URL.revokeObjectURL(old);
  activeUrls = urls;
}

function afterFirstPaint(load: () => void) {
  const schedule = () => requestAnimationFrame(() => requestAnimationFrame(() => {
    if ("requestIdleCallback" in window) window.requestIdleCallback(load, { timeout: 1500 });
    else setTimeout(load, 0);
  }));
  if (document.readyState === "complete") schedule();
  else window.addEventListener("load", schedule, { once: true });
}

export function applyPageBackground(choice = loadChoice(), uploaded?: BackgroundFiles) {
  const current = ++generation;
  const preset = choice.mode === "custom" ? "gradient" : choice.mode;
  setBackground(presetUrl(preset, true));
  afterFirstPaint(() => void (async () => {
    if (current !== generation) return;
    let full = presetUrl(preset);
    let preview: string | undefined;
    const urls: string[] = [];
    if (choice.mode === "custom") {
      const stored = uploaded ?? (choice.legacy ? undefined : await customBackground().catch(() => undefined));
      if (current !== generation) return;
      if (stored) {
        preview = URL.createObjectURL(stored.preview);
        full = URL.createObjectURL(stored.image);
        urls.push(preview, full);
        setBackground(preview, [preview]);
      } else if (choice.legacy) full = choice.legacy;
    }
    try {
      const image = new Image();
      image.fetchPriority = "low";
      image.src = full;
      await image.decode();
      if (current === generation) {
        setBackground(full, urls.filter(url => url === full));
        return;
      }
    } catch { /* Keep showing the preview if the detailed image is unavailable. */ }
    for (const url of urls) if (url !== preview || current !== generation) URL.revokeObjectURL(url);
  })());
}

function convert(file: File): Promise<BackgroundFiles> {
  const worker = new Worker(new URL("./background_worker.ts", import.meta.url), { type: "module" });
  return new Promise((resolve, reject) => {
    worker.onmessage = (event: MessageEvent<{ files?: BackgroundFiles; error?: string }>) => {
      worker.terminate();
      if (event.data.files) resolve(event.data.files);
      else reject(new Error(event.data.error || "无法转换图片"));
    };
    worker.onerror = () => { worker.terminate(); reject(new Error("无法启动图片转换")); };
    worker.postMessage(file);
  });
}

function PageBackgroundIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 1024 1024" fill="currentColor" aria-hidden="true">
      <circle cx="292.256" cy="356.256" r="64" />
      <path d="M874.688 148.064H149.344a64 64 0 0 0-64 64V812.16a64 64 0 0 0 64 64h725.344a64 64 0 0 0 64-64V211.84a64 64 0 0 0-64-64v0.224zM149.344 208.416h725.344c1.792 0.128 3.2 1.6 3.2 3.424v413.856l-113.728-141.632a81.056 81.056 0 0 0-122.432-4.48l-157.44 168.32a42.656 42.656 0 0 1-55.68 6.4l-100.064-67.84a60.16 60.16 0 0 0-64-1.504l-118.816 71.68V211.84c0-1.952 1.632-3.52 3.616-3.424z" />
    </svg>
  );
}

export function PageBackgroundSettings() {
  const [saved, setSaved] = useState(loadChoice);
  const [mode, setMode] = useState(saved.mode);
  const [file, setFile] = useState<BackgroundFiles>();
  const [savedPreview, setSavedPreview] = useState<Blob>();
  const [preview, setPreview] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let disposed = false;
    void customBackground().then(value => {
      if (!disposed) setSavedPreview(value?.preview);
    }).catch(() => undefined);
    return () => { disposed = true; };
  }, [saved]);
  useEffect(() => {
    const blob = file?.preview ?? savedPreview;
    if (mode !== "custom" || !blob) { setPreview(""); return; }
    const url = URL.createObjectURL(blob);
    setPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [file, savedPreview, mode]);
  async function selectFile(image: File) {
    setError("");
    if (!/^(image\/(jpeg|png|webp))$/.test(image.type) || image.size > 20 * 1024 * 1024) {
      setError("请选择不超过 20 MB 的 JPG、PNG 或 WebP 图片");
      return;
    }
    setBusy(true);
    try { setFile(await convert(image)); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "无法转换图片"); }
    finally { setBusy(false); }
  }
  const imageSrc = mode === "custom" ? preview || saved.legacy : presetUrl(mode, true);
  return (
    <Panel className="presentation-settings page-background-settings">
      <PageHeader title="页面背景" icon={<PageBackgroundIcon />}
        description="背景应用于当前浏览器的所有页面。" />
      <div className="settings-section page-background-options">
        <div>
          <Field label="背景来源">
            <select value={mode} disabled={busy} onChange={event => {
              setError("");
              setMode(event.target.value as Mode);
            }}>
              {Object.entries(presets).map(([key, name]) => <option key={key} value={key}>{name}</option>)}
              <option value="custom">自定义图片</option>
            </select>
          </Field>
          {mode === "custom" && (
            <Field label="选择背景图片" hint="JPG、8 位 PNG、WebP · 最大 20 MB">
              <input type="file" accept="image/jpeg,image/png,image/webp" disabled={busy} onChange={event => {
                const image = event.currentTarget.files?.[0];
                event.currentTarget.value = "";
                if (image) void selectFile(image);
              }} />
            </Field>
          )}
          {busy && <p role="status">正在生成无损背景…</p>}
          <ErrorLine error={error} />
        </div>
        {imageSrc && <img className="page-background-preview" src={imageSrc} alt="页面背景预览" />}
      </div>
      <SaveCard title="页面背景" dirty={mode !== saved.mode || (mode === "custom" && !!file)}
        disabled={busy || (mode === "custom" && !file && !savedPreview && saved.mode !== "custom")}
        localActionsEnabled={!busy}
        defaults={() => { setMode("gradient"); setFile(undefined); setError(""); }}
        discard={() => { setMode(saved.mode); setFile(undefined); setError(""); }}
        save={async () => {
          if (mode === saved.mode && (mode !== "custom" || !file)) return true;
          try {
            if (mode === "custom" && file) await customBackground(file);
            if (mode === "gradient") localStorage.removeItem(storageKey);
            else localStorage.setItem(storageKey, mode);
            const choice = { mode };
            applyPageBackground(choice, mode === "custom" ? file : undefined);
            setSaved(choice);
            if (mode === "custom" && file) setSavedPreview(file.preview);
            setFile(undefined);
            setError("");
            return true;
          } catch {
            setError("浏览器无法保存背景图片，请检查存储空间");
            return false;
          }
        }} />
    </Panel>
  );
}
