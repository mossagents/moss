import { CheckCircle2, Loader2, XCircle, Clock } from "lucide-react";
import { cn } from "@/lib/cn";
import type { WorkerState, WorkerTask } from "@/lib/types";

interface WorkerPanelProps {
  state: WorkerState;
}

export default function WorkerPanel({ state }: WorkerPanelProps) {
  const isCompleted = state.state === "completed";

  return (
    <div className="border-t border-border bg-surface px-4 md:px-8 py-3 animate-fade-in">
      <div className="max-w-3xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-2">
          <p className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
            Worker 进度
          </p>
          <div className="flex items-center gap-3 text-xs text-slate-500">
            {state.running > 0 && (
              <span className="flex items-center gap-1">
                <Loader2 size={11} className="animate-spin text-accent" />
                {state.running} 运行中
              </span>
            )}
            {state.succeeded > 0 && (
              <span className="flex items-center gap-1 text-emerald-400">
                <CheckCircle2 size={11} />
                {state.succeeded}
              </span>
            )}
            {state.failed > 0 && (
              <span className="flex items-center gap-1 text-rose-400">
                <XCircle size={11} />
                {state.failed}
              </span>
            )}
          </div>
        </div>

        {/* Progress bar */}
        <div className="h-1.5 bg-black/30 rounded-full overflow-hidden mb-2">
          <div
            className={cn(
              "h-full rounded-full transition-all duration-500",
              isCompleted
                ? state.failed > 0
                  ? "bg-amber-500"
                  : "bg-emerald-500"
                : "bg-accent",
            )}
            style={{
              width: `${
                state.tasks.length > 0
                  ? ((state.succeeded + state.failed) / state.tasks.length) * 100
                  : 0
              }%`,
            }}
          />
        </div>

        {/* Task list */}
        <div className="flex flex-wrap gap-1.5">
          {state.tasks.map((task) => (
            <TaskChip key={task.id} task={task} />
          ))}
        </div>
      </div>
    </div>
  );
}

function TaskChip({ task }: { task: WorkerTask }) {
  const statusIcon: Record<string, React.ReactNode> = {
    queued: <Clock size={11} className="text-slate-500" />,
    running: <Loader2 size={11} className="animate-spin text-accent" />,
    done: <CheckCircle2 size={11} className="text-emerald-400" />,
    failed: <XCircle size={11} className="text-rose-400" />,
  };

  return (
    <div
      className={cn(
        "flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs border transition-colors",
        task.status === "running"
          ? "bg-accent/8 border-accent/20 text-slate-300 pulse-glow"
          : "bg-black/15 border-border text-slate-400",
      )}
    >
      {statusIcon[task.status] || null}
      <span className="truncate max-w-[180px]">{task.description}</span>
    </div>
  );
}
