package knowledge

import (
	"context"
	"github.com/mossagents/moss/kernel/session"
	"sync"
)

// WorkingMemory 管理当前 session 的激活状态（对话摘要、临时变量、当前目标）。
// 生命周期与 Session 绑定，通过 Session.State KV 存储。
type WorkingMemory struct {
	mu   sync.RWMutex
	data map[string]any
}

// NewWorkingMemory 创建新的工作记忆。
func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{data: make(map[string]any)}
}

// NewWorkingMemoryFromSession 从现有 Session.State 初始化工作记忆（共享存储）。
func NewWorkingMemoryFromSession(sess *session.Session) *WorkingMemory {
	wm := &WorkingMemory{data: make(map[string]any)}
	// 从 session 恢复已有工作记忆
	if v, ok := sess.GetState("__working_memory__"); ok {
		if m, ok := v.(map[string]any); ok {
			for k, val := range m {
				wm.data[k] = val
			}
		}
	}
	return wm
}

// Set 设置一个工作记忆条目。
func (w *WorkingMemory) Set(_ context.Context, key string, value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data[key] = value
	return nil
}

// Get 获取一个工作记忆条目。
func (w *WorkingMemory) Get(_ context.Context, key string) (any, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	v, ok := w.data[key]
	return v, ok
}

// Delete 删除一个工作记忆条目。
func (w *WorkingMemory) Delete(_ context.Context, key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.data, key)
	return nil
}

// Summary 生成当前工作记忆的文本摘要，用于注入 context。
func (w *WorkingMemory) Summary(_ context.Context) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if len(w.data) == 0 {
		return ""
	}
	var parts []string
	for k, v := range w.data {
		parts = append(parts, k+": "+anyToString(v))
	}
	// 排序确保输出稳定
	sortStrings(parts)
	result := ""
	for _, p := range parts {
		result += p + "\n"
	}
	return result
}

// Clear 清空所有工作记忆。
func (w *WorkingMemory) Clear(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = make(map[string]any)
	return nil
}

// All 返回所有工作记忆的副本。
func (w *WorkingMemory) All() map[string]any {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make(map[string]any, len(w.data))
	for k, v := range w.data {
		out[k] = v
	}
	return out
}

// SyncToSession 将工作记忆持久化到 session.State（用于 session 保存前同步）。
func (w *WorkingMemory) SyncToSession(sess *session.Session) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make(map[string]any, len(w.data))
	for k, v := range w.data {
		out[k] = v
	}
	sess.SetState("__working_memory__", out)
}
