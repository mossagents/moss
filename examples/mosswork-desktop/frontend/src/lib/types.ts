export type MessageRole = "user" | "assistant" | "system";

export interface ChatMessagePart {
  type: "text" | "tool";
  text?: string;
  tool?: ToolExecution;
}

export interface ChatMessage {
  id: string;
  role: MessageRole;
  content: string;
  parts?: ChatMessagePart[];
  timestamp: number;
  streaming?: boolean;
  tools?: ToolExecution[];
}

export interface ToolExecution {
  name: string;
  status: "running" | "done" | "error";
  result?: string;
}

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
  approval?: {
    id: string;
    kind: string;
    session_id?: string;
    tool_name?: string;
    risk?: string;
    prompt: string;
    reason?: string;
  };
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

export interface WorkerTask {
  id: string;
  description: string;
  status: "queued" | "running" | "done" | "failed" | "completed" | "cancelled";
  steps: number;
  error?: string;
}

export interface WorkerState {
  state: "running" | "completed";
  running: number;
  succeeded: number;
  failed: number;
  tasks: WorkerTask[];
}

export interface SessionSummary {
  id: string;
  title?: string;
  goal: string;
  mode?: string;
  status: string;
  steps: number;
  created_at: string;
  ended_at?: string;
  current?: boolean;
}

export interface ScheduleEntry {
  id: string;
  schedule: string;
  goal: string;
  run_count: number;
  last_run?: string;
  next_run?: string;
}

export interface AutomationTask {
  id: string;
  schedule: string;
  goal: string;
  run_count: number;
  last_run?: string;
  next_run?: string;
}

export interface DashboardState {
  current_session_id?: string;
  sessions?: SessionSummary[];
  schedules?: ScheduleEntry[];
  worker?: WorkerState;
}

export interface AppConfig {
  provider: string;
  model: string;
  workspace: string;
  workers: number;
}
