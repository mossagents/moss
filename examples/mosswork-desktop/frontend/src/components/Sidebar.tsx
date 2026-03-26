import { Plus } from "lucide-react";
import type { AppConfig } from "@/lib/types";
import { cn } from "@/lib/cn";

interface SidebarProps {
  config: AppConfig | null;
  isRunning: boolean;
  onNewSession: () => void;
}

export default function Sidebar({ config, isRunning, onNewSession }: SidebarProps) {
  const initials = config?.provider?.[0]?.toUpperCase() ?? "M";
  const modelLabel = config?.model || "AI Agent";
  const providerLabel = config?.provider || "mosswork";

  return (
    <aside className="h-screen w-64 fixed left-0 top-0 bg-surface-container flex flex-col p-4 overflow-y-auto z-50 select-none shadow-botanical-sidebar">
      {/* Brand */}
      <div className="mb-8 px-2 pt-4">
        <h1 className="text-2xl font-bold text-on-surface tracking-tight font-headline">mosswork</h1>
        <p className="text-xs text-on-surface-variant font-medium opacity-70">Botanical Intelligence</p>
      </div>

      {/* New Session button */}
      <button
        onClick={onNewSession}
        disabled={isRunning}
        className={cn(
          "mb-8 w-full py-3 px-4 bg-primary text-on-primary rounded-xl font-bold text-sm flex items-center justify-center gap-2 shadow-sm transition-transform active:scale-90",
          isRunning && "opacity-50 pointer-events-none"
        )}
      >
        <Plus size={18} />
        新对话
      </button>

      {/* Navigation */}
      <nav className="flex-1 space-y-1">
        <NavItem icon="local_library" label="Library" />
        <NavItem icon="history" label="Recents" />
        <NavItem icon="space_dashboard" label="Workspace" active />
        <NavItem icon="settings" label="Settings" />
      </nav>

      {/* User Profile */}
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
            isRunning ? "status-dot-active animate-pulse" : "status-dot-inactive"
          )}
        />
      </div>
    </aside>
  );
}

function NavItem({
  icon,
  label,
  active,
}: {
  icon: string;
  label: string;
  active?: boolean;
}) {
  return (
    <a
      href="#"
      className={cn(
        "flex items-center gap-3 px-4 py-3 rounded-xl transition-colors",
        active
          ? "bg-surface-container-lowest text-on-surface shadow-sm"
          : "text-on-surface-variant hover:bg-surface-container-lowest/50"
      )}
    >
      <span className="material-symbols-outlined text-xl">{icon}</span>
      <span className="font-bold text-base tracking-tight font-headline">{label}</span>
    </a>
  );
}
