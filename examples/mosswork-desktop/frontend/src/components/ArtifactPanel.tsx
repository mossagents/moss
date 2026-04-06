interface ArtifactPanelProps {
  html: string;
  onClose: () => void;
}

export default function ArtifactPanel({ html, onClose }: ArtifactPanelProps) {
  return (
    <div className="fixed right-0 top-0 bottom-0 w-[400px] z-50 flex flex-col bg-surface shadow-2xl border-l border-border">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-surface-container-low shrink-0">
        <div className="flex items-center gap-2">
          <span className="material-symbols-outlined text-primary text-lg">web</span>
          <span className="font-bold text-sm text-on-surface">Agent 生成的界面</span>
        </div>
        <button
          onClick={onClose}
          className="p-1.5 rounded-lg text-on-surface-variant hover:bg-surface-container-high transition-colors"
          title="关闭"
        >
          <span className="material-symbols-outlined text-xl">close</span>
        </button>
      </div>
      <iframe
        className="flex-1 w-full border-none"
        sandbox="allow-scripts"
        srcDoc={html}
        title="Agent 生成的界面"
      />
    </div>
  );
}
