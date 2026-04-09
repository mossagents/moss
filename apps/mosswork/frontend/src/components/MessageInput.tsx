import { useState, useRef, useCallback } from "react";
import { Square } from "lucide-react";
import { cn } from "@/lib/cn.ts";
import { FileService } from "@/lib/api.ts";

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
    const el = e.target;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 192) + "px";
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
    <div className="absolute bottom-0 left-0 right-0 p-8 bg-linear-to-t from-background via-background to-transparent pointer-events-none">
      <div className="max-w-4xl mx-auto pointer-events-auto">
        {/* Attached files */}
        {files.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mb-2">
            {files.map((f, i) => (
              <span
                key={i}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full bg-primary-container/60 text-on-primary-container text-xs font-bold"
              >
                {f.replace(/\\/g, "/").split("/").pop()}
                <button
                  onClick={() => setFiles((prev) => prev.filter((_, idx) => idx !== i))}
                  className="ml-0.5 text-on-primary-container/50 hover:text-on-primary-container"
                >
                  ×
                </button>
              </span>
            ))}
          </div>
        )}

        {/* Input card */}
        <div
          className={cn(
            "bg-surface-container-high rounded-2xl p-2 flex items-end gap-2 transition-all duration-300 shadow-botanical-input",
            "focus-within:bg-surface-container-lowest focus-within:ring-2 focus-within:ring-primary-container"
          )}
        >
          {/* Action buttons */}
          <div className="flex gap-1 pb-1 px-1">
            <button
              onClick={handleAttach}
              className="p-2 hover:bg-surface-container-highest rounded-xl text-on-surface-variant transition-colors"
              title="附加文件"
            >
              <span className="material-symbols-outlined text-xl">attach_file</span>
            </button>
            <button
              className="p-2 hover:bg-surface-container-highest rounded-xl text-on-surface-variant transition-colors"
              title="语音输入"
            >
              <span className="material-symbols-outlined text-xl">mic</span>
            </button>
          </div>

          {/* Textarea */}
          <textarea
            ref={textareaRef}
            value={text}
            onChange={handleInput}
            onKeyDown={handleKeyDown}
            placeholder="描述一个场景… (Shift+Enter 换行)"
            rows={1}
            className="flex-1 bg-transparent border-none outline-none focus:ring-0 py-3 text-on-surface placeholder:text-on-surface-variant/50 resize-none max-h-48 text-sm"
          />

          {/* Right controls */}
          <div className="flex items-center gap-2 p-1">
            {/* Send / Stop */}
            {isRunning ? (
              <button
                onClick={onStop}
                className="w-10 h-10 bg-error text-on-error rounded-xl flex items-center justify-center shadow-sm hover:opacity-90 active:scale-95 transition-all"
                title="停止"
              >
                <Square size={16} fill="currentColor" />
              </button>
            ) : (
              <button
                onClick={handleSend}
                disabled={!text.trim() && files.length === 0}
                className={cn(
                  "w-10 h-10 bg-primary text-on-primary rounded-xl flex items-center justify-center shadow-sm transition-all active:scale-95",
                  (!text.trim() && files.length === 0) ? "opacity-40" : "hover:opacity-90"
                )}
                title="发送"
              >
                <span className="material-symbols-outlined text-xl">arrow_upward</span>
              </button>
            )}
          </div>
        </div>

        <p className="text-[10px] text-center mt-3 text-on-surface-variant opacity-60 font-medium">
          mosswork 可能会犯错，请验证重要信息。
        </p>
      </div>
    </div>
  );
}
