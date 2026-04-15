package scheduler

import (
	"context"
	"sync"
	"time"
)

// Lock 接口用于分布式任务去重。
// 多实例部署时，Scheduler 在执行 Job 前先获取锁，
// 确保同一 Job 同一时间只在一个实例执行。
type Lock interface {
	// TryLock 尝试获取锁（非阻塞）。
	// 成功时返回 unlock 函数，调用者执行完后必须调用 unlock。
	// 如果锁已被持有，返回 ErrLockHeld。
	TryLock(ctx context.Context, key string, ttl time.Duration) (unlock func(), err error)
}

// ErrLockHeld 表示锁已被其他持有者持有。
var ErrLockHeld = lockHeldError{}

type lockHeldError struct{}

func (lockHeldError) Error() string { return "lock is held by another holder" }

// LocalLock 是单实例下的 Lock 实现（基于内存互斥锁）。
// 在单进程部署中提供正确的互斥语义。
// 多实例部署时应替换为 RedisLock / EtcdLock。
type LocalLock struct {
	mu   sync.Mutex
	held map[string]bool
}

// NewLocalLock 创建 LocalLock。
func NewLocalLock() *LocalLock {
	return &LocalLock{held: make(map[string]bool)}
}

func (l *LocalLock) TryLock(_ context.Context, key string, ttl time.Duration) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.held[key] {
		return nil, ErrLockHeld
	}

	l.held[key] = true

	// 自动过期：防止忘记 unlock 导致永久死锁
	timer := time.AfterFunc(ttl, func() {
		l.mu.Lock()
		delete(l.held, key)
		l.mu.Unlock()
	})

	unlocked := false
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if !unlocked {
			unlocked = true
			timer.Stop()
			delete(l.held, key)
		}
	}, nil
}
