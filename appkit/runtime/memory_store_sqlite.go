package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/port"
	_ "modernc.org/sqlite"
)

type sqliteMemoryStore struct {
	db *sql.DB
}

func (s *sqliteMemoryStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func NewSQLiteMemoryStore(dbPath string) (port.MemoryStore, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("memory sqlite path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite memory store: %w", err)
	}
	if err := initSQLiteMemorySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteMemoryStore{db: db}, nil
}

func initSQLiteMemorySchema(db *sql.DB) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS memory_records (
  path TEXT PRIMARY KEY,
  id TEXT NOT NULL,
  content TEXT NOT NULL,
  summary TEXT NOT NULL,
  tags_json TEXT NOT NULL,
  citation_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_records_updated_at ON memory_records(updated_at DESC);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("init sqlite memory schema: %w", err)
	}
	return nil
}

func (s *sqliteMemoryStore) Upsert(ctx context.Context, record port.MemoryRecord) (*port.MemoryRecord, error) {
	if strings.TrimSpace(record.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	now := time.Now().UTC()
	key := normalizeMemoryPath(record.Path)
	existing, _ := s.GetByPath(ctx, key)
	if existing != nil {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
	} else {
		record.ID = uuid.New().String()
		record.CreatedAt = now
	}
	if strings.TrimSpace(record.Summary) == "" {
		record.Summary = summarizeMemoryContent(record.Content)
	}
	record.Path = key
	record.UpdatedAt = now
	tagsRaw, _ := json.Marshal(record.Tags)
	citationRaw, _ := json.Marshal(record.Citation)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memory_records(path,id,content,summary,tags_json,citation_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET
  id=excluded.id,
  content=excluded.content,
  summary=excluded.summary,
  tags_json=excluded.tags_json,
  citation_json=excluded.citation_json,
  created_at=excluded.created_at,
  updated_at=excluded.updated_at
`, record.Path, record.ID, record.Content, record.Summary, string(tagsRaw), string(citationRaw), record.CreatedAt.Format(time.RFC3339Nano), record.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	out := record
	return &out, nil
}

func (s *sqliteMemoryStore) GetByPath(ctx context.Context, path string) (*port.MemoryRecord, error) {
	key := normalizeMemoryPath(path)
	row := s.db.QueryRowContext(ctx, `SELECT id,path,content,summary,tags_json,citation_json,created_at,updated_at FROM memory_records WHERE path=?`, key)
	record, err := scanMemoryRecord(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("memory %q not found", key)
	}
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *sqliteMemoryStore) DeleteByPath(ctx context.Context, path string) error {
	key := normalizeMemoryPath(path)
	_, err := s.db.ExecContext(ctx, `DELETE FROM memory_records WHERE path=?`, key)
	return err
}

func (s *sqliteMemoryStore) List(ctx context.Context, limit int) ([]port.MemoryRecord, error) {
	q := `SELECT id,path,content,summary,tags_json,citation_json,created_at,updated_at FROM memory_records ORDER BY updated_at DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]port.MemoryRecord, 0)
	for rows.Next() {
		record, err := scanMemoryRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

func (s *sqliteMemoryStore) Search(ctx context.Context, query port.MemoryQuery) ([]port.MemoryRecord, error) {
	needle := "%" + strings.ToLower(strings.TrimSpace(query.Query)) + "%"
	q := `SELECT id,path,content,summary,tags_json,citation_json,created_at,updated_at FROM memory_records`
	args := []any{}
	hasWhere := false
	if strings.TrimSpace(query.Query) != "" {
		q += ` WHERE lower(path) LIKE ? OR lower(summary) LIKE ? OR lower(content) LIKE ?`
		args = append(args, needle, needle, needle)
		hasWhere = true
	}
	q += ` ORDER BY updated_at DESC`
	if query.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, query.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]port.MemoryRecord, 0)
	for rows.Next() {
		record, err := scanMemoryRecord(rows)
		if err != nil {
			return nil, err
		}
		if len(query.Tags) > 0 && !memoryHasAnyTag(record.Tags, query.Tags) {
			continue
		}
		out = append(out, *record)
	}
	if !hasWhere && query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, rows.Err()
}

func memoryHasAnyTag(actual []string, expected []string) bool {
	set := make(map[string]struct{}, len(actual))
	for _, tag := range actual {
		set[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	for _, tag := range expected {
		if _, ok := set[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemoryRecord(scanner memoryScanner) (*port.MemoryRecord, error) {
	var (
		id          string
		path        string
		content     string
		summary     string
		tagsRaw     string
		citationRaw string
		createdRaw  string
		updatedRaw  string
	)
	if err := scanner.Scan(&id, &path, &content, &summary, &tagsRaw, &citationRaw, &createdRaw, &updatedRaw); err != nil {
		return nil, err
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, createdRaw)
	updatedAt, _ := time.Parse(time.RFC3339Nano, updatedRaw)
	record := &port.MemoryRecord{
		ID:        id,
		Path:      path,
		Content:   content,
		Summary:   summary,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	_ = json.Unmarshal([]byte(tagsRaw), &record.Tags)
	_ = json.Unmarshal([]byte(citationRaw), &record.Citation)
	return record, nil
}
