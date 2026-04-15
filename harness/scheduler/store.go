package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// JobStore 提供 Job 的持久化存储能力。
type JobStore interface {
	SaveJobs(ctx context.Context, jobs []Job) error
	LoadJobs(ctx context.Context) ([]Job, error)
}

// FileJobStore 基于文件系统的 JobStore 实现。
// 所有 Job 保存为单个 JSON 文件。
type FileJobStore struct {
	path string
	mu   sync.Mutex
}

// NewFileJobStore 创建文件存储，path 为 JSON 文件路径（目录不存在则自动创建）。
func NewFileJobStore(path string) (*FileJobStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create job store dir: %w", err)
	}
	return &FileJobStore{path: path}, nil
}

func (fs *FileJobStore) SaveJobs(_ context.Context, jobs []Job) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal jobs: %w", err)
	}

	tmpPath := fs.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write jobs tmp: %w", err)
	}
	return os.Rename(tmpPath, fs.path)
}

func (fs *FileJobStore) LoadJobs(_ context.Context) ([]Job, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read jobs: %w", err)
	}

	var jobs []Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, fmt.Errorf("unmarshal jobs: %w", err)
	}
	return jobs, nil
}
