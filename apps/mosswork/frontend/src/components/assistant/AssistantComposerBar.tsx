import { ComposerPrimitive } from "@assistant-ui/react";

interface AssistantComposerBarProps {
  isRunning: boolean;
  onStop: () => void;
  attachmentFiles: string[];
  onAttachFiles: () => void;
  onRemoveAttachment: (index: number) => void;
  inputRef: (node: HTMLTextAreaElement | null) => void;
}

export default function AssistantComposerBar({
  isRunning,
  onStop,
  attachmentFiles,
  onAttachFiles,
  onRemoveAttachment,
  inputRef,
}: AssistantComposerBarProps) {
  return (
    <div className="absolute bottom-0 left-0 right-0 px-6 pb-6 bg-linear-to-t from-background via-background to-transparent pointer-events-none">
      <div className="max-w-3xl mx-auto pointer-events-auto">
        {attachmentFiles.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mb-2">
            {attachmentFiles.map((f, i) => (
              <span
                key={`${f}-${i}`}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full bg-primary-container/60 text-on-primary-container text-xs font-bold"
              >
                {f.replace(/\\/g, "/").split("/").pop()}
                <button
                  type="button"
                  onClick={() => onRemoveAttachment(i)}
                  className="ml-0.5 text-on-primary-container/50 hover:text-on-primary-container"
                  title="移除附件"
                >
                  ×
                </button>
              </span>
            ))}
          </div>
        )}

        <ComposerPrimitive.Root className="bg-surface-container-high rounded-2xl shadow-botanical-input transition-all duration-300 focus-within:bg-surface-container-lowest focus-within:ring-2 focus-within:ring-primary-container">
          {/* Input area */}
          <ComposerPrimitive.Input
            asChild
            submitMode="enter"
            placeholder="输入你的问题或需求..."
            className="flex-1"
          >
            <textarea
              ref={inputRef}
              className="w-full bg-transparent border-none outline-none focus:ring-0 px-4 pt-4 pb-2 text-on-surface placeholder:text-on-surface-variant/50 resize-none max-h-48 text-sm block"
              rows={3}
              placeholder="输入你的问题或需求..."
              aria-label="消息输入框"
              title="消息输入框"
            />
          </ComposerPrimitive.Input>

          {/* Toolbar row */}
          <div className="flex items-center justify-between px-3 pb-3">
            {/* Left: action buttons */}
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={onAttachFiles}
                className="flex items-center justify-center w-8 h-8 hover:bg-surface-container-highest rounded-xl text-on-surface-variant transition-colors"
                title="附加文件"
              >
                <span className="material-symbols-outlined text-[18px]">attach_file</span>
              </button>
              <button
                type="button"
                className="flex items-center justify-center h-8 px-2 gap-0.5 hover:bg-surface-container-highest rounded-xl text-on-surface-variant/70 transition-colors text-xs font-medium"
                title="提及"
              >
                <span className="text-base leading-none">@</span>
              </button>
              <button
                type="button"
                className="flex items-center justify-center h-8 px-2 gap-1 hover:bg-surface-container-highest rounded-xl text-on-surface-variant/70 transition-colors text-xs font-medium"
                title="工具"
              >
                <span className="material-symbols-outlined text-[15px]">keyboard_command_key</span>
                <span>工具</span>
                <span className="material-symbols-outlined text-[15px]">expand_more</span>
              </button>
            </div>

            {/* Right: shortcut hint + send/stop */}
            <div className="flex items-center gap-2">
              {!isRunning && (
                <span className="text-[11px] text-on-surface-variant/40 flex items-center gap-0.5 select-none">
                  <span className="material-symbols-outlined text-sm">keyboard_return</span>
                  发送
                </span>
              )}
              {isRunning ? (
                <button
                  type="button"
                  onClick={onStop}
                  className="w-8 h-8 bg-error text-on-error rounded-full flex items-center justify-center shadow-sm hover:opacity-90 active:scale-95 transition-all"
                  title="停止"
                >
                  <span className="material-symbols-outlined text-base">stop</span>
                </button>
              ) : (
                <ComposerPrimitive.Send asChild>
                  <button
                    type="submit"
                    title="发送"
                    className="w-8 h-8 bg-primary text-on-primary rounded-full flex items-center justify-center shadow-sm hover:opacity-90 active:scale-95 transition-all"
                  >
                    <span className="material-symbols-outlined text-base">arrow_upward</span>
                  </button>
                </ComposerPrimitive.Send>
              )}
            </div>
          </div>
        </ComposerPrimitive.Root>
      </div>
    </div>
  );
}
