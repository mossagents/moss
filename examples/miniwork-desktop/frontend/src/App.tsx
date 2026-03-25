import { useState, useCallback, useRef, useEffect } from "react";
import { useWailsEvent } from "@/lib/events";
import { ChatService } from "@/lib/api";
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
import ChatArea from "@/components/ChatArea";
import MessageInput from "@/components/MessageInput";
import AskDialog from "@/components/AskDialog";
import WorkerPanel from "@/components/WorkerPanel";

let msgCounter = 0;
function nextId() {
  return `msg-${++msgCounter}-${Date.now()}`;
}

export default function App() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [askData, setAskData] = useState<AskData | null>(null);
  const [workerState, setWorkerState] = useState<WorkerState | null>(null);

  // Track the current streaming assistant message id
  const streamingIdRef = useRef<string | null>(null);

  // Load config on mount
  useEffect(() => {
    ChatService.getConfig()
      .then((c: any) => setConfig(c as AppConfig))
      .catch(() => {});
  }, []);

  // ─── Event handlers ───────────────────────────────

  const appendAssistantChunk = useCallback((content: string) => {
    setMessages((prev) => {
      if (streamingIdRef.current) {
        return prev.map((m) =>
          m.id === streamingIdRef.current
            ? { ...m, content: m.content + content }
            : m,
        );
      }
      // Create new streaming message
      const id = nextId();
      streamingIdRef.current = id;
      return [
        ...prev,
        {
          id,
          role: "assistant",
          content,
          timestamp: Date.now(),
          streaming: true,
          tools: [],
        },
      ];
    });
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

  // ─── Render ───────────────────────────────────────

  return (
    <div className="flex h-full w-full bg-surface-dim">
      {/* Title bar drag region */}
      <div className="wails-drag fixed top-0 left-0 right-0 h-8 z-50" />

      <Sidebar
        config={config}
        isRunning={isRunning}
        onNewSession={handleNewSession}
      />

      <main className="flex flex-col flex-1 min-w-0">
        {/* Chat area */}
        <div className="flex-1 overflow-hidden relative">
          <ChatArea messages={messages} isRunning={isRunning} />
        </div>

        {/* Worker panel */}
        {workerState && workerState.tasks.length > 0 && (
          <WorkerPanel state={workerState} />
        )}

        {/* Input */}
        <MessageInput
          onSend={handleSend}
          onStop={handleStop}
          isRunning={isRunning}
        />
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
