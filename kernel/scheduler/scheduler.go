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
	"time"

	"github.com/mossagi/moss/kernel/session"
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
	mu      sync.Mutex
	jobs    map[string]*jobEntry
	handler JobHandler
	cancel  context.CancelFunc
	running bool
}

type jobEntry struct {
	job    Job
	timer  *time.Timer
	cancel context.CancelFunc
}

// New 创建一个新的 Scheduler。
func New() *Scheduler {
	return &Scheduler{
		jobs: make(map[string]*jobEntry),
	}
}

// AddJob 添加一个定时任务。如果已存在同 ID 的任务，先移除再添加。
func (s *Scheduler) AddJob(job Job) error {
	interval, once, err := parseSchedule(job.Schedule)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果已存在，先停止
	if existing, ok := s.jobs[job.ID]; ok {
		existing.timer.Stop()
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

	return nil
}

// RemoveJob 移除指定 ID 的任务。
func (s *Scheduler) RemoveJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	delete(s.jobs, id)
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
func (s *Scheduler) Start(ctx context.Context, handler JobHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.handler = handler
	s.running = true

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
	s.mu.Unlock()
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

func (s *Scheduler) scheduleEntry(entry *jobEntry, interval time.Duration, once bool) {
	ctx, cancel := context.WithCancel(context.Background())
	entry.cancel = cancel

	var fire func()
	fire = func() {
		s.mu.Lock()
		entry.job.LastRun = time.Now()
		entry.job.RunCount++
		handler := s.handler
		job := entry.job
		s.mu.Unlock()

		if handler != nil {
			handler(ctx, job)
		}

		if once {
			_ = s.RemoveJob(job.ID)
			return
		}

		// 重新调度下一次执行
		s.mu.Lock()
		if e, ok := s.jobs[job.ID]; ok && s.running {
			e.job.NextRun = time.Now().Add(interval)
			e.timer = time.AfterFunc(interval, fire)
		}
		s.mu.Unlock()
	}

	entry.timer = time.AfterFunc(interval, fire)
}

func (s *Scheduler) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.jobs {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		if entry.cancel != nil {
			entry.cancel()
		}
	}
	s.running = false
}

// parseSchedule 解析调度表达式。
// 支持: "@every 30m", "@every 1h", "@every 1h30m", "@once"（默认 0 延迟）
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

	return 0, false, fmt.Errorf("unsupported schedule: %q (use '@every <duration>' or '@once')", expr)
}
