import { ComposerPrimitive } from "@assistant-ui/react";

interface AssistantComposerBarProps {
  isRunning: boolean;
  onStop: () => void;
  attachmentFiles: string[];
  onAttachFiles: () => void;
  onRemoveAttachment: (index: number) => void;
}

export default function AssistantComposerBar({
  isRunning,
  onStop,
  attachmentFiles,
  onAttachFiles,
  onRemoveAttachment,
}: AssistantComposerBarProps) {
  return (
    <div className="absolute bottom-0 left-0 right-0 p-8 bg-linear-to-t from-background via-background to-transparent pointer-events-none">
      <div className="max-w-4xl mx-auto pointer-events-auto">
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

        <ComposerPrimitive.Root className="bg-surface-container-high rounded-2xl p-2 flex items-end gap-2 transition-all duration-300 shadow-botanical-input focus-within:bg-surface-container-lowest focus-within:ring-2 focus-within:ring-primary-container">
          <div className="flex gap-1 pb-1 px-1">
            <button
              type="button"
              onClick={onAttachFiles}
              className="p-2 hover:bg-surface-container-highest rounded-xl text-on-surface-variant transition-colors"
              title="附加文件"
            >
              <span className="material-symbols-outlined text-xl">attach_file</span>
            </button>
            <button
              type="button"
              className="p-2 hover:bg-surface-container-highest rounded-xl text-on-surface-variant/60 transition-colors cursor-not-allowed"
              title="语音输入"
              disabled
            >
              <span className="material-symbols-outlined text-xl">mic</span>
            </button>
          </div>

          <ComposerPrimitive.Input
            asChild
            submitMode="enter"
            placeholder="描述一个场景... (Shift+Enter 换行)"
            className="flex-1"
          >
            <textarea
              className="flex-1 bg-transparent border-none outline-none focus:ring-0 py-3 text-on-surface placeholder:text-on-surface-variant/50 resize-none max-h-48 text-sm"
              rows={1}
              placeholder="描述一个场景... (Shift+Enter 换行)"
              aria-label="消息输入框"
              title="消息输入框"
            />
          </ComposerPrimitive.Input>

          <div className="flex items-center gap-2 p-1">
            {isRunning ? (
              <button
                type="button"
                onClick={onStop}
                className="w-10 h-10 bg-error text-on-error rounded-xl flex items-center justify-center shadow-sm hover:opacity-90 active:scale-95 transition-all"
                title="停止"
              >
                <span className="material-symbols-outlined text-lg">stop</span>
              </button>
            ) : (
              <ComposerPrimitive.Send
                asChild
                className="w-10 h-10 bg-primary text-on-primary rounded-xl flex items-center justify-center shadow-sm hover:opacity-90 active:scale-95 transition-all"
              >
                <button type="submit" title="发送">
                  <span className="material-symbols-outlined text-lg">arrow_upward</span>
                </button>
              </ComposerPrimitive.Send>
            )}
          </div>
        </ComposerPrimitive.Root>
      </div>
    </div>
  );
}
