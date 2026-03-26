import { cn } from "@/lib/cn";
import type { AppConfig } from "@/lib/types";

interface RightPanelProps {
  config: AppConfig | null;
  isRunning: boolean;
}

export default function RightPanel({ config, isRunning }: RightPanelProps) {
  const workspaceName = config?.workspace
    ? config.workspace.replace(/\\/g, "/").split("/").pop() || config.workspace
    : null;

  return (
    <aside className="fixed right-0 top-0 bottom-0 w-80 bg-surface-container-low h-screen p-6 overflow-y-auto z-30 select-none border-botanical-left">
      <div className="flex items-center justify-between mb-8 mt-2">
        <h2 className="font-bold text-on-surface tracking-tight text-base font-headline">
          资产面板
        </h2>
        <button className="text-on-surface-variant hover:text-primary transition-colors">
          <span className="material-symbols-outlined">more_horiz</span>
        </button>
      </div>

      <div className="space-y-6">
        {/* Agent Status */}
        <div>
          <span className="text-[10px] font-bold text-on-surface-variant/60 tracking-widest uppercase mb-3 block">
            Agent 状态
          </span>
          <div className="bg-surface-container-lowest rounded-xl p-4">
            <div className="flex items-center gap-3 mb-3">
              <div
                className={cn(
                  "w-3 h-3 rounded-full shrink-0",
                  isRunning ? "status-dot-active" : "status-dot-inactive"
                )}
              />
              <span className="text-sm font-bold text-on-surface">
                {isRunning ? "运行中" : "空闲"}
              </span>
            </div>
            {config && (
              <div className="space-y-1.5 text-xs text-on-surface-variant">
                <div className="flex justify-between">
                  <span>服务商</span>
                  <span className="font-medium text-on-surface">{config.provider || "—"}</span>
                </div>
                <div className="flex justify-between">
                  <span>模型</span>
                  <span className="font-medium text-on-surface truncate ml-2 max-w-30 text-right">{config.model || "—"}</span>
                </div>
                {workspaceName && (
                  <div className="flex justify-between">
                    <span>工作区</span>
                    <span className="font-medium text-on-surface truncate ml-2 max-w-30 text-right">{workspaceName}</span>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Quick Actions */}
        <div>
          <span className="text-[10px] font-bold text-on-surface-variant/60 tracking-widest uppercase mb-3 block">
            快捷操作
          </span>
          <div className="space-y-2">
            <QuickAction icon="folder_open" label="打开工作区" />
            <QuickAction icon="settings" label="配置设置" />
            <QuickAction icon="history" label="查看历史" />
          </div>
        </div>

        {/* Workspace Context Card */}
        <div className="bg-primary/5 rounded-2xl p-5">
          <div className="flex items-center gap-2 mb-3">
            <div className="w-5 h-5 rounded-full bg-primary/20 flex items-center justify-center">
              <div
                className={cn(
                  "w-2.5 h-2.5 rounded-full bg-primary",
                  isRunning && "animate-pulse"
                )}
              />
            </div>
            <h4 className="text-xs font-bold text-on-primary-container">工作区上下文</h4>
          </div>
          <p className="text-xs text-on-surface-variant leading-relaxed">
            {workspaceName
              ? `当前工作区：${workspaceName}`
              : "暂未配置工作区，Agent 将在默认上下文中运行。"}
          </p>
          <div className="mt-4 flex items-center justify-between text-[10px] font-bold">
            <span className="text-primary">{isRunning ? "运行中" : "就绪"}</span>
            <span className="text-on-surface-variant/60">本地模式</span>
          </div>
        </div>
      </div>
    </aside>
  );
}

function QuickAction({ icon, label }: { icon: string; label: string }) {
  return (
    <button className="group w-full bg-surface-container-lowest p-3 rounded-xl flex items-center gap-3 hover:bg-white hover:shadow-sm transition-all cursor-pointer text-left">
      <div className="w-8 h-8 rounded-lg bg-primary-container/30 text-primary flex items-center justify-center shrink-0">
        <span className="material-symbols-outlined text-base">{icon}</span>
      </div>
      <span className="text-sm font-medium text-on-surface">{label}</span>
      <span className="material-symbols-outlined text-base text-on-surface-variant ml-auto opacity-0 group-hover:opacity-100 transition-opacity">arrow_forward</span>
    </button>
  );
}
