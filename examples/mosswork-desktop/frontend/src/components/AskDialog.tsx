import { useState } from "react";
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
    <div className="fixed inset-0 z-100 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-on-surface/20 backdrop-blur-sm"
        onClick={onDismiss}
      />

      {/* Dialog */}
      <div
        className="relative w-full max-w-md mx-4 bg-surface-container-lowest rounded-2xl animate-fade-in shadow-botanical-dialog"
      >
        {/* Header */}
        <div className="flex items-center gap-3 px-5 pt-5 pb-3">
          <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-tertiary-container text-on-tertiary-container">
            <span className="material-symbols-outlined">contact_support</span>
          </div>
          <div>
            <p className="text-sm font-bold text-on-surface">
              Agent 需要你的确认
            </p>
            <p className="text-xs text-on-surface-variant mt-0.5">
              {data.type === "confirm" ? "确认操作" : "请提供输入"}
            </p>
          </div>
        </div>

        {/* Prompt */}
        <div className="px-5 py-3">
          <p className="text-sm text-on-surface leading-relaxed whitespace-pre-wrap">
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
                  className="px-4 py-2 rounded-xl text-sm font-bold bg-primary-container text-on-primary-container hover:bg-primary-container/80 active:scale-[0.97] transition-all"
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
                  "flex-1 px-3.5 py-2 rounded-xl text-sm bg-surface-container border border-outline-variant/40 text-on-surface",
                  "placeholder:text-on-surface-variant/50",
                  "focus:outline-none focus:border-primary/40 focus:ring-1 focus:ring-primary/20"
                )}
              />
              <button
                onClick={handleSubmit}
                disabled={!input.trim()}
                className={cn(
                  "px-4 py-2 rounded-xl text-sm font-bold transition-all",
                  input.trim()
                    ? "bg-primary text-on-primary hover:opacity-90 active:scale-[0.97]"
                    : "bg-surface-container text-on-surface-variant/40"
                )}
              >
                确认
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
