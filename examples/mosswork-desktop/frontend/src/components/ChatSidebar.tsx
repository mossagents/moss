import { useState } from "react";
import { cn } from "@/lib/cn";
import type { SessionSummary } from "@/lib/types";

interface ChatSidebarProps {
  sessions: SessionSummary[];
  currentSessionId?: string;
  onNewSession: () => void;
  onResumeSession: (id: string) => void;
  onDeleteSession: (id: string) => void;
  onDeleteSessions: (ids: string[]) => void;
  isRunning: boolean;
}

export default function ChatSidebar({
  sessions,
  currentSessionId,
  onNewSession,
  onResumeSession,
  onDeleteSession,
  onDeleteSessions,
  isRunning,
}: ChatSidebarProps) {
  const [manageMode, setManageMode] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const recent = sessions.slice(0, 50);

  function toggleManage() {
    setManageMode((v) => !v);
    setSelected(new Set());
  }

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function selectAll() {
    setSelected(new Set(recent.map((s) => s.id)));
  }

  function clearSelection() {
    setSelected(new Set());
  }

  async function handleBulkDelete() {
    if (selected.size === 0) return;
    onDeleteSessions(Array.from(selected));
    setSelected(new Set());
    setManageMode(false);
  }

  function getDisplayTitle(s: SessionSummary) {
    return (s.title && s.title !== "New Chat") ? s.title
      : (s.goal && s.goal !== "interactive desktop assistant") ? s.goal
      : "New Chat";
  }

  return (
    <aside className="fixed left-14 top-0 bottom-0 w-60 bg-surface-container flex flex-col z-40 border-r border-border select-none">
      {/* Header: New Chat + Manage toggle */}
      <div className="pt-10 pb-3 px-3 flex gap-2">
        <button
          onClick={onNewSession}
          disabled={manageMode}
          className={cn(
            "flex-1 py-2.5 px-3 bg-primary text-on-primary rounded-xl font-bold text-sm flex items-center justify-center gap-2 shadow-sm transition-all active:scale-95",
            manageMode && "opacity-50 pointer-events-none",
          )}
        >
          <span className="material-symbols-outlined text-base">add</span>
          新对话
        </button>
        <button
          onClick={toggleManage}
          title={manageMode ? "完成" : "管理会话"}
          className={cn(
            "w-9 h-9 mt-0.5 flex items-center justify-center rounded-xl transition-colors text-sm font-bold",
            manageMode
              ? "bg-primary-container text-on-primary-container"
              : "bg-surface-container-high text-on-surface-variant hover:bg-surface-container-low",
          )}
        >
          <span className="material-symbols-outlined text-base">
            {manageMode ? "close" : "checklist"}
          </span>
        </button>
      </div>

      {/* Manage-mode toolbar */}
      {manageMode && (
        <div className="px-3 pb-2 flex items-center gap-1.5">
          <button
            onClick={selected.size === recent.length ? clearSelection : selectAll}
            className="flex-1 py-1 text-[11px] font-medium text-on-surface-variant hover:text-on-surface rounded-lg hover:bg-surface-container-low transition-colors"
          >
            {selected.size === recent.length ? "取消全选" : "全选"}
          </button>
          <button
            onClick={handleBulkDelete}
            disabled={selected.size === 0}
            className={cn(
              "flex items-center gap-1 py-1 px-2 rounded-lg text-[11px] font-bold transition-colors",
              selected.size > 0
                ? "bg-error/15 text-error hover:bg-error/25"
                : "text-on-surface-variant/40 pointer-events-none",
            )}
          >
            <span className="material-symbols-outlined text-sm">delete</span>
            {selected.size > 0 ? `删除 (${selected.size})` : "删除"}
          </button>
        </div>
      )}

      <div className="px-3 mb-2 text-[10px] font-bold tracking-widest uppercase text-on-surface-variant/60">
        最近会话
      </div>

      <div className="flex-1 overflow-y-auto px-2 space-y-0.5 pb-4">
        {recent.length === 0 && (
          <div className="text-xs text-on-surface-variant px-2 py-2">暂无历史会话</div>
        )}
        {recent.map((s) => {
          const title = getDisplayTitle(s);
          const isSelected = selected.has(s.id);
          const isCurrent = s.id === currentSessionId;

          return (
            <div
              key={s.id}
              className={cn(
                "group relative flex items-center rounded-lg transition-colors",
                isCurrent && !manageMode
                  ? "bg-primary-container/40"
                  : isSelected
                  ? "bg-error/10"
                  : "hover:bg-surface-container-low",
              )}
            >
              {/* Checkbox (manage mode) */}
              {manageMode && (
                <button
                  onClick={() => toggleSelect(s.id)}
                  className="pl-2 pr-1 py-2 flex items-center shrink-0"
                >
                  <span
                    className={cn(
                      "material-symbols-outlined text-base transition-colors",
                      isSelected ? "text-error" : "text-on-surface-variant/50",
                    )}
                  >
                    {isSelected ? "check_box" : "check_box_outline_blank"}
                  </span>
                </button>
              )}

              {/* Session button */}
              <button
                onClick={() => manageMode ? toggleSelect(s.id) : onResumeSession(s.id)}
                className="flex-1 text-left px-3 py-2 min-w-0"
                title={title}
              >
                <div
                  className={cn(
                    "font-medium truncate text-xs",
                    isCurrent && !manageMode
                      ? "text-on-primary-container font-semibold"
                      : "text-on-surface-variant",
                  )}
                >
                  {title}
                </div>
                <div className="text-[10px] opacity-50 truncate mt-0.5">{s.id.slice(0, 14)}…</div>
              </button>

              {/* Single-delete button (hover, non-manage mode) */}
              {!manageMode && (
                <button
                  onClick={(e) => { e.stopPropagation(); onDeleteSession(s.id); }}
                  title="删除会话"
                  className="opacity-0 group-hover:opacity-100 mr-1.5 p-1 rounded-md text-on-surface-variant/50 hover:text-error hover:bg-error/10 transition-all shrink-0"
                >
                  <span className="material-symbols-outlined text-sm">delete</span>
                </button>
              )}
            </div>
          );
        })}
      </div>
    </aside>
  );
}
