import { ActionMenu, type MenuAction } from "./action_menu";
import { useTableColumnSizing } from "./table_column_sizing";
import { CopyButton, Pagination, usePagination } from "./resource_controls";
import { labelIcon } from "./ui_icons";
import {
  Children,
  Fragment,
  cloneElement,
  isValidElement,
  type ReactElement,
} from "react";
import { createPortal } from "react-dom";
import {
  useEffect,
  useLayoutEffect,
  useId,
  useState,
  useCallback,
  useRef,
  createContext,
  useContext,
  type ReactNode,
  type FormEvent,
  type InputHTMLAttributes,
  type HTMLAttributes,
} from "react";
import { X, AlertCircle, CircleDot, Info as InfoIcon } from "lucide-react";
import { api, errorMessage, type Dict } from "./api";
export const ChartRefreshContext = createContext(10000);
export const LiveRefreshContext = createContext(2000);
export function useChartRefresh() {
  return useContext(ChartRefreshContext);
}
function recordPolicyPaused(v: Dict) {
  const p = v?.policy as Dict | undefined;
  return p?.enabled === false;
}
const health = new Map<
  string,
  {
    at: number;
    fetchedAt?: number;
    error: string;
    interval: number;
    paused: boolean;
  }
>();
export function DataFreshness({
  statusRefresh = 5000,
}: {
  statusRefresh?: number;
}) {
  const [, tick] = useState(0);
  useEffect(() => {
    const timer = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(timer);
  }, []);
  const chartRefresh = useChartRefresh();
  const liveRefresh = useContext(LiveRefreshContext);
  const rows = [...health.entries()]
    .filter(([p]) =>
      /^\/api\/(state|system(?:$|\/history)|traffic(?:$|\/history)|latency-history)/.test(
        p,
      ),
    )
    .map(([, v]) => v);
  const failed = rows.some((r) => !!r.error),
    stale = rows.some(
      (r) => r.at > 0 && Date.now() - r.at > Math.max(r.interval * 3, 15000),
    ),
    paused = rows.some((r) => r.paused),
    ready = rows.length > 0 && rows.every((r) => r.at > 0);
  const time = rows.length
    ? Math.min(
        ...rows
          .filter((r) => Number(r.fetchedAt) > 0)
          .map((r) => Number(r.fetchedAt)),
      )
    : 0;
  const label = failed
    ? "数据获取失败"
    : stale
      ? "部分采样数据已过期"
      : paused
        ? "部分采集已暂停"
        : ready
          ? "数据获取正常"
          : "正在获取数据";
  return (
    <div
      className="data-freshness"
      title="后台采集产生数据，页面更新负责读取数据。最近获取表示请求成功时间；检测结果和采样时间仍以各自记录为准。"
    >
      页面更新：状态 {statusRefresh / 1000} 秒 · 实时指标 {liveRefresh / 1000}{" "}
      秒 · 历史图 {chartRefresh / 1000} 秒{" ｜ 最近获取："}
      {time && Number.isFinite(time)
        ? new Date(time).toLocaleTimeString("zh-CN", { hour12: false })
        : "—"}{" "}
      ｜{" "}
      <span className={failed || stale ? "poor" : paused ? "fair" : "accent"}>
        ● {label}
      </span>
    </div>
  );
}
// Read-only live snapshots are shared by cards mounted at the same time.
// Each caller still owns its subscription and ignores results after unmount.
const liveSnapshots = new Map<
  string,
  { at: number; promise: Promise<unknown> }
>();
function liveSnapshot<T>(path: string): Promise<T> {
  const cached = liveSnapshots.get(path);
  if (cached && Date.now() - cached.at < 750)
    return cached.promise as Promise<T>;
  const entry = {
    at: Number.POSITIVE_INFINITY,
    promise: Promise.resolve<unknown>(null),
  };
  entry.promise = api<T>(path).then(
    (value) => {
      entry.at = Date.now();
      return value;
    },
    (error) => {
      if (liveSnapshots.get(path) === entry) liveSnapshots.delete(path);
      throw error;
    },
  );
  liveSnapshots.set(path, entry);
  return entry.promise as Promise<T>;
}
export function useResource<T>(path: string, interval = 10000, enabled = true) {
  const chartRefresh = useChartRefresh();
  const liveRefresh = useContext(LiveRefreshContext);
  if (
    ["/api/traffic", "/api/system"].includes(path) ||
    /^\/api\/connections(?:\?|$)/.test(path)
  )
    interval = liveRefresh;
  if (
    /^\/api\/(?:system\/history|traffic\/history|connections\/history|latency-history)/.test(
      path,
    )
  )
    interval = chartRefresh;
  const [data, setData] = useState<T | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true);
  const previousScope = useRef("");
  const seq = useRef(0),
    pending = useRef<AbortController | null>(null);
  const refresh = useCallback(
    async (force = false) => {
      if (!enabled) return;
      if (pending.current && force !== true) return;
      pending.current?.abort();
      const controller = new AbortController();
      pending.current = controller;
      const current = ++seq.current;
      try {
        const next = await (["/api/traffic", "/api/system"].includes(path)
          ? liveSnapshot<T>(path)
          : api<T>(path, undefined, controller.signal));
        if (current === seq.current) {
          setData(next);
          setError("");
          const value = next as unknown as Dict;
          const current = (value.current || {}) as Dict;
          const sourceError =
            current.supported === false
              ? String(current.status || "采样不可用")
              : String(value.error || "");
          health.set(path, {
            at:
              current.time && !recordPolicyPaused(value)
                ? Math.min(Date.now(), Number(current.time) * 1000)
                : Date.now(),
            fetchedAt: Date.now(),
            error: sourceError,
            interval,
            paused: recordPolicyPaused(value),
          });
        }
      } catch (e) {
        if (current === seq.current && !controller.signal.aborted) {
          setError(errorMessage(e));
          const old = health.get(path);
          health.set(path, {
            at: old?.at || 0,
            error: errorMessage(e),
            interval,
            paused: false,
          });
        }
      } finally {
        if (pending.current === controller) pending.current = null;
        if (current === seq.current) setLoading(false);
      }
    },
    [path, interval, enabled],
  );
  useEffect(() => {
    if (!enabled) { setLoading(false); return; }
    const scope = path.replace(/([?&])(start|end)=[^&]*/g, "");
    if (previousScope.current !== scope) {
      setData(null);
      health.set(path, { at: 0, error: "", interval, paused: false });
    }
    previousScope.current = scope;
    setLoading(true);
    void refresh();
    const timer = setInterval(() => {
      if (!document.hidden) void refresh();
    }, interval);
    return () => {
      clearInterval(timer);
      health.delete(path);
      seq.current++;
      pending.current?.abort();
      pending.current = null;
    };
  }, [refresh, interval, enabled]);
  return {
    data,
    error,
    loading,
    refresh: useCallback(() => refresh(), [refresh]),
    refreshLatest: useCallback(() => refresh(true), [refresh]),
  };
}
export function SecondaryNav({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  const [target, setTarget] = useState<HTMLElement | null>(null);
  useEffect(
    () => setTarget(document.getElementById("secondary-navigation")),
    [],
  );
  return target
    ? createPortal(
        <nav className="secondary-nav" aria-label={label}>
          {children}
        </nav>,
        target,
      )
    : null;
}
export function Button({
  children,
  onClick,
  variant = "secondary",
  disabled = false,
  type = "button",
  title,
  size,
  iconOnly,
  ariaLabel,
  highlighted = false,
}: {
  children: ReactNode;
  onClick?: () => void;
  variant?: "primary" | "secondary" | "danger" | "ghost";
  disabled?: boolean;
  type?: "button" | "submit";
  title?: string;
  size?: "regular" | "compact";
  iconOnly?: boolean;
  ariaLabel?: string;
  highlighted?: boolean;
}) {
  const hasIcon = (value: ReactNode): boolean =>
    Children.toArray(value).some(
      (child) =>
        isValidElement<{ children?: ReactNode }>(child) &&
        (child.type === "svg" ||
          (typeof child.type !== "string" && child.type !== Fragment) ||
          hasIcon(child.props.children)),
    );
  const caption = (value: ReactNode): string =>
    Children.toArray(value)
      .map((child) =>
        typeof child === "string" || typeof child === "number"
          ? String(child)
          : isValidElement<{ children?: ReactNode }>(child)
            ? caption(child.props.children)
            : "",
      )
      .join("");
  const actionCaption = caption(children) || title || ariaLabel || "";
  const normalizedAction =
    /检测/.test(actionCaption) && !/取消/.test(actionCaption)
      ? "检测"
      : /编辑/.test(actionCaption)
        ? "编辑"
        : /^(开启.+|启用)/.test(actionCaption)
          ? "启用"
          : /^(关闭.+|停用)/.test(actionCaption)
            ? "停用"
            : "";
  const normalizeActionIcon = !!normalizedAction;
  return (
    <button
      className={
        "button " +
        (variant === "primary" && !highlighted ? "secondary" : variant)
      }
      data-action={actionCaption}
      data-highlighted={highlighted || undefined}
      data-control-size={size}
      data-control-icon={
        (iconOnly ?? !caption(children).trim()) ? "true" : undefined
      }
      type={type}
      disabled={disabled}
      onClick={onClick}
      title={title}
      aria-label={ariaLabel}
    >
      {normalizeActionIcon && labelIcon(normalizedAction, 14)}
      {!normalizeActionIcon &&
        !hasIcon(children) &&
        (labelIcon(caption(children), 14) || (
          <CircleDot size={14} aria-hidden="true" />
        ))}
      {normalizeActionIcon
        ? Children.toArray(children).filter(
            (child) =>
              !(
                isValidElement(child) &&
                child.type !== Fragment &&
                (child.type === "svg" || typeof child.type !== "string")
              ),
          )
        : children}
    </button>
  );
}
export function PageHeader({
  title,
  description,
  actions,
  titleExtra,
  titleControl,
  icon,
  tone,
}: {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
  titleExtra?: ReactNode;
  titleControl?: ReactNode;
  icon?: ReactNode;
  tone?: string;
}) {
  icon = icon || labelIcon(title, 20);
  const help = description ? (
    <SettingInfo label={title} text={description} />
  ) : null;
  const heading = titleControl ? (
    <div className="page-title-control">
      {titleControl}
      {help}
    </div>
  ) : (
    <h2
      aria-label={title}
      className={icon ? "metric-heading tone-" + (tone || "green") : undefined}
    >
      {icon}
      {title}
      {help}
    </h2>
  );
  return (
    <div className="page-heading">
      <div>
        {titleExtra ? (
          <div className="page-title-row">
            {heading}
            {titleExtra}
          </div>
        ) : (
          heading
        )}
      </div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  );
}
export function SettingsSection({
  title,
  info,
  children,
  className = "",
}: {
  title?: string;
  info?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={"settings-section " + className} aria-label={title}>
      {title && (
        <h3 aria-label={title} className="settings-section-title">
          {title}
          {info && <SettingInfo label={title} text={info} />}
        </h3>
      )}
      {children}
    </section>
  );
}

export function Panel({
  children,
  className = "",
  ...attributes
}: { children: ReactNode } & HTMLAttributes<HTMLElement>) {
  return <section className={"panel " + className} {...attributes}>{children}</section>;
}
export function Empty({ children = "暂无记录" }: { children?: ReactNode }) {
  return <div className="empty">{children}</div>;
}
const DetailsCopyContext = createContext(false);
export function Modal({
  title,
  description,
  children,
  onClose,
  wide = false,
  titleControl,
  className = "",
  copyText,
}: {
  className?: string;
  copyText?: string;
  title: string;
  description?: ReactNode;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
  titleControl?: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null),
    close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const d = ref.current;
    d?.querySelector<HTMLButtonElement>(
      ".modal-heading > button",
    )?.setAttribute("autofocus", "");
    d?.showModal();
    const handler = (e: Event) => {
      e.preventDefault();
      close.current();
    };
    d?.addEventListener("cancel", handler);
    return () => {
      d?.removeEventListener("cancel", handler);
      d?.close();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      aria-label={title}
      className={"modal " + (wide ? "wide " : "") + className}
    >
      <div className="modal-heading">
        <h2 aria-label={title}>
          {titleControl || title}
          {description && <SettingInfo label={title} text={description} />}
        </h2>
        <Button variant="ghost" size="compact" onClick={onClose} title="关闭">
          <X size={20} />
        </Button>
      </div>
      <div className="modal-body">
        {copyText !== undefined && (
          <div className="modal-detail-tools">
            <CopyButton text={copyText} />
          </div>
        )}
        <DetailsCopyContext.Provider value={copyText !== undefined}>
          {children}
        </DetailsCopyContext.Provider>
      </div>
    </dialog>
  );
}
export function Confirm({
  title,
  message,
  onConfirm,
  onClose,
  danger = false,
  ack,
}: {
  title: string;
  message: ReactNode;
  onConfirm: () => void;
  onClose: () => void;
  danger?: boolean;
  ack?: string;
}) {
  const [checked, setChecked] = useState(false);
  return (
    <Modal title={title} onClose={onClose}>
      <div className="confirm-message">{message}</div>
      {ack && (
        <label className="check">
          <input
            type="checkbox"
            checked={checked}
            onChange={(e) => setChecked(e.target.checked)}
          />
          {ack}
        </label>
      )}
      <div className="modal-actions">
        <Button onClick={onClose}>取消</Button>
        <Button
          variant={danger ? "danger" : "primary"}
          disabled={!!ack && !checked}
          onClick={onConfirm}
        >
          确认执行
        </Button>
      </div>
    </Modal>
  );
}
export function SettingInfo({
  label,
  text,
}: {
  label: string;
  text: ReactNode;
}) {
  const trigger = useRef<HTMLSpanElement>(null),
    tooltip = useRef<HTMLDivElement>(null),
    hide = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [open, setOpen] = useState(false),
    [position, setPosition] = useState({ top: 0, left: 0 });
  const id = useId();
  const cancelHide = () => {
    if (hide.current) clearTimeout(hide.current);
    hide.current = null;
  };
  const show = () => {
    cancelHide();
    setOpen(true);
  };
  const scheduleHide = () => {
    cancelHide();
    hide.current = setTimeout(() => {
      if (
        !trigger.current?.contains(document.activeElement) &&
        !tooltip.current?.contains(document.activeElement)
      )
        setOpen(false);
    }, 140);
  };
  useEffect(() => () => cancelHide(), []);
  useLayoutEffect(() => {
    if (!open) return;
    const align = () => {
      const r = trigger.current?.getBoundingClientRect(),
        t = tooltip.current;
      if (!r || !t) return;
      if (r.bottom < 0 || r.top > innerHeight) {
        setOpen(false);
        return;
      }
      const right = document.documentElement.getBoundingClientRect().right,
        w = t.getBoundingClientRect().width,
        h = t.getBoundingClientRect().height;
      let top = r.bottom + 8;
      if (top + h > innerHeight - 12 && r.top > h + 12) top = r.top - h - 8;
      setPosition({
        left: Math.max(12, Math.min(r.left, right - w - 12)),
        top: Math.max(12, Math.min(top, innerHeight - h - 12)),
      });
    };
    const outside = (e: PointerEvent) => {
      if (
        !trigger.current?.contains(e.target as Node) &&
        !tooltip.current?.contains(e.target as Node)
      )
        setOpen(false);
    };
    align();
    addEventListener("resize", align);
    addEventListener("scroll", align, true);
    document.addEventListener("pointerdown", outside);
    return () => {
      removeEventListener("resize", align);
      removeEventListener("scroll", align, true);
      document.removeEventListener("pointerdown", outside);
    };
  }, [open, text]);
  return (
    <span
      className="setting-info"
      ref={trigger}
      tabIndex={0}
      role="button"
      aria-label={label + "说明"}
      aria-expanded={open}
      aria-describedby={open ? id : undefined}
      onMouseEnter={show}
      onMouseLeave={scheduleHide}
      onFocus={show}
      onBlur={scheduleHide}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        show();
        e.currentTarget.focus();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          setOpen(false);
          e.currentTarget.blur();
        }
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          show();
        }
      }}
    >
      <InfoIcon size={14} />
      {open &&
        createPortal(
          <div
            ref={tooltip}
            id={id}
            role="tooltip"
            className="setting-tooltip"
            style={position}
            onMouseEnter={cancelHide}
            onMouseLeave={scheduleHide}
          >
            {text}
          </div>,
          trigger.current?.closest("dialog[open]") || document.body,
        )}
    </span>
  );
}

export function Field({
  label,
  icon,
  titleExtra,
  children,
  hint,
  info,
  error,
  validateNumber = false,
}: {
  label: string;
  icon?: ReactNode;
  titleExtra?: ReactNode;
  children: ReactNode;
  hint?: string;
  info?: ReactNode;
  error?: string;
  validateNumber?: boolean;
}) {
  const errorId = useId();
  const field = useRef<HTMLLabelElement>(null);
  useLayoutEffect(() => {
    const root = field.current;
    const title = root?.querySelector<HTMLElement>(":scope > .field-title");
    if (!root || !title) return;
    const control = root.querySelector<HTMLElement>(
      ".icon-select-trigger,input:not([type=checkbox]):not([type=radio]):not([type=hidden]),select,textarea",
    );
    const align = () => {
      const scale = Number(
        root.closest<HTMLElement>("[data-preview-scale]")?.dataset
          .previewScale || 1,
      );
      const caption = control?.matches(".icon-select-trigger")
        ? control.querySelector<HTMLElement>(
            ":scope > span:not(.icon-select-symbol)",
          )
        : null;
      const style = control && getComputedStyle(control);
      const left = caption
        ? caption.getBoundingClientRect().left
        : control
          ? control.getBoundingClientRect().left +
            (parseFloat(style!.borderLeftWidth) +
              parseFloat(style!.paddingLeft)) *
              scale
          : title.getBoundingClientRect().left + 13 * scale;
      root.style.setProperty(
        "--field-title-inset",
        Math.max(0, (left - title.getBoundingClientRect().left) / scale) + "px",
      );
    };
    align();
    const observer = new ResizeObserver(align);
    observer.observe(root);
    if (control) observer.observe(control);
    return () => observer.disconnect();
  }, [children]);
  const [editing, setEditing] = useState(false);
  const input = Children.toArray(children).find((child) =>
    isValidElement(child),
  ) as
    | ReactElement<{
        value?: unknown;
        min?: number;
        max?: number;
        step?: number | string;
        "aria-describedby"?: string;
      }>
    | undefined;
  let message = error || "";
  if (validateNumber && input) {
    const { value, min, max, step } = input.props;
    const n = Number(value);
    if (
      value !== undefined &&
      (!Number.isFinite(n) ||
        (min !== undefined && n < min) ||
        (max !== undefined && n > max) ||
        ((step === undefined || step === 1) && !Number.isInteger(n)))
    )
      message = `须为${min}～${max}的${step === undefined || step === 1 ? "整数" : "数值"}`;
  }
  const visibleMessage = editing ? "" : message;
  const controls = Children.map(children, (child) =>
    isValidElement(child)
      ? cloneElement(child as ReactElement<Record<string, unknown>>, {
          "aria-invalid": visibleMessage
            ? true
            : (child.props as Record<string, unknown>)["aria-invalid"],
          "aria-describedby": [
            (child.props as Record<string, unknown>)["aria-describedby"],
            visibleMessage ? errorId : undefined,
          ]
            .filter(Boolean)
            .join(" "),
        })
      : child,
  );
  return (
    <label
      className="field"
      ref={field}
      onFocusCapture={(event) => {
        if (
          event.target instanceof HTMLInputElement ||
          event.target instanceof HTMLTextAreaElement
        )
          setEditing(true);
      }}
      onBlurCapture={(event) => {
        if (
          event.target instanceof HTMLInputElement ||
          event.target instanceof HTMLTextAreaElement
        )
          setEditing(false);
      }}
    >
      <span className="field-title">
        {icon}
        {label}
        {(info || hint) && (
          <SettingInfo
            label={label}
            text={
              <>
                {info}
                {info && hint && <br />}
                {hint}
              </>
            }
          />
        )}
        {titleExtra}
        {visibleMessage && (
          <span className="field-error" id={errorId} role="alert">
            <AlertCircle size={12} />
            {visibleMessage}
          </span>
        )}
      </span>
      {controls}
    </label>
  );
}
export function Metric({
  value,
  label,
  caption,
  icon,
  tone,
}: {
  value: ReactNode;
  label: string;
  caption?: string;
  icon?: ReactNode;
  tone?: string;
}) {
  return (
    <div className={"metric" + (tone ? " tone-" + tone : "")}>
      <strong>{value}</strong>
      <span className="metric-label">
        {icon}
        {label}
      </span>
      {caption && <small>{caption}</small>}
    </div>
  );
}
export function ErrorLine({ error }: { error?: string }) {
  return error ? (
    <p className="error-line" role="alert">
      <AlertCircle size={15} />
      {error}
    </p>
  ) : null;
}
// Preserve row handlers and disabled states while collapsing multi-action cells.
function rowActions(content: ReactNode, label: string): ReactNode {
  type Props = Parameters<typeof Button>[0];
  const buttons: ReactElement<Props>[] = [];
  let compatible = true;
  const collect = (value: ReactNode) =>
    Children.forEach(value, (child) => {
      if (child == null || typeof child === "boolean") return;
      if (typeof child === "string" && !child.trim()) return;
      if (!isValidElement<{ children?: ReactNode }>(child)) {
        compatible = false;
        return;
      }
      if (child.type === Button) {
        const button = child as ReactElement<Props>;
        if (!button.props.onClick || button.props.type === "submit")
          compatible = false;
        buttons.push(button);
      } else if (
        child.type === Fragment ||
        child.type === "div" ||
        child.type === "span"
      )
        collect(child.props.children);
      else compatible = false;
    });
  const caption = (value: ReactNode): string =>
    Children.toArray(value)
      .map((child) => {
        if (typeof child === "string" || typeof child === "number")
          return String(child);
        if (
          isValidElement<{ children?: ReactNode }>(child) &&
          (typeof child.type === "string" || child.type === Fragment)
        )
          return caption(child.props.children);
        return "";
      })
      .join("")
      .trim();
  collect(content);
  if (!compatible || buttons.length < 1) return content;
  const items: MenuAction[] = buttons.map((button) => ({
    label: caption(button.props.children) || button.props.title || "操作",
    onClick: button.props.onClick!,
    disabled: button.props.disabled,
    title: button.props.title,
    icon: Children.toArray(button.props.children).find(
      (child) =>
        isValidElement(child) &&
        (child.type === "svg" ||
          (typeof child.type !== "string" && child.type !== Fragment)),
    ),
  }));
  return <ActionMenu label={label} items={items} />;
}
export function RowActions({
  children,
  label,
}: {
  children: ReactNode;
  label: string;
}) {
  return <>{rowActions(children, label)}</>;
}
export type Column<T> = {
  width?: string;
  maxWidth?: number;
  fitContent?: boolean;
  key: string;
  label: string;
  heading?: ReactNode;
  sortDirection?: "ascending" | "descending" | "none";
  render: (row: T, index: number) => ReactNode;
  className?: string;
};
export function Table<T>({
  rows,
  columns,
  rowKey,
  total,
  selectedCount,
  paginate = true,
  keepHeaders = false,
  footerActions,
  pageSizeLimit,
  autoPageSize,
  paginationResetKey,
  columnSizingKey,
  toolbar,
}: {
  rows: T[];
  columns: Column<T>[];
  rowKey: (row: T) => string;
  total?: number;
  selectedCount?: number;
  paginate?: boolean;
  keepHeaders?: boolean;
  footerActions?: ReactNode;
  pageSizeLimit?: number;
  autoPageSize?: number;
  paginationResetKey?: string;
  columnSizingKey?: string;
  toolbar?: ReactNode;
}) {
  const edgeWidth = (c: Column<T>) =>
    c.label === "选择" || ["selected", "select"].includes(c.key)
      ? 36
      : c.label === "序号" || c.key === "serial"
        ? 42
        : ["操作", "详情"].includes(c.label) ||
            ["actions", "details"].includes(c.key)
          ? 48
          : 0;
  const minTableWidth = columns.reduce(
    (sum, c) => sum + (edgeWidth(c) || (c.width && /^\d+px$/.test(c.width) ? Number.parseFloat(c.width) : 140)),
    0,
  );
  const pagination = usePagination(rows, paginationResetKey ?? rows.map(rowKey).join("\u001f"), pageSizeLimit, autoPageSize);
  const shown = paginate ? pagination.items : rows;
  const offset = paginate ? pagination.start : 0;
  const tableRef = useRef<HTMLTableElement>(null);
  const sizing = useTableColumnSizing(tableRef, columns, rows, pagination.start + ":" + pagination.pageSize, columnSizingKey);
  const [firstWidth, setFirstWidth] = useState(64);
  useLayoutEffect(() => {
    const table = tableRef.current;
    if (!table) return;
    const measure = () => {
      const cells = Array.from(
        table.querySelectorAll<HTMLElement>(".first-cell-content"),
      );
      const width = Math.ceil(
        Math.max(
          32,
          ...cells.map((cell) => cell.getBoundingClientRect().width),
        ) + 32,
      );
      setFirstWidth((old) => (old === width ? old : width));
    };
    measure();
    const observer = new ResizeObserver(measure);
    table
      .querySelectorAll(".first-cell-content")
      .forEach((cell) => observer.observe(cell));
    return () => observer.disconnect();
  }, [rows, columns]);
  return rows.length || keepHeaders || sizing.enabled ? (
    <>
      {sizing.enabled && <div className="adaptive-table-tools">
        <div className="adaptive-table-width-control">
          <Button disabled={!sizing.manual} onClick={sizing.reset}><svg viewBox="0 0 1024 1024" width="18" height="18" fill="currentColor" aria-hidden="true"><path d="M422.4 486.4H288l57.6-57.6c12.8-12.8 12.8-25.6 0-38.4-12.8-6.4-25.6-6.4-38.4 0L224 480V128h-64v768h64V544l83.2 89.6c12.8 6.4 25.6 6.4 38.4 0 12.8-12.8 12.8-25.6 0-38.4l-57.6-57.6h134.4c12.8 0 25.6-12.8 25.6-25.6s-12.8-25.6-25.6-25.6zM480 192h64v640h-64zM800 128v352l-83.2-89.6c-12.8-6.4-25.6-6.4-38.4 0-12.8 12.8-12.8 25.6 0 38.4l57.6 57.6H601.6c-12.8 0-25.6 12.8-25.6 25.6s12.8 25.6 25.6 25.6h134.4l-57.6 57.6c-12.8 12.8-12.8 25.6 0 38.4 6.4 12.8 25.6 12.8 32 0l83.2-89.6V896h64V128h-57.6z" /></svg>自动列宽</Button>
          <SettingInfo label="自动列宽" text="默认按当前页内容自动计算列宽，空间不足时横向滚动，空间有余时按比例扩大自动列并补满区域（不超过最大列宽）。拖动表头分隔线可手动调整，双击恢复该列自动宽度；点击自动列宽恢复整表。最小宽度容纳完整表头，默认最大480px，手动宽度在当前浏览器中保存。" />
        </div>
        {toolbar && <div className="adaptive-table-actions">{toolbar}</div>}
      </div>}
      <div className="table-wrap">
        <table
          className={
            columns.some((c) => c.label === "节点" || c.label === "节点 / 订阅")
              ? "content-width-table balanced-columns has-node-column" + (sizing.enabled ? " adaptive-columns" : "")
              : "content-width-table balanced-columns" + (sizing.enabled ? " adaptive-columns" : "")
          }
          ref={tableRef}
          style={
            {
              "--first-column-width": firstWidth + "px",
              "--table-min-width": (sizing.enabled ? sizing.total : minTableWidth) + "px",
              minWidth: sizing.enabled ? sizing.total : minTableWidth,
              ...(sizing.enabled ? { "--adaptive-table-width": sizing.total + "px" } : {}),
            } as React.CSSProperties
          }
        >
          <colgroup>
            {columns.map((c) => (
              <col
                key={c.key}
                style={sizing.enabled ? {width:sizing.width(c)} : c.width ? { width: c.width } : edgeWidth(c) ? { width: edgeWidth(c) } : undefined}
              />
            ))}
          </colgroup>
          <thead>
            <tr>
              {columns.map((c, columnIndex) => (
                <th
                  key={c.key}
                  className={c.className}
                  data-column={c.key}
                  data-label={c.label}
                  aria-sort={c.sortDirection}
                >
                  {sizing.enabled ? <><span className="adaptive-column-heading">{c.heading || c.label}</span>{sizing.handle(c)}</> : <>
                  {columnIndex === 0 ? (
                    <span className="first-cell-content">
                      {c.heading || c.label}
                    </span>
                  ) : (
                    c.heading || c.label
                  )}
                  </>}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {!shown.length && (
              <tr className="table-empty-row">
                <td colSpan={columns.length}>
                  <Empty>当前筛选无结果</Empty>
                </td>
              </tr>
            )}
            {shown.map((row, localIndex) => {
              const index = localIndex + offset;
              return (
                <tr key={rowKey(row)}>
                  {columns.map((c, columnIndex) => (
                    <td
                      key={c.key}
                      className={c.className}
                      data-column={c.key}
                      data-label={c.label}
                    >
                      {sizing.enabled ? <div className="adaptive-cell-content" title={typeof row === "object" && row !== null ? undefined : String(row)}>
                        {c.label === "操作" || c.key === "actions" ? rowActions(c.render(row,index),"第"+(index+1)+"行") : c.render(row,index)}
                      </div> : <>
                      {columnIndex === 0 ? (
                        <span className="first-cell-content">
                          {c.label === "操作" || c.key === "actions"
                            ? rowActions(
                                c.render(row, index),
                                "第" + (index + 1) + "行",
                              )
                            : c.render(row, index)}
                        </span>
                      ) : c.label === "操作" || c.key === "actions" ? (
                        rowActions(
                          c.render(row, index),
                          "第" + (index + 1) + "行",
                        )
                      ) : (
                        c.render(row, index)
                      )}
                      </>}
                    </td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {paginate && (
        <Pagination
          value={pagination}
          total={total}
          selected={selectedCount}
          actions={footerActions}
        />
      )}
    </>
  ) : (
    <>
      <Empty />
      {paginate && (
        <Pagination
          value={pagination}
          total={total}
          selected={selectedCount}
          actions={footerActions}
        />
      )}
    </>
  );
}
export function JsonDetails({
  data,
  copyData,
}: {
  data: unknown;
  copyData?: unknown;
}) {
  const copyingInModal = useContext(DetailsCopyContext);
  const content = JSON.stringify(data, null, 2) || "";
  return (
    <div className="details-with-copy">
      {!copyingInModal && (
        <CopyButton
          text={
            copyData === undefined ? content : JSON.stringify(copyData, null, 2)
          }
        />
      )}
      <pre className="json-details">{content}</pre>
    </div>
  );
}
export function KeyValue({ data }: { data: Dict }) {
  return (
    <dl className="key-values">
      {Object.entries(data).map(([key, value]) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>
            {typeof value === "object" ? (
              <JsonDetails data={value} />
            ) : (
              String(value ?? "—")
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}
export function Form({
  children,
  onSubmit,
}: {
  children: ReactNode;
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <form autoComplete="off" onSubmit={onSubmit}>
      {children}
    </form>
  );
}

// Preset URL facts follow v2rayN ServiceLib/Global.cs; editor supports arbitrary HTTP(S) URLs.
export const LATENCY_URLS = [
  "https://www.google.com/generate_204",
  "https://www.youtube.com/generate_204",
  "https://www.googlevideo.com/generate_204",
  "https://www.gstatic.com/generate_204",
  "https://www.apple.com/library/test/success.html",
  "http://www.msftconnecttest.com/connecttest.txt",
  "https://cp.cloudflare.com/generate_204",
  "https://captive.apple.com/hotspot-detect.html",
];
export const SPEED_URLS = [
  "https://speed.cloudflare.com/__down?bytes=10000000",
  "https://speed.cloudflare.com/__down?bytes=50000000",
  "https://speed.cloudflare.com/__down?bytes=99999999",
  "https://proof.ovh.net/files/10Mb.dat",
  "https://cachefly.cachefly.net/50mb.test",
  "https://cachefly.cachefly.net/100mb.test",
  "https://cachefly.cachefly.net/1mb.test",
  "https://cachefly.cachefly.net/10mb.test",
];
export function AddressInput({
  value,
  onChange,
  kind,
  disabled = false,
}: {
  value: string;
  onChange: (s: string) => void;
  kind: "latency" | "speed";
  disabled?: boolean;
}) {
  const choices = kind === "latency" ? LATENCY_URLS : SPEED_URLS;
  return (
    <div className="address-input">
      <select
        aria-label={kind === "latency" ? "常用真连接检测地址" : "常用测速地址"}
        disabled={disabled}
        value={choices.includes(value) ? value : ""}
        onChange={(e) => {
          if (e.target.value) onChange(e.target.value);
        }}
      >
        <option value="">手动输入</option>
        {choices.map((url, index) => (
          <option key={url} value={url}>
            {new URL(url).hostname} · {index + 1}
          </option>
        ))}
      </select>
      <input
        aria-label={kind === "latency" ? "真连接检测地址" : "测速文件地址"}
        type="url"
        disabled={disabled}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  );
}

// Keep the editable text separate from the parsed numeric form value, so
// clearing zero does not immediately write a new visible zero into the field.
export function NumberInput({
  value,
  onChange,
  onFocus,
  onBlur,
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  const [draft, setDraft] = useState(String(value ?? ""));
  const editing = useRef(false);
  useEffect(() => {
    if (!editing.current) setDraft(String(value ?? ""));
  }, [value]);
  return (
    <input
      {...props}
      type="number"
      value={draft}
      onFocus={(e) => {
        editing.current = true;
        onFocus?.(e);
      }}
      onChange={(e) => {
        setDraft(e.target.value);
        onChange?.(e);
      }}
      onBlur={(e) => {
        editing.current = false;
        const n = Number(e.target.value);
        setDraft(
          e.target.value === ""
            ? String(value ?? "")
            : Number.isFinite(n)
              ? String(n)
              : String(value ?? ""),
        );
        onBlur?.(e);
      }}
    />
  );
}
