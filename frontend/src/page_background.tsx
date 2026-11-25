import { useState } from "react";
import { ErrorLine, Field, PageHeader, Panel } from "./ui";
import { SaveCard } from "./settings_save";

const storageKey = "foxray.page-background";
const backgrounds = {
  default: "/background-20261121-v2.webp",
  grid: "/background-grid.webp",
};
const isCustom = (value: string) => /^data:image\/webp;base64,[A-Za-z0-9+/]+={0,2}$/.test(value);
const imageSource = (value: string) => value === "grid" ? backgrounds.grid : isCustom(value) ? value : backgrounds.default;

function loadBackground(): string {
  try {
    const value = localStorage.getItem(storageKey) ?? "default";
    return value === "grid" || isCustom(value) ? value : "default";
  } catch {
    return "default";
  }
}

export function applyPageBackground(value = loadBackground()) {
  document.documentElement.style.setProperty("--page-background-image", `url("${imageSource(value)}")`);
}

async function encodeBackground(file: File) {
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    const scale = Math.min(1, 2560 / Math.max(image.naturalWidth, image.naturalHeight));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("无法处理此图片");
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    for (const quality of [0.87, 0.74, 0.58]) {
      const data = canvas.toDataURL("image/webp", quality);
      if (isCustom(data) && data.length <= 2_000_000) return data;
    }
    throw new Error("图片过大，请换一张图片");
  } finally {
    URL.revokeObjectURL(url);
  }
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
  const [saved, setSaved] = useState(loadBackground);
  const [draft, setDraft] = useState(saved);
  const [custom, setCustom] = useState(isCustom(saved) ? saved : "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const mode = isCustom(draft) ? "custom" : draft;
  async function selectFile(file: File) {
    setError("");
    if (!/^(image\/(jpeg|png|webp))$/.test(file.type) || file.size > 20 * 1024 * 1024) {
      setError("请选择不超过 20 MB 的 JPG、PNG 或 WebP 图片");
      return;
    }
    setBusy(true);
    try {
      const data = await encodeBackground(file);
      setCustom(data);
      setDraft(data);
    } catch {
      setError("无法读取或压缩此图片，请换一张图片");
    } finally {
      setBusy(false);
    }
  }
  return (
    <Panel className="presentation-settings page-background-settings">
      <PageHeader title="页面背景" icon={<PageBackgroundIcon />}
        description="背景应用于当前浏览器的所有页面。" />
      <div className="settings-section page-background-options">
        <div>
          <Field label="背景来源">
            <select value={mode} disabled={busy} onChange={event => {
              setError("");
              setDraft(event.target.value === "custom" ? custom || "custom" : event.target.value);
            }}>
              <option value="default">默认背景</option>
              <option value="grid">内置 · 深色网格</option>
              <option value="custom">自定义图片</option>
            </select>
          </Field>
          {mode === "custom" && (
            <Field label="选择背景图片" hint="JPG、PNG、WebP · 最大 20 MB">
              <input type="file" accept="image/jpeg,image/png,image/webp" disabled={busy} onChange={event => {
                const file = event.currentTarget.files?.[0];
                event.currentTarget.value = "";
                if (file) void selectFile(file);
              }} />
            </Field>
          )}
          <ErrorLine error={error} />
        </div>
        {draft !== "custom" && <img className="page-background-preview" src={imageSource(draft)} alt="页面背景预览" />}
      </div>
      <SaveCard title="页面背景" dirty={draft !== saved} disabled={busy || draft === "custom"}
        localActionsEnabled={!busy}
        defaults={() => { setDraft("default"); setError(""); }}
        discard={() => { setDraft(saved); setCustom(isCustom(saved) ? saved : ""); setError(""); }}
        save={async () => {
          try {
            if (draft === "default") localStorage.removeItem(storageKey);
            else localStorage.setItem(storageKey, draft);
            applyPageBackground(draft);
            setSaved(draft);
            setError("");
            return true;
          } catch {
            setError("浏览器存储空间不足，无法保存背景图片");
            return false;
          }
        }} />
    </Panel>
  );
}
