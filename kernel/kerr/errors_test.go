package kerr

import (
	"errors"
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(ErrToolNotFound, "tool 'foo' not found")
	if err.Code != ErrToolNotFound {
		t.Errorf("expected code %s, got %s", ErrToolNotFound, err.Code)
	}
	if err.Retryable {
		t.Error("expected non-retryable")
	}
	want := "[TOOL_NOT_FOUND] tool 'foo' not found"
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
}

func TestWrap(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := Wrap(ErrLLMCall, "failed to call model", cause)

	if !errors.Is(err, cause) {
		t.Error("Unwrap should recover cause via errors.Is")
	}
	if err.Code != ErrLLMCall {
		t.Errorf("expected code %s, got %s", ErrLLMCall, err.Code)
	}
	if err.Cause != cause {
		t.Error("Cause should match")
	}
}

func TestRetryable(t *testing.T) {
	err := Retryable(ErrLLMTimeout, "request timed out", nil)
	if !err.Retryable {
		t.Error("expected retryable")
	}
	if !IsRetryable(err) {
		t.Error("IsRetryable should return true")
	}

	err2 := New(ErrToolNotFound, "not found")
	if IsRetryable(err2) {
		t.Error("IsRetryable should return false for non-retryable")
	}
}

func TestWithMeta(t *testing.T) {
	err := New(ErrToolExecution, "execution failed").
		WithMeta("tool_name", "run_command").
		WithMeta("session_id", "sess_123")

	if err.Meta["tool_name"] != "run_command" {
		t.Errorf("expected tool_name=run_command, got %v", err.Meta["tool_name"])
	}
	if err.Meta["session_id"] != "sess_123" {
		t.Errorf("expected session_id=sess_123, got %v", err.Meta["session_id"])
	}
}

func TestGetCode(t *testing.T) {
	err := New(ErrPolicyDenied, "denied")
	if GetCode(err) != ErrPolicyDenied {
		t.Errorf("expected %s, got %s", ErrPolicyDenied, GetCode(err))
	}

	// 普通 error 返回 ErrInternal
	plain := fmt.Errorf("something went wrong")
	if GetCode(plain) != ErrInternal {
		t.Errorf("expected %s for plain error, got %s", ErrInternal, GetCode(plain))
	}

	// wrapped error
	wrapped := fmt.Errorf("outer: %w", err)
	if GetCode(wrapped) != ErrPolicyDenied {
		t.Errorf("expected %s for wrapped error, got %s", ErrPolicyDenied, GetCode(wrapped))
	}
}

func TestErrorIs(t *testing.T) {
	err1 := New(ErrBudgetExhausted, "tokens exceeded")
	err2 := New(ErrBudgetExhausted, "steps exceeded")

	if !errors.Is(err1, err2) {
		t.Error("errors with same Code should match via errors.Is")
	}

	err3 := New(ErrToolNotFound, "not found")
	if errors.Is(err1, err3) {
		t.Error("errors with different Code should not match")
	}
}

func TestErrorWithCauseString(t *testing.T) {
	cause := fmt.Errorf("dial tcp: timeout")
	err := Wrap(ErrLLMTimeout, "model call timeout", cause)
	want := "[LLM_TIMEOUT] model call timeout: dial tcp: timeout"
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
}
