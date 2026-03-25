import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { User, Bot, AlertTriangle, Wrench, Check, Loader2 } from "lucide-react";
import { cn } from "@/lib/cn";
import type { ChatMessage as ChatMessageType, ToolExecution } from "@/lib/types";

interface ChatMessageProps {
  message: ChatMessageType;
}

export default function ChatMessage({ message }: ChatMessageProps) {
  const { role, content, streaming, tools } = message;

  if (role === "system") {
    return (
      <div className="flex items-start gap-2 py-2 animate-fade-in">
        <AlertTriangle size={14} className="text-amber-400 mt-0.5 shrink-0" />
        <p className="text-sm text-amber-300/80">{content}</p>
      </div>
    );
  }

  const isUser = role === "user";

  return (
    <div
      className={cn(
        "flex gap-3 py-3 animate-fade-in",
        isUser ? "flex-row-reverse" : "flex-row",
      )}
    >
      {/* Avatar */}
      <div
        className={cn(
          "flex items-center justify-center shrink-0 w-8 h-8 rounded-xl mt-0.5",
          isUser
            ? "bg-blue-500/15 text-blue-400"
            : "bg-accent/12 text-accent",
        )}
      >
        {isUser ? <User size={15} /> : <Bot size={15} />}
      </div>

      {/* Content bubble */}
      <div
        className={cn(
          "min-w-0 max-w-[85%] rounded-2xl px-4 py-3 text-[14px]",
          isUser
            ? "bg-blue-500/12 text-slate-200 rounded-tr-md"
            : "bg-surface-bright text-slate-200 rounded-tl-md border border-border",
        )}
      >
        {/* Tool executions */}
        {tools && tools.length > 0 && (
          <div className="mb-2 space-y-1.5">
            {tools.map((tool, i) => (
              <ToolChip key={i} tool={tool} />
            ))}
          </div>
        )}

        {/* Message text */}
        {content && (
          <div className={cn("prose-chat", streaming && "typing-cursor")}>
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {content}
            </ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  );
}

function ToolChip({ tool }: { tool: ToolExecution }) {
  return (
    <div className="flex items-center gap-2 px-2.5 py-1.5 rounded-lg bg-black/20 border border-border text-xs">
      <Wrench size={12} className="text-slate-500 shrink-0" />
      <span className="text-slate-400 truncate">{tool.name}</span>
      <span className="ml-auto shrink-0">
        {tool.status === "running" && (
          <Loader2 size={12} className="text-accent animate-spin" />
        )}
        {tool.status === "done" && (
          <Check size={12} className="text-emerald-400" />
        )}
        {tool.status === "error" && (
          <AlertTriangle size={12} className="text-rose-400" />
        )}
      </span>
    </div>
  );
}
