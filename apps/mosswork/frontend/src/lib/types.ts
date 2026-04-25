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
  thinking?: string;
  parts?: ChatMessagePart[];
  timestamp: number;
  streaming?: boolean;
  tools?: ToolExecution[];
  sessionId?: string;
  historyIndex?: number;
  retryable?: boolean;
}

export interface ToolExecution {
  callId?: string;   // matches call_id from backend meta for reliable start/result pairing
  name: string;
  status: "running" | "done" | "error";
  input?: string;
  summary?: string;  // args_preview from backend — concise summary of call arguments
  result?: string;
}

export interface StreamData {
  content: string;
  session_id?: string;
  meta?: Record<string, any>;
}

export interface ToolStartData {
  content: string;
  session_id?: string;
  meta?: {
    tool?: string;       // tool name (from backend)
    call_id?: string;    // unique call id for pairing with result
    risk?: string;
    args_preview?: string;
    name?: string;       // legacy fallback
  };
}

export interface ToolResultData {
  content: string;
  session_id?: string;
  meta?: {
    tool?: string;       // tool name (from backend)
    call_id?: string;    // unique call id matching the start event
    is_error?: boolean;
    duration_ms?: number;
    name?: string;       // legacy fallback
  };
}

export interface AskData {
  type: string;
  prompt: string;
  session_id?: string;
  options?: string[];
  approval?: {
    id: string;
    kind: string;
    session_id?: string;
    tool_name?: string;
    risk?: string;
    prompt: string;
    reason?: string;
    reason_code?: string;
    input?: Record<string, any>;
    action_label?: string;
    action_value?: string;
    scope_label?: string;
    scope_value?: string;
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
  session_id?: string;
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
  source?: string;
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
  status?: "running" | "paused" | "completed" | "failed";
  last_run_result?: "success" | "failure";
}

export interface ModelPreset {
  provider: string;
  group?: string;
  label: string;
  model: string;
  base_url?: string;
}

export interface ToolInfo {
  name: string;
  description: string;
  risk: "low" | "medium" | "high";
  source?: string;
}

export interface SkillInfo {
  name: string;
  description: string;
  depends_on?: string[];
  required_env?: string[];
  source?: string;
  active: boolean;
}

export interface AppSettings {
  provider: string;
  model: string;
  baseURL: string;
  apiKey: string;
  workers: number;
}

export interface DashboardState {
  current_session_id?: string;
  session_mode?: string;
  session_mode_committed?: boolean;
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
