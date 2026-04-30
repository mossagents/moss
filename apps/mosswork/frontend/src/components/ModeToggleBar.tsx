import { cn } from "@/lib/cn.ts";

export type ChatMode = "normal" | "swarm";

interface ModeToggleBarProps {
  mode: ChatMode;
  onChange: (mode: ChatMode) => void;
}

export default function ModeToggleBar({ mode, onChange }: ModeToggleBarProps) {
  return (
    <div className="flex items-center justify-center px-6 py-2 shrink-0 relative">
      <div className="flex items-center bg-surface-container rounded-full p-1">
        <ModeButton
          active={mode === "normal"}
          icon="chat_bubble"
          label="普通模式"
          onClick={() => onChange("normal")}
        />
        <ModeButton
          active={mode === "swarm"}
          icon="workspace_premium"
          label="Swarm 模式"
          onClick={() => onChange("swarm")}
        />
      </div>
      <button
        className="absolute right-6 p-2 rounded-xl text-on-surface-variant hover:bg-surface-container-high transition-colors"
        title="过滤/设置"
        type="button"
      >
        <span className="material-symbols-outlined text-xl">tune</span>
      </button>
    </div>
  );
}

function ModeButton({
  active,
  icon,
  label,
  onClick,
}: {
  active: boolean;
  icon: string;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-1.5 px-5 py-2 rounded-full text-sm font-medium transition-all",
        active
          ? "bg-white shadow-sm text-on-surface"
          : "text-on-surface-variant hover:text-on-surface",
      )}
    >
      <span className="material-symbols-outlined text-[18px]">{icon}</span>
      {label}
    </button>
  );
}
