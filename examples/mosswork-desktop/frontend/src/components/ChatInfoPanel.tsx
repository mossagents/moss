import { cn } from "@/lib/cn";
import type { ChatMessage, SessionSummary, SkillInfo, AppConfig } from "@/lib/types";
import type { ReactNode } from "react";

interface ChatInfoPanelProps {
  messages: ChatMessage[];
  totalTokens: number;
  currentSessionId?: string;
  sessions: SessionSummary[];
  skills: SkillInfo[];
  config: AppConfig | null;
  onInsertSkill: (name: string) => void;
}

export default function ChatInfoPanel({
  messages,
  totalTokens,
  currentSessionId,
  sessions,
  skills,
  config,
  onInsertSkill,
}: ChatInfoPanelProps) {
  const currentSession = sessions.find((s) => s.id === currentSessionId);
  const userMessages = messages.filter((m) => m.role === "user").length;
  const assistantMessages = messages.filter((m) => m.role === "assistant").length;
  const totalMessages = userMessages + assistantMessages;

  const createdAt = currentSession?.created_at
    ? new Date(currentSession.created_at).toLocaleString("zh-CN", {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })
    : null;

  const sortedSkills = [...skills].sort((a, b) => {
    if (a.active !== b.active) return a.active ? -1 : 1;
    return a.name.localeCompare(b.name);
  });

  return (
    <aside className="fixed right-0 top-0 bottom-0 w-80 bg-surface-container-low border-l border-outline-variant/30 overflow-y-auto z-20 flex flex-col">
      {/* Header */}
      <div className="px-4 py-3 mt-8 shrink-0 border-b border-outline-variant/20">
        <span className="text-xs font-bold text-on-surface-variant/70 tracking-widest uppercase">会话信息</span>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-3 space-y-5">
        {/* Session Stats */}
        <section>
          <SectionLabel>当前会话</SectionLabel>
          <div className="bg-surface-container-lowest rounded-xl p-3 space-y-2">
            <StatRow icon="chat" label="消息数" value={String(totalMessages)} />
            <StatRow icon="person" label="发送" value={String(userMessages)} />
            <StatRow icon="smart_toy" label="回复" value={String(assistantMessages)} />
            <StatRow
              icon="token"
              label="Token"
              value={totalTokens > 0 ? formatNumber(totalTokens) : "—"}
            />
            {currentSession?.steps != null && currentSession.steps > 0 && (
              <StatRow icon="steps" label="步骤" value={String(currentSession.steps)} />
            )}
            {createdAt && <StatRow icon="schedule" label="创建" value={createdAt} />}
            {config?.model && (
              <StatRow icon="model_training" label="模型" value={config.model} truncate />
            )}
          </div>
        </section>

        {/* Keyboard shortcuts */}
        <section>
          <SectionLabel>快捷键</SectionLabel>
          <div className="bg-surface-container-lowest rounded-xl p-3 space-y-2">
            <ShortcutRow keys={["Enter"]} label="发送消息" />
            <ShortcutRow keys={["Shift", "Enter"]} label="换行" />
            <ShortcutRow keys={["Esc"]} label="停止执行" />
            <ShortcutRow keys={["/sessions"]} label="列出会话" isCmd />
            <ShortcutRow keys={["/compact"]} label="压缩上下文" isCmd />
            <ShortcutRow keys={["/schedules"]} label="列出定时任务" isCmd />
            <ShortcutRow keys={["/dashboard"]} label="查看仪表盘" isCmd />
          </div>
        </section>

        {/* Available Skills */}
        <section>
          <SectionLabel>可用技能 ({skills.length})</SectionLabel>
          <p className="text-[11px] text-on-surface-variant/70 leading-snug px-1">
            点击技能即可在输入框中插入 <code className="font-mono">/&lt;skill_name&gt;</code> 引用。
          </p>
          {skills.length === 0 ? (
            <div className="text-xs text-on-surface-variant/60 px-1">暂无技能</div>
          ) : (
            <div className="space-y-1.5">
              {sortedSkills.map((skill) => (
                <SkillRow key={skill.name} skill={skill} onInsert={onInsertSkill} />
              ))}
            </div>
          )}
        </section>
      </div>
    </aside>
  );
}

function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <span className="text-[10px] font-bold text-on-surface-variant/60 tracking-widest uppercase mb-2 block">
      {children}
    </span>
  );
}

function StatRow({
  icon,
  label,
  value,
  truncate,
}: {
  icon: string;
  label: string;
  value: string;
  truncate?: boolean;
}) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className="material-symbols-outlined text-[13px] text-on-surface-variant/60 shrink-0">{icon}</span>
      <span className="text-on-surface-variant flex-1">{label}</span>
      <span className={cn("font-medium text-on-surface text-right", truncate && "truncate max-w-24")}>{value}</span>
    </div>
  );
}

function ShortcutRow({
  keys,
  label,
  isCmd,
}: {
  keys: string[];
  label: string;
  isCmd?: boolean;
}) {
  return (
    <div className="flex items-center justify-between text-xs gap-2">
      <span className="text-on-surface-variant flex-1">{label}</span>
      <div className="flex items-center gap-1 shrink-0">
        {isCmd ? (
          <code className="bg-surface-container px-1.5 py-0.5 rounded text-[10px] font-mono text-on-surface-variant border border-outline-variant/30">
            {keys[0]}
          </code>
        ) : (
          keys.map((k, i) => (
            <span key={i} className="flex items-center gap-1">
              {i > 0 && <span className="text-on-surface-variant/40">+</span>}
              <kbd className="bg-surface-container px-1.5 py-0.5 rounded text-[10px] font-sans text-on-surface-variant border border-outline-variant/30 leading-none">
                {k}
              </kbd>
            </span>
          ))
        )}
      </div>
    </div>
  );
}

const SKILL_STYLES = {
  active: {
    badge: "bg-primary/10 text-primary",
    action: "text-primary",
  },
  available: {
    badge: "bg-surface-container text-on-surface-variant",
    action: "text-on-surface-variant",
  },
};

function SkillRow({
  skill,
  onInsert,
}: {
  skill: SkillInfo;
  onInsert: (name: string) => void;
}) {
  const style = skill.active ? SKILL_STYLES.active : SKILL_STYLES.available;
  const source = skill.source ? skill.source.replace(/\\/g, "/").split("/").slice(-2).join("/") : "";
  return (
    <button
      type="button"
      onClick={() => onInsert(skill.name)}
      className="w-full bg-surface-container-lowest rounded-lg px-3 py-2 flex items-start gap-2 group text-left hover:bg-surface-container transition-colors"
      title={`引用技能 ${skill.name}`}
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium text-on-surface truncate">{skill.name}</span>
          <span className={cn("text-[9px] font-bold px-1 py-px rounded shrink-0", style.badge)}>
            {skill.active ? "已激活" : "可用"}
          </span>
          <span className={cn("text-[10px] font-medium ml-auto shrink-0 opacity-0 group-hover:opacity-100 transition-opacity", style.action)}>
            引用
          </span>
        </div>
        {skill.description && (
          <p className="text-[11px] text-on-surface-variant/70 mt-0.5 line-clamp-2 leading-snug">
            {skill.description}
          </p>
        )}
        {(source || skill.depends_on?.length || skill.required_env?.length) && (
          <div className="mt-1.5 space-y-1">
            {source && (
              <p className="text-[10px] text-on-surface-variant/55 truncate">{source}</p>
            )}
            {skill.depends_on && skill.depends_on.length > 0 && (
              <p className="text-[10px] text-on-surface-variant/55 truncate">
                依赖: {skill.depends_on.join(", ")}
              </p>
            )}
          </div>
        )}
      </div>
    </button>
  );
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}
