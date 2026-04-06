import { useState, useEffect } from "react";
import { ChatService } from "@/lib/api";
import type { ModelPreset, AppSettings } from "@/lib/types";
import { cn } from "@/lib/cn";

// Canonical API types supported by the moss kernel.
const MODEL_TYPES = [
  { value: "openai-completions", label: "OpenAI Completions" },
  { value: "openai-responses", label: "OpenAI Responses", badge: "预览" },
  { value: "claude", label: "Claude" },
  { value: "gemini", label: "Gemini" },
] as const;

type ModelTypeValue = (typeof MODEL_TYPES)[number]["value"];

// Sub-groups for the openai-completions type (different OpenAI-compatible endpoints).
const OPENAI_COMPAT_GROUPS = [
  { value: "openai", label: "OpenAI", baseURL: "https://api.openai.com/v1" },
  { value: "deepseek", label: "DeepSeek", baseURL: "https://api.deepseek.com/v1" },
  { value: "ollama", label: "Ollama (本地)", baseURL: "http://localhost:11434/v1" },
  { value: "custom", label: "自定义", baseURL: "" },
] as const;

const DEFAULT_BASE_URLS: Record<string, string> = {
  "openai-completions": "https://api.openai.com/v1",
  "openai-responses": "https://api.openai.com/v1",
  claude: "https://api.anthropic.com",
  gemini: "",
};

const MODEL_TYPE_LABELS: Record<string, string> = {
  "openai-completions": "OpenAI Completions",
  "openai-responses": "OpenAI Responses",
  claude: "Claude",
  gemini: "Gemini",
};

/** Derives the openai-completions sub-group from the saved base URL. */
function detectSubGroup(baseURL: string): string {
  if (baseURL.includes("deepseek.com")) return "deepseek";
  if (baseURL.includes("localhost:11434") || baseURL.includes("127.0.0.1:11434")) return "ollama";
  if (baseURL.includes("openai.com") || baseURL === "") return "openai";
  return "custom";
}

export default function SettingsView() {
  const [presets, setPresets] = useState<ModelPreset[]>([]);
  const [settings, setSettings] = useState<AppSettings | null>(null);

  const [modelType, setModelType] = useState<ModelTypeValue>("openai-completions");
  const [subGroup, setSubGroup] = useState<string>("openai");
  const [model, setModel] = useState<string>("");
  const [baseURL, setBaseURL] = useState<string>("");
  const [apiKey, setApiKey] = useState<string>("");
  const [showKey, setShowKey] = useState(false);
  const [customModel, setCustomModel] = useState(false);

  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null);

  // Load settings and presets on mount.
  useEffect(() => {
    Promise.all([ChatService.getSettings(), ChatService.getPresetModels()])
      .then(([s, p]) => {
        const cfg = s as AppSettings;
        setSettings(cfg);
        setPresets(p as ModelPreset[]);

        const type = (cfg.provider as ModelTypeValue) || "openai-completions";
        setModelType(type);
        setBaseURL(cfg.baseURL || "");
        setApiKey(cfg.apiKey || "");

        if (type === "openai-completions") {
          const sg = detectSubGroup(cfg.baseURL || "");
          setSubGroup(sg);
          const presetsForGroup = (p as ModelPreset[]).filter(
            (m) => m.provider === type && m.group === sg,
          );
          const inPreset = presetsForGroup.some((m) => m.model === cfg.model);
          setCustomModel(!inPreset && !!cfg.model && sg !== "custom");
        } else {
          const inPreset = (p as ModelPreset[]).some(
            (m) => m.provider === type && m.model === cfg.model,
          );
          setCustomModel(!inPreset && !!cfg.model);
        }
        setModel(cfg.model || "");
      })
      .catch(() => {});
  }, []);

  const modelsForSelection = presets.filter(
    (p) =>
      p.provider === modelType &&
      (modelType !== "openai-completions" || subGroup === "custom" || p.group === subGroup),
  );

  function handleModelTypeChange(type: ModelTypeValue) {
    setModelType(type);
    setCustomModel(false);
    setStatus(null);

    if (type === "openai-completions") {
      const sg = "openai";
      setSubGroup(sg);
      const first = presets.find((m) => m.provider === type && m.group === sg);
      setModel(first?.model ?? "");
      setBaseURL(OPENAI_COMPAT_GROUPS.find((g) => g.value === sg)?.baseURL ?? "");
    } else {
      setSubGroup("");
      const first = presets.find((m) => m.provider === type);
      setModel(first?.model ?? "");
      setBaseURL(DEFAULT_BASE_URLS[type] ?? "");
    }
  }

  function handleSubGroupChange(sg: string) {
    setSubGroup(sg);
    setCustomModel(false);
    setStatus(null);
    const group = OPENAI_COMPAT_GROUPS.find((g) => g.value === sg);
    if (sg !== "custom") {
      setBaseURL(group?.baseURL ?? "");
      const first = presets.find((m) => m.provider === modelType && m.group === sg);
      setModel(first?.model ?? "");
    } else {
      setBaseURL("");
      setModel("");
    }
  }

  function handlePresetModelChange(m: string) {
    if (m === "__custom__") {
      setCustomModel(true);
      setModel("");
    } else {
      setCustomModel(false);
      setModel(m);
    }
    setStatus(null);
  }

  async function handleSave() {
    if (!model.trim()) {
      setStatus({ ok: false, msg: "请输入模型名称" });
      return;
    }
    setSaving(true);
    setStatus(null);
    try {
      await ChatService.updateModel(modelType, model.trim(), baseURL.trim(), apiKey);
      setStatus({
        ok: true,
        msg: `已切换到 ${MODEL_TYPE_LABELS[modelType] ?? modelType} / ${model} ✓`,
      });
      const s = (await ChatService.getSettings()) as AppSettings;
      setSettings(s);
      setApiKey(s.apiKey || "");
    } catch (err: any) {
      setStatus({ ok: false, msg: String(err?.message ?? err ?? "保存失败") });
    } finally {
      setSaving(false);
    }
  }

  const needsAPIKey = !(modelType === "openai-completions" && subGroup === "ollama");

  return (
    <div className="absolute inset-0 left-14 overflow-y-auto bg-surface">
      <div className="max-w-2xl mx-auto px-6 py-10">
        <h1 className="text-2xl font-bold text-on-surface mb-1 font-headline">设置</h1>
        <p className="text-sm text-on-surface-variant mb-8">配置 AI 模型与连接参数</p>

        {/* Model Card */}
        <section className="bg-surface-container rounded-2xl p-6 mb-5 border border-border">
          <h2 className="text-base font-semibold text-on-surface mb-1 flex items-center gap-2">
            <span className="material-symbols-outlined text-primary text-lg">model_training</span>
            模型配置
          </h2>
          <p className="text-xs text-on-surface-variant mb-5">
            当前：
            <span className="font-bold text-primary">
              {settings
                ? `${MODEL_TYPE_LABELS[settings.provider] ?? settings.provider} / ${settings.model || "未设置"}`
                : "加载中…"}
            </span>
          </p>

          {/* Model Type */}
          <div className="mb-4">
            <label className="block text-xs font-semibold text-on-surface-variant mb-1.5 uppercase tracking-wider">
              模型类型
            </label>
            <div className="flex flex-wrap gap-2">
              {MODEL_TYPES.map((t) => (
                <button
                  key={t.value}
                  type="button"
                  onClick={() => handleModelTypeChange(t.value)}
                  className={cn(
                    "px-3 py-1.5 rounded-full text-xs font-bold border transition-colors flex items-center gap-1.5",
                    modelType === t.value
                      ? "bg-primary text-on-primary border-primary"
                      : "border-border text-on-surface-variant hover:bg-surface-container-high",
                  )}
                >
                  {t.label}
                  {"badge" in t && (
                    <span
                      className={cn(
                        "text-[9px] font-bold px-1 py-0.5 rounded",
                        modelType === t.value
                          ? "bg-on-primary/20 text-on-primary"
                          : "bg-primary/10 text-primary",
                      )}
                    >
                      {t.badge}
                    </span>
                  )}
                </button>
              ))}
            </div>
            {modelType === "openai-responses" && (
              <p className="text-[10px] text-on-surface-variant mt-1.5">
                OpenAI Responses API 尚在预览阶段，功能可能受限。
              </p>
            )}
          </div>

          {/* Sub-group selector (only for openai-completions) */}
          {modelType === "openai-completions" && (
            <div className="mb-4">
              <label className="block text-xs font-semibold text-on-surface-variant mb-1.5 uppercase tracking-wider">
                服务商
              </label>
              <div className="flex flex-wrap gap-2">
                {OPENAI_COMPAT_GROUPS.map((g) => (
                  <button
                    key={g.value}
                    type="button"
                    onClick={() => handleSubGroupChange(g.value)}
                    className={cn(
                      "px-3 py-1.5 rounded-full text-xs font-bold border transition-colors",
                      subGroup === g.value
                        ? "bg-secondary text-on-secondary border-secondary"
                        : "border-border text-on-surface-variant hover:bg-surface-container-high",
                    )}
                  >
                    {g.label}
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Model selector */}
          <div className="mb-4">
            <label className="block text-xs font-semibold text-on-surface-variant mb-1.5 uppercase tracking-wider">
              模型
            </label>
            {modelsForSelection.length > 0 && (
              <select
                value={customModel ? "__custom__" : model}
                onChange={(e) => handlePresetModelChange(e.target.value)}
                className="w-full px-3 py-2 rounded-xl border border-border bg-surface text-on-surface text-sm mb-2 focus:outline-none focus:ring-2 focus:ring-primary/30"
              >
                {modelsForSelection.map((m) => (
                  <option key={m.model} value={m.model}>
                    {m.label} ({m.model})
                  </option>
                ))}
                <option value="__custom__">自定义…</option>
              </select>
            )}
            {(customModel || modelsForSelection.length === 0) && (
              <input
                type="text"
                placeholder="输入模型 ID，例如 gpt-4o"
                value={model}
                onChange={(e) => {
                  setModel(e.target.value);
                  setStatus(null);
                }}
                className="w-full px-3 py-2 rounded-xl border border-border bg-surface text-on-surface text-sm focus:outline-none focus:ring-2 focus:ring-primary/30"
              />
            )}
          </div>

          {/* Base URL */}
          <div className="mb-4">
            <label className="block text-xs font-semibold text-on-surface-variant mb-1.5 uppercase tracking-wider">
              API 地址
            </label>
            <input
              type="text"
              placeholder={DEFAULT_BASE_URLS[modelType] || "https://api.example.com/v1"}
              value={baseURL}
              onChange={(e) => {
                setBaseURL(e.target.value);
                setStatus(null);
              }}
              className="w-full px-3 py-2 rounded-xl border border-border bg-surface text-on-surface text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary/30"
            />
            <p className="text-[10px] text-on-surface-variant mt-1">
              留空使用默认地址。Ollama 本地服务建议填写 http://localhost:11434/v1
            </p>
          </div>

          {/* API Key */}
          {needsAPIKey && (
            <div className="mb-4">
              <label className="block text-xs font-semibold text-on-surface-variant mb-1.5 uppercase tracking-wider">
                API Key
              </label>
              <div className="relative">
                <input
                  type={showKey ? "text" : "password"}
                  placeholder="sk-…"
                  value={apiKey}
                  onChange={(e) => {
                    setApiKey(e.target.value);
                    setStatus(null);
                  }}
                  className="w-full px-3 py-2 pr-10 rounded-xl border border-border bg-surface text-on-surface text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary/30"
                />
                <button
                  type="button"
                  onClick={() => setShowKey((v) => !v)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-on-surface-variant hover:text-on-surface"
                >
                  <span className="material-symbols-outlined text-base">
                    {showKey ? "visibility_off" : "visibility"}
                  </span>
                </button>
              </div>
              <p className="text-[10px] text-on-surface-variant mt-1">
                密钥存储在本地配置文件中（明文）。请勿在公共设备使用。
              </p>
            </div>
          )}

          {/* Status */}
          {status && (
            <div
              className={cn(
                "mb-4 px-4 py-2.5 rounded-xl text-sm font-medium",
                status.ok
                  ? "bg-primary-container/40 text-on-primary-container"
                  : "bg-error-container/30 text-error",
              )}
            >
              {status.msg}
            </div>
          )}

          {/* Save button */}
          <button
            type="button"
            onClick={handleSave}
            disabled={saving}
            className={cn(
              "w-full py-2.5 rounded-xl font-bold text-sm transition-all active:scale-95",
              saving
                ? "bg-primary/50 text-on-primary/70 pointer-events-none"
                : "bg-primary text-on-primary shadow-sm hover:opacity-90",
            )}
          >
            {saving ? (
              <span className="flex items-center justify-center gap-2">
                <span className="material-symbols-outlined text-base animate-spin-1s">refresh</span>
                正在应用…
              </span>
            ) : (
              "保存并应用"
            )}
          </button>
        </section>

        {/* About Card */}
        <section className="bg-surface-container rounded-2xl p-6 border border-border">
          <h2 className="text-base font-semibold text-on-surface mb-3 flex items-center gap-2">
            <span className="material-symbols-outlined text-primary text-lg">info</span>
            关于
          </h2>
          <div className="space-y-1.5 text-sm text-on-surface-variant">
            <div className="flex justify-between">
              <span>工作区</span>
              <span className="font-mono text-xs text-on-surface truncate max-w-[60%]">
                {settings?.workers !== undefined ? `${settings.workers} workers` : "—"}
              </span>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
