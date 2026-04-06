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
  DashboardState,
  SessionSummary,
  ScheduleEntry,
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

function normalizeWorkerState(input: any): WorkerState | null {
  if (!input || typeof input !== "object") return null;
  const tasks = Array.isArray(input.tasks)
    ? input.tasks.map((t: any) => ({
      id: String(t?.id ?? ""),
      description: String(t?.description ?? t?.id ?? ""),
      status: String(t?.status ?? "queued") as WorkerState["tasks"][number]["status"],
      steps: Number(t?.steps ?? 0),
      error: t?.error ? String(t.error) : undefined,
    }))
    : [];

  return {
    state: input.state === "running" ? "running" : "completed",
    running: Number(input.running ?? 0),
    succeeded: Number(input.succeeded ?? 0),
    failed: Number(input.failed ?? 0),
    tasks,
  };
}

export default function App() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [askData, setAskData] = useState<AskData | null>(null);
  const [workerState, setWorkerState] = useState<WorkerState | null>(null);
  const [pendingFiles, setPendingFiles] = useState<string[]>([]);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [schedules, setSchedules] = useState<ScheduleEntry[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string | undefined>(undefined);

  const streamingIdRef = useRef<string | null>(null);
  const pendingFilesRef = useRef<string[]>([]);

  useEffect(() => {
    pendingFilesRef.current = pendingFiles;
  }, [pendingFiles]);

  useEffect(() => {
    ChatService.getConfig()
      .then((c: any) => setConfig(c as AppConfig))
      .catch(() => { });
  }, []);

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

  useWailsEvent<AskData>("chat:ask", (data) => {
    setAskData(data);
  });

  useWailsEvent<DoneData>("chat:done", (data) => {
    const finalOutput = data?.output?.trim();
    const streamingId = streamingIdRef.current;

    setMessages((prev) => {
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

    if (data?.session_id) {
      setCurrentSessionId(data.session_id);
    }
    streamingIdRef.current = null;
    setIsRunning(false);
  });

  useWailsEvent<ErrorData>("chat:error", (data) => {
    streamingIdRef.current = null;
    setIsRunning(false);
    const msg = data?.message || (typeof data === "string" ? data : "Unknown error");
    const id = nextId();
    setMessages((prev) => [
      ...prev,
      { id, role: "system", content: `Error: ${msg}`, timestamp: Date.now() },
    ]);
  });

  useWailsEvent("chat:cancelled", () => {
    streamingIdRef.current = null;
    setIsRunning(false);
    const id = nextId();
    setMessages((prev) => [
      ...prev,
      { id, role: "system", content: "已取消执行", timestamp: Date.now() },
    ]);
  });

  useWailsEvent<DashboardState>("desktop:dashboard", (data) => {
    if (!data || typeof data !== "object") return;
    if (Array.isArray(data.sessions)) setSessions(data.sessions);
    if (Array.isArray(data.schedules)) setSchedules(data.schedules);
    if (typeof data.current_session_id === "string") setCurrentSessionId(data.current_session_id);
    const ws = normalizeWorkerState((data as DashboardState).worker);
    if (ws) setWorkerState(ws);
  });

  useWailsEvent<SessionSummary[]>("desktop:sessions", (data) => {
    if (Array.isArray(data)) setSessions(data);
  });

  useWailsEvent<{ session_id: string; title: string }>("session:title", (data) => {
    if (!data?.session_id || !data?.title) return;
    setSessions((prev) =>
      prev.map((s) => (s.id === data.session_id ? { ...s, title: data.title } : s)),
    );
  });

  useWailsEvent<ScheduleEntry[]>("desktop:schedules", (data) => {
    if (Array.isArray(data)) setSchedules(data);
  });

  useWailsEvent<any>("worker:update", (data) => {
    let raw = data;
    if (typeof data === "string") {
      try {
        raw = JSON.parse(data);
      } catch {
        raw = null;
      }
    }
    const ws = normalizeWorkerState(raw);
    if (ws) setWorkerState(ws);
  });

  const handleRunCommand = useCallback(async (cmd: string) => {
    setIsRunning(true);
    try {
      await ChatService.sendCommand(cmd);
    } catch (err: any) {
      setIsRunning(false);
      const id = nextId();
      setMessages((prev) => [
        ...prev,
        { id, role: "system", content: `Failed: ${err?.message ?? err}`, timestamp: Date.now() },
      ]);
    }
  }, []);

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
    } catch { }
  }, []);

  const handleNewSession = useCallback(async () => {
    try {
      await ChatService.newSession();
      setMessages([]);
      setPendingFiles([]);
      streamingIdRef.current = null;
      setWorkerState(null);
    } catch { }
  }, []);

  const handleResumeSession = useCallback(async (id: string) => {
    await handleRunCommand(`/resume ${id}`);
  }, [handleRunCommand]);

  const handleAskResponse = useCallback(async (response: string) => {
    setAskData(null);
    try {
      await ChatService.respondToAsk(response);
    } catch { }
  }, []);

  const handleAskDismiss = useCallback(() => {
    setAskData(null);
    ChatService.respondToAsk("").catch(() => { });
  }, []);

  const handlePickAttachments = useCallback(async () => {
    try {
      const picked = await FileService.openFiles();
      if (picked?.length) {
        setPendingFiles((prev) => [...prev, ...picked]);
      }
    } catch { }
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

  return (
    <div className="flex h-full w-full bg-background text-on-surface">
      <div className="wails-drag fixed top-0 left-0 right-0 h-8 z-60" />

      <Sidebar
        config={config}
        isRunning={isRunning}
        onNewSession={handleNewSession}
        sessions={sessions}
        currentSessionId={currentSessionId}
        onResumeSession={handleResumeSession}
      />

      <RightPanel
        config={config}
        isRunning={isRunning}
        sessions={sessions}
        schedules={schedules}
        onRunCommand={handleRunCommand}
      />

      <main className="absolute left-64 right-80 top-0 bottom-0 flex flex-col bg-background">
        <TopBar
          onNewSession={handleNewSession}
          onOffload={() => handleRunCommand("/offload 20 topbar")}
          onShowDashboard={() => handleRunCommand("/dashboard")}
          currentSessionId={currentSessionId}
          currentSessionTitle={sessions.find((s) => s.id === currentSessionId)?.title}
        />

        <AssistantRuntimeProvider runtime={runtime}>
          <div className="flex-1 overflow-hidden relative mt-16">
            <AssistantThreadArea showTypingIndicator={showTypingIndicator} />

            {workerState && workerState.tasks.length > 0 && (
              <div className="absolute bottom-0 left-0 right-0 pb-2">
                <WorkerPanel state={workerState} />
              </div>
            )}
          </div>

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
