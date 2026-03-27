interface TopBarProps {
  onNewSession: () => void;
  onOffload: () => void;
  onShowDashboard: () => void;
  currentSessionId?: string;
}

export default function TopBar({ onNewSession, onOffload, onShowDashboard, currentSessionId }: TopBarProps) {
  const shortSession = currentSessionId ? currentSessionId.slice(0, 10) : "—";

  return (
    <header className="fixed top-0 right-80 left-64 h-16 z-40 flex justify-between items-center px-8 bg-white/80 backdrop-blur-md border-botanical-bottom">
      <div className="flex items-center gap-4">
        <div className="text-xs text-on-surface-variant">
          当前会话：<span className="font-semibold text-on-surface">{shortSession}</span>
        </div>
        <button
          onClick={onShowDashboard}
          className="text-xs px-3 py-1.5 rounded-full bg-surface-container-low text-on-surface hover:bg-surface-container-high transition-colors"
        >
          仪表盘
        </button>
        <button
          onClick={onOffload}
          className="text-xs px-3 py-1.5 rounded-full bg-surface-container-low text-on-surface hover:bg-surface-container-high transition-colors"
        >
          Offload
        </button>
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={onNewSession}
          className="bg-primary-container text-on-primary-container px-4 py-1.5 rounded-full text-xs font-bold hover:opacity-80 transition-opacity"
        >
          新建
        </button>
        <button className="text-on-surface-variant hover:text-primary transition-colors opacity-80 hover:opacity-100">
          <span className="material-symbols-outlined">notifications</span>
        </button>
      </div>
    </header>
  );
}
