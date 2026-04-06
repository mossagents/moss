import type { AutomationTask } from "@/lib/types";

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

interface AutomationViewProps {
  tasks: AutomationTask[];
  onAdd: () => void;
  onRemove: (id: string) => void;
  onRunNow: (id: string) => void;
}

export default function AutomationView({ tasks, onAdd, onRemove, onRunNow }: AutomationViewProps) {
  return (
    <div className="h-full flex flex-col bg-background">
      {/* Header */}
      <div className="px-8 pt-12 pb-6 border-b border-border flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-on-surface font-headline">自动化任务</h1>
          <p className="text-sm text-on-surface-variant mt-1">创建和管理定时 AI 任务</p>
        </div>
        <button
          onClick={onAdd}
          className="flex items-center gap-2 px-4 py-2.5 bg-primary text-on-primary rounded-xl font-bold text-sm shadow-sm hover:opacity-90 active:scale-95 transition-all"
        >
          <span className="material-symbols-outlined text-base">add</span>
          新建自动化
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-8 py-6">
        {tasks.length === 0 ? (
          <EmptyState onAdd={onAdd} />
        ) : (
          <div className="space-y-3 max-w-3xl">
            {tasks.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                onRemove={() => onRemove(task.id)}
                onRunNow={() => onRunNow(task.id)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function TaskCard({
  task,
  onRemove,
  onRunNow,
}: {
  task: AutomationTask;
  onRemove: () => void;
  onRunNow: () => void;
}) {
  return (
    <div className="bg-surface-container rounded-2xl p-5 border border-border hover:border-primary/30 transition-colors">
      <div className="flex items-start justify-between gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1.5">
            <span className="font-bold text-on-surface text-sm">{task.id}</span>
            <span className="px-2 py-0.5 rounded-full bg-primary-container/60 text-on-primary-container text-xs font-bold">
              {scheduleLabel(task.schedule)}
            </span>
            {task.run_count > 0 && (
              <span className="text-xs text-on-surface-variant">已运行 {task.run_count} 次</span>
            )}
          </div>
          <p className="text-sm text-on-surface-variant line-clamp-2">{task.goal}</p>
          {task.last_run && (
            <p className="text-xs text-on-surface-variant/60 mt-2">
              上次运行：{task.last_run}
              {task.next_run && <span className="ml-3">下次运行：{task.next_run}</span>}
            </p>
          )}
        </div>

        <div className="flex gap-2 shrink-0">
          <button
            onClick={onRunNow}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-bold text-primary border border-primary/30 rounded-lg hover:bg-primary/10 transition-colors"
            title="立即执行"
          >
            <span className="material-symbols-outlined text-sm">play_arrow</span>
            立即执行
          </button>
          <button
            onClick={onRemove}
            className="p-1.5 text-error/60 hover:text-error hover:bg-error-container/20 rounded-lg transition-colors"
            title="删除"
          >
            <span className="material-symbols-outlined text-sm">delete</span>
          </button>
        </div>
      </div>
    </div>
  );
}

function EmptyState({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[400px] select-none">
      <div className="w-20 h-20 rounded-2xl bg-surface-container-high flex items-center justify-center mb-5 shadow-sm">
        <span className="material-symbols-outlined text-4xl text-on-surface-variant/40">schedule</span>
      </div>
      <h3 className="text-lg font-bold text-on-surface mb-2">还没有自动化任务</h3>
      <p className="text-sm text-on-surface-variant text-center max-w-xs mb-6">
        创建定时 AI 任务，让 AI 按计划自动执行工作。
      </p>
      <button
        onClick={onAdd}
        className="flex items-center gap-2 px-5 py-2.5 bg-primary text-on-primary rounded-xl font-bold text-sm shadow-sm hover:opacity-90 active:scale-95 transition-all"
      >
        <span className="material-symbols-outlined text-base">add</span>
        新建第一个自动化任务
      </button>
    </div>
  );
}
