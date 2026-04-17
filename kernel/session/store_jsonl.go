package session

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// loadPersistedSessionFile 兼容两种磁盘格式：
// 1) 旧版单个 JSON 对象
// 2) 新版 append-only JSONL（每行一个 persistedSession 快照）
func loadPersistedSessionFile(path string) (persistedSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return persistedSession{}, err
	}
	return parsePersistedSessionData(data)
}

func parsePersistedSessionData(data []byte) (persistedSession, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return persistedSession{}, fmt.Errorf("empty session file")
	}

	// Fast path: legacy single JSON object.
	var raw persistedSession
	if err := json.Unmarshal([]byte(trimmed), &raw); err == nil {
		return raw, nil
	}

	// JSONL path: line-by-line, keep latest valid snapshot.
	lines := strings.Split(trimmed, "\n")
	found := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item persistedSession
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return persistedSession{}, fmt.Errorf("unmarshal session jsonl line: %w", err)
		}
		raw = item
		found = true
	}
	if !found {
		return persistedSession{}, fmt.Errorf("empty session file")
	}
	return raw, nil
}

func appendPersistedSessionJSONL(path string, snap persistedSession) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open session log: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err == nil && info.Size() > 0 {
		buf := make([]byte, 1)
		if _, err := f.ReadAt(buf, info.Size()-1); err == nil && buf[0] != '\n' {
			if _, err := f.Write([]byte("\n")); err != nil {
				return fmt.Errorf("write session separator: %w", err)
			}
		}
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append session log: %w", err)
	}
	return nil
}

func rewritePersistedSessionJSONL(path string, snap persistedSession) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("rewrite session tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}
