import { useState, useRef, useEffect } from "react";
import { cn } from "@/lib/cn.ts";
import type { AutomationTask } from "@/lib/types.ts";

type TaskStatus = "running" | "paused" | "completed" | "failed";

function scheduleLabel(expr: string): string {
  const map: Record<string, string> = {
    "@every 30m": "每30分钟",
    "@every 1h": "每小时",
    "@every 2h": "每2小时",
    "@every 6h": "每6小时",
    "@every 12h": "每12小时",
    "@every 24h": "每天",
    "@every 72h": "每3天",
    "@every 168h": "每周",
  };
  return map[expr] ?? expr;
}

function fmtTime(iso: string | undefined): string {
  if (!iso) return "—";
  try {
    const d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    const now = new Date();
    const todayMs = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const dMs = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    const diffDays = Math.round((dMs - todayMs) / 86400000);
    const hm = d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
    const wd = ["日", "一", "二", "三", "四", "五", "六"][d.getDay()];
    if (diffDays === 0) return `今天 ${hm}`;
    if (diffDays === -1) return `昨天 ${hm}`;
    if (diffDays === 1) return `明天 ${hm}`;
    if (diffDays >= -7 && diffDays < -1) return `上${wd} ${hm}`;
    if (diffDays > 1 && diffDays <= 7) return `本周${wd} ${hm}`;
    return `${d.getMonth() + 1}月${d.getDate()}日 ${hm}`;
  } catch {
    return iso;
  }
}

function resolveStatus(task: AutomationTask): TaskStatus {
  if (task.status) return task.status;
  if (task.next_run) return "running";
  if (task.last_run && task.run_count > 0) return "completed";
  return "running";
}

function taskIcon(task: AutomationTask): string {
  const text = (task.id + " " + task.goal).toLowerCase();
  if (/report|报告|日报|周报/.test(text)) return "description";
  if (/mail|email|发送|邮件/.test(text)) return "mail";
  if (/progress|进度|提醒/.test(text)) return "bar_chart";
  if (/backup|备份/.test(text)) return "storage";
  if (/search|query|查询|排名|关键词/.test(text)) return "search";
  if (/monitor|监控|竞品/.test(text)) return "track_changes";
  if (/health|check|检查|系统/.test(text)) return "notifications";
  if (/cloud|upload|send|deploy/.test(text)) return "cloud_upload";
  return "task_alt";
}

const STATUS_CFG: Record<TaskStatus, { label: string; icon: string; badgeCls: string; dotCls: string }> = {
  running:   { label: "运行中", icon: "radio_button_checked", badgeCls: "bg-emerald-50 text-emerald-700",    dotCls: "bg-emerald-500" },
  paused:    { label: "已暂停", icon: "pause",                badgeCls: "bg-orange-50 text-orange-600",     dotCls: "bg-orange-500" },
  completed: { label: "已完成", icon: "check",                badgeCls: "bg-surface-container text-on-surface-variant", dotCls: "bg-on-surface-variant/60" },
  failed:    { label: "失败",   icon: "close",                badgeCls: "bg-red-50 text-red-600",           dotCls: "bg-red-500" },
};

const FILTER_ITEMS: { key: "all" | TaskStatus; label: string }[] = [
  { key: "all",       label: "全部任务" },
  { key: "running",   label: "运行中"   },
  { key: "paused",    label: "已暂停"   },
  { key: "completed", label: "已完成"   },
  { key: "failed",    label: "失败"     },
];

interface AutomationViewProps {
  tasks: AutomationTask[];
  onAdd: () => void;
  onRemove: (id: string) => void;
  onRunNow: (id: string) => void;
  onPause?: (id: string) => void;
  onEdit?: (task: AutomationTask) => void;
}

export default function AutomationView({ tasks, onAdd, onRemove, onRunNow, onPause, onEdit }: AutomationViewProps) {
  const [filter, setFilter] = useState<"all" | TaskStatus>("all");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const PAGE_SIZE = 10;

  const counts = {
    all:       tasks.length,
    running:   tasks.filter(t => resolveStatus(t) === "running").length,
    paused:    tasks.filter(t => resolveStatus(t) === "paused").length,
    completed: tasks.filter(t => resolveStatus(t) === "completed").length,
    failed:    tasks.filter(t => resolveStatus(t) === "failed").length,
  };

  const filtered = tasks.filter(t => {
    if (filter !== "all" && resolveStatus(t) !== filter) return false;
    if (search.trim()) {
      const q = search.toLowerCase();
      return t.id.toLowerCase().includes(q) || t.goal.toLowerCase().includes(q);
    }
    return true;
  });

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, totalPages);
  const displayed = filtered.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE);

  useEffect(() => { setPage(1); }, [filter, search]);

  return (
    <div className="h-full flex overflow-hidden">
      {/* Left sub-sidebar */}
      <aside className="w-52 border-r border-border flex flex-col shrink-0 bg-surface-container-low">
        <div className="h-8 shrink-0" />
        <div className="px-5 pt-4 pb-3 shrink-0">
          <h2 className="text-base font-bold text-on-surface font-headline">定时任务</h2>
        </div>
        <div className="px-3 pb-4 shrink-0">
          <button
            onClick={onAdd}
            className="w-full flex items-center justify-center gap-1.5 py-2.5 bg-primary text-on-primary rounded-xl text-sm font-bold hover:opacity-90 active:scale-95 transition-all shadow-sm"
          >
            <span className="material-symbols-outlined text-base">add</span>
            新建任务
          </button>
        </div>

        <nav className="flex-1 px-2 space-y-0.5 overflow-y-auto">
          {FILTER_ITEMS.map(item => (
            <button
              key={item.key}
              type="button"
              onClick={() => setFilter(item.key)}
              className={cn(
                "w-full flex items-center justify-between px-3 py-2.5 rounded-xl text-sm transition-colors",
                filter === item.key
                  ? "bg-primary/10 text-primary font-semibold"
                  : "text-on-surface-variant hover:bg-surface-container-high",
              )}
            >
              <span>{item.label}</span>
              <span className={cn(
                "text-xs font-bold px-2 py-0.5 rounded-full min-w-[24px] text-center",
                filter === item.key
                  ? "bg-primary/15 text-primary"
                  : "bg-surface-container-highest text-on-surface-variant",
              )}>
                {counts[item.key]}
              </span>
            </button>
          ))}
        </nav>

        <div className="px-2 py-3 border-t border-border shrink-0">
          <button
            type="button"
            className="w-full flex items-center gap-2 px-3 py-2 rounded-xl text-sm text-on-surface-variant/60 hover:bg-surface-container-high hover:text-on-surface-variant transition-colors"
          >
            <span className="material-symbols-outlined text-base">delete</span>
            回收站
          </button>
        </div>
      </aside>

      {/* Main content */}
      <div className="flex-1 flex flex-col min-w-0">
        <div className="h-8 shrink-0" />

        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 shrink-0">
          <h1 className="text-xl font-bold text-on-surface font-headline">定时任务</h1>
          <div className="flex items-center gap-2">
            <div className="relative">
              <span className="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-[18px] text-on-surface-variant/40 pointer-events-none">search</span>
              <input
                type="text"
                value={search}
                onChange={e => setSearch(e.target.value)}
                placeholder="搜索任务"
                className="w-52 pl-9 pr-4 py-2 rounded-xl bg-surface-container text-sm text-on-surface placeholder:text-on-surface-variant/40 outline-none border border-transparent focus:border-primary/40 transition-colors"
              />
            </div>
            <button
              type="button"
              className="flex items-center gap-1 px-3 py-2 rounded-xl bg-surface-container text-sm text-on-surface-variant hover:bg-surface-container-high transition-colors border border-outline-variant/30"
            >
              <span className="material-symbols-outlined text-[18px]">filter_list</span>
              筛选
              <span className="material-symbols-outlined text-[16px]">expand_more</span>
            </button>
          </div>
        </div>

        {/* Table area */}
        <div className="flex-1 overflow-auto px-6">
          {displayed.length === 0 ? (
            <EmptyState onAdd={onAdd} hasFilter={filter !== "all" || !!search.trim()} />
          ) : (
            <table className="w-full text-sm border-separate border-spacing-0">
              <thead>
                <tr>
                  <th className="text-left py-3 pr-4 text-xs font-semibold text-on-surface-variant/70 border-b border-border w-[36%]">任务名称</th>
                  <th className="text-left py-3 pr-4 text-xs font-semibold text-on-surface-variant/70 border-b border-border w-[12%]">状态</th>
                  <th className="text-left py-3 pr-4 text-xs font-semibold text-on-surface-variant/70 border-b border-border w-[13%]">触发规则</th>
                  <th className="text-left py-3 pr-4 text-xs font-semibold text-on-surface-variant/70 border-b border-border w-[13%]">下次运行时间</th>
                  <th className="text-left py-3 pr-4 text-xs font-semibold text-on-surface-variant/70 border-b border-border w-[15%]">最后运行时间</th>
                  <th className="text-right py-3 text-xs font-semibold text-on-surface-variant/70 border-b border-border w-[11%]">操作</th>
                </tr>
              </thead>
              <tbody>
                {displayed.map(task => (
                  <TaskRow
                    key={task.id}
                    task={task}
                    onRunNow={() => onRunNow(task.id)}
                    onPause={onPause ? () => onPause(task.id) : undefined}
                    onEdit={onEdit ? () => onEdit(task) : undefined}
                    onRemove={() => onRemove(task.id)}
                  />
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-6 py-3 border-t border-border shrink-0">
          <div className="flex items-center gap-3 text-sm text-on-surface-variant">
            <span>共 {filtered.length} 项</span>
            <select className="bg-surface-container rounded-lg px-2 py-1 text-xs outline-none border border-outline-variant/30 cursor-pointer">
              <option value="10">10条/页</option>
              <option value="20">20条/页</option>
              <option value="50">50条/页</option>
            </select>
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setPage(p => Math.max(1, p - 1))}
              disabled={safePage <= 1}
              className="w-8 h-8 flex items-center justify-center rounded-lg text-on-surface-variant hover:bg-surface-container disabled:opacity-30 disabled:cursor-not-allowed transition-colors border border-outline-variant/30"
            >
              <span className="material-symbols-outlined text-base">chevron_left</span>
            </button>
            {Array.from({ length: totalPages }, (_, i) => i + 1).map(p => (
              <button
                key={p}
                onClick={() => setPage(p)}
                className={cn(
                  "w-8 h-8 flex items-center justify-center rounded-lg text-sm transition-colors",
                  p === safePage
                    ? "bg-primary text-on-primary font-bold"
                    : "text-on-surface-variant hover:bg-surface-container border border-outline-variant/30",
                )}
              >
                {p}
              </button>
            ))}
            <button
              onClick={() => setPage(p => Math.min(totalPages, p + 1))}
              disabled={safePage >= totalPages}
              className="w-8 h-8 flex items-center justify-center rounded-lg text-on-surface-variant hover:bg-surface-container disabled:opacity-30 disabled:cursor-not-allowed transition-colors border border-outline-variant/30"
            >
              <span className="material-symbols-outlined text-base">chevron_right</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function TaskRow({
  task,
  onRunNow,
  onPause,
  onEdit,
  onRemove,
}: {
  task: AutomationTask;
  onRunNow: () => void;
  onPause?: () => void;
  onEdit?: () => void;
  onRemove: () => void;
}) {
  const status = resolveStatus(task);
  const cfg = STATUS_CFG[status];
  const [showMore, setShowMore] = useState(false);
  const moreRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!showMore) return;
    function handler(e: MouseEvent) {
      if (moreRef.current && !moreRef.current.contains(e.target as Node)) setShowMore(false);
    }
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [showMore]);

  const lastResult = task.last_run_result ?? (task.last_run ? (status === "failed" ? "failure" : "success") : null);

  return (
    <tr className="border-b border-border/50 hover:bg-surface-container/30 transition-colors group">
      {/* Name + icon */}
      <td className="py-4 pr-4">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-surface-container flex items-center justify-center shrink-0">
            <span className="material-symbols-outlined text-[18px] text-on-surface-variant/60">{taskIcon(task)}</span>
          </div>
          <div className="min-w-0">
            <p className="font-semibold text-on-surface truncate text-sm">{task.id}</p>
            <p className="text-xs text-on-surface-variant/60 truncate mt-0.5">{task.goal}</p>
          </div>
        </div>
      </td>

      {/* Status */}
      <td className="py-4 pr-4">
        <span className={cn("inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium", cfg.badgeCls)}>
          <span className="material-symbols-outlined text-[13px]">{cfg.icon}</span>
          {cfg.label}
        </span>
      </td>

      {/* Schedule */}
      <td className="py-4 pr-4 text-sm text-on-surface-variant">{scheduleLabel(task.schedule)}</td>

      {/* Next run */}
      <td className="py-4 pr-4 text-sm text-on-surface-variant">{fmtTime(task.next_run)}</td>

      {/* Last run + result */}
      <td className="py-4 pr-4">
        {task.last_run ? (
          <div>
            <p className="text-sm text-on-surface-variant">{fmtTime(task.last_run)}</p>
            {lastResult && (
              <p className={cn("text-xs flex items-center gap-1 mt-0.5", lastResult === "failure" ? "text-red-500" : "text-emerald-600")}>
                <span className={cn("w-1.5 h-1.5 rounded-full inline-block shrink-0", lastResult === "failure" ? "bg-red-500" : "bg-emerald-500")} />
                {lastResult === "failure" ? "失败" : "成功"}
              </p>
            )}
          </div>
        ) : (
          <span className="text-sm text-on-surface-variant/40">—</span>
        )}
      </td>

      {/* Actions */}
      <td className="py-4">
        <div className="flex items-center justify-end gap-0.5">
          {status === "running" ? (
            <ActionBtn icon="pause" title="暂停" onClick={onPause} />
          ) : (
            <ActionBtn
              icon={status === "completed" ? "play_circle" : "play_arrow"}
              title="立即执行"
              onClick={onRunNow}
            />
          )}
          {status !== "completed" && (
            <ActionBtn icon="edit" title="编辑" onClick={onEdit} />
          )}
          <div className="relative" ref={moreRef}>
            <ActionBtn icon="more_horiz" title="更多" onClick={() => setShowMore(v => !v)} />
            {showMore && (
              <div className="absolute right-0 top-full mt-1 bg-surface rounded-xl shadow-lg border border-outline-variant/30 py-1 z-50 min-w-[108px]">
                <button
                  type="button"
                  onClick={() => { onRunNow(); setShowMore(false); }}
                  className="w-full flex items-center gap-2 px-3 py-2 text-sm text-on-surface hover:bg-surface-container transition-colors"
                >
                  <span className="material-symbols-outlined text-base">play_arrow</span>
                  立即执行
                </button>
                <button
                  type="button"
                  onClick={() => { onRemove(); setShowMore(false); }}
                  className="w-full flex items-center gap-2 px-3 py-2 text-sm text-red-600 hover:bg-red-50 transition-colors"
                >
                  <span className="material-symbols-outlined text-base">delete</span>
                  删除
                </button>
              </div>
            )}
          </div>
        </div>
      </td>
    </tr>
  );
}

function ActionBtn({ icon, title, onClick }: { icon: string; title: string; onClick?: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!onClick}
      title={title}
      className="w-7 h-7 flex items-center justify-center rounded-lg text-on-surface-variant/50 hover:bg-surface-container-high hover:text-on-surface-variant disabled:opacity-20 disabled:cursor-not-allowed transition-colors"
    >
      <span className="material-symbols-outlined text-[18px]">{icon}</span>
    </button>
  );
}

function EmptyState({ onAdd, hasFilter }: { onAdd: () => void; hasFilter: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center min-h-[400px] select-none">
      <div className="w-16 h-16 rounded-2xl bg-surface-container-high flex items-center justify-center mb-5">
        <span className="material-symbols-outlined text-4xl text-on-surface-variant/40">schedule</span>
      </div>
      {hasFilter ? (
        <>
          <h3 className="text-base font-bold text-on-surface mb-2">没有找到符合条件的任务</h3>
          <p className="text-sm text-on-surface-variant/70">尝试清除搜索或筛选条件</p>
        </>
      ) : (
        <>
          <h3 className="text-base font-bold text-on-surface mb-2">还没有定时任务</h3>
          <p className="text-sm text-on-surface-variant text-center max-w-xs mb-5">
            创建定时 AI 任务，让 AI 按计划自动完成工作。
          </p>
          <button
            onClick={onAdd}
            className="flex items-center gap-2 px-5 py-2.5 bg-primary text-on-primary rounded-xl font-bold text-sm shadow-sm hover:opacity-90 active:scale-95 transition-all"
          >
            <span className="material-symbols-outlined text-base">add</span>
            新建定时任务
          </button>
        </>
      )}
    </div>
  );
}

