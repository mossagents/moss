import { useState } from "react";
import { MessageCircleQuestion } from "lucide-react";
import { cn } from "@/lib/cn";
import type { AskData } from "@/lib/types";

interface AskDialogProps {
  data: AskData;
  onRespond: (response: string) => void;
  onDismiss: () => void;
}

export default function AskDialog({ data, onRespond, onDismiss }: AskDialogProps) {
  const [input, setInput] = useState("");

  const handleSubmit = () => {
    const val = input.trim();
    if (val) onRespond(val);
  };

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        onClick={onDismiss}
      />

      {/* Dialog */}
      <div className="relative w-full max-w-md mx-4 bg-surface border border-border-bright rounded-2xl shadow-2xl animate-fade-in">
        {/* Header */}
        <div className="flex items-center gap-3 px-5 pt-5 pb-3">
          <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-amber-500/12 text-amber-400">
            <MessageCircleQuestion size={20} />
          </div>
          <div>
            <p className="text-sm font-semibold text-slate-200">
              Agent 需要你的确认
            </p>
            <p className="text-xs text-slate-500 mt-0.5">
              {data.type === "confirm" ? "确认操作" : "请提供输入"}
            </p>
          </div>
        </div>

        {/* Prompt */}
        <div className="px-5 py-3">
          <p className="text-sm text-slate-300 leading-relaxed whitespace-pre-wrap">
            {data.prompt}
          </p>
        </div>

        {/* Options or text input */}
        <div className="px-5 pb-5">
          {data.options && data.options.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {data.options.map((opt) => (
                <button
                  key={opt}
                  onClick={() => onRespond(opt)}
                  className={cn(
                    "px-4 py-2 rounded-xl text-sm font-medium transition-all",
                    "bg-accent/10 text-accent hover:bg-accent/20 active:scale-[0.97]",
                  )}
                >
                  {opt}
                </button>
              ))}
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <input
                type="text"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleSubmit();
                  if (e.key === "Escape") onDismiss();
                }}
                placeholder="输入回复…"
                autoFocus
                className={cn(
                  "flex-1 px-3.5 py-2 rounded-xl text-sm",
                  "bg-surface-bright border border-border text-slate-200",
                  "placeholder:text-slate-500",
                  "focus:outline-none focus:border-accent/40 focus:ring-1 focus:ring-accent/20",
                )}
              />
              <button
                onClick={handleSubmit}
                disabled={!input.trim()}
                className={cn(
                  "px-4 py-2 rounded-xl text-sm font-medium transition-all",
                  input.trim()
                    ? "bg-accent text-white hover:bg-accent/80 active:scale-[0.97]"
                    : "bg-surface-hover text-slate-600",
                )}
              >
                确认
              </button>
            </div>
          )}

          {/* Dismiss link */}
          <button
            onClick={onDismiss}
            className="mt-3 text-xs text-slate-500 hover:text-slate-400 transition-colors"
          >
            跳过
          </button>
        </div>
      </div>
    </div>
  );
}
