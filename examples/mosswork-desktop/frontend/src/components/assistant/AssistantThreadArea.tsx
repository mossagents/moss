import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  AuiIf,
  MessagePrimitive,
  ThreadPrimitive,
  MessagePartPrimitive,
  useMessage,
  useAuiState,
  getExternalStoreMessage,
} from "@assistant-ui/react";
import type { ChatMessage, ToolExecution } from "@/lib/types";
import { cn } from "@/lib/cn";

interface AssistantThreadAreaProps {
  showTypingIndicator: boolean;
}

export default function AssistantThreadArea({ showTypingIndicator }: AssistantThreadAreaProps) {
  return (
    <ThreadPrimitive.Root className="h-full">
      <ThreadPrimitive.Viewport className="h-full overflow-y-auto px-4 md:px-8 pt-8 pb-4">
        <AuiIf condition={(s) => s.thread.isEmpty}>
          <div className="flex flex-col items-center justify-center h-full select-none">
            <div className="w-16 h-16 rounded-2xl bg-primary flex items-center justify-center mb-5 shadow-botanical-empty">
              <span className="material-symbols-outlined text-on-primary text-3xl">auto_awesome</span>
            </div>
            <h2 className="text-xl font-bold text-on-surface mb-1.5 font-headline">你好，有什么可以帮你？</h2>
            <p className="text-sm text-on-surface-variant max-w-sm text-center leading-relaxed">
              我可以帮你完成任务、分析文件、编写代码等。
              <br />
              请在下方输入你的需求。
            </p>
          </div>
        </AuiIf>

        <div className="max-w-4xl mx-auto space-y-12">
          <ThreadPrimitive.Messages>
            {({ message }) => {
              if (message.role === "user") return <UserMessage />;
              if (message.role === "assistant") return <AssistantMessage />;
              return <SystemMessage />;
            }}
          </ThreadPrimitive.Messages>

          {showTypingIndicator && (
            <div className="flex gap-6 animate-fade-in">
              <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center shrink-0 mt-1 shadow-sm">
                <span className="material-symbols-outlined text-on-primary text-sm">auto_awesome</span>
              </div>
              <div className="flex items-center gap-1.5 py-3">
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d0" />
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d150" />
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d300" />
              </div>
            </div>
          )}
        </div>
      </ThreadPrimitive.Viewport>
    </ThreadPrimitive.Root>
  );
}

function UserMessage() {
  return (
    <MessagePrimitive.Root data-role="user" className="flex flex-col items-end animate-fade-in">
      <div className="bg-surface-container-high rounded-2xl rounded-tr-none p-4 max-w-[80%]">
        <MessagePrimitive.Parts>
          {({ part }) => {
            if (part.type !== "text") return null;
            return <p className="text-on-surface font-medium leading-relaxed text-sm whitespace-pre-wrap">{part.text}</p>;
          }}
        </MessagePrimitive.Parts>
      </div>
      <MessageMetaTime />
    </MessagePrimitive.Root>
  );
}

function AssistantMessage() {
  const raw = useAuiState((s) => getExternalStoreMessage<ChatMessage>(s.message));
  const rawMessages = Array.isArray(raw) ? raw : raw ? [raw] : [];
  const content = rawMessages.map((m) => m.content || "").join("");
  const tools = rawMessages.flatMap((m) => m.tools || []);
  const isStreaming = useMessage((s) => s.status?.type === "running");

  return (
    <MessagePrimitive.Root data-role="assistant" className="flex gap-6 animate-fade-in">
      <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center shrink-0 mt-1 shadow-sm">
        <span className="material-symbols-outlined text-on-primary text-sm">auto_awesome</span>
      </div>
      <div className="flex-1 space-y-4 min-w-0">
        {tools.length > 0 && (
          <div className="space-y-1.5">
            {tools.map((tool, i) => (
              <ToolChip key={`${tool.name}-${i}`} tool={tool} />
            ))}
          </div>
        )}

        {content && (
          <div className={cn("prose-chat text-sm leading-relaxed", isStreaming && "typing-cursor")}>
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
            {isStreaming && <span className="inline-block ml-1">▋</span>}
          </div>
        )}

        {/* Keep primitive parts mounted so assistant-ui runtime state remains active. */}
        <div className="hidden">
          <MessagePrimitive.Parts>
            {() => (
              <MessagePartPrimitive.InProgress>
                <span />
              </MessagePartPrimitive.InProgress>
            )}
          </MessagePrimitive.Parts>
        </div>
      </div>
    </MessagePrimitive.Root>
  );
}

function SystemMessage() {
  return (
    <MessagePrimitive.Root data-role="system" className="flex items-start gap-2 py-2 animate-fade-in">
      <span className="material-symbols-outlined text-sm text-error mt-0.5 shrink-0">warning</span>
      <div className="text-sm text-error/80">
        <MessagePrimitive.Parts>
          {({ part }) => {
            if (part.type !== "text") return null;
            return <span>{part.text}</span>;
          }}
        </MessagePrimitive.Parts>
      </div>
    </MessagePrimitive.Root>
  );
}

function MessageMetaTime() {
  const createdAt = useMessage((s) => s.createdAt);
  return (
    <span className="text-[10px] text-on-surface-variant mt-2 font-medium tracking-wider uppercase">
      {createdAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
    </span>
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
            : "bg-error-container/20 text-error",
      )}
    >
      {tool.status === "running" && <span className="material-symbols-outlined text-sm animate-spin-1s">refresh</span>}
      {tool.status === "done" && <span className="material-symbols-outlined text-sm">check_circle</span>}
      {tool.status === "error" && <span className="material-symbols-outlined text-sm">error</span>}
      <span>{tool.name}</span>
    </div>
  );
}

