package gateway

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

type Task func(context.Context) error

type Future interface {
	Wait(ctx context.Context) error
}

type future struct {
	done chan error
}

func newFuture() *future {
	return &future{done: make(chan error, 1)}
}

func (f *future) resolve(err error) {
	f.done <- err
}

func (f *future) Wait(ctx context.Context) error {
	select {
	case err := <-f.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type LaneStats struct {
	Depth  int
	Active int
	Max    int
}

type laneItem struct {
	ctx    context.Context
	task   Task
	future *future
}

type laneState struct {
	queue  []laneItem
	active int
	max    int
}

type LaneQueue struct {
	mu          sync.Mutex
	lanes       map[string]*laneState
	defaultMax  int
	concurrency map[string]int
}

func NewLaneQueue() *LaneQueue {
	return &LaneQueue{
		lanes:       make(map[string]*laneState),
		defaultMax:  1,
		concurrency: make(map[string]int),
	}
}

func (q *LaneQueue) SetLaneConcurrency(lane string, max int) {
	if max <= 0 {
		max = 1
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.concurrency[lane] = max
	if st, ok := q.lanes[lane]; ok {
		st.max = max
		q.pumpLaneLocked(lane, st)
	}
}

func (q *LaneQueue) Enqueue(ctx context.Context, lane string, task Task) Future {
	if lane == "" {
		lane = "main"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fut := newFuture()
	q.mu.Lock()
	st := q.getOrCreateLaneLocked(lane)
	st.queue = append(st.queue, laneItem{ctx: ctx, task: task, future: fut})
	q.pumpLaneLocked(lane, st)
	q.mu.Unlock()
	return fut
}

func (q *LaneQueue) Stats() map[string]LaneStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make(map[string]LaneStats, len(q.lanes))
	for lane, st := range q.lanes {
		out[lane] = LaneStats{
			Depth:  len(st.queue),
			Active: st.active,
			Max:    st.max,
		}
	}
	return out
}

func (q *LaneQueue) getOrCreateLaneLocked(lane string) *laneState {
	if st, ok := q.lanes[lane]; ok {
		return st
	}
	max := q.defaultMax
	if v, ok := q.concurrency[lane]; ok && v > 0 {
		max = v
	}
	st := &laneState{max: max}
	q.lanes[lane] = st
	return st
}

func (q *LaneQueue) pumpLaneLocked(lane string, st *laneState) {
	for st.active < st.max && len(st.queue) > 0 {
		item := st.queue[0]
		st.queue = st.queue[1:]
		st.active++
		go q.runTask(lane, item)
	}
}

func (q *LaneQueue) runTask(lane string, item laneItem) {
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("lane task panic: %v\n%s", recovered, debug.Stack())
		}
		item.future.resolve(err)

		q.mu.Lock()
		defer q.mu.Unlock()
		st := q.getOrCreateLaneLocked(lane)
		if st.active > 0 {
			st.active--
		}
		q.pumpLaneLocked(lane, st)
	}()
	if item.task == nil {
		err = fmt.Errorf("nil task")
		return
	}
	err = item.task(item.ctx)
}
