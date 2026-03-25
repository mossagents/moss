interface TopBarProps {
  onNewSession: () => void;
}

export default function TopBar({ onNewSession }: TopBarProps) {
  return (
    <header className="fixed top-0 right-80 left-64 h-16 z-40 flex justify-between items-center px-8 bg-white/80 backdrop-blur-md border-botanical-bottom">
      {/* Left: search + nav */}
      <div className="flex items-center gap-6">
        <div className="flex items-center bg-surface-container-low px-4 py-1.5 rounded-full w-60">
          <span className="material-symbols-outlined text-on-surface-variant text-lg mr-2">search</span>
          <input
            className="bg-transparent border-none outline-none focus:ring-0 text-sm w-full placeholder:text-on-surface-variant/60 text-on-surface"
            placeholder="搜索…"
            type="text"
          />
        </div>
        <nav className="flex gap-6 items-center">
          <a
            href="#"
            className="text-on-surface-variant text-sm font-medium hover:text-primary transition-colors"
          >
            任务
          </a>
          <a
            href="#"
            className="text-on-surface-variant text-sm font-medium hover:text-primary transition-colors"
          >
            历史
          </a>
        </nav>
      </div>

      {/* Right: actions */}
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
        <button className="text-on-surface-variant hover:text-primary transition-colors opacity-80 hover:opacity-100">
          <span className="material-symbols-outlined">account_circle</span>
        </button>
      </div>
    </header>
  );
}
