import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/cn.ts";
import type { ChatMessage as ChatMessageType, ToolExecution } from "@/lib/types.ts";

interface ChatMessageProps {
  message: ChatMessageType;
}

export default function ChatMessage({ message }: ChatMessageProps) {
  const { role, content, streaming, tools } = message;

  if (role === "system") {
    return (
      <div className="flex items-start gap-2 py-2 animate-fade-in">
        <span className="material-symbols-outlined text-sm text-error mt-0.5 shrink-0">warning</span>
        <p className="text-sm text-error/80">{content}</p>
      </div>
    );
  }

  const isUser = role === "user";

  if (isUser) {
    return (
      <div className="flex flex-col items-end animate-fade-in">
        <div className="bg-surface-container-high rounded-2xl rounded-tr-none p-4 max-w-[80%]">
          <p className="text-on-surface font-medium leading-relaxed text-sm">{content}</p>
        </div>
        <span className="text-[10px] text-on-surface-variant mt-2 font-medium tracking-wider uppercase">
          {new Date(message.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
        </span>
      </div>
    );
  }

  // Assistant message
  return (
    <div className="flex gap-6 animate-fade-in">
      <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center shrink-0 mt-1 shadow-sm">
        <span className="material-symbols-outlined text-on-primary text-sm">auto_awesome</span>
      </div>
      <div className="flex-1 space-y-4 min-w-0">
        {/* Tool executions */}
        {tools && tools.length > 0 && (
          <div className="space-y-1.5">
            {tools.map((tool, i) => (
              <ToolChip key={i} tool={tool} />
            ))}
          </div>
        )}

        {/* Message content */}
        {content && (
          <div className={cn("prose-chat text-sm leading-relaxed", streaming && "typing-cursor")}>
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  );
}

function ToolChip({ tool }: { tool: ToolExecution }) {
  return (
    <div
      className={cn(
        "inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-bold",
        tool.status === "running"
          ? "bg-tertiary-container text-on-tertiary-container"
          : tool.status === "done"
          ? "bg-primary-container/50 text-on-primary-container"
          : "bg-error-container/20 text-error"
      )}
    >
      {tool.status === "running" && (
        <span className="material-symbols-outlined text-sm animate-spin-1s">refresh</span>
      )}
      {tool.status === "done" && (
        <span className="material-symbols-outlined text-sm">check_circle</span>
      )}
      {tool.status === "error" && (
        <span className="material-symbols-outlined text-sm">error</span>
      )}
      <span>{tool.name}</span>
    </div>
  );
}
