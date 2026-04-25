import { cn } from "@/lib/cn.ts";
import type { SessionSummary } from "@/lib/types.ts";

interface ChatSidebarProps {
  sessions: SessionSummary[];
  currentSessionId?: string;
  onNewSession: () => void;
  onResumeSession: (id: string) => void;
  onDeleteSession: (id: string) => void;
  onDeleteSessions: (ids: string[]) => void;
  isRunning: boolean;
}

function formatSessionDate(dateStr?: string): string {
  if (!dateStr) return "";
  try {
    const date = new Date(dateStr);
    if (isNaN(date.getTime())) return "";
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterday = new Date(today.getTime() - 86400000);
    const msgDate = new Date(date.getFullYear(), date.getMonth(), date.getDate());
    if (msgDate.getTime() === today.getTime()) {
      return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
    } else if (msgDate.getTime() === yesterday.getTime()) {
      return "昨天";
    } else {
      return `${date.getMonth() + 1}月${date.getDate()}日`;
    }
  } catch {
    return "";
  }
}

export default function ChatSidebar({
  sessions,
  currentSessionId,
  onNewSession,
  onResumeSession,
  onDeleteSession,
}: ChatSidebarProps) {
  const recent = sessions.slice(0, 50);

  function getDisplayTitle(s: SessionSummary) {
    return (s.title && s.title !== "New Chat")
      ? s.title
      : (s.goal && s.goal !== "interactive desktop assistant")
        ? s.goal
        : "新对话";
  }

  return (
    <aside className="fixed left-14 top-0 bottom-0 w-60 bg-surface-container flex flex-col z-40 border-r border-border select-none">
      {/* Wails drag region spacer */}
      <div className="h-8 shrink-0" />

      {/* New chat button row */}
      <div className="px-3 pb-3 flex items-center gap-2">
        <button
          onClick={onNewSession}
          className="flex-1 py-2.5 px-4 bg-primary text-on-primary rounded-xl font-bold text-sm flex items-center gap-2 shadow-sm transition-all active:scale-95 hover:opacity-90"
        >
          <span className="material-symbols-outlined text-base">add</span>
          新建对话
        </button>
        <kbd className="shrink-0 text-[11px] font-sans px-2 py-1 rounded-lg bg-surface-container-lowest text-on-surface-variant border border-border/60">
          ⌘K
        </kbd>
      </div>

      {/* Section label */}
      <div className="px-4 mb-2 text-[11px] font-semibold text-on-surface-variant/70">
        最近对话
      </div>

      {/* Session list */}
      <div className="flex-1 overflow-y-auto px-2 space-y-0.5 pb-3">
        {recent.length === 0 && (
          <div className="text-xs text-on-surface-variant/60 px-2 py-2">暂无历史对话</div>
        )}
        {recent.map((s) => {
          const title = getDisplayTitle(s);
          const isCurrent = s.id === currentSessionId;
          const dateLabel = formatSessionDate(s.created_at);

          return (
            <div
              key={s.id}
              className={cn(
                "group relative flex items-center rounded-lg transition-colors",
                isCurrent
                  ? "bg-primary/10"
                  : "hover:bg-surface-container-low",
              )}
            >
              <button
                onClick={() => onResumeSession(s.id)}
                className="flex-1 flex items-center justify-between px-3 py-2.5 min-w-0 text-left"
                title={title}
              >
                <span
                  className={cn(
                    "text-xs truncate flex-1 min-w-0 mr-2",
                    isCurrent
                      ? "text-primary font-semibold"
                      : "text-on-surface-variant font-medium",
                  )}
                >
                  {title}
                </span>
                {s.source === "expert" && (
                  <span className="material-symbols-outlined text-[13px] text-amber-500 shrink-0 mr-1" title="专家模式">workspace_premium</span>
                )}
                {s.source === "scheduled" && (
                  <span className="material-symbols-outlined text-[13px] text-blue-400 shrink-0 mr-1" title="定时任务">schedule</span>
                )}
                <span className="text-[11px] text-on-surface-variant/50 shrink-0 tabular-nums">
                  {dateLabel}
                </span>
              </button>

              {/* Hover delete button */}
              <button
                onClick={(e) => { e.stopPropagation(); onDeleteSession(s.id); }}
                title="删除对话"
                className="opacity-0 group-hover:opacity-100 mr-1.5 p-1 rounded-md text-on-surface-variant/40 hover:text-error hover:bg-error/10 transition-all shrink-0"
              >
                <span className="material-symbols-outlined text-sm">delete</span>
              </button>
            </div>
          );
        })}
      </div>

      {/* View all link */}
      <div className="px-4 pb-3 border-t border-border/30 pt-2">
        <button
          type="button"
          className="flex items-center gap-0.5 text-xs text-on-surface-variant/60 hover:text-primary transition-colors"
        >
          查看全部对话
          <span className="material-symbols-outlined text-sm">chevron_right</span>
        </button>
      </div>
    </aside>
  );
}


