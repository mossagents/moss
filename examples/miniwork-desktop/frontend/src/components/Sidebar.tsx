import { Bot, Plus, Cpu, FolderOpen } from "lucide-react";
import type { AppConfig } from "@/lib/types";
import { cn } from "@/lib/cn";

interface SidebarProps {
  config: AppConfig | null;
  isRunning: boolean;
  onNewSession: () => void;
}

export default function Sidebar({ config, isRunning, onNewSession }: SidebarProps) {
  return (
    <aside className="flex flex-col w-[260px] min-w-[260px] bg-surface border-r border-border pt-10 select-none">
      {/* Brand */}
      <div className="flex items-center gap-2.5 px-5 pb-5">
        <div className="flex items-center justify-center w-9 h-9 rounded-xl bg-accent/15 text-accent">
          <Bot size={20} />
        </div>
        <div>
          <h1 className="text-[15px] font-semibold text-slate-100 leading-tight">
            Moss Desktop
          </h1>
          <p className="text-[11px] text-slate-500 leading-tight mt-0.5">
            AI Agent Workspace
          </p>
        </div>
      </div>

      {/* New session button */}
      <div className="px-4 mb-4">
        <button
          onClick={onNewSession}
          disabled={isRunning}
          className={cn(
            "flex items-center gap-2 w-full px-3.5 py-2.5 rounded-xl text-sm font-medium transition-all",
            "bg-accent/10 text-accent hover:bg-accent/20 active:scale-[0.98]",
            isRunning && "opacity-40 pointer-events-none",
          )}
        >
          <Plus size={16} />
          新对话
        </button>
      </div>

      {/* Divider */}
      <div className="mx-5 border-t border-border" />

      {/* Config info */}
      <div className="flex-1 overflow-y-auto px-5 pt-4 space-y-3.5">
        <p className="text-[10px] uppercase tracking-wider text-slate-500 font-semibold">
          配置
        </p>

        {config ? (
          <>
            <ConfigItem
              icon={<Cpu size={14} />}
              label="服务商"
              value={config.provider || "—"}
            />
            <ConfigItem
              icon={<Bot size={14} />}
              label="模型"
              value={config.model || "—"}
            />
            <ConfigItem
              icon={<FolderOpen size={14} />}
              label="工作区"
              value={
                config.workspace
                  ? config.workspace.replace(/\\/g, "/").split("/").pop() || config.workspace
                  : "—"
              }
            />
          </>
        ) : (
          <p className="text-xs text-slate-600">加载中…</p>
        )}
      </div>

      {/* Status bar */}
      <div className="px-5 py-3.5 border-t border-border">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              "w-2 h-2 rounded-full",
              isRunning
                ? "bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.5)]"
                : "bg-slate-600",
            )}
          />
          <span className="text-[11px] text-slate-500">
            {isRunning ? "Agent 运行中" : "空闲"}
          </span>
        </div>
      </div>
    </aside>
  );
}

function ConfigItem({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
}) {
  return (
    <div className="flex items-start gap-2.5">
      <span className="text-slate-500 mt-0.5 shrink-0">{icon}</span>
      <div className="min-w-0">
        <p className="text-[10px] text-slate-500 leading-none mb-0.5">{label}</p>
        <p className="text-[13px] text-slate-300 truncate">{value}</p>
      </div>
    </div>
  );
}
