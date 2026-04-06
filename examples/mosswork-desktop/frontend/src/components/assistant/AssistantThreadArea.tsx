import { useEffect, useState } from "react";
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
  onArtifact?: (html: string) => void;
}

function extractArtifact(content: string): { cleaned: string; artifact: string | null } {
  const match = content.match(/<artifact>([\s\S]*?)<\/artifact>/i);
  if (!match) return { cleaned: content, artifact: null };
  return {
    cleaned: content.replace(/<artifact>[\s\S]*?<\/artifact>/gi, "").trim(),
    artifact: match[1],
  };
}

export default function AssistantThreadArea({ showTypingIndicator, onArtifact }: AssistantThreadAreaProps) {
  return (
    <ThreadPrimitive.Root className="h-full">
      <ThreadPrimitive.Viewport className="h-full overflow-y-auto px-4 md:px-8 pt-8 pb-4">
        <AuiIf condition={(s) => s.thread.isEmpty}>
          <div className="flex flex-col items-center justify-center h-full select-none">
            <div className="w-16 h-16 rounded-2xl overflow-hidden mb-5 shadow-botanical-empty shrink-0">
              <img src="/logo.png" alt="Moss" className="w-full h-full object-cover" />
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
              if (message.role === "assistant") return <AssistantMessage onArtifact={onArtifact} />;
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

function ThinkingBlock({ text, streaming }: { text: string; streaming: boolean }) {
  const [open, setOpen] = useState(streaming); // auto-open while streaming, collapse when done

  useEffect(() => {
    if (!streaming) setOpen(false);
  }, [streaming]);

  return (
    <div className="rounded-xl border border-outline-variant/40 overflow-hidden text-xs">
      <button
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-2 px-3 py-2 text-on-surface-variant hover:bg-surface-container/60 transition-colors text-left"
      >
        <span className={cn("material-symbols-outlined text-sm", streaming && "animate-spin-1s")}>
          {streaming ? "autorenew" : "psychology"}
        </span>
        <span className="font-medium">{streaming ? "思考中..." : "思考过程"}</span>
        <span className="material-symbols-outlined text-sm ml-auto">
          {open ? "expand_less" : "expand_more"}
        </span>
      </button>
      {open && (
        <div className="px-3 pb-3 pt-1 text-on-surface-variant/70 leading-relaxed whitespace-pre-wrap font-mono text-[11px] max-h-48 overflow-y-auto border-t border-outline-variant/30">
          {text}
        </div>
      )}
    </div>
  );
}

function AssistantMessage({ onArtifact }: { onArtifact?: (html: string) => void }) {
  const raw = useAuiState((s) => getExternalStoreMessage<ChatMessage>(s.message));
  const rawMessages = Array.isArray(raw) ? raw : raw ? [raw] : [];
  const isStreaming = useMessage((s) => s.status?.type === "running");

  const fullContent = rawMessages.map((m) => m.content || "").join("");
  const fullThinking = rawMessages.map((m) => m.thinking || "").join("");
  const { artifact } = extractArtifact(fullContent);

  useEffect(() => {
    if (artifact && !isStreaming && onArtifact) {
      onArtifact(artifact);
    }
  }, [artifact, isStreaming, onArtifact]);

  return (
    <MessagePrimitive.Root data-role="assistant" className="flex gap-6 animate-fade-in">
      <div className="w-8 h-8 rounded-lg overflow-hidden shrink-0 mt-1 shadow-sm">
        <img src="/logo.png" alt="Moss" className="w-full h-full object-cover" />
      </div>
      <div className="flex-1 space-y-3 min-w-0">
        {fullThinking && (
          <ThinkingBlock text={fullThinking} streaming={isStreaming && !fullContent} />
        )}

        {/* Tool chips — shown before text when tools precede any content */}
        {rawMessages.flatMap((m) => m.tools ?? []).map((tool, i) => (
          <ToolChip key={i} tool={tool} />
        ))}

        {/* Main text — always rendered as a single ReactMarkdown for correct markdown context */}
        {(() => {
          const { cleaned } = extractArtifact(fullContent);
          if (!cleaned) return null;
          return (
            <div className={cn("prose-chat text-sm leading-relaxed", isStreaming && "typing-cursor")}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{cleaned}</ReactMarkdown>
              {isStreaming && <span className="inline-block ml-1">▋</span>}
            </div>
          );
        })()}

        {artifact && !isStreaming && (
          <div className="flex items-center gap-2 px-3 py-2 rounded-xl bg-primary-container/30 text-on-primary-container text-xs font-bold">
            <span className="material-symbols-outlined text-sm">web</span>
            已生成界面 · 查看右侧面板
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
