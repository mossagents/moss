import { cn } from "@/lib/cn.ts";

export type ExpertDepth = "fast" | "standard" | "deep";
export type ExpertOutputLength = "brief" | "standard" | "detailed" | "comprehensive";

interface ExpertParamsBarProps {
  breadth: number;
  depth: ExpertDepth;
  onBreadthChange: (v: number) => void;
  onDepthChange: (v: ExpertDepth) => void;
}

const BREADTH_OPTIONS = [1, 2, 3, 4, 5];

const DEPTH_OPTIONS: { value: ExpertDepth; label: string; hint: string }[] = [
  { value: "fast", label: "快速", hint: "10步" },
  { value: "standard", label: "标准", hint: "30步" },
  { value: "deep", label: "深度", hint: "60步" },
];

export default function ExpertParamsBar({ breadth, depth, onBreadthChange, onDepthChange }: ExpertParamsBarProps) {
  return (
    <div className="flex items-center gap-6 px-6 py-2 border-b border-outline-variant/20 bg-surface-container-lowest/60 shrink-0">
      {/* Breadth */}
      <div className="flex items-center gap-2">
        <span className="text-[11px] font-medium text-on-surface-variant/70 shrink-0">广度</span>
        <div className="flex items-center gap-0.5 rounded-lg bg-surface-container p-0.5">
          {BREADTH_OPTIONS.map((n) => (
            <button
              key={n}
              type="button"
              onClick={() => onBreadthChange(n)}
              className={cn(
                "w-7 h-6 rounded-md text-xs font-semibold transition-all",
                n === breadth
                  ? "bg-primary text-on-primary shadow-sm"
                  : "text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high",
              )}
            >
              {n}
            </button>
          ))}
        </div>
        <span className="text-[10px] text-on-surface-variant/50">个方向</span>
      </div>

      <div className="h-4 w-px bg-outline-variant/30" />

      {/* Depth */}
      <div className="flex items-center gap-2">
        <span className="text-[11px] font-medium text-on-surface-variant/70 shrink-0">深度</span>
        <div className="flex items-center gap-0.5 rounded-lg bg-surface-container p-0.5">
          {DEPTH_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              onClick={() => onDepthChange(opt.value)}
              title={opt.hint}
              className={cn(
                "px-2.5 h-6 rounded-md text-xs font-semibold transition-all",
                opt.value === depth
                  ? "bg-primary text-on-primary shadow-sm"
                  : "text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high",
              )}
            >
              {opt.label}
            </button>
          ))}
        </div>
        <span className="text-[10px] text-on-surface-variant/50">
          {DEPTH_OPTIONS.find((d) => d.value === depth)?.hint}
        </span>
      </div>
    </div>
  );
}
