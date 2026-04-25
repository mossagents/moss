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
import type { ChatMessage, SkillInfo, ToolExecution } from "@/lib/types.ts";
import { cn } from "@/lib/cn.ts";

interface AssistantThreadAreaProps {
  showTypingIndicator: boolean;
  statusText?: string;
  onArtifact?: (html: string) => void;
  skills: SkillInfo[];
  onRetryMessage: (message: ChatMessage) => void;
}

function parseSkillLikeInvocation(content: string): { name: string; task: string } | null {
  const match = content.match(/^Use skill or tool '([^']+)' to complete this request:\s*\n?([\s\S]*)$/);
  if (!match) return null;
  return {
    name: match[1].trim(),
    task: match[2].trim(),
  };
}

function extractArtifact(content: string): { cleaned: string; artifact: string | null } {
  const match = content.match(/<artifact>([\s\S]*?)<\/artifact>/i);
  if (!match) return { cleaned: content, artifact: null };
  return {
    cleaned: content.replace(/<artifact>[\s\S]*?<\/artifact>/gi, "").trim(),
    artifact: match[1],
  };
}

export default function AssistantThreadArea({ showTypingIndicator, statusText, onArtifact, skills, onRetryMessage }: AssistantThreadAreaProps) {
  // Track the last user message seen during message rendering so AssistantMessage
  // can offer a retry action (which logically retries from the preceding user turn).
  let lastRetryableUserMsg: ChatMessage | null = null;

  return (
    <ThreadPrimitive.Root className="h-full">
      <ThreadPrimitive.Viewport className="h-full overflow-y-auto px-4 md:px-8 pt-6 pb-4">
        <AuiIf condition={(s) => s.thread.isEmpty}>
          <div className="flex flex-col items-center justify-center h-full select-none pb-8">
            {/* Logo circle */}
            <div className="w-20 h-20 rounded-full border-2 border-outline-variant/50 flex items-center justify-center mb-5 shrink-0">
              <img src="/logo.png" alt="Moss" className="w-12 h-12 object-contain" />
            </div>
            <h1 className="text-3xl font-bold text-on-surface mb-2 font-headline">你好，MOSS！</h1>
            <p className="text-sm text-on-surface-variant mb-8">
              我是你的 AI 助手，有什么可以帮你？
            </p>
            {/* Feature cards 2×2 */}
            <div className="grid grid-cols-2 gap-3 w-full max-w-lg">
              <FeatureCard icon="description" title="分析文件" desc="上传文件，分析内容" />
              <FeatureCard icon="code" title="生成代码" desc="编写代码片段" />
              <FeatureCard icon="lightbulb" title="头脑风暴" desc="激发创意和想法" />
              <FeatureCard icon="security" title="解决问题" desc="寻求解决方案" />
            </div>
          </div>
        </AuiIf>

        <div className="max-w-4xl mx-auto space-y-12">
          <ThreadPrimitive.Messages>
            {({ message }) => {
              if (message.role === "user") {
                return (
                  <UserMessage
                    skills={skills}
                    onRetryMessage={onRetryMessage}
                    onCapture={(m) => { lastRetryableUserMsg = m; }}
                  />
                );
              }
              if (message.role === "assistant") {
                const userMsg = lastRetryableUserMsg;
                return (
                  <AssistantMessage
                    onArtifact={onArtifact}
                    retryUserMessage={userMsg}
                    onRetryMessage={onRetryMessage}
                  />
                );
              }
              return <SystemMessage />;
            }}
          </ThreadPrimitive.Messages>

          {showTypingIndicator && (
            <div className="flex gap-6 animate-fade-in">
              <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center shrink-0 mt-1 shadow-sm">
                <span className="material-symbols-outlined text-on-primary text-sm">auto_awesome</span>
              </div>
              <div className="flex items-center gap-2 py-3">
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d0 shrink-0" />
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d150 shrink-0" />
                <span className="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce bounce-d300 shrink-0" />
                {statusText && (
                  <span className="text-sm text-on-surface-variant ml-1">{statusText}</span>
                )}
              </div>
            </div>
          )}
        </div>
      </ThreadPrimitive.Viewport>
    </ThreadPrimitive.Root>
  );
}

function UserMessage({ skills, onRetryMessage, onCapture }: { skills: SkillInfo[]; onRetryMessage: (message: ChatMessage) => void; onCapture: (m: ChatMessage) => void }) {
  const raw = useAuiState((s) => getExternalStoreMessage<ChatMessage>(s.message));
  const rawMessage = Array.isArray(raw) ? raw[0] : raw;
  const text = useMessage((s) => {
    const textPart = s.content.find((part) => part.type === "text");
    return textPart?.type === "text" ? textPart.text : "";
  });

  // Capture this user message so the following AssistantMessage can reference it for retry.
  if (rawMessage) onCapture(rawMessage);

  const skillInvocation = parseSkillLikeInvocation(text);
  const matchedSkill = skillInvocation
    ? skills.find((skill) => skill.name === skillInvocation.name)
    : undefined;

  return (
    <MessagePrimitive.Root data-role="user" className="flex flex-col items-end animate-fade-in">
      <div className="bg-surface-container-high rounded-2xl rounded-tr-none p-4 max-w-[80%]">
        {skillInvocation ? (
          <div className="space-y-2 min-w-[16rem]">
            <div className="inline-flex max-w-full items-center gap-1.5 rounded-lg bg-surface-container-lowest px-3 py-2">
              <span className="text-xs font-medium text-on-surface truncate">
                {matchedSkill?.name ?? skillInvocation.name}
              </span>
            </div>
            {skillInvocation.task && (
              <p className="text-on-surface font-medium leading-relaxed text-sm whitespace-pre-wrap">{skillInvocation.task}</p>
            )}
          </div>
        ) : (
          <MessagePrimitive.Parts>
            {({ part }) => {
              if (part.type !== "text") return null;
              return <p className="text-on-surface font-medium leading-relaxed text-sm whitespace-pre-wrap">{part.text}</p>;
            }}
          </MessagePrimitive.Parts>
        )}
      </div>
      <div className="mt-2 flex items-center justify-end gap-2">
        <MessageMetaTime />
      </div>
    </MessagePrimitive.Root>
  );
}

function ThinkingBlock({ text, streaming, label }: { text: string; streaming: boolean; label?: string }) {
  const [open, setOpen] = useState(streaming); // auto-open while streaming, collapse when done
  const isResearch = !!label;

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
          {streaming ? "autorenew" : (isResearch ? "manage_search" : "psychology")}
        </span>
        <span className="font-medium">
          {streaming ? (isResearch ? "研究中..." : "思考中...") : (label ?? "思考过程")}
        </span>
        <span className="material-symbols-outlined text-sm ml-auto">
          {open ? "expand_less" : "expand_more"}
        </span>
      </button>
      {open && (
        <div className="px-4 pb-4 pt-2 text-on-surface-variant/80 leading-[1.8] whitespace-pre-wrap text-[13px] max-h-64 overflow-y-auto border-t border-outline-variant/30">
          {text}
        </div>
      )}
    </div>
  );
}

function AssistantMessage({ onArtifact, retryUserMessage, onRetryMessage }: {
  onArtifact?: (html: string) => void;
  retryUserMessage?: ChatMessage | null;
  onRetryMessage?: (message: ChatMessage) => void;
}) {
  const raw = useAuiState((s) => getExternalStoreMessage<ChatMessage>(s.message));
  const rawMessages = Array.isArray(raw) ? raw : raw ? [raw] : [];
  const isStreaming = useMessage((s) => s.status?.type === "running");
  const [copied, setCopied] = useState(false);

  const fullContent = rawMessages.map((m) => m.content || "").join("");
  const fullThinking = rawMessages.map((m) => m.thinking || "").join("");
  const { artifact, cleaned } = extractArtifact(fullContent);

  useEffect(() => {
    if (artifact && !isStreaming && onArtifact) {
      onArtifact(artifact);
    }
  }, [artifact, isStreaming, onArtifact]);

  const handleCopy = () => {
    if (!cleaned) return;
    navigator.clipboard.writeText(cleaned).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  const canRetry = !isStreaming && retryUserMessage?.retryable && retryUserMessage.historyIndex != null;

  return (
    <MessagePrimitive.Root data-role="assistant" className="flex gap-6 animate-fade-in">
      <div className="w-8 h-8 rounded-lg overflow-hidden shrink-0 mt-1 shadow-sm">
        <img src="/logo.png" alt="Moss" className="w-full h-full object-cover" />
      </div>
      <div className="flex-1 space-y-3 min-w-0">
        {fullThinking && (
          <ThinkingBlock
            text={fullThinking}
            streaming={isStreaming && !fullContent}
            label={!fullContent ? "研究过程" : undefined}
          />
        )}

        {/* Tool chips — shown before text when tools precede any content */}
        {rawMessages.flatMap((m) => m.tools ?? []).map((tool, i) => (
          <ToolChip key={i} tool={tool} />
        ))}

        {/* Main text — always rendered as a single ReactMarkdown for correct markdown context */}
        {(() => {
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

        {/* Action buttons: copy + retry — shown only when done streaming */}
        {!isStreaming && cleaned && (
          <div className="flex items-center gap-1 pt-1">
            <button
              type="button"
              onClick={handleCopy}
              title="复制回复"
              className={cn(
                "inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-[11px] leading-none",
                "text-on-surface-variant/70 transition-all",
                "hover:bg-surface-container-lowest hover:text-primary opacity-70 hover:opacity-100",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/20",
              )}
            >
              <span className="material-symbols-outlined text-[12px] leading-none">
                {copied ? "check" : "content_copy"}
              </span>
              {copied ? "已复制" : "复制"}
            </button>
            {canRetry && (
              <button
                type="button"
                onClick={() => onRetryMessage?.(retryUserMessage!)}
                title="从该消息重新执行"
                className={cn(
                  "inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-[11px] leading-none",
                  "text-on-surface-variant/70 transition-all",
                  "hover:bg-surface-container-lowest hover:text-primary opacity-70 hover:opacity-100",
                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/20",
                )}
              >
                <span className="material-symbols-outlined text-[12px] leading-none">restart_alt</span>
                重试
              </button>
            )}
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

function FeatureCard({ icon, title, desc }: { icon: string; title: string; desc: string }) {
  return (
    <div className="bg-surface-container-lowest rounded-2xl p-4 flex flex-col gap-1.5 border border-outline-variant/20 hover:bg-surface-container transition-colors cursor-default select-none">
      <span className="material-symbols-outlined text-2xl text-primary">{icon}</span>
      <span className="text-sm font-semibold text-on-surface">{title}</span>
      <span className="text-xs text-on-surface-variant leading-snug">{desc}</span>
    </div>
  );
}

function MessageMetaTime() {
  const createdAt = useMessage((s) => s.createdAt);
  return (
    <span className="text-[11px] leading-none text-on-surface-variant/70 tabular-nums">
      {createdAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
    </span>
  );
}

function tryPrettyJson(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

function ToolChip({ tool }: { tool: ToolExecution }) {
  const hasContent = !!(tool.input || tool.result);
  const [open, setOpen] = useState(false);

  // Auto-expand when content arrives while running; collapse when done
  useEffect(() => {
    if (tool.status === "running" && (tool.input || tool.result)) setOpen(true);
    else if (tool.status !== "running") setOpen(false);
  }, [tool.status, tool.input, tool.result]);

  const statusColor =
    tool.status === "running"
      ? "bg-tertiary-container text-on-tertiary-container border-tertiary-container"
      : tool.status === "done"
        ? "bg-primary-container/40 text-on-primary-container border-primary-container/40"
        : "bg-error-container/20 text-error border-error-container/30";

  return (
    <div className={cn("rounded-xl border text-xs overflow-hidden", statusColor)}>
      {/* Header row — always visible */}
      <div
        className={cn(
          "flex items-start gap-2 px-3 py-1.5",
          hasContent && "cursor-pointer select-none hover:brightness-95 transition-all",
        )}
        onClick={() => hasContent && setOpen((o) => !o)}
      >
        {tool.status === "running" && (
          <span className="material-symbols-outlined text-sm animate-spin-1s shrink-0 mt-0.5">refresh</span>
        )}
        {tool.status === "done" && (
          <span className="material-symbols-outlined text-sm shrink-0 mt-0.5">check_circle</span>
        )}
        {tool.status === "error" && (
          <span className="material-symbols-outlined text-sm shrink-0 mt-0.5">error</span>
        )}
        <div className="flex-1 min-w-0">
          <span className="font-bold font-mono">{tool.name}</span>
          {tool.summary && (
            <p className="opacity-60 font-mono truncate mt-0.5">{tool.summary}</p>
          )}
        </div>
        {hasContent && (
          <span className="material-symbols-outlined text-sm shrink-0 opacity-60 mt-0.5">
            {open ? "expand_less" : "expand_more"}
          </span>
        )}
      </div>

      {/* Expanded body */}
      {open && (
        <div className="border-t border-current/10 divide-y divide-current/10">
          {tool.input && (
            <div className="px-3 py-2">
              <p className="text-[10px] font-bold uppercase tracking-wider opacity-60 mb-1">参数</p>
              <pre className="font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all opacity-80 max-h-32 overflow-y-auto">
                {tryPrettyJson(tool.input)}
              </pre>
            </div>
          )}
          {tool.result && (
            <div className="px-3 py-2">
              <p className="text-[10px] font-bold uppercase tracking-wider opacity-60 mb-1">结果</p>
              <pre className="font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all opacity-80 max-h-40 overflow-y-auto">
                {tool.result}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
