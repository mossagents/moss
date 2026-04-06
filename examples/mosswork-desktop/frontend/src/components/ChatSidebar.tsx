import { cn } from "@/lib/cn";
import type { SessionSummary } from "@/lib/types";

interface ChatSidebarProps {
  sessions: SessionSummary[];
  currentSessionId?: string;
  onNewSession: () => void;
  onResumeSession: (id: string) => void;
  isRunning: boolean;
}

export default function ChatSidebar({
  sessions,
  currentSessionId,
  onNewSession,
  onResumeSession,
  isRunning,
}: ChatSidebarProps) {
  const recent = sessions.slice(0, 20);

  return (
    <aside className="fixed left-14 top-0 bottom-0 w-60 bg-surface-container flex flex-col z-40 border-r border-border select-none">
      <div className="pt-10 pb-3 px-3">
        <button
          onClick={onNewSession}
          disabled={isRunning}
          className={cn(
            "w-full py-2.5 px-3 bg-primary text-on-primary rounded-xl font-bold text-sm flex items-center justify-center gap-2 shadow-sm transition-all active:scale-95",
            isRunning && "opacity-50 pointer-events-none",
          )}
        >
          <span className="material-symbols-outlined text-base">add</span>
          新对话
        </button>
      </div>

      <div className="px-3 mb-2 text-[10px] font-bold tracking-widest uppercase text-on-surface-variant/60">
        最近会话
      </div>

      <div className="flex-1 overflow-y-auto px-2 space-y-0.5 pb-4">
        {recent.length === 0 && (
          <div className="text-xs text-on-surface-variant px-2 py-2">暂无历史会话</div>
        )}
        {recent.map((s) => (
          <button
            key={s.id}
            onClick={() => onResumeSession(s.id)}
            className={cn(
              "w-full text-left px-3 py-2 rounded-lg text-xs transition-colors",
              s.id === currentSessionId
                ? "bg-primary-container/40 text-on-primary-container font-semibold"
                : "text-on-surface-variant hover:bg-surface-container-low",
            )}
            title={s.title || s.goal}
          >
            <div className="font-medium truncate">{s.title || s.goal || s.id}</div>
            <div className="text-[10px] opacity-60 truncate mt-0.5">{s.id.slice(0, 16)}...</div>
          </button>
        ))}
      </div>
    </aside>
  );
}
