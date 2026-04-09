import { Plus } from "lucide-react";
import type { AppConfig, SessionSummary } from "@/lib/types.ts";
import { cn } from "@/lib/cn.ts";

interface SidebarProps {
  config: AppConfig | null;
  isRunning: boolean;
  onNewSession: () => void;
  sessions: SessionSummary[];
  currentSessionId?: string;
  onResumeSession: (id: string) => void;
}

export default function Sidebar({
  config,
  isRunning,
  onNewSession,
  sessions,
  currentSessionId,
  onResumeSession,
}: SidebarProps) {
  const initials = config?.provider?.[0]?.toUpperCase() ?? "M";
  const modelLabel = config?.model || "AI Agent";
  const providerLabel = config?.provider || "mosswork";
  const recent = sessions.slice(0, 8);

  return (
    <aside className="h-screen w-64 fixed left-0 top-0 bg-surface-container flex flex-col p-4 overflow-y-auto z-50 select-none shadow-botanical-sidebar">
      <div className="mb-6 px-2 pt-4">
        <h1 className="text-2xl font-bold text-on-surface tracking-tight font-headline">mosswork</h1>
        <p className="text-xs text-on-surface-variant font-medium opacity-70">Lightweight Cowork Desktop</p>
      </div>

      <button
        onClick={onNewSession}
        disabled={isRunning}
        className={cn(
          "mb-6 w-full py-3 px-4 bg-primary text-on-primary rounded-xl font-bold text-sm flex items-center justify-center gap-2 shadow-sm transition-transform active:scale-90",
          isRunning && "opacity-50 pointer-events-none",
        )}
      >
        <Plus size={18} />
        新对话
      </button>

      <div className="mb-3 px-2 text-[10px] font-bold tracking-widest uppercase text-on-surface-variant/70">最近会话</div>
      <div className="space-y-1 mb-6">
        {recent.length === 0 && (
          <div className="text-xs text-on-surface-variant px-2 py-1">暂无历史会话</div>
        )}
        {recent.map((s) => (
          <button
            key={s.id}
            onClick={() => onResumeSession(s.id)}
            className={cn(
              "w-full text-left px-3 py-2 rounded-lg text-xs transition-colors",
              s.id === currentSessionId
                ? "bg-surface-container-lowest text-on-surface"
                : "text-on-surface-variant hover:bg-surface-container-low",
            )}
            title={s.title || s.goal}
          >
            <div className="font-semibold truncate">{s.title || s.goal || s.id}</div>
            <div className="text-[10px] opacity-70 truncate">{s.id}</div>
          </button>
        ))}
      </div>

      <div className="mt-auto pt-4 flex items-center gap-3 px-2">
        <div className="w-10 h-10 rounded-full bg-surface-container-highest flex items-center justify-center shrink-0">
          <span className="text-sm font-bold text-on-surface-variant">{initials}</span>
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-sm font-bold text-on-surface truncate">{providerLabel}</p>
          <p className="text-xs text-on-surface-variant truncate">{modelLabel}</p>
        </div>
        <span
          className={cn(
            "w-2 h-2 rounded-full shrink-0",
            isRunning ? "status-dot-active animate-pulse" : "status-dot-inactive",
          )}
        />
      </div>
    </aside>
  );
}
