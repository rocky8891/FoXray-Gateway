import { SettingInfo } from "./ui";
import { createPortal } from "react-dom";
import { SlidersHorizontal } from "lucide-react";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useCallback,
  useId,
  type ReactNode,
} from "react";
import { Button } from "./ui";
type Entry = {
  title: string;
  save: () => Promise<boolean>;
  node: HTMLElement | null;
  discard?: () => void;
  defaults?: () => void;
  disabled?: boolean;
};
const Registry = createContext<
  ((key: string, entry: Entry | null) => void) | null
>(null);
export function SettingsSaveProvider({
  children,
  expected = 0,
}: {
  children: ReactNode;
  expected?: number;
}) {
  const entries = useRef(new Map<string, Entry>()),
    [count, setCount] = useState(0),
    [blockedCount, setBlockedCount] = useState(0),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState("");
  const register = useCallback((key: string, entry: Entry | null) => {
    if (entry) entries.current.set(key, entry);
    else entries.current.delete(key);
    setCount(entries.current.size);
    setBlockedCount(
      [...entries.current.values()].filter((item) => item.disabled).length,
    );
  }, []);
  return (
    <Registry.Provider value={register}>
      {children}
      {count > 1 && (
        <div className="save-all-footer">
          <div>
            <h2 className="metric-heading tone-green">
              <SlidersHorizontal size={18} />
              配置与同步
              <SettingInfo
                label="配置与同步"
                text="在此统一保存本页配置；恢复默认与放弃保存仅修改草稿。"
              />
            </h2>
            {message && (
              <p className="small muted" role="status">
                {message}
              </p>
            )}
          </div>
          <Button
            disabled={busy}
            onClick={() => {
              for (const item of entries.current.values()) item.defaults?.();
              setMessage("已恢复本页默认草稿；保存并应用后生效");
            }}
          >
            全部恢复默认
          </Button>
          <Button
            disabled={busy}
            onClick={() => {
              for (const item of entries.current.values()) item.discard?.();
              setMessage("已放弃未保存修改");
            }}
          >
            放弃保存
          </Button>
          <Button
            variant="primary"
            disabled={busy || count < expected || blockedCount > 0}
            onClick={() => {
              setBusy(true);
              void (async () => {
                try {
                  for (const item of [...entries.current.values()].sort(
                    (a, b) =>
                      a.node && b.node
                        ? a.node.compareDocumentPosition(b.node) &
                          Node.DOCUMENT_POSITION_FOLLOWING
                          ? -1
                          : 1
                        : 0,
                  )) {
                    setMessage("正在提交：" + item.title);
                    if (!(await item.save())) {
                      setMessage("未完成：" + item.title + "；已停止后续提交");
                      return;
                    }
                  }
                  setMessage("全部配置已提交；异步应用状态见当前任务");
                } catch {
                  setMessage("提交失败，请查看操作提示");
                } finally {
                  setBusy(false);
                }
              })();
            }}
          >
            全部保存并应用
          </Button>
        </div>
      )}
    </Registry.Provider>
  );
}
export function SaveCard({
  title,
  save,
  disabled = false,
  defaults,
  discard,
  duplicateTarget,
  localActionsEnabled = false,
  saveLabel = "保存并应用",
  defaultsLabel = "恢复默认",
}: {
  title: string;
  duplicateTarget?: HTMLElement | null;
  localActionsEnabled?: boolean;
  saveLabel?: string;
  defaultsLabel?: string;
  save: () => Promise<boolean>;
  disabled?: boolean;
  defaults?: () => void;
  discard?: () => void;
}) {
  const element = useRef<HTMLDivElement>(null);
  const register = useContext(Registry),
    id = useId(),
    latest = useRef(save),
    blocked = useRef(disabled),
    [busy, setBusy] = useState(false);
  const reset = useRef(discard);
  const defaultRef = useRef(defaults);
  defaultRef.current = defaults;
  reset.current = discard;
  latest.current = save;
  blocked.current = disabled;
  const inFlight = useRef(false);
  const perform = useCallback(async () => {
    if (blocked.current || inFlight.current) return false;
    inFlight.current = true;
    setBusy(true);
    try {
      return await latest.current();
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }, []);
  useEffect(() => {
    register?.(id, {
      title,
      save: perform,
      node: element.current,
      discard: () => reset.current?.(),
      defaults: () => defaultRef.current?.(),
      disabled: disabled || busy,
    });
    return () => register?.(id, null);
  }, [register, id, title, perform, disabled, busy]);
  const controls = (duplicate = false) => (
    <div className="save-card-footer" ref={duplicate ? undefined : element}>
      <Button
        disabled={(disabled && !localActionsEnabled) || busy || !defaults}
        onClick={() => defaults?.()}
      >
        {defaultsLabel}
      </Button>
      <Button
        disabled={(disabled && !localActionsEnabled) || busy || !discard}
        onClick={() => discard?.()}
      >
        放弃保存
      </Button>
      <Button
        variant="primary"
        disabled={disabled || busy}
        onClick={() => void perform()}
      >
        {saveLabel}
      </Button>
    </div>
  );
  return (
    <>
      {controls()}
      {duplicateTarget && createPortal(controls(true), duplicateTarget)}
    </>
  );
}
