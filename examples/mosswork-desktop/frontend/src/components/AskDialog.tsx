import { useState } from "react";
import { cn } from "@/lib/cn";
import type { AskData } from "@/lib/types";

interface AskDialogProps {
  data: AskData;
  onRespond: (response: string) => void;
  onDismiss: () => void;
}

const RISK_CONFIG: Record<string, { label: string; color: string; icon: string }> = {
  high:   { label: "高风险", color: "bg-error-container/70 text-error",                    icon: "dangerous" },
  medium: { label: "中风险", color: "bg-tertiary-container text-on-tertiary-container",     icon: "warning" },
  low:    { label: "低风险", color: "bg-primary-container/60 text-on-primary-container",    icon: "info" },
};

function getRiskConfig(risk?: string) {
  if (!risk) return null;
  return RISK_CONFIG[risk.toLowerCase()] ?? {
    label: risk,
    color: "bg-surface-container text-on-surface-variant",
    icon: "help",
  };
}

export default function AskDialog({ data, onRespond, onDismiss }: AskDialogProps) {
  const [input, setInput] = useState("");
  const isApproval = data.type === "confirm" && !!data.approval;

  const handleSubmit = () => {
    const val = input.trim();
    if (val) onRespond(val);
  };

  return (
    <div className="fixed inset-0 z-100 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-on-surface/20 backdrop-blur-sm"
        onClick={onDismiss}
      />

      {/* Dialog */}
      <div className="relative w-full max-w-md mx-4 bg-surface-container-lowest rounded-2xl animate-fade-in shadow-botanical-dialog overflow-hidden">

        {isApproval ? (
          <ApprovalContent data={data} onAllow={() => onRespond("yes")} onDeny={onDismiss} />
        ) : (
          <GenericAskContent
            data={data}
            input={input}
            setInput={setInput}
            onRespond={onRespond}
            onSubmit={handleSubmit}
            onDismiss={onDismiss}
          />
        )}
      </div>
    </div>
  );
}

function ApprovalContent({
  data,
  onAllow,
  onDeny,
}: {
  data: AskData;
  onAllow: () => void;
  onDeny: () => void;
}) {
  const { approval } = data;
  const risk = getRiskConfig(approval?.risk);

  // For run_command, surface the command; otherwise show key args
  const inputEntries = approval?.input ? Object.entries(approval.input) : [];
  const commandValue = approval?.input?.command as string | undefined;
  // Show all args except 'command' (already shown separately) as supplementary info
  const extraArgs = inputEntries.filter(([k]) => k !== "command");

  return (
    <>
      {/* Header */}
      <div className="flex items-center gap-3 px-5 pt-5 pb-4">
        <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-error-container/50 text-error shrink-0">
          <span className="material-symbols-outlined">security</span>
        </div>
        <div>
          <p className="text-sm font-bold text-on-surface">工具授权请求</p>
          <p className="text-xs text-on-surface-variant mt-0.5">Agent 请求执行以下操作，请确认是否允许</p>
        </div>
      </div>

      {/* Details */}
      <div className="mx-5 mb-4 rounded-xl bg-surface-container border border-border overflow-hidden">
        <div className="divide-y divide-border">
          {/* Tool name + risk */}
          <div className="flex items-center justify-between px-4 py-3">
            <div>
              <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant mb-0.5">工具</p>
              <p className="text-sm font-bold font-mono text-on-surface">
                {approval?.tool_name || "unknown"}
              </p>
            </div>
            {risk && (
              <span className={cn("px-2.5 py-1 rounded-full text-[11px] font-bold flex items-center gap-1", risk.color)}>
                <span className="material-symbols-outlined text-sm">{risk.icon}</span>
                {risk.label}
              </span>
            )}
          </div>

          {/* Command — shown prominently for run_command */}
          {commandValue && (
            <div className="px-4 py-3">
              <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant mb-1.5">命令</p>
              <div className="bg-surface-container-high rounded-lg px-3 py-2 border border-outline-variant/30">
                <pre className="font-mono text-[12px] text-on-surface leading-relaxed whitespace-pre-wrap break-all">
                  {commandValue}
                </pre>
              </div>
            </div>
          )}

          {/* Extra args (non-command keys) */}
          {extraArgs.length > 0 && (
            <div className="px-4 py-3">
              <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant mb-1.5">参数</p>
              <div className="space-y-1">
                {extraArgs.map(([k, v]) => (
                  <div key={k} className="flex gap-2 items-start text-xs">
                    <span className="font-mono text-on-surface-variant shrink-0">{k}:</span>
                    <span className="font-mono text-on-surface break-all">
                      {typeof v === "string" ? v : JSON.stringify(v)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* For tools with no structured input but has action_value */}
          {!commandValue && extraArgs.length === 0 && approval?.action_value && (
            <div className="px-4 py-3">
              <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant mb-1">
                {approval.action_label || "操作"}
              </p>
              <p className="text-sm font-mono text-on-surface break-all">{approval.action_value}</p>
            </div>
          )}

          {/* Reason */}
          {(approval?.reason || data.prompt) && (
            <div className="px-4 py-3">
              <p className="text-[10px] font-bold uppercase tracking-wider text-on-surface-variant mb-0.5">原因</p>
              <p className="text-sm text-on-surface leading-relaxed">
                {approval?.reason || data.prompt}
              </p>
            </div>
          )}
        </div>
      </div>

      {/* Action buttons */}
      <div className="flex gap-2 px-5 pb-5">
        <button
          onClick={onDeny}
          className="flex-1 py-2.5 rounded-xl text-sm font-bold border border-border text-on-surface-variant hover:bg-surface-container transition-colors active:scale-[0.97]"
        >
          拒绝
        </button>
        <button
          onClick={onAllow}
          className="flex-1 py-2.5 rounded-xl text-sm font-bold bg-primary text-on-primary hover:opacity-90 transition-all active:scale-[0.97] shadow-sm"
        >
          允许
        </button>
      </div>
    </>
  );
}

function GenericAskContent({
  data,
  input,
  setInput,
  onRespond,
  onSubmit,
  onDismiss,
}: {
  data: AskData;
  input: string;
  setInput: (v: string) => void;
  onRespond: (v: string) => void;
  onSubmit: () => void;
  onDismiss: () => void;
}) {
  return (
    <>
      {/* Header */}
      <div className="flex items-center gap-3 px-5 pt-5 pb-3">
        <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-tertiary-container text-on-tertiary-container shrink-0">
          <span className="material-symbols-outlined">contact_support</span>
        </div>
        <div>
          <p className="text-sm font-bold text-on-surface">Agent 需要你的输入</p>
          <p className="text-xs text-on-surface-variant mt-0.5">请提供以下信息</p>
        </div>
      </div>

      {/* Prompt */}
      <div className="px-5 py-3">
        <p className="text-sm text-on-surface leading-relaxed whitespace-pre-wrap">{data.prompt}</p>
      </div>

      {/* Options or text input */}
      <div className="px-5 pb-5">
        {data.options && data.options.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {data.options.map((opt) => (
              <button
                key={opt}
                onClick={() => onRespond(opt)}
              >
                {opt}
              </button>
            ))}
          </div>
        ) : (
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") onSubmit();
                if (e.key === "Escape") onDismiss();
              }}
              placeholder="输入回复…"
              autoFocus
              className={cn(
                "flex-1 px-3.5 py-2 rounded-xl text-sm bg-surface-container border border-outline-variant/40 text-on-surface",
                "placeholder:text-on-surface-variant/50",
                "focus:outline-none focus:border-primary/40 focus:ring-1 focus:ring-primary/20",
              )}
            />
            <button
              onClick={onSubmit}
              disabled={!input.trim()}
              className={cn(
                "px-4 py-2 rounded-xl text-sm font-bold transition-all",
                input.trim()
                  ? "bg-primary text-on-primary hover:opacity-90 active:scale-[0.97]"
                  : "bg-surface-container text-on-surface-variant/40",
              )}
            >
              确认
            </button>
          </div>
        )}
      </div>
    </>
  );
}
