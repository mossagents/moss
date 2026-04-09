import { cn } from "@/lib/cn.ts";
import type { WorkerState, WorkerTask } from "@/lib/types.ts";

interface WorkerPanelProps {
  state: WorkerState;
}

export default function WorkerPanel({ state }: WorkerPanelProps) {
  const isCompleted = state.state === "completed";
  const total = state.tasks.length;
  const done = state.succeeded + state.failed;
  const pct = total > 0 ? (done / total) * 100 : 0;

  return (
    <div className="mx-4 md:mx-8 mb-2 animate-fade-in">
      <div className="max-w-4xl mx-auto">
        <div className="bg-surface-container-lowest rounded-2xl px-5 py-4 border border-outline-variant/20 shadow-sm">
          <div className="flex items-center justify-between mb-3">
            <p className="text-xs font-bold text-on-surface-variant uppercase tracking-wider">Agent 进度</p>
            <div className="flex items-center gap-3 text-xs text-on-surface-variant">
              {state.running > 0 && (
                <span className="flex items-center gap-1 text-primary">
                  <span className="material-symbols-outlined text-sm animate-spin-1s">refresh</span>
                  {state.running} 运行中
                </span>
              )}
              {state.succeeded > 0 && (
                <span className="flex items-center gap-1 text-primary">
                  <span className="material-symbols-outlined text-sm">check_circle</span>
                  {state.succeeded}
                </span>
              )}
              {state.failed > 0 && (
                <span className="flex items-center gap-1 text-error">
                  <span className="material-symbols-outlined text-sm">cancel</span>
                  {state.failed}
                </span>
              )}
            </div>
          </div>

          <div className="h-1.5 bg-surface-container-high rounded-full overflow-hidden mb-3">
            <div
              className={cn(
                "progress-fill h-full rounded-full transition-all duration-500",
                isCompleted && state.failed > 0 ? "bg-error" : "bg-primary",
              )}
              style={{ "--progress-width": `${pct}%` } as React.CSSProperties}
            />
          </div>

          <div className="flex flex-wrap gap-1.5">
            {state.tasks.map((task) => (
              <TaskChip key={task.id} task={task} />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function TaskChip({ task }: { task: WorkerTask }) {
  const status =
    task.status === "completed"
      ? "done"
      : task.status === "cancelled"
        ? "failed"
        : task.status;

  return (
    <div
      className={cn(
        "flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border transition-colors",
        status === "running"
          ? "bg-primary/8 border-primary/20 text-on-surface pulse-glow"
          : status === "done"
            ? "bg-primary-container/40 border-primary-container text-on-primary-container"
            : status === "failed"
              ? "bg-error-container/20 border-error-container/30 text-error"
              : "bg-surface-container border-outline-variant/30 text-on-surface-variant",
      )}
      title={task.error || task.description}
    >
      {status === "running" && <span className="material-symbols-outlined text-xs animate-spin-1s">refresh</span>}
      {status === "done" && <span className="material-symbols-outlined text-xs">check</span>}
      {status === "failed" && <span className="material-symbols-outlined text-xs">close</span>}
      {status === "queued" && <span className="material-symbols-outlined text-xs">schedule</span>}
      <span className="truncate max-w-45">{task.description || task.id}</span>
    </div>
  );
}
