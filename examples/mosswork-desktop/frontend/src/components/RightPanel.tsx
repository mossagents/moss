import { cn } from "@/lib/cn";
import type { AppConfig, ScheduleEntry, SessionSummary } from "@/lib/types";

interface RightPanelProps {
  config: AppConfig | null;
  isRunning: boolean;
  sessions: SessionSummary[];
  schedules: ScheduleEntry[];
  onRunCommand: (cmd: string) => void;
}

export default function RightPanel({ config, isRunning, sessions, schedules, onRunCommand }: RightPanelProps) {
  const workspaceName = config?.workspace
    ? config.workspace.replace(/\\/g, "/").split("/").pop() || config.workspace
    : null;

  return (
    <aside className="fixed right-0 top-0 bottom-0 w-80 bg-surface-container-low h-screen p-6 overflow-y-auto z-30 select-none border-botanical-left">
      <div className="flex items-center justify-between mb-6 mt-2">
        <h2 className="font-bold text-on-surface tracking-tight text-base font-headline">协作面板</h2>
      </div>

      <div className="space-y-6">
        <div>
          <span className="text-[10px] font-bold text-on-surface-variant/60 tracking-widest uppercase mb-3 block">Agent 状态</span>
          <div className="bg-surface-container-lowest rounded-xl p-4">
            <div className="flex items-center gap-3 mb-3">
              <div className={cn("w-3 h-3 rounded-full shrink-0", isRunning ? "status-dot-active" : "status-dot-inactive")} />
              <span className="text-sm font-bold text-on-surface">{isRunning ? "运行中" : "空闲"}</span>
            </div>
            <div className="space-y-1.5 text-xs text-on-surface-variant">
              <div className="flex justify-between">
                <span>会话</span>
                <span className="font-medium text-on-surface">{sessions.length}</span>
              </div>
              <div className="flex justify-between">
                <span>定时任务</span>
                <span className="font-medium text-on-surface">{schedules.length}</span>
              </div>
              {workspaceName && (
                <div className="flex justify-between">
                  <span>工作区</span>
                  <span className="font-medium text-on-surface truncate ml-2 max-w-30 text-right">{workspaceName}</span>
                </div>
              )}
            </div>
          </div>
        </div>

        <div>
          <span className="text-[10px] font-bold text-on-surface-variant/60 tracking-widest uppercase mb-3 block">快捷命令</span>
          <div className="space-y-2">
            <QuickAction icon="history" label="列出会话" onClick={() => onRunCommand("/sessions")} />
            <QuickAction icon="event" label="列出定时任务" onClick={() => onRunCommand("/schedules")} />
            <QuickAction icon="compress" label="执行 Offload" onClick={() => onRunCommand("/offload 20 right-panel")} />
            <QuickAction icon="dashboard" label="查看仪表盘" onClick={() => onRunCommand("/dashboard")} />
          </div>
        </div>

        <div>
          <span className="text-[10px] font-bold text-on-surface-variant/60 tracking-widest uppercase mb-3 block">最近定时任务</span>
          <div className="space-y-2">
            {schedules.length === 0 && (
              <div className="bg-surface-container-lowest rounded-xl p-3 text-xs text-on-surface-variant">暂无定时任务</div>
            )}
            {schedules.slice(0, 5).map((s) => (
              <div key={s.id} className="bg-surface-container-lowest rounded-xl p-3 text-xs">
                <div className="font-semibold text-on-surface truncate">{s.goal || s.id}</div>
                <div className="text-on-surface-variant mt-1">{s.schedule}</div>
                <div className="text-on-surface-variant/70 mt-1">run: {s.run_count}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </aside>
  );
}

function QuickAction({ icon, label, onClick }: { icon: string; label: string; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="group w-full bg-surface-container-lowest p-3 rounded-xl flex items-center gap-3 hover:bg-white hover:shadow-sm transition-all cursor-pointer text-left"
    >
      <div className="w-8 h-8 rounded-lg bg-primary-container/30 text-primary flex items-center justify-center shrink-0">
        <span className="material-symbols-outlined text-base">{icon}</span>
      </div>
      <span className="text-sm font-medium text-on-surface">{label}</span>
      <span className="material-symbols-outlined text-base text-on-surface-variant ml-auto opacity-0 group-hover:opacity-100 transition-opacity">arrow_forward</span>
    </button>
  );
}
