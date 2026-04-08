package interaction

import (
	"context"
	"fmt"
	"time"
)

// TimedApproval 是 UserIO 的装饰器，为 RequestApproval 添加超时和记录持久化能力。
// 超时后自动返回拒绝决策并将记录状态标记为 timed_out。
type TimedApproval struct {
	inner   UserIO
	store   ApprovalStore
	timeout time.Duration
}

// NewTimedApproval 创建 TimedApproval 装饰器。
// timeout 为 0 时不设超时（行为等同原始 UserIO）。
func NewTimedApproval(inner UserIO, store ApprovalStore, timeout time.Duration) *TimedApproval {
	return &TimedApproval{inner: inner, store: store, timeout: timeout}
}

// Send 直接委托给内层 UserIO。
func (t *TimedApproval) Send(ctx context.Context, msg OutputMessage) error {
	return t.inner.Send(ctx, msg)
}

// Ask 委托给内层 UserIO，非审批类请求不触发超时逻辑。
// 审批类请求（req.Approval != nil）受 timeout 控制，结果持久化到 ApprovalStore。
func (t *TimedApproval) Ask(ctx context.Context, req InputRequest) (InputResponse, error) {
	if req.Approval == nil {
		return t.inner.Ask(ctx, req)
	}
	return t.askWithTimeout(ctx, req)
}

func (t *TimedApproval) askWithTimeout(ctx context.Context, req InputRequest) (InputResponse, error) {
	approvalReq := req.Approval
	now := time.Now()

	// 持久化初始 pending 记录
	record := ApprovalRecord{
		Request:   *approvalReq,
		Status:    ApprovalStatusPending,
		CreatedAt: now,
	}
	if t.store != nil {
		_ = t.store.Save(ctx, record)
	}

	// 为内层调用设置超时
	askCtx := ctx
	var cancel context.CancelFunc
	if t.timeout > 0 {
		askCtx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}

	type result struct {
		resp InputResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := t.inner.Ask(askCtx, req)
		ch <- result{resp, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			t.updateRecord(ctx, record, ApprovalStatusDenied, nil)
			return InputResponse{}, r.err
		}
		status := ApprovalStatusDenied
		if r.resp.Approved {
			status = ApprovalStatusApproved
		}
		t.updateRecord(ctx, record, status, r.resp.Decision)
		return r.resp, nil

	case <-askCtx.Done():
		t.updateRecord(ctx, record, ApprovalStatusTimedOut, nil)
		return InputResponse{
			Approved: false,
			Decision: &ApprovalDecision{
				RequestID: approvalReq.ID,
				Approved:  false,
				Type:      ApprovalDecisionDeny,
				Reason:    fmt.Sprintf("approval timed out after %s", t.timeout),
				Source:    "timed_approval",
				DecidedAt: time.Now(),
			},
		}, nil
	}
}

func (t *TimedApproval) updateRecord(ctx context.Context, r ApprovalRecord, status ApprovalStatus, decision *ApprovalDecision) {
	if t.store == nil {
		return
	}
	now := time.Now()
	r.Status = status
	r.Decision = decision
	r.ResolvedAt = &now
	_ = t.store.Save(ctx, r)
}
