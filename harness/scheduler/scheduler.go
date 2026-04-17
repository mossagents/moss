// Package scheduler 提供轻量级的定时任务调度能力。
//
// 支持两种调度表达式：
//   - 间隔模式: "@every 30m", "@every 6h", "@every 1h30m"
//   - 一次性模式: "@once" （仅执行一次，执行后自动移除）
//
// 用法:
//
//	sched := scheduler.New()
//	sched.AddJob(scheduler.Job{
//	    ID:       "crawl-news",
//	    Schedule: "@every 6h",
//	    Goal:     "Crawl news.ycombinator.com and save top stories",
//	    Config:   session.SessionConfig{...},
//	})
//	sched.Start(ctx, func(ctx context.Context, job Job) { ... })
//	defer sched.Stop()
package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mossagents/moss/kernel/session"
)

// Job 表示一个定时任务。
type Job struct {
	ID       string                `json:"id"`
	Schedule string                `json:"schedule"` // "@every 30m" 或 "@once"
	Goal     string                `json:"goal"`
	Config   session.SessionConfig `json:"config"`
	LastRun  time.Time             `json:"last_run,omitempty"`
	NextRun  time.Time             `json:"next_run,omitempty"`
	RunCount int                   `json:"run_count"`
}

// JobHandler 定义当 Job 触发时的回调。
type JobHandler func(ctx context.Context, job Job)

// Scheduler 管理定时任务的执行。
type Scheduler struct {
	mu                 sync.Mutex
	persistMu          sync.Mutex
	persistSeq         uint64
	persistedSeq       uint64
	jobs               map[string]*jobEntry
	handler            JobHandler
	parentCtx          context.Context
	cancel             context.CancelFunc
	running            bool
	store              JobStore // 可选，持久化存储
	lock               Lock     // 可选，分布式锁
	onPersistError     func(error)
	persistErrCh       chan error
	persistErrCancel   context.CancelFunc
	persistErrWorkerWG sync.WaitGroup
	persistErrDrops    atomic.Uint64
}

type jobEntry struct {
	job    Job
	timer  *time.Timer
	cancel context.CancelFunc
}

// New 创建一个新的 Scheduler。
func New(opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{
		jobs: make(map[string]*jobEntry),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SchedulerOption 是 Scheduler 的函数式配置选项。
type SchedulerOption func(*Scheduler)

// WithPersistence 设置 Job 持久化存储。
func WithPersistence(store JobStore) SchedulerOption {
	return func(s *Scheduler) { s.store = store }
}

// WithLock 设置分布式锁，用于多实例部署时的 Job 去重。
// 未设置时 Job 在每个实例上都会执行。
func WithLock(lock Lock) SchedulerOption {
	return func(s *Scheduler) { s.lock = lock }
}

// WithPersistErrorHandler 设置持久化错误处理回调。
// 回调通过有界异步队列分发，不会阻塞调度器主流程。
func WithPersistErrorHandler(fn func(error)) SchedulerOption {
	return func(s *Scheduler) { s.onPersistError = fn }
}

// AddJob 添加一个定时任务。如果已存在同 ID 的任务，先移除再添加。
func (s *Scheduler) AddJob(job Job) error {
	interval, once, err := parseSchedule(job.Schedule)
	if err != nil {
		return err
	}

	s.mu.Lock()
	// 如果已存在，先停止
	if existing, ok := s.jobs[job.ID]; ok {
		if existing.timer != nil {
			existing.timer.Stop()
		}
		if existing.cancel != nil {
			existing.cancel()
		}
	}

	job.NextRun = time.Now().Add(interval)
	entry := &jobEntry{job: job}
	s.jobs[job.ID] = entry

	// 如果 scheduler 已启动，立即设置 timer
	if s.running && s.handler != nil {
		s.scheduleEntry(entry, interval, once)
	}
	seq, snapshot := s.snapshotJobsLocked()
	s.mu.Unlock()
	s.persistSnapshot(seq, snapshot)
	return nil
}

// RemoveJob 移除指定 ID 的任务。
func (s *Scheduler) RemoveJob(id string) error {
	s.mu.Lock()

	entry, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("job not found: %s", id)
	}

	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	delete(s.jobs, id)
	seq, snapshot := s.snapshotJobsLocked()
	s.mu.Unlock()
	s.persistSnapshot(seq, snapshot)
	return nil
}

// ListJobs 返回所有已注册的任务。
func (s *Scheduler) ListJobs() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs := make([]Job, 0, len(s.jobs))
	for _, entry := range s.jobs {
		jobs = append(jobs, entry.job)
	}
	return jobs
}

// Start 启动调度器，handler 在每次任务触发时被调用。
// ctx 用于全局生命周期管理：当 ctx 取消时，调度器停止且所有 job 的 context 也被取消。
// 启动时会自动从 JobStore 恢复之前持久化的任务。
func (s *Scheduler) Start(ctx context.Context, handler JobHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.parentCtx = ctx
	s.handler = handler
	s.running = true
	s.startPersistErrorWorkerLocked()

	// 从持久化存储恢复 jobs
	if s.store != nil {
		if saved, err := s.store.LoadJobs(ctx); err == nil {
			for _, job := range saved {
				if _, exists := s.jobs[job.ID]; !exists {
					entry := &jobEntry{job: job}
					s.jobs[job.ID] = entry
				}
			}
		}
	}

	// 为所有已注册的 job 启动 timer
	for _, entry := range s.jobs {
		interval, once, err := parseSchedule(entry.job.Schedule)
		if err != nil {
			continue
		}
		s.scheduleEntry(entry, interval, once)
	}

	// 监听 ctx 完成 → 停止所有 timer
	go func() {
		<-ctx.Done()
		s.stopAll()
	}()
}

// Stop 停止调度器，取消所有正在等待的任务。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
	persistCancel := s.persistErrCancel
	s.persistErrCancel = nil
	s.mu.Unlock()

	if persistCancel != nil {
		persistCancel()
		s.persistErrWorkerWG.Wait()
	}
}

// Running 返回调度器是否在运行。
func (s *Scheduler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Count 返回已注册任务数。
func (s *Scheduler) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

// PersistErrorDrops 返回持久化错误回调队列丢弃计数。
func (s *Scheduler) PersistErrorDrops() uint64 {
	return s.persistErrDrops.Load()
}

// Trigger 立即执行指定任务一次，不改变其调度表达式。
func (s *Scheduler) Trigger(id string) error {
	s.mu.Lock()
	entry, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("job not found: %s", id)
	}
	handler := s.handler
	parent := s.parentCtx
	if parent == nil {
		parent = context.Background()
	}
	s.mu.Unlock()

	if handler == nil {
		return fmt.Errorf("scheduler handler is not set")
	}
	go s.fireNow(parent, id, entry)
	return nil
}

func (s *Scheduler) scheduleEntry(entry *jobEntry, interval time.Duration, once bool) {
	// job context 继承 parentCtx，这样全局 cancel 时 handler 内的 ctx 也会响应
	parent := s.parentCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	entry.cancel = cancel

	jobID := entry.job.ID // 捕获 ID 用于闭包内安全比较

	var fire func()
	fire = func() {
		s.executeFire(ctx, jobID, entry, interval, once, cancel, true, fire)
	}

	entry.timer = time.AfterFunc(interval, fire)
}

func (s *Scheduler) fireNow(parent context.Context, jobID string, entry *jobEntry) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	s.executeFire(ctx, jobID, entry, 0, false, func() {}, false, nil)
}

func (s *Scheduler) executeFire(ctx context.Context, jobID string, entry *jobEntry, interval time.Duration, once bool, cancel func(), reschedule bool, rearm func()) {
	s.mu.Lock()
	// 检查 entry 是否仍有效（可能已被 handler 内 RemoveJob 移除）
	current, exists := s.jobs[jobID]
	if !exists || current != entry {
		s.mu.Unlock()
		return
	}
	entry.job.LastRun = time.Now()
	entry.job.RunCount++
	handler := s.handler
	job := entry.job
	lock := s.lock
	seq, snapshot := s.snapshotJobsLocked()
	s.mu.Unlock()
	s.persistSnapshot(seq, snapshot)

	// 分布式锁：多实例部署时防止重复执行
	acquired := true
	var lockUnlock func()
	lockTTL := interval
	if lockTTL <= 0 {
		lockTTL = time.Minute
	}
	if lock != nil {
		u, lockErr := lock.TryLock(ctx, "scheduler:"+jobID, lockTTL)
		if lockErr != nil {
			acquired = false
		} else {
			lockUnlock = u
		}
	}

	if acquired && handler != nil {
		handler(ctx, job)
	}
	if lockUnlock != nil {
		lockUnlock()
	}

	if once {
		// @once/@after 任务：handler 完成后才移除（不会 cancel 正在执行的 ctx）
		var (
			seq      uint64
			snapshot []Job
		)
		s.mu.Lock()
		if e, ok := s.jobs[jobID]; ok && e == entry {
			if e.timer != nil {
				e.timer.Stop()
			}
			delete(s.jobs, jobID)
			seq, snapshot = s.snapshotJobsLocked()
		}
		s.mu.Unlock()
		s.persistSnapshot(seq, snapshot)
		cancel()
		return
	}

	if reschedule {
		// 重新调度下一次执行
		var (
			seq      uint64
			snapshot []Job
		)
		s.mu.Lock()
		if e, ok := s.jobs[jobID]; ok && e == entry && s.running {
			e.job.NextRun = time.Now().Add(interval)
			if rearm != nil {
				e.timer = time.AfterFunc(interval, rearm)
			}
			seq, snapshot = s.snapshotJobsLocked()
		}
		s.mu.Unlock()
		s.persistSnapshot(seq, snapshot)
	}
}

func (s *Scheduler) stopAll() {
	s.mu.Lock()
	for _, entry := range s.jobs {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		if entry.cancel != nil {
			entry.cancel()
		}
	}
	s.running = false
	persistCancel := s.persistErrCancel
	s.persistErrCancel = nil
	s.mu.Unlock()

	if persistCancel != nil {
		persistCancel()
		s.persistErrWorkerWG.Wait()
	}
}

// snapshotJobsLocked 在持有锁的状态下复制 jobs 快照。
// 调用方必须已持有 s.mu。
func (s *Scheduler) snapshotJobsLocked() (uint64, []Job) {
	s.persistSeq++
	if len(s.jobs) == 0 {
		return s.persistSeq, nil
	}
	jobs := make([]Job, 0, len(s.jobs))
	for _, e := range s.jobs {
		jobs = append(jobs, e.job)
	}
	return s.persistSeq, jobs
}

// persistSnapshot 在锁外持久化，避免阻塞调度器锁竞争路径。
func (s *Scheduler) persistSnapshot(seq uint64, jobs []Job) {
	if s.store == nil {
		return
	}
	if seq == 0 {
		return
	}
	if jobs == nil {
		jobs = []Job{}
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if seq < s.persistedSeq {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.store.SaveJobs(ctx, jobs); err != nil {
		s.dispatchPersistError(err)
		return
	}
	s.persistedSeq = seq
}

func (s *Scheduler) startPersistErrorWorkerLocked() {
	if s.onPersistError == nil || s.persistErrCancel != nil {
		return
	}
	if s.persistErrCh == nil {
		s.persistErrCh = make(chan error, 64)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.persistErrCancel = cancel
	s.persistErrWorkerWG.Add(1)
	go func() {
		defer s.persistErrWorkerWG.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-s.persistErrCh:
				if err != nil {
					s.onPersistError(err)
				}
			}
		}
	}()
}

func (s *Scheduler) dispatchPersistError(err error) {
	if err == nil || s.onPersistError == nil {
		return
	}
	s.mu.Lock()
	ch := s.persistErrCh
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- err:
	default:
		s.persistErrDrops.Add(1)
	}
}

// parseSchedule 解析调度表达式。
// 支持: "@every 30m", "@every 1h", "@every 1h30m", "@after 10m", "@once"（默认 0 延迟）
func parseSchedule(expr string) (interval time.Duration, once bool, err error) {
	expr = strings.TrimSpace(expr)

	if expr == "@once" {
		return 0, true, nil
	}

	if strings.HasPrefix(expr, "@every ") {
		durStr := strings.TrimPrefix(expr, "@every ")
		d, err := time.ParseDuration(durStr)
		if err != nil {
			return 0, false, fmt.Errorf("invalid duration %q: %w", durStr, err)
		}
		if d < time.Second {
			return 0, false, fmt.Errorf("interval too short: %s (minimum 1s)", d)
		}
		return d, false, nil
	}

	if strings.HasPrefix(expr, "@after ") {
		durStr := strings.TrimPrefix(expr, "@after ")
		d, err := time.ParseDuration(durStr)
		if err != nil {
			return 0, false, fmt.Errorf("invalid duration %q: %w", durStr, err)
		}
		if d < time.Second {
			return 0, false, fmt.Errorf("interval too short: %s (minimum 1s)", d)
		}
		return d, true, nil
	}

	return 0, false, fmt.Errorf("unsupported schedule: %q (use '@every <duration>', '@after <duration>' or '@once')", expr)
}
