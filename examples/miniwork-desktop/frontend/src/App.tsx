import { useState, useCallback, useRef, useEffect } from "react";
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
  type AppendMessage,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { useWailsEvent } from "@/lib/events";
import { ChatService, FileService } from "@/lib/api";
import type {
  ChatMessage,
  ToolExecution,
  AppConfig,
  WorkerState,
  AskData,
  StreamData,
  ToolStartData,
  ToolResultData,
  DoneData,
  ErrorData,
} from "@/lib/types";
import Sidebar from "@/components/Sidebar";
import AssistantThreadArea from "@/components/assistant/AssistantThreadArea";
import AssistantComposerBar from "@/components/assistant/AssistantComposerBar";
import AskDialog from "@/components/AskDialog";
import WorkerPanel from "@/components/WorkerPanel";
import TopBar from "@/components/TopBar";
import RightPanel from "@/components/RightPanel";

let msgCounter = 0;
function nextId() {
  return `msg-${++msgCounter}-${Date.now()}`;
}

function convertChatMessage(message: ChatMessage): ThreadMessageLike {
  type ThreadMessagePart = Exclude<ThreadMessageLike["content"], string>[number];
  const parts: ThreadMessagePart[] = [];
  if (message.content) {
    parts.push({ type: "text", text: message.content });
  }
  if (message.tools?.length) {
    message.tools.forEach((tool, i) => {
      parts.push({
        type: "tool-call",
        toolCallId: `${message.id}-tool-${i}`,
        toolName: tool.name,
        args: {},
        argsText: "{}",
        result: tool.result ? { output: tool.result } : undefined,
        isError: tool.status === "error",
      });
    });
  }

  const status =
    message.role === "assistant"
      ? message.streaming
        ? { type: "running" as const }
        : { type: "complete" as const, reason: "stop" as const }
      : undefined;

  return {
    id: message.id,
    role: message.role,
    content: parts.length > 0 ? parts : "",
    createdAt: new Date(message.timestamp),
    status,
  };
}

export default function App() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [askData, setAskData] = useState<AskData | null>(null);
  const [workerState, setWorkerState] = useState<WorkerState | null>(null);
  const [pendingFiles, setPendingFiles] = useState<string[]>([]);

  // Track the current streaming assistant message id
  const streamingIdRef = useRef<string | null>(null);
  const pendingFilesRef = useRef<string[]>([]);

  useEffect(() => {
    pendingFilesRef.current = pendingFiles;
  }, [pendingFiles]);

  // Load config on mount
  useEffect(() => {
    ChatService.getConfig()
      .then((c: any) => setConfig(c as AppConfig))
      .catch(() => {});
  }, []);

  // ─── Event handlers ───────────────────────────────

  const appendAssistantChunk = useCallback((content: string) => {
    const currentStreamingId = streamingIdRef.current;

    if (currentStreamingId) {
      setMessages((prev) =>
        prev.map((m) =>
          m.id === currentStreamingId
            ? { ...m, content: m.content + content }
            : m,
        ),
      );
      return;
    }

    // Create a new streaming message once and keep updater pure for StrictMode.
    const id = nextId();
    streamingIdRef.current = id;
    setMessages((prev) => [
      ...prev,
      {
        id,
        role: "assistant",
        content,
        timestamp: Date.now(),
        streaming: true,
        tools: [],
      },
    ]);
  }, []);

  useWailsEvent<StreamData>("chat:stream", (data) => {
    console.log("[chat:stream]", typeof data, data);
    appendAssistantChunk(data?.content ?? "");
  });

  useWailsEvent("chat:stream_end", () => {
    if (streamingIdRef.current) {
      setMessages((prev) =>
        prev.map((m) =>
          m.id === streamingIdRef.current ? { ...m, streaming: false } : m,
        ),
      );
      streamingIdRef.current = null;
    }
  });

  useWailsEvent<StreamData>("chat:text", (data) => {
    // Finalize any streaming then add text block
    streamingIdRef.current = null;
    const id = nextId();
    setMessages((prev) => [
      ...prev,
      { id, role: "assistant", content: data?.content ?? "", timestamp: Date.now() },
    ]);
  });

  useWailsEvent<ToolStartData>("chat:tool_start", (data) => {
    const toolName = data?.meta?.name || data?.content || "tool";
    setMessages((prev) => {
      const last = prev[prev.length - 1];
      if (last && last.role === "assistant") {
        const tools: ToolExecution[] = [
          ...(last.tools || []),
          { name: toolName, status: "running" },
        ];
        return [...prev.slice(0, -1), { ...last, tools }];
      }
      // Create a new assistant message with tool
      const id = nextId();
      return [
        ...prev,
        {
          id,
          role: "assistant",
          content: "",
          timestamp: Date.now(),
          tools: [{ name: toolName, status: "running" }],
        },
      ];
    });
  });

  useWailsEvent<ToolResultData>("chat:tool_result", (data) => {
    const toolName = data?.meta?.name || "";
    setMessages((prev) => {
      const last = prev[prev.length - 1];
      if (last && last.role === "assistant" && last.tools?.length) {
        const tools = last.tools.map((t) =>
          t.name === toolName && t.status === "running"
            ? { ...t, status: "done" as const, result: data?.content }
            : t,
        );
        return [...prev.slice(0, -1), { ...last, tools }];
      }
      return prev;
    });
  });

  useWailsEvent<string>("chat:progress", () => {
    // Progress events just maintain the running indicator
  });

  useWailsEvent<AskData>("chat:ask", (data) => {
    setAskData(data);
  });

  useWailsEvent<DoneData>("chat:done", (data) => {
    console.log("[chat:done]", data);

    const finalOutput = data?.output?.trim();
    const streamingId = streamingIdRef.current;

    setMessages((prev) => {
      // Always clear streaming flag on the current streaming message if present.
      if (streamingId) {
        let foundStreaming = false;
        const next = prev.map((m) => {
          if (m.id !== streamingId) return m;
          foundStreaming = true;
          return {
            ...m,
            content: finalOutput || m.content,
            streaming: false,
          };
        });

        if (foundStreaming) return next;
      }

      // Fallback: patch last assistant message, or append one if none exists.
      if (finalOutput) {
        for (let i = prev.length - 1; i >= 0; i--) {
          if (prev[i]?.role === "assistant") {
            const updated = [...prev];
            updated[i] = { ...updated[i], content: finalOutput, streaming: false };
            return updated;
          }
        }

        const id = nextId();
        return [
          ...prev,
          { id, role: "assistant", content: finalOutput, timestamp: Date.now(), streaming: false },
        ];
      }

      return prev;
    });

    streamingIdRef.current = null;
    setIsRunning(false);
    setWorkerState(null);
  });

  useWailsEvent<ErrorData>("chat:error", (data) => {
    console.error("[chat:error]", data);
    streamingIdRef.current = null;
    setIsRunning(false);
    const msg = data?.message || (typeof data === "string" ? data : "Unknown error");
    const id = nextId();
    setMessages((prev) => [
      ...prev,
      { id, role: "system", content: `Error: ${msg}`, timestamp: Date.now() },
    ]);
  });

  useWailsEvent("chat:cancelled", (data: any) => {
    console.log("[chat:cancelled]", data);
    streamingIdRef.current = null;
    setIsRunning(false);
    const id = nextId();
    setMessages((prev) => [
      ...prev,
      { id, role: "system", content: "\u5df2\u53d6\u6d88\u6267\u884c", timestamp: Date.now() },
    ]);
  });

  useWailsEvent<string>("worker:update", (data) => {
    try {
      const parsed = typeof data === "string" ? JSON.parse(data) : data;
      setWorkerState(parsed as WorkerState);
    } catch {
      // ignore parse errors
    }
  });

  // ─── User actions ─────────────────────────────────

  const handleSend = useCallback(
    async (content: string, files?: string[]) => {
      const id = nextId();
      setMessages((prev) => [
        ...prev,
        { id, role: "user", content, timestamp: Date.now() },
      ]);
      setIsRunning(true);
      try {
        if (files?.length) {
          await ChatService.sendMessageWithAttachments(content, files);
        } else {
          await ChatService.sendMessage(content);
        }
      } catch (err: any) {
        setIsRunning(false);
        const eid = nextId();
        setMessages((prev) => [
          ...prev,
          { id: eid, role: "system", content: `Failed: ${err?.message ?? err}`, timestamp: Date.now() },
        ]);
      }
    },
    [],
  );

  const handleStop = useCallback(async () => {
    try {
      await ChatService.stopAgent();
    } catch {}
  }, []);

  const handleNewSession = useCallback(async () => {
    try {
      await ChatService.newSession();
      setMessages([]);
      setWorkerState(null);
      setPendingFiles([]);
      streamingIdRef.current = null;
    } catch {}
  }, []);

  const handleAskResponse = useCallback(async (response: string) => {
    setAskData(null);
    try {
      await ChatService.respondToAsk(response);
    } catch {}
  }, []);

  const handleAskDismiss = useCallback(() => {
    setAskData(null);
    ChatService.respondToAsk("").catch(() => {});
  }, []);

  const handlePickAttachments = useCallback(async () => {
    try {
      const picked = await FileService.openFiles();
      if (picked?.length) {
        setPendingFiles((prev) => [...prev, ...picked]);
      }
    } catch {}
  }, []);

  const handleRemoveAttachment = useCallback((index: number) => {
    setPendingFiles((prev) => prev.filter((_, i) => i !== index));
  }, []);

  const handleAssistantNew = useCallback(
    async (message: AppendMessage) => {
      const text = message.content
        .filter((part): part is Extract<typeof part, { type: "text" }> => part.type === "text")
        .map((part) => part.text)
        .join("\n")
        .trim();

      if (!text) return;
      const files = pendingFilesRef.current;
      await handleSend(text, files.length > 0 ? files : undefined);
      setPendingFiles([]);
    },
    [handleSend],
  );

  const runtime = useExternalStoreRuntime<ChatMessage>({
    messages,
    convertMessage: convertChatMessage,
    isDisabled: isRunning,
    onNew: handleAssistantNew,
    onCancel: handleStop,
  });

  const hasStreamingAssistant = messages.some((m) => m.role === "assistant" && m.streaming);
  const lastRole = messages[messages.length - 1]?.role;
  const showTypingIndicator = isRunning && !hasStreamingAssistant && lastRole !== "assistant";

  // ─── Render ───────────────────────────────────────

  return (
    <div className="flex h-full w-full bg-background text-on-surface">
      {/* Title bar drag region */}
      <div className="wails-drag fixed top-0 left-0 right-0 h-8 z-60" />

      {/* Left sidebar */}
      <Sidebar
        config={config}
        isRunning={isRunning}
        onNewSession={handleNewSession}
      />

      {/* Right panel */}
      <RightPanel config={config} isRunning={isRunning} />

      {/* Main content */}
      <main className="absolute left-64 right-80 top-0 bottom-0 flex flex-col bg-background">
        {/* Top bar */}
        <TopBar onNewSession={handleNewSession} />

        <AssistantRuntimeProvider runtime={runtime}>
          {/* Chat area — starts below topbar, ends above input */}
          <div className="flex-1 overflow-hidden relative mt-16">
            <AssistantThreadArea showTypingIndicator={showTypingIndicator} />

            {/* Worker panel (overlays bottom of chat area) */}
            {workerState && workerState.tasks.length > 0 && (
              <div className="absolute bottom-0 left-0 right-0 pb-2">
                <WorkerPanel state={workerState} />
              </div>
            )}
          </div>

          {/* Input bar */}
          <div className="relative h-36 shrink-0">
            <AssistantComposerBar
              isRunning={isRunning}
              onStop={handleStop}
              attachmentFiles={pendingFiles}
              onAttachFiles={handlePickAttachments}
              onRemoveAttachment={handleRemoveAttachment}
            />
          </div>
        </AssistantRuntimeProvider>
      </main>

      {/* Ask dialog overlay */}
      {askData && (
        <AskDialog
          data={askData}
          onRespond={handleAskResponse}
          onDismiss={handleAskDismiss}
        />
      )}
    </div>
  );
}
