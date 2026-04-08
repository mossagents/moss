package appkit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	kobs "github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

// ServeConfig 配置 Gateway Serve 模式。
type ServeConfig struct {
	// Prompt 是 CLI 终端的输入提示符。
	Prompt string

	// SystemPrompt 为 Gateway 创建 Session 时注入的系统提示词。
	SystemPrompt string

	// SessionStore 为 Gateway 路由提供可选的会话持久化支持。
	SessionStore session.SessionStore

	// RouterConfig 会话路由配置。
	RouterConfig session.RouterConfig

	// OnError 可选的错误回调。
	OnError func(error)

	// DeliveryDir 为 gateway 可靠投递配置持久化目录（可选）。
	DeliveryDir string

	// RouteScope 会话路由键粒度（main/per-peer/per-channel-peer/per-account-channel-peer）。
	RouteScope string
}

// Serve 以 Gateway 模式运行：CLI Channel → Router → Kernel.Run → 回复。
//
// 这是 REPL 的 Gateway 替代方案，基于 P0 引入的 Channel + Router 抽象。
// 与 REPL 不同，Serve 通过 Channel 接口驱动，可扩展到 WebSocket 等通道。
func Serve(ctx context.Context, cfg ServeConfig, k *kernel.Kernel) error {
	return runtime.ServeCLI(ctx, runtime.ServeConfig{
		Prompt:       cfg.Prompt,
		SystemPrompt: cfg.SystemPrompt,
		SessionStore: cfg.SessionStore,
		RouterConfig: cfg.RouterConfig,
		OnError:      cfg.OnError,
		DeliveryDir:  cfg.DeliveryDir,
		RouteScope:   cfg.RouteScope,
	}, k)
}

// HealthStatus enumerates the possible kernel health states.
type HealthStatus string

const (
	HealthStatusOK          HealthStatus = "ok"
	HealthStatusShuttingDown HealthStatus = "shutting_down"
)

// HealthOutput captures a point-in-time operability snapshot of the kernel.
// It satisfies the P2-SERVE-001 acceptance criteria:
//
//	status       — "ok" | "shutting_down"
//	active_runs  — number of currently executing agent loops
//	latency      — avg LLM / tool latency sourced from P2-OBS-001 metrics
type HealthOutput struct {
	Status           HealthStatus `json:"status"`
	ActiveRuns       int          `json:"active_runs"`
	LLMLatencyAvgMs  float64      `json:"llm_latency_avg_ms"`
	ToolLatencyAvgMs float64      `json:"tool_latency_avg_ms"`
	SuccessRate      float64      `json:"success_rate"`
	ToolErrorRate    float64      `json:"tool_error_rate"`
	TotalRuns        float64      `json:"total_runs"`
}

// Health returns a point-in-time health snapshot for the kernel.
//
// metrics is an optional normalized metrics snapshot from P2-OBS-001
// (kernel/observe.NormalizedMetricsSnapshot).  If omitted, latency and
// success-rate fields default to zero.
func Health(k *kernel.Kernel, metrics ...kobs.NormalizedMetricsSnapshot) HealthOutput {
	status := HealthStatusOK
	if k.IsShuttingDown() {
		status = HealthStatusShuttingDown
	}

	out := HealthOutput{
		Status:     status,
		ActiveRuns: k.ActiveRunCount(),
	}

	if len(metrics) > 0 {
		m := metrics[0].Map()
		out.LLMLatencyAvgMs = m["latency.llm_avg_ms"]
		out.ToolLatencyAvgMs = m["latency.tool_avg_ms"]
		out.SuccessRate = m["success.rate"]
		out.ToolErrorRate = m["tool_error.rate"]
		out.TotalRuns = m["success.run_total"]
	}

	return out
}

// HealthJSON serializes a health snapshot to a JSON string.
// Returns an error string if serialization fails.
func HealthJSON(k *kernel.Kernel, metrics ...kobs.NormalizedMetricsSnapshot) string {
	h := Health(k, metrics...)
	b, err := json.Marshal(h)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}

// HealthText returns a human-readable, single-line health summary.
//
// Example output:
//
//	status=ok active_runs=2 llm_latency_avg_ms=1234.5 tool_latency_avg_ms=45.0 success_rate=0.97 tool_error_rate=0.01 total_runs=100
func HealthText(k *kernel.Kernel, metrics ...kobs.NormalizedMetricsSnapshot) string {
	h := Health(k, metrics...)
	return fmt.Sprintf(
		"status=%s active_runs=%d llm_latency_avg_ms=%.1f tool_latency_avg_ms=%.1f success_rate=%.4f tool_error_rate=%.4f total_runs=%.0f",
		h.Status,
		h.ActiveRuns,
		h.LLMLatencyAvgMs,
		h.ToolLatencyAvgMs,
		h.SuccessRate,
		h.ToolErrorRate,
		h.TotalRuns,
	)
}

