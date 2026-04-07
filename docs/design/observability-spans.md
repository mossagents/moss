# Observability 全链路 Span 设计

## 问题描述

现有 `port.Observer` 接口基于事件回调（fire-and-forget），
已覆盖 LLM 调用、工具调用、Session 生命周期等事件，
但缺乏分布式 Tracing（Span/Trace）支持和结构化 Metrics 聚合层。

上层应用难以直接接入 OpenTelemetry 或 Prometheus。

## 设计目标

1. 提供 `OTelObserver`：将现有 Observer 事件转化为 OTel Span（向后兼容）
2. 提供 `MetricsObserver`：聚合 LLM/Tool 指标到内建 Metrics Collector
3. 不强制用户使用 OTel SDK，保持可选集成
4. Metrics 层零依赖（纯 Go），支持 Prometheus 文本格式导出

---

## OTel Span 注入

### 方案

Observer 事件（`LLMCallEvent`）已包含完整的时序信息（`StartedAt`, `Duration`），
可以在事件发生后创建"回溯 Span"（backdated span），
无需侵入 Kernel 循环。

```
OnLLMCall(ctx, e):
    span = tracer.Start(ctx, "moss.llm.call",
        WithTimestamp(e.StartedAt),
        WithSpanKind(Client))
    span.SetAttributes(model, tokens, stop_reason, ...)
    if e.Error != nil: span.RecordError(e.Error)
    span.End(WithTimestamp(e.StartedAt + e.Duration))
```

### Span 命名约定

| 事件类型 | Span 名称 | 属性 |
|----------|-----------|------|
| LLM 调用 | `moss.llm.call` | `ai.model`, `ai.usage.prompt_tokens`, `ai.usage.completion_tokens`, `session.id` |
| 工具调用 | `moss.tool.call` | `tool.name`, `tool.risk`, `session.id` |
| Session  | `moss.session` | `session.id`, `session.type` |
| 错误     | `moss.error` | `error.phase`, `error.message` |

属性命名遵循 [OpenTelemetry Semantic Conventions for AI](https://opentelemetry.io/docs/specs/semconv/ai/)。

### 依赖

```
go.opentelemetry.io/otel         (API only, no SDK)
go.opentelemetry.io/otel/trace   (Tracer interface)
```

用户负责配置 TracerProvider（可使用 Jaeger/OTLP/Zipkin 等导出器）。

---

## Metrics 内建层

### 设计

零依赖内建 Metrics，使用原子计数器和滑动桶直方图：

```go
type Collector interface {
    Counter(name string) Counter
    Histogram(name string) Histogram
    Snapshot() []MetricFamily
}

type Counter interface { Inc(); Add(n float64); Value() float64 }
type Histogram interface { Observe(v float64); Snapshot() HistogramSnapshot }
```

### 预置 Metrics

| 名称 | 类型 | 描述 |
|------|------|------|
| `moss_llm_calls_total` | Counter | LLM 调用次数 |
| `moss_llm_errors_total` | Counter | LLM 错误次数 |
| `moss_llm_tokens_total` | Counter | 累计 Token 用量 |
| `moss_llm_duration_seconds` | Histogram | LLM 调用耗时 |
| `moss_tool_calls_total` | Counter | 工具调用次数 |
| `moss_tool_errors_total` | Counter | 工具错误次数 |
| `moss_tool_duration_seconds` | Histogram | 工具调用耗时 |

### 导出

```go
// Prometheus 文本格式
text := collector.ExportPromText()
// http.HandleFunc("/metrics", func(w, r) { w.Write(text) })
```

---

## 集成方式

```go
// 1. 注册 OTel Span Observer
otelObs := observe.NewOTelObserver(otel.GetTracerProvider())
// 2. 注册 Metrics Observer
metricsCollector := metrics.NewMemoryCollector()
metricsObs := metrics.NewObserver(metricsCollector)
// 3. 合并注册
kern := kernel.New(..., kernel.WithObserver(
    port.JoinObservers(otelObs, metricsObs),
))
```

---

## 影响范围

- `kernel/observe/otel_observer.go` — OTelObserver（新包）
- `kernel/metrics/` — Collector, MetricsObserver（新包）
- `go.mod` — 新增 OTel API 依赖（仅 API，无 SDK）
