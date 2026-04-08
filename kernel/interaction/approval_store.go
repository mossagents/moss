package interaction

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ApprovalStatus 表示审批记录的当前状态。
type ApprovalStatus string

const (
	ApprovalStatusPending   ApprovalStatus = "pending"
	ApprovalStatusApproved  ApprovalStatus = "approved"
	ApprovalStatusDenied    ApprovalStatus = "denied"
	ApprovalStatusTimedOut  ApprovalStatus = "timed_out"
	ApprovalStatusCancelled ApprovalStatus = "cancelled"
)

// ApprovalRecord 持久化记录一次审批的全生命周期。
type ApprovalRecord struct {
	Request    ApprovalRequest   `json:"request"`
	Decision   *ApprovalDecision `json:"decision,omitempty"`
	Status     ApprovalStatus    `json:"status"`
	CreatedAt  time.Time         `json:"created_at"`
	ResolvedAt *time.Time        `json:"resolved_at,omitempty"`
}

// ApprovalStore 提供审批记录的持久化与查询能力。
type ApprovalStore interface {
	// Save 保存审批记录（插入或更新）。
	Save(ctx context.Context, record ApprovalRecord) error
	// Get 按请求 ID 查询审批记录。
	Get(ctx context.Context, requestID string) (*ApprovalRecord, error)
	// List 列出指定 session 的所有审批记录，按创建时间升序。
	List(ctx context.Context, sessionID string) ([]ApprovalRecord, error)
}

// ErrApprovalNotFound 表示审批记录不存在。
var ErrApprovalNotFound = errors.New("approval record not found")

// MemoryApprovalStore 是 ApprovalStore 的内存实现，线程安全。
type MemoryApprovalStore struct {
	mu      sync.RWMutex
	records map[string]ApprovalRecord // key: request.ID
}

// NewMemoryApprovalStore 创建内存 ApprovalStore。
func NewMemoryApprovalStore() *MemoryApprovalStore {
	return &MemoryApprovalStore{records: make(map[string]ApprovalRecord)}
}

func (s *MemoryApprovalStore) Save(_ context.Context, record ApprovalRecord) error {
	if record.Request.ID == "" {
		return fmt.Errorf("approval store: request ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.Request.ID] = record
	return nil
}

func (s *MemoryApprovalStore) Get(_ context.Context, requestID string) (*ApprovalRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[requestID]
	if !ok {
		return nil, ErrApprovalNotFound
	}
	cp := r
	return &cp, nil
}

func (s *MemoryApprovalStore) List(_ context.Context, sessionID string) ([]ApprovalRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []ApprovalRecord
	for _, r := range s.records {
		if sessionID == "" || r.Request.SessionID == sessionID {
			result = append(result, r)
		}
	}
	// 按创建时间升序排序
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].CreatedAt.Before(result[j-1].CreatedAt); j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result, nil
}
