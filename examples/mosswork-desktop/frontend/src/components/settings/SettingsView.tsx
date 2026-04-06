import { useState, useEffect } from "react";
import { ChatService } from "@/lib/api";
import type { ModelPreset, AppSettings } from "@/lib/types";
import { cn } from "@/lib/cn";

const PROVIDERS = ["openai", "anthropic", "deepseek", "ollama", "custom"] as const;

const PROVIDER_LABELS: Record<string, string> = {
  openai: "OpenAI",
  anthropic: "Anthropic",
  deepseek: "DeepSeek",
  ollama: "Ollama (本地)",
  custom: "自定义",
};

const DEFAULT_BASE_URLS: Record<string, string> = {
  openai: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com",
  deepseek: "https://api.deepseek.com/v1",
  ollama: "http://localhost:11434/v1",
  custom: "",
};

export default function SettingsView() {
  const [presets, setPresets] = useState<ModelPreset[]>([]);
  const [settings, setSettings] = useState<AppSettings | null>(null);

  const [provider, setProvider] = useState<string>("openai");
  const [model, setModel] = useState<string>("");
  const [baseURL, setBaseURL] = useState<string>("");
  const [apiKey, setApiKey] = useState<string>("");
  const [showKey, setShowKey] = useState(false);
  const [customModel, setCustomModel] = useState(false);

  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null);

  // Load settings and presets on mount
  useEffect(() => {
    Promise.all([
      ChatService.getSettings(),
      ChatService.getPresetModels(),
    ]).then(([s, p]) => {
      const cfg = s as AppSettings;
      setSettings(cfg);
      setPresets(p as ModelPreset[]);
      const prov = cfg.provider || "openai";
      setProvider(prov);
      setModel(cfg.model || "");
      setBaseURL(cfg.baseURL || "");
      setApiKey(cfg.apiKey || "");
      // If saved model isn't in presets for that provider, use custom
      const inPreset = (p as ModelPreset[]).some(
        (m) => m.provider === prov && m.model === cfg.model,
      );
      setCustomModel(!inPreset && !!cfg.model);
    }).catch(() => {});
  }, []);

  const modelsForProvider = presets.filter((p) => p.provider === provider);

  function handleProviderChange(p: string) {
    setProvider(p);
    setCustomModel(false);
    const first = presets.find((m) => m.provider === p);
    setModel(first?.model ?? "");
    setBaseURL(p !== "custom" ? (DEFAULT_BASE_URLS[p] ?? "") : "");
    setStatus(null);
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
      await ChatService.updateModel(provider, model.trim(), baseURL.trim(), apiKey);
      setStatus({ ok: true, msg: `已切换到 ${provider} / ${model} ✓` });
      // Refresh settings
      const s = await ChatService.getSettings() as AppSettings;
      setSettings(s);
      setApiKey(s.apiKey || "");
    } catch (err: any) {
      setStatus({ ok: false, msg: String(err?.message ?? err ?? "保存失败") });
    } finally {
      setSaving(false);
    }
  }

  const needsAPIKey = provider !== "ollama";

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
              {settings ? `${settings.provider} / ${settings.model || "未设置"}` : "加载中…"}
            </span>
          </p>

          {/* Provider */}
          <div className="mb-4">
            <label className="block text-xs font-semibold text-on-surface-variant mb-1.5 uppercase tracking-wider">
              服务商
            </label>
            <div className="flex flex-wrap gap-2">
              {PROVIDERS.map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => handleProviderChange(p)}
                  className={cn(
                    "px-3 py-1.5 rounded-full text-xs font-bold border transition-colors",
                    provider === p
                      ? "bg-primary text-on-primary border-primary"
                      : "border-border text-on-surface-variant hover:bg-surface-container-high",
                  )}
                >
                  {PROVIDER_LABELS[p]}
                </button>
              ))}
            </div>
          </div>

          {/* Model selector */}
          <div className="mb-4">
            <label className="block text-xs font-semibold text-on-surface-variant mb-1.5 uppercase tracking-wider">
              模型
            </label>
            {modelsForProvider.length > 0 && (
              <select
                value={customModel ? "__custom__" : model}
                onChange={(e) => handlePresetModelChange(e.target.value)}
                className="w-full px-3 py-2 rounded-xl border border-border bg-surface text-on-surface text-sm mb-2 focus:outline-none focus:ring-2 focus:ring-primary/30"
              >
                {modelsForProvider.map((m) => (
                  <option key={m.model} value={m.model}>
                    {m.label} ({m.model})
                  </option>
                ))}
                <option value="__custom__">自定义…</option>
              </select>
            )}
            {(customModel || modelsForProvider.length === 0) && (
              <input
                type="text"
                placeholder="输入模型 ID，例如 gpt-4o"
                value={model}
                onChange={(e) => { setModel(e.target.value); setStatus(null); }}
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
              placeholder={DEFAULT_BASE_URLS[provider] || "https://api.example.com/v1"}
              value={baseURL}
              onChange={(e) => { setBaseURL(e.target.value); setStatus(null); }}
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
                  onChange={(e) => { setApiKey(e.target.value); setStatus(null); }}
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
