import { CopyButton } from "./resource_controls";
import { ReadBadge, taskReadKeys, useReadState } from "./read_state";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import {
  AlertCircle,
  CheckCircle2,
  ListTodo,
  LoaderCircle,
  X,
} from "lucide-react";
import { api, errorMessage, timestamp, type Task } from "./api";
import { Button, Empty, ErrorLine, useResource } from "./ui";

export type OperationNotice = {
  kind: "success" | "error" | "info";
  title: string;
  message: string;
  operation?: string;
  taskId?: string;
};
type Detail = { task: Task } | { notice: OperationNotice };
const visibleTask = (task: Task) =>
  !(task.source === "automatic" && task.group === "probe.latency");
const terminal = (task: Task) => !["running", "queued"].includes(task.status);
const statusClass = (status: string) =>
  status === "success"
    ? "accent"
    : status === "partial"
      ? "fair"
      : status === "running" || status === "queued"
        ? "muted"
        : "poor";
const statusLabel = (status: string) =>
  status === "success"
    ? "执行成功"
    : status === "partial"
      ? "部分成功"
        : status === "queued" ? "排队等待" : status === "running"
        ? "正在执行"
        : "执行失败";
function StatusIcon({ status }: { status: string }) {
  return status === "running" || status === "queued" ? (
    <LoaderCircle size={16} className="spin" />
  ) : status === "success" ? (
    <CheckCircle2 size={16} className="accent" />
  ) : (
    <AlertCircle size={16} className={status === "partial" ? "fair" : "poor"} />
  );
}
export function Tasks({
  notice,
  onClear,
  onWarningChange,
}: {
  notice: OperationNotice | null;
  onClear: () => void;
  onWarningChange?: (message: string) => void;
}) {
  const resource = useResource<{ tasks: Task[] }>("/api/tasks", 2000);
  const tasks = (resource.data?.tasks || []).filter(visibleTask);
  const [open, setOpen] = useState(false),
    [tab, setTab] = useState<"current" | "history">("history"),
    [page, setPage] = useState(0);
  const [detail, setDetail] = useState<Detail | null>(null),
    [toast, setToast] = useState<OperationNotice | null>(null);
  const [toastPosition, setToastPosition] = useState<
    { top: number; left: number; maxWidth: number } | undefined
  >();
  const [panelPosition, setPanelPosition] = useState<
    { top: number; right: number; maxHeight: number; width: number } | undefined
  >();
  const [localHistory, setLocalHistory] = useState<Task[]>([]),
    [history, setHistory] = useState<Task[]>([]),
    [next, setNext] = useState("");
  const [cursor, setCursor] = useState(""),
    [historyVersion, setHistoryVersion] = useState(0),
    [loading, setLoading] = useState(false),
    [failure, setFailure] = useState("");
  const [pageSize, setPageSize] = useState(() => (innerHeight < 650 ? 3 : 5));
  const { isRead, markRead } = useReadState();
  const panelRoot = useRef<HTMLElement>(null);
  const toastRoot = useRef<HTMLDivElement>(null);
  const root = useRef<HTMLDivElement>(null),
    lastManual = useRef<Task | null>(null),
    advance = useRef(false),
    sequence = useRef(0);
  useLayoutEffect(() => {
    if (!toast && !open) return;
    const header = root.current?.closest("header"),
      tools = root.current?.closest(".header-tools");
    const align = () => {
      const edge = tools?.getBoundingClientRect(),
        bar = header?.getBoundingClientRect(),
        trigger = root.current?.getBoundingClientRect();
      if (!edge || !bar || !trigger) return;
      const viewportRight =
        document.documentElement.getBoundingClientRect().right;
      const rightEdge = viewportRight - 12,
        top = Math.max(8, bar.bottom + 6);
      setPanelPosition({
        top,
        right: Math.max(12, viewportRight - rightEdge),
        maxHeight: Math.max(120, innerHeight - top - 12),
        width: Math.min(390, rightEdge - 12),
      });
      const toastRightEdge = viewportRight - 12;
      const icon = root.current
        ?.querySelector(".task-center-trigger>svg")
        ?.getBoundingClientRect();
      const toastWidth = Math.min(
        240,
        toastRoot.current?.getBoundingClientRect().width || 240,
      );
      const anchor =
        icon && icon.width > 0
          ? icon
          : root.current
              ?.querySelector(".task-center-trigger>span")
              ?.getBoundingClientRect();
      const left = Math.max(
        12,
        Math.min(
          anchor?.left ?? trigger.left,
          toastRightEdge - Math.min(toastWidth, 140),
        ),
      );
      setToastPosition({
        top: Math.max(8, edge.bottom + 6),
        left,
        maxWidth: Math.min(240, toastRightEdge - left),
      });
    };
    align();
    const resize = new ResizeObserver(align);
    if (header) resize.observe(header);
    if (tools) resize.observe(tools);
    addEventListener("resize", align);
    addEventListener("scroll", align, { passive: true });
    return () => {
      resize.disconnect();
      removeEventListener("resize", align);
      removeEventListener("scroll", align);
    };
  }, [toast, open]);
  useEffect(() => {
    const resize = () => setPageSize(innerHeight < 650 ? 3 : 5);
    addEventListener("resize", resize);
    return () => removeEventListener("resize", resize);
  }, []);
  useEffect(() => {
    if (!notice) return;
    setOpen(false);
    setDetail(null);
    setToast(notice);
    lastManual.current = null;
    if (!notice.taskId && notice.kind !== "info") {
      const stamp = Date.now() / 1000;
      const task: Task = {
        id: "local-" + stamp + "-" + ++sequence.current,
        title: notice.operation || "本次操作",
        group: "manual.operation",
        source: "manual",
        status: notice.kind,
        started: stamp,
        finished: stamp,
        message: notice.message,
        details: [],
      };
      setLocalHistory((old) => [task, ...old].slice(0, 50));
    }
  }, [notice]);
  const current = toast?.taskId
    ? tasks.find((task) => task.id === toast.taskId)
    : undefined;
  if (current) lastManual.current = current;
  const manualTask =
    current ||
    (lastManual.current?.id === toast?.taskId ? lastManual.current : null);
  const toastStatus =
    manualTask?.status ||
    (toast?.kind === "info" ? "running" : toast?.kind || "success");
  useEffect(() => {
    if (!toast || toastStatus === "running") return;
    const timer = setTimeout(() => setToast(null), 8000);
    return () => clearTimeout(timer);
  }, [toast, toastStatus]);
  useEffect(() => {
    if (detail && "task" in detail && detail.task.status === "running") {
      const latest = tasks.find((t) => t.id === detail.task.id);
      if (latest && terminal(latest)) {
        markRead(taskReadKeys(latest));
        setDetail({ task: latest });
      }
    }
  }, [resource.data, detail, markRead]);
  useEffect(() => {
    if (!open) return;
    const outside = (e: PointerEvent) => {
      if (
        !root.current?.contains(e.target as Node) &&
        !panelRoot.current?.contains(e.target as Node)
      )
        setOpen(false);
    };
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", escape);
    };
  }, [open]);
  useEffect(() => {
    if (tab !== "history") return;
    const controller = new AbortController();
    setLoading(true);
    setFailure("");
    void api<{ items: Task[]; next?: string; error?: string }>(
      "/api/tasks/history" +
        (cursor ? "?before=" + encodeURIComponent(cursor) : ""),
      undefined,
      controller.signal,
    )
      .then((data) => {
        if (data.error) throw Error(data.error);
        setHistory((old) =>
          cursor
            ? [
                ...new Map(
                  [...old, ...data.items].map((t) => [t.id, t]),
                ).values(),
              ]
            : data.items,
        );
        setNext(data.next || "");
        if (advance.current) {
          setPage((p) => p + 1);
          advance.current = false;
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted) setFailure(errorMessage(e));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [open, tab, cursor, historyVersion]);
  const running = tasks.filter((t) => !terminal(t));
  const unread = [...new Map([...tasks, ...history.filter(visibleTask), ...localHistory].map(t => [t.id,t])).values()]
    .filter((t) => terminal(t) && !isRead(taskReadKeys(t)));
  const unreadErrors = unread.filter((t) => t.status === "error").length;
  const unreadPartial = unread.filter((t) => t.status === "partial").length;
  const warning = resource.error
    ? "任务状态刷新失败"
    : failure
      ? "任务历史读取失败"
      : unreadErrors || unreadPartial
        ? [
            unreadErrors ? `${unreadErrors} 条失败` : "",
            unreadPartial ? `${unreadPartial} 条部分成功` : "",
          ]
            .filter(Boolean)
            .join("、") + "任务未读，可在右侧任务中查看"
        : "";
  useEffect(() => {
    onWarningChange?.(warning);
  }, [onWarningChange, warning]);
  const count = running.length + unread.length;
  const historyRows = [
    ...new Map(
      [
        ...tasks.filter(terminal),
        ...localHistory,
        ...history.filter(visibleTask),
      ].map((t) => [t.id, t]),
    ).values(),
  ].sort((a, b) => b.finished - a.finished);
  const rows = tab === "current" ? running : historyRows;
  const currentPage = Math.min(
      page,
      Math.max(0, Math.ceil(rows.length / pageSize) - 1),
    ),
    start = currentPage * pageSize;
  const showDetail = (value: Detail) => {
    setToast(null);
    onClear();
    setDetail(value);
    setOpen(true);
    if ("task" in value && terminal(value.task)) markRead(taskReadKeys(value.task));
    if ("notice" in value) {
      const local = localHistory.find(t => t.message === value.notice.message && t.title === (value.notice.operation || "本次操作"));
      if (local) markRead(taskReadKeys(local));
    }
  };
  const closeToast = () => {
    setToast(null);
    lastManual.current = null;
    onClear();
  };
  return (
    <div className="task-center-shell" ref={root}>
      <button
        type="button"
        className="button ghost task-center-trigger"
        title={`任务：${running.length} 项执行中，${unread.length} 项未读结果`}
        aria-label={`任务，${running.length}项执行中，${unread.length}项未读结果`}
        aria-expanded={open}
        onClick={() => {
          closeToast();
          setOpen((v) => !v);
          setDetail(null);
          setPage(0);
          setCursor("");
        }}
      >
        <ListTodo size={18} />
        <span>任务</span>
        <span
          className={"task-count " + (running.length ? "running" : "unread") + (!count ? " empty" : "")}
          aria-hidden={!count}
        >
          {count > 99 ? "99+" : count}
        </span>
      </button>
      {open &&
        createPortal(
          <section
            className="task-center-panel"
            aria-label="任务中心"
            ref={panelRoot}
            style={panelPosition}
          >
            {detail ? (
              <div className="task-inline-detail">
                <div className="task-detail-heading">
                  <Button onClick={() => setDetail(null)}>返回列表</Button>
                  <strong>
                    {"task" in detail
                      ? detail.task.title
                      : detail.notice.operation || "执行结果"}
                  </strong>
                  <CopyButton
                    text={
                      "task" in detail
                        ? JSON.stringify(
                            tasks.find((t) => t.id === detail.task.id) ||
                              detail.task,
                            null,
                            2,
                          )
                        : [detail.notice.title, detail.notice.message].join(
                            "\n",
                          )
                    }
                  />
                </div>
                {"task" in detail ? (
                  (() => {
                    const task =
                      tasks.find((t) => t.id === detail.task.id) || detail.task;
                    return (
                      <div className="task-result-detail">
                        <p className={statusClass(task.status)}>
                          {statusLabel(task.status)}
                        </p>
                        <p className="small muted">
                          开始 {timestamp(task.started)}
                          {task.finished
                            ? " · 结束 " + timestamp(task.finished)
                            : ""}
                        </p>
                        <p>{task.message}</p>
                        {(task.details || []).map((line, i) => (
                          <p key={i}>{line}</p>
                        ))}
                        {task.status === "running" && (
                          <Button
                            variant="danger"
                            onClick={() =>
                              void api("/api/tasks/cancel", { id: task.id })
                                .then(resource.refresh)
                                .catch((e) => setFailure(errorMessage(e)))
                            }
                          >
                            取消任务
                          </Button>
                        )}
                      </div>
                    );
                  })()
                ) : (
                  <div className="task-result-detail">
                    <p
                      className={
                        detail.notice.kind === "error" ? "poor" : "accent"
                      }
                    >
                      {detail.notice.title}
                    </p>
                    <p>{detail.notice.message}</p>
                  </div>
                )}
              </div>
            ) : (
              <>
                <div className="task-center-tabs" role="tablist">
                  <button
                    role="tab"
                    aria-selected={tab === "history"}
                    onClick={() => {
                      setTab("history");
                      setPage(0);
                      setCursor("");
                    }}
                  >
                    操作历史
                  </button>
                  <button
                    role="tab"
                    aria-selected={tab === "current"}
                    onClick={() => {
                      setTab("current");
                      setPage(0);
                    }}
                  >
                    当前任务（{running.length}）
                  </button>
                </div>
                <ErrorLine error={resource.error || failure} />
                <div className="task-center-rows">
                  {rows.slice(start, start + pageSize).map((t) => (
                    <button
                      className={"task-center-row" + (terminal(t) && !isRead(taskReadKeys(t)) ? " unread" : "")}
                      key={t.id}
                      onClick={() => showDetail({ task: t })}
                    >
                      <StatusIcon status={t.status} />
                      <span>
                        <strong>{t.title}</strong>
                        {terminal(t) && <ReadBadge read={isRead(taskReadKeys(t))} />}
                        <small>
                          {timestamp(t.finished || t.started)}
                          {t.source === "automatic"
                            ? " · 自动"
                            : t.source === "manual"
                              ? " · 手动"
                              : ""}
                        </small>
                      </span>
                      <span className={statusClass(t.status)}>
                        {statusLabel(t.status)}
                      </span>
                    </button>
                  ))}
                  {!rows.length && (
                    <Empty>
                      {loading
                        ? "正在读取…"
                        : tab === "current"
                          ? "暂无执行中的任务"
                          : "暂无操作历史"}
                    </Empty>
                  )}
                </div>
                <div className="task-center-footer">
                  {tab === "history" && <Button size="compact"
                    title="将已加载的任务历史标为已读；阅读状态仅保存在当前浏览器"
                    disabled={!historyRows.some(t => !isRead(taskReadKeys(t)))}
                    onClick={() => markRead(historyRows.flatMap(taskReadKeys))}>
                    全部标为已读
                  </Button>}
                  <Button
                    disabled={!currentPage}
                    onClick={() => setPage(currentPage - 1)}
                  >
                    上一页
                  </Button>
                  <span>
                    {rows.length ? currentPage + 1 : 0} /{" "}
                    {Math.max(1, Math.ceil(rows.length / pageSize))}
                  </span>
                  <Button
                    disabled={
                      loading ||
                      (start + pageSize >= rows.length &&
                        !(tab === "history" && next))
                    }
                    onClick={() => {
                      if (start + pageSize < rows.length)
                        setPage(currentPage + 1);
                      else {
                        advance.current = true;
                        setCursor(next);
                      }
                    }}
                  >
                    下一页
                  </Button>
                  <Button
                    variant="ghost"
                    onClick={() => {
                      void resource.refresh();
                      advance.current = false;
                      setCursor("");
                      setPage(0);
                      setHistoryVersion((n) => n + 1);
                    }}
                  >
                    刷新
                  </Button>
                </div>
              </>
            )}
          </section>,
          document.body,
        )}
      {toast &&
        createPortal(
          <aside
            className={"task-toast " + toastStatus}
            ref={toastRoot}
            style={toastPosition}
            aria-live="polite"
            role={toastStatus === "error" ? "alert" : "status"}
          >
            <StatusIcon status={toastStatus} />
            {toastStatus === "running" ? (
              <span className="task-toast-label">
                {manualTask?.title || toast.operation || "任务"}正在执行
              </span>
            ) : (
              <button
                className="task-toast-label"
                aria-label={
                  "查看执行结果：" +
                  (manualTask?.title || toast.operation || "本次操作")
                }
                onClick={() =>
                  showDetail(
                    manualTask ? { task: manualTask } : { notice: toast },
                  )
                }
              >
                {manualTask?.title || toast.operation || "本次操作"}
                {statusLabel(toastStatus)}
              </button>
            )}
            <Button variant="ghost" onClick={closeToast} title="关闭任务提示">
              <X size={16} />
            </Button>
          </aside>,
          document.body,
        )}
    </div>
  );
}
