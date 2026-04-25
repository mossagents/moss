import { cn } from "@/lib/cn.ts";

interface NavSidebarProps {
  activeModule: "chat" | "automation" | "settings";
  onModuleChange: (m: "chat" | "automation" | "settings") => void;
}

export default function NavSidebar({ activeModule, onModuleChange }: NavSidebarProps) {
  return (
    <aside className="fixed left-0 top-0 bottom-0 w-14 bg-surface-container-high flex flex-col items-center pb-3 z-50 select-none border-r border-border">
      {/* Logo with wails drag region spacing */}
      <div className="pt-2 pb-1 flex flex-col items-center">
        <div className="w-9 h-9 rounded-xl overflow-hidden shadow-sm shrink-0">
          <img src="/logo.png" alt="Moss" className="w-full h-full object-cover" />
        </div>
        {/* Active indicator dot */}
        <div className="w-1.5 h-1.5 rounded-full bg-primary mt-1.5 shrink-0" />
      </div>

      {/* Navigation icons */}
      <div className="flex flex-col gap-1 flex-1 mt-3">
        <NavItem
          icon="chat"
          label="对话"
          active={activeModule === "chat"}
          onClick={() => onModuleChange("chat")}
        />
        <NavItem
          icon="schedule"
          label="自动化"
          active={activeModule === "automation"}
          onClick={() => onModuleChange("automation")}
        />
      </div>

      {/* Bottom: Settings + Avatar */}
      <div className="flex flex-col items-center gap-2 mt-auto">
        <NavItem
          icon="settings"
          label="设置"
          active={activeModule === "settings"}
          onClick={() => onModuleChange("settings")}
        />
        <div className="w-8 h-8 rounded-full bg-primary flex items-center justify-center text-on-primary text-sm font-bold shadow-sm">
          M
        </div>
      </div>
    </aside>
  );
}

function NavItem({
  icon,
  label,
  active,
  onClick,
}: {
  icon: string;
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      className={cn(
        "w-10 h-10 rounded-xl flex items-center justify-center transition-colors",
        active
          ? "bg-primary text-on-primary shadow-sm"
          : "text-on-surface-variant hover:bg-surface-container-highest",
      )}
    >
      <span className="material-symbols-outlined text-xl">{icon}</span>
    </button>
  );
}
