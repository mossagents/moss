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
  ChatMessagePart,
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
  AutomationTask,
} from "@/lib/types";
import NavSidebar from "@/components/NavSidebar";
import ChatSidebar from "@/components/ChatSidebar";
import AssistantThreadArea from "@/components/assistant/AssistantThreadArea";
import AssistantComposerBar from "@/components/assistant/AssistantComposerBar";
import AskDialog from "@/components/AskDialog";
import WorkerPanel from "@/components/WorkerPanel";
import ArtifactPanel from "@/components/ArtifactPanel";
import AutomationView from "@/components/automation/AutomationView";
import AutomationFormModal from "@/components/automation/AutomationFormModal";
import SettingsView from "@/components/settings/SettingsView";

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
  const [module, setModule] = useState<"chat" | "automation" | "settings">("chat");
  const [artifact, setArtifact] = useState<string | null>(null);

  // Chat state
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [askData, setAskData] = useState<AskData | null>(null);
  const [workerState, setWorkerState] = useState<WorkerState | null>(null);
  const [pendingFiles, setPendingFiles] = useState<string[]>([]);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string | undefined>(undefined);

  // Automation state
  const [automationTasks, setAutomationTasks] = useState<AutomationTask[]>([]);
  const [showAutomationForm, setShowAutomationForm] = useState(false);

  const streamingIdRef = useRef<string | null>(null);
  const pendingFilesRef = useRef<string[]>([]);

  useEffect(() => {
    pendingFilesRef.current = pendingFiles;
  }, [pendingFiles]);

  useEffect(() => {
    ChatService.getConfig()
      .then((c: any) => setConfig(c as AppConfig))
      .catch(() => {});
  }, []);

  // Load automations on mount
  useEffect(() => {
    ChatService.getAutomations()
      .then((tasks: any) => {
        if (Array.isArray(tasks)) setAutomationTasks(tasks as AutomationTask[]);
      })
      .catch(() => {});
  }, []);

  const appendAssistantChunk = useCallback((textChunk: string) => {
    const addText = (parts: ChatMessagePart[], chunk: string): ChatMessagePart[] => {
      const last = parts[parts.length - 1];
      if (last?.type === "text") {
        return [...parts.slice(0, -1), { type: "text", text: (last.text ?? "") + chunk }];
      }
      return [...parts, { type: "text", text: chunk }];
    };

    if (streamingIdRef.current) {
      // Fast path: append to current turn's message
      const id = streamingIdRef.current;
      setMessages((prev) =>
        prev.map((m) => {
          if (m.id !== id) return m;
          return {
            ...m,
            content: m.content + textChunk,
            parts: addText(m.parts ?? [], textChunk),
            streaming: true,
          };
        }),
      );
      return;
    }

    // First token of a new turn: create message and set ref BEFORE setMessages
    // so subsequent events in the same microtask see it immediately.
    const id = nextId();
    streamingIdRef.current = id;
    setMessages((prev) => [
      ...prev,
      {
        id, role: "assistant", content: textChunk,
        parts: [{ type: "text", text: textChunk }],
        timestamp: Date.now(), streaming: true, tools: [],
      },
    ]);
  }, []);

  useWailsEvent<StreamData>("chat:stream", (data) => {
    appendAssistantChunk(data?.content ?? "");
  });

  useWailsEvent("chat:stream_end", () => {
    // Only mark the message as visually not-streaming.
    // Do NOT clear streamingIdRef — it stays set until chat:done/error/cancelled
    // so that subsequent stream tokens (after tool calls) append to the same bubble.
    if (streamingIdRef.current) {
      const id = streamingIdRef.current;
      setMessages((prev) =>
        prev.map((m) => m.id === id ? { ...m, streaming: false } : m),
      );
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

  // Reasoning/thinking content from models like DeepSeek-R1.
  // Aggregated into the SAME assistant message bubble as a collapsible section.
  useWailsEvent<StreamData>("chat:thinking", (data) => {
    const chunk = data?.content ?? "";
    if (!chunk) return;
    if (streamingIdRef.current) {
      const id = streamingIdRef.current;
      setMessages((prev) =>
        prev.map((m) =>
          m.id === id ? { ...m, thinking: (m.thinking ?? "") + chunk } : m,
        ),
      );
      return;
    }
    // First thinking token of a new turn — create the assistant message now
    const id = nextId();
    streamingIdRef.current = id;
    setMessages((prev) => [
      ...prev,
      {
        id, role: "assistant", content: "", thinking: chunk,
        parts: [], timestamp: Date.now(), streaming: true, tools: [],
      },
    ]);
  });

  useWailsEvent<ToolStartData>("chat:tool_start", (data) => {
    const toolName = data?.meta?.name || data?.content || "tool";
    const newTool: ToolExecution = { name: toolName, status: "running" };
    const currentId = streamingIdRef.current;
    setMessages((prev) => {
      const target = currentId
        ? prev.find((m) => m.id === currentId)
        : prev[prev.length - 1];
      if (target?.role === "assistant") {
        return prev.map((m) =>
          m.id === target.id
            ? {
                ...m,
                tools: [...(m.tools ?? []), newTool],
                parts: [...(m.parts ?? []), { type: "tool" as const, tool: newTool }],
              }
            : m,
        );
      }
      const id = nextId();
      if (!streamingIdRef.current) streamingIdRef.current = id;
      return [...prev, {
        id, role: "assistant", content: "", timestamp: Date.now(),
        tools: [newTool], parts: [{ type: "tool" as const, tool: newTool }],
      }];
    });
  });

  useWailsEvent<ToolResultData>("chat:tool_result", (data) => {
    const toolName = data?.meta?.name || "";
    const currentId = streamingIdRef.current;
    setMessages((prev) => {
      const target = currentId
        ? prev.find((m) => m.id === currentId)
        : prev[prev.length - 1];
      if (target?.role === "assistant") {
        const updateTool = (t: ToolExecution) =>
          t.name === toolName && t.status === "running"
            ? { ...t, status: "done" as const, result: data?.content }
            : t;
        const tools = (target.tools ?? []).map(updateTool);
        const parts = (target.parts ?? []).map((p) =>
          p.type === "tool" && p.tool ? { ...p, tool: updateTool(p.tool) } : p,
        );
        return prev.map((m) => m.id === target.id ? { ...m, tools, parts } : m);
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
    if (Array.isArray(data.schedules)) setAutomationTasks(data.schedules as AutomationTask[]);
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

  useWailsEvent<AutomationTask[]>("desktop:schedules", (data) => {
    if (Array.isArray(data)) setAutomationTasks(data);
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
    } catch {}
  }, []);

  const handleNewSession = useCallback(async () => {
    try {
      await ChatService.newSession();
      setMessages([]);
      setPendingFiles([]);
      setArtifact(null);
      streamingIdRef.current = null;
      setWorkerState(null);
    } catch {}
  }, []);

  const handleResumeSession = useCallback(
    async (id: string) => {
      await handleRunCommand(`/resume ${id}`);
    },
    [handleRunCommand],
  );

  const handleDeleteSession = useCallback(async (id: string) => {
    try {
      await ChatService.deleteSession(id);
      // If we deleted the current session, clear the chat view
      if (id === currentSessionId) {
        setMessages([]);
        setArtifact(null);
        streamingIdRef.current = null;
      }
    } catch {}
  }, [currentSessionId]);

  const handleDeleteSessions = useCallback(async (ids: string[]) => {
    try {
      await ChatService.deleteSessions(ids);
      if (ids.includes(currentSessionId ?? "")) {
        setMessages([]);
        setArtifact(null);
        streamingIdRef.current = null;
      }
    } catch {}
  }, [currentSessionId]);

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

  // Automation handlers
  const handleAddAutomation = useCallback(async (id: string, schedule: string, goal: string) => {
    try {
      await ChatService.addAutomation(id, schedule, goal);
      const tasks = await ChatService.getAutomations();
      if (Array.isArray(tasks)) setAutomationTasks(tasks as AutomationTask[]);
    } catch (err: any) {
      console.error("addAutomation failed:", err);
    }
  }, []);

  const handleRemoveAutomation = useCallback(async (id: string) => {
    try {
      await ChatService.removeAutomation(id);
      setAutomationTasks((prev) => prev.filter((t) => t.id !== id));
    } catch (err: any) {
      console.error("removeAutomation failed:", err);
    }
  }, []);

  const handleRunAutomationNow = useCallback(async (id: string) => {
    try {
      await ChatService.runAutomationNow(id);
    } catch (err: any) {
      console.error("runAutomationNow failed:", err);
    }
  }, []);

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

  // suppress unused warning for config
  void config;

  return (
    <div className="flex h-full w-full bg-background text-on-surface">
      <div className="wails-drag fixed top-0 left-0 right-0 h-8 z-60 pointer-events-none" />

      <NavSidebar activeModule={module} onModuleChange={setModule} />

      {module === "chat" && (
        <>
          <ChatSidebar
            sessions={sessions}
            currentSessionId={currentSessionId}
            onNewSession={handleNewSession}
            onResumeSession={handleResumeSession}
            onDeleteSession={handleDeleteSession}
            onDeleteSessions={handleDeleteSessions}
            isRunning={isRunning}
          />

          <main
            className="absolute top-0 bottom-0 flex flex-col"
            style={{
              left: "296px",
              right: artifact ? "400px" : "0",
            }}
          >
            <AssistantRuntimeProvider runtime={runtime}>
              <div className="flex-1 overflow-hidden relative mt-8">
                <AssistantThreadArea
                  showTypingIndicator={showTypingIndicator}
                  onArtifact={setArtifact}
                />

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

          {artifact && (
            <ArtifactPanel html={artifact} onClose={() => setArtifact(null)} />
          )}
        </>
      )}

      {module === "automation" && (
        <main className="absolute top-0 bottom-0" style={{ left: "56px", right: "0" }}>
          <AutomationView
            tasks={automationTasks}
            onAdd={() => setShowAutomationForm(true)}
            onRemove={handleRemoveAutomation}
            onRunNow={handleRunAutomationNow}
          />
        </main>
      )}

      {module === "settings" && <SettingsView />}

      {askData && (
        <AskDialog
          data={askData}
          onRespond={handleAskResponse}
          onDismiss={handleAskDismiss}
        />
      )}

      <AutomationFormModal
        open={showAutomationForm}
        onClose={() => setShowAutomationForm(false)}
        onSave={handleAddAutomation}
      />
    </div>
  );
}

