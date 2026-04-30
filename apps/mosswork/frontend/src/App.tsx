import { useState, useCallback, useRef, useEffect } from "react";
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
  type AppendMessage,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { useWailsEvent } from "@/lib/events.ts";
import { ChatService, FileService } from "@/lib/api.ts";
import type {
  ChatMessage,
  ChatMessagePart,
  ToolExecution,
  AppConfig,
  AskData,
  StreamData,
  ToolStartData,
  ToolResultData,
  DoneData,
  ErrorData,
  DashboardState,
  SessionSummary,
  AutomationTask,
  MessageRole,
  SkillInfo,
} from "@/lib/types.ts";
import NavSidebar from "@/components/NavSidebar.tsx";
import ChatSidebar from "@/components/ChatSidebar.tsx";
import ModeToggleBar, { type ChatMode } from "@/components/ModeToggleBar.tsx";
import { type SwarmDepth, type SwarmOutputLength } from "@/components/SwarmParamsBar.tsx";
import AssistantThreadArea from "@/components/assistant/AssistantThreadArea.tsx";
import AssistantComposerBar from "@/components/assistant/AssistantComposerBar.tsx";
import AskDialog from "@/components/AskDialog.tsx";
import ArtifactPanel from "@/components/ArtifactPanel.tsx";
import AutomationView from "@/components/automation/AutomationView.tsx";
import AutomationFormModal from "@/components/automation/AutomationFormModal.tsx";
import SettingsView from "@/components/settings/SettingsView.tsx";
import ChatInfoPanel from "@/components/ChatInfoPanel.tsx";

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

function mapToolParts(
  parts: ChatMessagePart[] | undefined,
  updateTool: (tool: ToolExecution) => ToolExecution,
): ChatMessagePart[] | undefined {
  if (!parts?.length) return parts;
  return parts.map((part) =>
    part.type === "tool" && part.tool
      ? { ...part, tool: updateTool(part.tool) }
      : part,
  );
}

function updateRunningTools(
  messages: ChatMessage[],
  matcher: (tool: ToolExecution) => boolean,
  updateTool: (tool: ToolExecution) => ToolExecution,
): ChatMessage[] {
  return messages.map((message) => {
    if (message.role !== "assistant" || !message.tools?.length) return message;
    let changed = false;
    const tools = message.tools.map((tool) => {
      if (tool.status !== "running" || !matcher(tool)) return tool;
      changed = true;
      return updateTool(tool);
    });
    if (!changed) return message;
    return {
      ...message,
      tools,
      parts: mapToolParts(message.parts, (tool) =>
        tool.status === "running" && matcher(tool) ? updateTool(tool) : tool,
      ),
    };
  });
}

function settleAllRunningTools(
  messages: ChatMessage[],
  status: ToolExecution["status"],
): ChatMessage[] {
  return updateRunningTools(
    messages,
    () => true,
    (tool) => ({ ...tool, status }),
  );
}

function mapHistoryToMessages(history: any[], sessionId: string): ChatMessage[] {
  return history.map((h: any) => ({
    id: nextId(),
    role: (h.role ?? "assistant") as MessageRole,
    content: h.content ?? "",
    thinking: h.thinking || undefined,
    tools: Array.isArray(h.tools) && h.tools.length > 0
      ? h.tools.map((t: any) => ({
          name: t.name ?? "",
          status: (t.is_error ? "error" : "done") as "done" | "error" | "running",
          input: t.input || undefined,
          result: t.result || undefined,
        }))
      : undefined,
    timestamp: Date.now(),
    sessionId,
    historyIndex: typeof h.history_index === "number" ? h.history_index : undefined,
    retryable: !!h.retryable,
  }));
}

export default function App() {
  const [module, setModule] = useState<"chat" | "automation" | "settings">("chat");
  const [artifact, setArtifact] = useState<string | null>(null);

  // Chat state
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [statusText, setStatusText] = useState<string>("");
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [askData, setAskData] = useState<AskData | null>(null);
  const [pendingFiles, setPendingFiles] = useState<string[]>([]);
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string | undefined>(undefined);

  // Automation state
  const [automationTasks, setAutomationTasks] = useState<AutomationTask[]>([]);
  const [showAutomationForm, setShowAutomationForm] = useState(false);

  // Chat mode toggle
  const [chatMode, setChatMode] = useState<ChatMode>("normal");
  const [modeCommitted, setModeCommitted] = useState(false);

  // Swarm mode params (breadth = # of sub-questions, depth = research depth, outputLength = response length)
  const [swarmBreadth, setSwarmBreadth] = useState(3);
  const [swarmDepth, setSwarmDepth] = useState<SwarmDepth>("standard");
  const [swarmOutputLength, setSwarmOutputLength] = useState<SwarmOutputLength>("standard");

  // Right info panel
  const [skills, setSkills] = useState<SkillInfo[]>([]);
  const [sessionTokens, setSessionTokens] = useState(0);

  const streamingIdRef = useRef<string | null>(null);
  const pendingFilesRef = useRef<string[]>([]);
  const composerInputRef = useRef<HTMLTextAreaElement | null>(null);
  // currentSessionIdRef mirrors currentSessionId for use in event handler closures
  const currentSessionIdRef = useRef<string | undefined>(undefined);
  const loadedSessionIdRef = useRef<string | undefined>(undefined);

  useEffect(() => {
    pendingFilesRef.current = pendingFiles;
  }, [pendingFiles]);

  // Keep ref in sync with state (for use inside event handler closures)
  useEffect(() => {
    currentSessionIdRef.current = currentSessionId;
  }, [currentSessionId]);

  const loadSessionHistory = useCallback(async (id: string) => {
    const history = await ChatService.getSessionHistory(id);
    loadedSessionIdRef.current = id;
    if (Array.isArray(history)) {
      setMessages(mapHistoryToMessages(history, id));
    } else {
      setMessages([]);
    }
  }, []);

  useEffect(() => {
    if (!currentSessionId || isRunning || loadedSessionIdRef.current === currentSessionId) return;
    loadSessionHistory(currentSessionId).catch(() => {});
  }, [currentSessionId, isRunning, loadSessionHistory]);

  useEffect(() => {
    ChatService.getConfig()
      .then((c: any) => setConfig(c as AppConfig))
      .catch(() => {});
    ChatService.getSkills()
      .then((s: any) => { if (Array.isArray(s)) setSkills(s as SkillInfo[]); })
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
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    setIsRunning(true);
    setStatusText("正在生成回复...");
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

  // Reset the streaming ref so the next chat:stream/text creates a new message bubble.
  // Used by swarm mode to separate the research-plan message from the synthesis answer.
  useWailsEvent("chat:reset_stream", () => {
    streamingIdRef.current = null;
  });

  useWailsEvent<StreamData>("chat:text", (data) => {
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    const text = data?.content ?? "";
    if (!text) return;

    if (streamingIdRef.current) {
      const id = streamingIdRef.current;
      setMessages((prev) =>
        prev.map((m) =>
          m.id === id
            ? {
                ...m,
                content: text,
                parts: [{ type: "text", text }],
                streaming: true,
              }
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
        content: text,
        parts: [{ type: "text", text }],
        timestamp: Date.now(),
        streaming: true,
        tools: [],
      },
    ]);
  });

  useWailsEvent<StreamData>("chat:thinking", (data) => {
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    const chunk = data?.content ?? "";
    if (!chunk) return;
    const shouldAppend = data?.append !== false;
    setIsRunning(true);
    if (streamingIdRef.current) {
      const id = streamingIdRef.current;
      setMessages((prev) =>
        prev.map((m) =>
          m.id === id ? { ...m, thinking: shouldAppend ? (m.thinking ?? "") + chunk : chunk } : m,
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
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    const toolName = data?.meta?.tool || data?.meta?.name || data?.content || "tool";
    const callId = data?.meta?.call_id;
    const summary = data?.meta?.args_preview || undefined;
    setIsRunning(true);
    setStatusText(`调用 ${toolName}...`);
    // Store raw input only when content differs from the tool name
    const toolInput = data?.content && data.content !== toolName ? data.content : undefined;
    const newTool: ToolExecution = { callId, name: toolName, status: "running", input: toolInput, summary };
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
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    const toolName = data?.meta?.tool || data?.meta?.name || "";
    const callId = data?.meta?.call_id;
    const isError = data?.meta?.is_error ?? false;
    setMessages((prev) => {
      const matchesTool = (tool: ToolExecution) =>
        callId ? tool.callId === callId : tool.name === toolName;
      return updateRunningTools(
        prev,
        matchesTool,
        (tool) => ({
          ...tool,
          status: isError ? "error" : "done",
          result: data?.content,
        }),
      );
    });
  });

  useWailsEvent<AskData>("chat:ask", (data) => {
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    setAskData(data);
  });

  useWailsEvent<DoneData>("chat:done", (data) => {
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
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
    if (typeof data?.tokens_used === "number" && data.tokens_used > 0) {
      setSessionTokens((prev) => prev + data.tokens_used);
    }
    setMessages((prev) => settleAllRunningTools(prev, "done"));
    streamingIdRef.current = null;
    setIsRunning(false);
    setStatusText("");
    if (data?.session_id) {
      loadedSessionIdRef.current = undefined;
      loadSessionHistory(data.session_id).catch(() => {});
    }
  });

  useWailsEvent<ErrorData>("chat:error", (data) => {
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    setMessages((prev) => settleAllRunningTools(prev, "error"));
    streamingIdRef.current = null;
    setIsRunning(false);
    setStatusText("");
    const msg = data?.message || (typeof data === "string" ? data : "Unknown error");
    const id = nextId();
    setMessages((prev) => [
      ...prev,
      { id, role: "system", content: `Error: ${msg}`, timestamp: Date.now() },
    ]);
  });

  useWailsEvent("chat:cancelled", () => {
    setMessages((prev) => settleAllRunningTools(prev, "error"));
    streamingIdRef.current = null;
    setIsRunning(false);
    setStatusText("");
    const id = nextId();
    setMessages((prev) => [
      ...prev,
      { id, role: "system", content: "已取消执行", timestamp: Date.now() },
    ]);
  });

  useWailsEvent<StreamData>("chat:progress", (data) => {
    if (data?.session_id && currentSessionIdRef.current && data.session_id !== currentSessionIdRef.current) return;
    setIsRunning(true);
    if (data?.content) setStatusText(data.content);
  });

  useWailsEvent<DashboardState>("desktop:dashboard", (data) => {
    if (!data || typeof data !== "object") return;
    if (Array.isArray(data.sessions)) setSessions(data.sessions);
    if (Array.isArray(data.schedules)) setAutomationTasks(data.schedules as AutomationTask[]);
    if (typeof data.current_session_id === "string") setCurrentSessionId(data.current_session_id);
    if (typeof data.session_mode === "string") setChatMode(data.session_mode as ChatMode);
    if (typeof data.session_mode_committed === "boolean") setModeCommitted(data.session_mode_committed);
    // If the current session is running (e.g. started by automation), reflect that in UI state
    if (typeof data.current_session_id === "string" && Array.isArray(data.sessions)) {
      const cur = data.sessions.find((s) => s.id === data.current_session_id);
      if (cur?.status === "running") {
        setIsRunning(true);
        setStatusText((prev) => prev || "正在执行...");
      }
    }
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

  const handleSend = useCallback(
    async (content: string, files?: string[]) => {
      setModeCommitted(true);
      const id = nextId();
      setMessages((prev) => [
        ...prev,
        { id, role: "user", content, timestamp: Date.now(), sessionId: currentSessionId },
      ]);
      setIsRunning(true);
      setStatusText("正在处理...");
      try {
        // Sync swarm params to backend before sending in swarm mode.
        if (chatMode === "swarm") {
          try { await ChatService.setSwarmParams(swarmBreadth, swarmDepth, swarmOutputLength); } catch {}
        }
        if (files?.length) {
          await ChatService.sendMessageWithAttachments(content, files);
        } else {
          await ChatService.sendMessage(content);
        }
      } catch (err: any) {
        setIsRunning(false);
        setStatusText("");
        const eid = nextId();
        setMessages((prev) => [
          ...prev,
          { id: eid, role: "system", content: `Failed: ${err?.message ?? err}`, timestamp: Date.now() },
        ]);
      }
    },
    [currentSessionId, chatMode, swarmBreadth, swarmDepth, swarmOutputLength],
  );

  const handleStop = useCallback(async () => {
    try {
      await ChatService.stopAgent();
    } catch {}
  }, []);

  const handleNewSession = useCallback(async () => {
    setIsRunning(false);
    setStatusText("");
    setSessionTokens(0);
    setModeCommitted(false);
    setChatMode("normal");
    setSwarmBreadth(3);
    setSwarmDepth("standard");
    try {
      await ChatService.newSession();
      loadedSessionIdRef.current = undefined;
      setMessages([]);
      setPendingFiles([]);
      setArtifact(null);
      streamingIdRef.current = null;
    } catch {}
  }, []);

  const handleResumeSession = useCallback(async (id: string) => {
    setIsRunning(false);
    setStatusText("");
    setSessionTokens(0);
    try {
      // Clear current UI state before switching
      loadedSessionIdRef.current = undefined;
      setMessages([]);
      setArtifact(null);
      streamingIdRef.current = null;
      // Switch the backend session
      await ChatService.resumeSession(id);
      setCurrentSessionId(id);
      await loadSessionHistory(id);
    } catch {
      // silently ignore session switch errors
    }
  }, [loadSessionHistory]);

  const handleRetryMessage = useCallback(async (message: ChatMessage) => {
    const sessionId = message.sessionId ?? currentSessionId;
    const historyIndex = message.historyIndex;
    if (!sessionId || historyIndex == null) return;

    setIsRunning(false);
    setStatusText("");
    setArtifact(null);
    streamingIdRef.current = null;
    loadedSessionIdRef.current = undefined;

    if (sessionId !== currentSessionId) {
      currentSessionIdRef.current = sessionId;
      setCurrentSessionId(sessionId);
      setMessages([]);
      await ChatService.resumeSession(sessionId);
    }

    await ChatService.sendCommand(`/retry ${historyIndex}`);
    await loadSessionHistory(sessionId);
  }, [currentSessionId, loadSessionHistory]);

  const handleDeleteSession = useCallback(async (id: string) => {
    try {
      await ChatService.deleteSession(id);
      // If we deleted the current session, clear the chat view
      if (id === currentSessionId) {
        loadedSessionIdRef.current = undefined;
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
        loadedSessionIdRef.current = undefined;
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
    ChatService.respondToAsk("", false).catch(() => {});
  }, []);

  const handleInsertSkill = useCallback((skillName: string) => {
    const el = composerInputRef.current;
    if (!el) return;

    const insertion = `/${skillName} `;
    const current = el.value ?? "";
    const start = el.selectionStart ?? current.length;
    const end = el.selectionEnd ?? current.length;
    const prefix = current && start === current.length && !/[\s\n]$/.test(current) ? "\n" : "";
    const nextValue = `${current.slice(0, start)}${prefix}${insertion}${current.slice(end)}`;
    const caret = start + prefix.length + insertion.length;
    const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
    valueSetter?.call(el, nextValue);
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.focus();
    el.setSelectionRange(caret, caret);
  }, []);
  void handleInsertSkill;

  const handleComposerInputRef= useCallback((node: HTMLTextAreaElement | null) => {
    composerInputRef.current = node;
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
              right: artifact ? "400px" : "320px",
            }}
          >
            {/* Titlebar spacer + mode toggle */}
            <div className="h-8 shrink-0" />
            {!modeCommitted && (
              <ModeToggleBar mode={chatMode} onChange={(mode) => { setChatMode(mode); setModeCommitted(true); ChatService.setChatMode(mode); }} />
            )}
            <div className="border-b border-border/50 shrink-0" />

            <AssistantRuntimeProvider runtime={runtime}>
              <div className="flex-1 overflow-hidden relative">
                <AssistantThreadArea
                  showTypingIndicator={showTypingIndicator}
                  statusText={statusText}
                  onArtifact={setArtifact}
                  skills={skills}
                  onRetryMessage={handleRetryMessage}
                />
              </div>

              <div className="relative h-36 shrink-0">
                <AssistantComposerBar
                  isRunning={isRunning}
                  onStop={handleStop}
                  attachmentFiles={pendingFiles}
                  onAttachFiles={handlePickAttachments}
                  onRemoveAttachment={handleRemoveAttachment}
                  inputRef={handleComposerInputRef}
                />
              </div>
            </AssistantRuntimeProvider>
          </main>

          {artifact && (
            <ArtifactPanel html={artifact} onClose={() => setArtifact(null)} />
          )}

          <ChatInfoPanel
            messages={messages}
            totalTokens={sessionTokens}
            currentSessionId={currentSessionId}
            sessions={sessions}
            config={config}
            chatMode={chatMode}
            automationTasks={automationTasks}
            onAddAutomation={() => setShowAutomationForm(true)}
            onViewAllAutomations={() => setModule("automation")}
            swarmBreadth={swarmBreadth}
            swarmDepth={swarmDepth}
            swarmOutputLength={swarmOutputLength}
            onBreadthChange={setSwarmBreadth}
            onDepthChange={setSwarmDepth}
            onOutputLengthChange={setSwarmOutputLength}
          />
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

