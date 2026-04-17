// Package errors 提供 Moss Kernel 的结构化错误类型。
//
// 所有 Kernel 子系统返回的错误均可通过 errors.As 解析为 *Error，
// 获取机器可读的错误码、可重试标志和附加上下文。
package errors

import (
	"errors"
	"fmt"
)

// Code 是错误分类码，用于机器可读的错误识别。
type Code string

const (
	// ── Budget ────────────────────────────────────────
	ErrBudgetExhausted Code = "BUDGET_EXHAUSTED"

	// ── Tool ─────────────────────────────────────────
	ErrToolNotFound      Code = "TOOL_NOT_FOUND"
	ErrToolExecution     Code = "TOOL_EXECUTION"
	ErrToolTimeout       Code = "TOOL_TIMEOUT"
	ErrToolSchemaInvalid Code = "TOOL_SCHEMA_INVALID"

	// ── LLM ──────────────────────────────────────────
	ErrLLMCall     Code = "LLM_CALL"
	ErrLLMTimeout  Code = "LLM_TIMEOUT"
	ErrLLMRejected Code = "LLM_REJECTED" // 熔断器拒绝

	// ── Sandbox ──────────────────────────────────────
	ErrSandboxDenied  Code = "SANDBOX_DENIED"
	ErrSandboxIO      Code = "SANDBOX_IO"
	ErrSandboxTimeout Code = "SANDBOX_TIMEOUT"

	// ── Session ──────────────────────────────────────
	ErrSessionNotFound Code = "SESSION_NOT_FOUND"
	ErrSessionRunning  Code = "SESSION_RUNNING"

	// ── Policy / Auth ────────────────────────────────
	ErrPolicyDenied Code = "POLICY_DENIED"
	ErrRateLimit    Code = "RATE_LIMIT"

	// ── Checkpoint ───────────────────────────────────
	ErrCheckpointFailed   Code = "CHECKPOINT_FAILED"
	ErrCheckpointNotFound Code = "CHECKPOINT_NOT_FOUND"

	// ── Agent Delegation ─────────────────────────────
	ErrDelegationDepth    Code = "DELEGATION_DEPTH"
	ErrDelegationContract Code = "DELEGATION_CONTRACT"
	ErrAgentNotFound      Code = "AGENT_NOT_FOUND"

	// ── General ──────────────────────────────────────
	ErrValidation Code = "VALIDATION"
	ErrShutdown   Code = "SHUTDOWN"
	ErrInternal   Code = "INTERNAL"
)

// Error 是 Moss Kernel 的结构化错误。
type Error struct {
	Code         Code           `json:"code"`
	Message      string         `json:"message"`
	Cause        error          `json:"-"`
	CauseMessage string         `json:"cause,omitempty"`
	Retryable    bool           `json:"retryable"`
	Meta         map[string]any `json:"meta,omitempty"`
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// Is 支持 errors.Is 匹配：相同 Code 视为同一类错误。
func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// New 创建一个新的结构化错误。
func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// Wrap 包装一个底层错误为结构化错误。
func Wrap(code Code, msg string, cause error) *Error {
	e := &Error{Code: code, Message: msg, Cause: cause}
	if cause != nil {
		e.CauseMessage = cause.Error()
	}
	return e
}

// Retryable 创建一个可重试的结构化错误。
func Retryable(code Code, msg string, cause error) *Error {
	e := &Error{Code: code, Message: msg, Cause: cause, Retryable: true}
	if cause != nil {
		e.CauseMessage = cause.Error()
	}
	return e
}

// WithMeta 向错误添加上下文元数据（链式调用）。
func (e *Error) WithMeta(key string, value any) *Error {
	if e.Meta == nil {
		e.Meta = make(map[string]any)
	}
	e.Meta[key] = value
	return e
}

// IsRetryable 检查错误链中是否有可重试的 errors.Error。
func IsRetryable(err error) bool {
	var ke *Error
	if errors.As(err, &ke) {
		return ke.Retryable
	}
	return false
}

// GetCode 从错误链中提取 errors.Code。
// 如果不是 errors.Error，返回 ErrInternal。
func GetCode(err error) Code {
	var ke *Error
	if errors.As(err, &ke) {
		return ke.Code
	}
	return ErrInternal
}
