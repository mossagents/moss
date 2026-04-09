import { useState, useEffect } from "react";
import { cn } from "@/lib/cn.ts";

const PRESETS = [
  { label: "每30分钟", value: "@every 30m" },
  { label: "每小时", value: "@every 1h" },
  { label: "每2小时", value: "@every 2h" },
  { label: "每6小时", value: "@every 6h" },
  { label: "每12小时", value: "@every 12h" },
  { label: "每天", value: "@every 24h" },
  { label: "每3天", value: "@every 72h" },
  { label: "每周", value: "@every 168h" },
  { label: "自定义", value: "__custom__" },
];

interface AutomationFormModalProps {
  open: boolean;
  onClose: () => void;
  onSave: (id: string, schedule: string, goal: string) => void;
}

export default function AutomationFormModal({ open, onClose, onSave }: AutomationFormModalProps) {
  const [id, setId] = useState("");
  const [goal, setGoal] = useState("");
  const [schedulePreset, setSchedulePreset] = useState("@every 24h");
  const [customSchedule, setCustomSchedule] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    if (open) {
      setId("");
      setGoal("");
      setSchedulePreset("@every 24h");
      setCustomSchedule("");
      setErrors({});
    }
  }, [open]);

  if (!open) return null;

  const schedule = schedulePreset === "__custom__" ? customSchedule : schedulePreset;

  function validate() {
    const e: Record<string, string> = {};
    if (!id.trim()) e.id = "任务名称不能为空";
    if (!goal.trim()) e.goal = "目标描述不能为空";
    if (!schedule.trim()) e.schedule = "定时规则不能为空";
    setErrors(e);
    return Object.keys(e).length === 0;
  }

  function handleSave() {
    if (!validate()) return;
    onSave(id.trim(), schedule.trim(), goal.trim());
    onClose();
  }

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/30 backdrop-blur-sm" onClick={onClose} />
      <div className="relative z-10 bg-surface rounded-2xl shadow-2xl w-full max-w-md mx-4 p-6">
        <div className="flex items-center justify-between mb-5">
          <h2 className="text-lg font-bold text-on-surface font-headline">新建自动化任务</h2>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg text-on-surface-variant hover:bg-surface-container-high transition-colors"
          >
            <span className="material-symbols-outlined text-xl">close</span>
          </button>
        </div>

        <div className="space-y-4">
          {/* Task ID */}
          <div>
            <label className="block text-xs font-bold text-on-surface-variant mb-1.5 uppercase tracking-wide">
              任务名称 *
            </label>
            <input
              type="text"
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder="例如: daily-report"
              className={cn(
                "w-full px-3 py-2.5 bg-surface-container rounded-xl text-sm text-on-surface placeholder:text-on-surface-variant/40 outline-none border transition-colors",
                errors.id ? "border-error" : "border-transparent focus:border-primary/40",
              )}
            />
            {errors.id && <p className="text-xs text-error mt-1">{errors.id}</p>}
          </div>

          {/* Goal */}
          <div>
            <label className="block text-xs font-bold text-on-surface-variant mb-1.5 uppercase tracking-wide">
              目标描述 *
            </label>
            <textarea
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              placeholder="描述 AI 需要完成的任务..."
              rows={4}
              className={cn(
                "w-full px-3 py-2.5 bg-surface-container rounded-xl text-sm text-on-surface placeholder:text-on-surface-variant/40 outline-none border transition-colors resize-none",
                errors.goal ? "border-error" : "border-transparent focus:border-primary/40",
              )}
            />
            {errors.goal && <p className="text-xs text-error mt-1">{errors.goal}</p>}
          </div>

          {/* Schedule */}
          <div>
            <label className="block text-xs font-bold text-on-surface-variant mb-1.5 uppercase tracking-wide">
              定时规则 *
            </label>
            <select
              value={schedulePreset}
              onChange={(e) => setSchedulePreset(e.target.value)}
              className="w-full px-3 py-2.5 bg-surface-container rounded-xl text-sm text-on-surface outline-none border border-transparent focus:border-primary/40 transition-colors cursor-pointer"
            >
              {PRESETS.map((p) => (
                <option key={p.value} value={p.value}>{p.label}</option>
              ))}
            </select>

            {schedulePreset === "__custom__" && (
              <div className="mt-2">
                <input
                  type="text"
                  value={customSchedule}
                  onChange={(e) => setCustomSchedule(e.target.value)}
                  placeholder="例如: @every 4h"
                  className={cn(
                    "w-full px-3 py-2.5 bg-surface-container rounded-xl text-sm text-on-surface placeholder:text-on-surface-variant/40 outline-none border transition-colors",
                    errors.schedule ? "border-error" : "border-transparent focus:border-primary/40",
                  )}
                />
                {errors.schedule && <p className="text-xs text-error mt-1">{errors.schedule}</p>}
              </div>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="flex gap-3 mt-6">
          <button
            onClick={onClose}
            className="flex-1 py-2.5 rounded-xl text-sm font-bold text-on-surface-variant bg-surface-container hover:bg-surface-container-high transition-colors"
          >
            取消
          </button>
          <button
            onClick={handleSave}
            className="flex-1 py-2.5 rounded-xl text-sm font-bold text-on-primary bg-primary hover:opacity-90 active:scale-95 transition-all shadow-sm"
          >
            保存
          </button>
        </div>
      </div>
    </div>
  );
}
