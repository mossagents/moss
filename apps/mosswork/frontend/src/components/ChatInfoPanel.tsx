import { cn } from "@/lib/cn.ts";
import type { ChatMessage, SessionSummary, AppConfig, AutomationTask } from "@/lib/types.ts";
import type { ChatMode } from "@/components/ModeToggleBar.tsx";
import type { ExpertDepth } from "@/components/ExpertParamsBar.tsx";

interface ChatInfoPanelProps {
  messages: ChatMessage[];
  totalTokens: number;
  currentSessionId?: string;
  sessions: SessionSummary[];
  config: AppConfig | null;
  chatMode: ChatMode;
  automationTasks: AutomationTask[];
  onAddAutomation: () => void;
  onViewAllAutomations: () => void;
  expertBreadth?: number;
  expertDepth?: ExpertDepth;
  onBreadthChange?: (v: number) => void;
  onDepthChange?: (v: ExpertDepth) => void;
}

export default function ChatInfoPanel({
  messages,
  totalTokens,
  currentSessionId,
  sessions,
  config,
  chatMode,
  automationTasks,
  onAddAutomation,
  onViewAllAutomations,
  expertBreadth = 3,
  expertDepth = "standard",
  onBreadthChange,
  onDepthChange,
}: ChatInfoPanelProps) {
  void config;

  const currentSession = sessions.find((s) => s.id === currentSessionId);
  const totalMessages = messages.filter(
    (m) => m.role === "user" || m.role === "assistant",
  ).length;

  const createdAt = currentSession?.created_at
    ? (() => {
        const d = new Date(currentSession.created_at);
        return `${d.getMonth() + 1}月${d.getDate()}日 ${d
          .getHours()
          .toString()
          .padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
      })()
    : "—";

  const modeLabel = chatMode === "normal" ? "普通模式" : "专家模式";
  const modeIcon = chatMode === "normal" ? "chat_bubble" : "workspace_premium";

  return (
    <aside className="fixed right-0 top-0 bottom-0 w-80 bg-surface-container-low border-l border-border/40 overflow-y-auto z-20 flex flex-col select-none">
      {/* Wails title bar spacer */}
      <div className="h-8 shrink-0" />

      {/* Session info header */}
      <div className="px-4 py-3 flex items-center justify-between">
        <span className="text-sm font-semibold text-on-surface">会话信息</span>
        <button
          type="button"
          className="p-1 rounded-lg text-on-surface-variant hover:bg-surface-container transition-colors"
        >
          <span className="material-symbols-outlined text-lg">more_horiz</span>
        </button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 pb-4 space-y-4">
        {/* Stats 2×2 grid */}
        <div className="grid grid-cols-2 gap-2">
          <StatCard icon="chat_bubble_outline" label="消息数" value={String(totalMessages)} />
          <StatCard
            icon="data_usage"
            label="Token 使用"
            value={totalTokens > 0 ? formatNumber(totalTokens) : "0"}
          />
          <StatCard icon="calendar_today" label="创建时间" value={createdAt} small />
          <StatCard icon={modeIcon} label="模式" value={modeLabel} />
        </div>

        {/* Expert mode params */}
        {chatMode === "expert" && (
          <section>
            <div className="mb-2">
              <span className="text-sm font-semibold text-on-surface">研究参数</span>
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-on-surface-variant">广度</span>
                <select
                  value={expertBreadth}
                  onChange={(e) => onBreadthChange?.(Number(e.target.value))}
                  className="text-xs bg-surface-container-lowest border border-outline-variant/40 rounded-lg px-2 py-1 text-on-surface focus:outline-none focus:ring-1 focus:ring-primary/40 cursor-pointer"
                >
                  {[1, 2, 3, 4, 5].map((n) => (
                    <option key={n} value={n}>{n} 个方向</option>
                  ))}
                </select>
              </div>
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-on-surface-variant">深度</span>
                <select
                  value={expertDepth}
                  onChange={(e) => onDepthChange?.(e.target.value as ExpertDepth)}
                  className="text-xs bg-surface-container-lowest border border-outline-variant/40 rounded-lg px-2 py-1 text-on-surface focus:outline-none focus:ring-1 focus:ring-primary/40 cursor-pointer"
                >
                  <option value="fast">快速（10步）</option>
                  <option value="standard">标准（30步）</option>
                  <option value="deep">深度（60步）</option>
                </select>
              </div>
            </div>
          </section>
        )}

        {/* Automation tasks section */}
        <section>
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-semibold text-on-surface">定时任务</span>
            <button
              type="button"
              onClick={onViewAllAutomations}
              className="text-xs text-primary hover:opacity-75 transition-opacity flex items-center gap-0.5"
            >
              查看全部
              <span className="material-symbols-outlined text-sm">chevron_right</span>
            </button>
          </div>

          {automationTasks.length === 0 ? (
            <p className="text-xs text-on-surface-variant/60 px-1 py-1">暂无定时任务</p>
          ) : (
            <div className="space-y-2">
              {automationTasks.slice(0, 3).map((task) => (
                <TaskRow key={task.id} task={task} />
              ))}
            </div>
          )}

          <button
            type="button"
            onClick={onAddAutomation}
            className="mt-3 w-full flex items-center justify-center gap-1.5 py-2.5 rounded-xl border border-border/60 text-xs font-medium hover:bg-surface-container transition-colors"
          >
            <span className="material-symbols-outlined text-sm text-primary">add</span>
            <span className="text-primary">新建定时任务</span>
          </button>
        </section>

        {/* Shortcuts section */}
        <section>
          <div className="mb-3">
            <span className="text-sm font-semibold text-on-surface">快捷操作</span>
          </div>
          <div className="space-y-2.5">
            <ShortcutRow label="发送消息" icon="send" keys={["Enter"]} />
            <ShortcutRow label="换行" icon="keyboard_return" keys={["Shift", "Enter"]} />
            <ShortcutRow label="上传文件" icon="upload" keys={["⌘", "U"]} />
            <ShortcutRow label="切换模式" icon="swap_horiz" keys={["⌘", "M"]} />
            <ShortcutRow label="清空对话" icon="delete_sweep" keys={["⌘", "K"]} />
          </div>
        </section>
      </div>
    </aside>
  );
}

function StatCard({
  icon,
  label,
  value,
  small,
}: {
  icon: string;
  label: string;
  value: string;
  small?: boolean;
}) {
  return (
    <div className="bg-surface-container-lowest rounded-xl p-3">
      <div className="flex items-center gap-1.5 mb-1.5">
        <span className="material-symbols-outlined text-sm text-on-surface-variant/70">{icon}</span>
        <span className="text-xs text-on-surface-variant">{label}</span>
      </div>
      <p className={cn("font-semibold text-on-surface leading-tight", small ? "text-xs" : "text-lg")}>
        {value}
      </p>
    </div>
  );
}

function TaskRow({ task }: { task: AutomationTask }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2.5 bg-surface-container-lowest rounded-xl">
      <span className="material-symbols-outlined text-base text-on-surface-variant/70 shrink-0">
        schedule
      </span>
      <div className="flex-1 min-w-0">
        <p className="text-xs font-medium text-on-surface truncate">{task.goal}</p>
        <p className="text-[11px] text-on-surface-variant/60 mt-0.5">
          {scheduleLabel(task.schedule)}
        </p>
      </div>
      {/* Toggle — tasks that exist are active */}
      <div className="w-9 h-5 bg-primary rounded-full relative shrink-0 cursor-default">
        <div className="w-4 h-4 rounded-full bg-white absolute right-0.5 top-0.5 shadow-sm" />
      </div>
    </div>
  );
}

function ShortcutRow({
  label,
  icon,
  keys,
}: {
  label: string;
  icon: string;
  keys: string[];
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <div className="flex items-center gap-2 min-w-0">
        <span className="material-symbols-outlined text-base text-on-surface-variant/60 shrink-0">
          {icon}
        </span>
        <span className="text-xs text-on-surface-variant truncate">{label}</span>
      </div>
      <div className="flex items-center gap-1 shrink-0">
        {keys.map((k, i) => (
          <span key={i} className="flex items-center gap-1">
            {i > 0 && <span className="text-on-surface-variant/40 text-xs">+</span>}
            <kbd className="text-[10px] bg-surface-container px-1.5 py-0.5 rounded border border-outline-variant/30 text-on-surface-variant font-sans leading-none">
              {k}
            </kbd>
          </span>
        ))}
      </div>
    </div>
  );
}

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
  return map[expr] || expr;
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}


