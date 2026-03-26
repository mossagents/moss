// ─── Message types ──────────────────────────────────

export type MessageRole = "user" | "assistant" | "system";

export interface ChatMessage {
  id: string;
  role: MessageRole;
  content: string;
  timestamp: number;
  streaming?: boolean;
  tools?: ToolExecution[];
}

export interface ToolExecution {
  name: string;
  status: "running" | "done" | "error";
  result?: string;
}

// ─── Event data shapes ─────────────────────────────

export interface StreamData {
  content: string;
  meta?: Record<string, any>;
}

export interface ToolStartData {
  content: string;
  meta?: { name?: string };
}

export interface ToolResultData {
  content: string;
  meta?: { name?: string; success?: boolean };
}

export interface AskData {
  type: string;
  prompt: string;
  options?: string[];
  meta?: Record<string, any>;
}

export interface DoneData {
  session_id: string;
  steps: number;
  tokens_used: number;
  output: string;
}

export interface ErrorData {
  message: string;
}

// ─── Worker tracking ────────────────────────────────

export interface WorkerState {
  state: "running" | "completed";
  running: number;
  succeeded: number;
  failed: number;
  tasks: WorkerTask[];
}

export interface WorkerTask {
  id: string;
  description: string;
  status: "queued" | "running" | "done" | "failed";
  steps: number;
}

// ─── Config ──────────────────────────────────────────

export interface AppConfig {
  provider: string;
  model: string;
  workspace: string;
  workers: number;
}
