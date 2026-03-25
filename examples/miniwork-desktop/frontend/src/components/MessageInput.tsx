import { useState, useRef, useCallback } from "react";
import { Send, Square, Paperclip } from "lucide-react";
import { cn } from "@/lib/cn";
import { FileService } from "@/lib/api";

interface MessageInputProps {
  onSend: (content: string, files?: string[]) => void;
  onStop: () => void;
  isRunning: boolean;
}

export default function MessageInput({
  onSend,
  onStop,
  isRunning,
}: MessageInputProps) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState<string[]>([]);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = useCallback(() => {
    const trimmed = text.trim();
    if (!trimmed && files.length === 0) return;
    onSend(trimmed, files.length > 0 ? files : undefined);
    setText("");
    setFiles([]);
    // Reset textarea height
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  }, [text, files, onSend]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (!isRunning) handleSend();
    }
  };

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setText(e.target.value);
    // Auto-resize
    const el = e.target;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 160) + "px";
  };

  const handleAttach = async () => {
    try {
      const picked = await FileService.openFiles();
      if (picked?.length) {
        setFiles((prev) => [...prev, ...picked]);
      }
    } catch {}
  };

  return (
    <div className="border-t border-border bg-surface px-4 md:px-8 py-3">
      <div className="max-w-3xl mx-auto">
        {/* Attached files */}
        {files.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mb-2">
            {files.map((f, i) => (
              <span
                key={i}
                className="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-accent/10 text-accent text-xs"
              >
                {f.replace(/\\/g, "/").split("/").pop()}
                <button
                  onClick={() =>
                    setFiles((prev) => prev.filter((_, idx) => idx !== i))
                  }
                  className="ml-0.5 text-accent/50 hover:text-accent"
                >
                  ×
                </button>
              </span>
            ))}
          </div>
        )}

        <div className="flex items-end gap-2">
          {/* Attach button */}
          <button
            onClick={handleAttach}
            className="flex items-center justify-center shrink-0 w-9 h-9 rounded-xl text-slate-500 hover:text-slate-300 hover:bg-surface-hover transition-colors"
            title="附加文件"
          >
            <Paperclip size={18} />
          </button>

          {/* Textarea */}
          <div className="flex-1 relative">
            <textarea
              ref={textareaRef}
              value={text}
              onChange={handleInput}
              onKeyDown={handleKeyDown}
              placeholder="输入消息… (Shift+Enter 换行)"
              rows={1}
              className={cn(
                "w-full resize-none rounded-xl border border-border bg-surface-bright/60",
                "px-4 py-2.5 text-[14px] text-slate-200 placeholder:text-slate-500",
                "focus:outline-none focus:border-accent/40 focus:ring-1 focus:ring-accent/20",
                "transition-colors",
              )}
            />
          </div>

          {/* Send / Stop button */}
          {isRunning ? (
            <button
              onClick={onStop}
              className="flex items-center justify-center shrink-0 w-9 h-9 rounded-xl bg-rose-500/15 text-rose-400 hover:bg-rose-500/25 transition-colors"
              title="停止"
            >
              <Square size={16} />
            </button>
          ) : (
            <button
              onClick={handleSend}
              disabled={!text.trim() && files.length === 0}
              className={cn(
                "flex items-center justify-center shrink-0 w-9 h-9 rounded-xl transition-all",
                text.trim() || files.length > 0
                  ? "bg-accent text-white hover:bg-accent/80 active:scale-95"
                  : "bg-surface-hover text-slate-600",
              )}
              title="发送"
            >
              <Send size={16} />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
