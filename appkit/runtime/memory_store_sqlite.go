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
	requiredColumns := map[string]struct{}{
		"path": {}, "id": {}, "content": {}, "summary": {}, "tags_json": {}, "citation_json": {},
		"stage": {}, "status": {}, "group_key": {}, "workspace": {}, "cwd": {}, "git_branch": {},
		"source_kind": {}, "source_id": {}, "source_path": {}, "source_updated_at": {},
		"usage_count": {}, "last_used_at": {}, "created_at": {}, "updated_at": {},
	}
	compatible, err := sqliteTableHasColumns(db, "memory_records", requiredColumns)
	if err != nil {
		return err
	}
	if !compatible {
		if _, err := db.Exec(`DROP TABLE IF EXISTS memory_records`); err != nil {
			return fmt.Errorf("drop incompatible memory schema: %w", err)
		}
	}
	const ddl = `
CREATE TABLE IF NOT EXISTS memory_records (
  path TEXT PRIMARY KEY,
  id TEXT NOT NULL,
  content TEXT NOT NULL,
  summary TEXT NOT NULL,
  tags_json TEXT NOT NULL,
  citation_json TEXT NOT NULL,
  stage TEXT NOT NULL,
  status TEXT NOT NULL,
  group_key TEXT NOT NULL,
  workspace TEXT NOT NULL,
  cwd TEXT NOT NULL,
  git_branch TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  source_id TEXT NOT NULL,
  source_path TEXT NOT NULL,
  source_updated_at TEXT NOT NULL,
  usage_count INTEGER NOT NULL,
  last_used_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_records_stage_status_updated ON memory_records(stage, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_records_group_status ON memory_records(group_key, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_records_usage ON memory_records(usage_count DESC, last_used_at DESC, updated_at DESC);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("init sqlite memory schema: %w", err)
	}
	return nil
}

func sqliteTableHasColumns(db *sql.DB, table string, required map[string]struct{}) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("inspect sqlite memory schema: %w", err)
	}
	defer rows.Close()
	found := make(map[string]struct{}, len(required))
	var (
		cid       int
		name      string
		valueType string
		notNull   int
		defaultV  any
		pk        int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &valueType, &notNull, &defaultV, &pk); err != nil {
			return false, fmt.Errorf("scan sqlite memory schema: %w", err)
		}
		found[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read sqlite memory schema: %w", err)
	}
	if len(found) == 0 {
		return false, nil
	}
	for name := range required {
		if _, ok := found[name]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func (s *sqliteMemoryStore) Upsert(ctx context.Context, record port.MemoryRecord) (*port.MemoryRecord, error) {
	if strings.TrimSpace(record.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	now := time.Now().UTC()
	key := normalizeMemoryPath(record.Path)
	existing, _ := s.GetByPath(ctx, key)
	record = normalizeMemoryRecord(record, existing, now)
	if existing == nil {
		record.ID = uuid.New().String()
	}
	tagsRaw, _ := json.Marshal(record.Tags)
	citationRaw, _ := json.Marshal(record.Citation)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memory_records(
  path,id,content,summary,tags_json,citation_json,stage,status,group_key,workspace,cwd,git_branch,
  source_kind,source_id,source_path,source_updated_at,usage_count,last_used_at,created_at,updated_at
)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET
  id=excluded.id,
  content=excluded.content,
  summary=excluded.summary,
  tags_json=excluded.tags_json,
  citation_json=excluded.citation_json,
  stage=excluded.stage,
  status=excluded.status,
  group_key=excluded.group_key,
  workspace=excluded.workspace,
  cwd=excluded.cwd,
  git_branch=excluded.git_branch,
  source_kind=excluded.source_kind,
  source_id=excluded.source_id,
  source_path=excluded.source_path,
  source_updated_at=excluded.source_updated_at,
  usage_count=excluded.usage_count,
  last_used_at=excluded.last_used_at,
  created_at=excluded.created_at,
  updated_at=excluded.updated_at
`, record.Path, record.ID, record.Content, record.Summary, string(tagsRaw), string(citationRaw), string(record.Stage), string(record.Status), record.Group, record.Workspace, record.CWD, record.GitBranch, record.SourceKind, record.SourceID, record.SourcePath, formatMemoryTime(record.SourceUpdatedAt), record.UsageCount, formatMemoryTime(record.LastUsedAt), formatMemoryTime(record.CreatedAt), formatMemoryTime(record.UpdatedAt))
	if err != nil {
		return nil, err
	}
	out := record
	return &out, nil
}

func (s *sqliteMemoryStore) GetByPath(ctx context.Context, path string) (*port.MemoryRecord, error) {
	key := normalizeMemoryPath(path)
	row := s.db.QueryRowContext(ctx, `SELECT id,path,content,summary,tags_json,citation_json,stage,status,group_key,workspace,cwd,git_branch,source_kind,source_id,source_path,source_updated_at,usage_count,last_used_at,created_at,updated_at FROM memory_records WHERE path=?`, key)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,path,content,summary,tags_json,citation_json,stage,status,group_key,workspace,cwd,git_branch,source_kind,source_id,source_path,source_updated_at,usage_count,last_used_at,created_at,updated_at FROM memory_records`)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortMemoryRecords(out, port.MemoryQuery{})
	return trimMemoryRecords(out, limit), nil
}

func (s *sqliteMemoryStore) Search(ctx context.Context, query port.MemoryQuery) ([]port.MemoryRecord, error) {
	q := `SELECT id,path,content,summary,tags_json,citation_json,stage,status,group_key,workspace,cwd,git_branch,source_kind,source_id,source_path,source_updated_at,usage_count,last_used_at,created_at,updated_at FROM memory_records WHERE 1=1`
	args := make([]any, 0, 16)
	if group := normalizeMemoryPath(query.Group); group != "" {
		q += ` AND group_key=?`
		args = append(args, group)
	}
	if workspace := strings.TrimSpace(query.Workspace); workspace != "" {
		q += ` AND lower(workspace)=?`
		args = append(args, strings.ToLower(workspace))
	}
	if len(query.Stages) > 0 {
		parts := make([]string, 0, len(query.Stages))
		for _, stage := range query.Stages {
			if stage == "" {
				continue
			}
			parts = append(parts, "?")
			args = append(args, string(stage))
		}
		if len(parts) > 0 {
			q += ` AND stage IN (` + strings.Join(parts, ",") + `)`
		}
	}
	if len(query.Statuses) > 0 {
		parts := make([]string, 0, len(query.Statuses))
		for _, status := range query.Statuses {
			if status == "" {
				continue
			}
			parts = append(parts, "?")
			args = append(args, string(status))
		}
		if len(parts) > 0 {
			q += ` AND status IN (` + strings.Join(parts, ",") + `)`
		}
	}
	if needle := strings.ToLower(strings.TrimSpace(query.Query)); needle != "" {
		like := "%" + needle + "%"
		q += ` AND (lower(path) LIKE ? OR lower(summary) LIKE ? OR lower(content) LIKE ? OR lower(source_path) LIKE ? OR lower(group_key) LIKE ? OR lower(cwd) LIKE ? OR lower(git_branch) LIKE ?)`
		args = append(args, like, like, like, like, like, like, like)
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
		if !memoryMatchesQuery(*record, query) {
			continue
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortMemoryRecords(out, query)
	return trimMemoryRecords(out, query.Limit), nil
}

func (s *sqliteMemoryStore) RecordUsage(ctx context.Context, paths []string, usedAt time.Time) error {
	usedAt = usedAt.UTC()
	if usedAt.IsZero() {
		usedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, path := range dedupeStrings(paths) {
		key := normalizeMemoryPath(path)
		if _, execErr := tx.ExecContext(ctx, `
UPDATE memory_records
SET usage_count = usage_count + 1,
    last_used_at = ?,
    updated_at = CASE WHEN updated_at > ? THEN updated_at ELSE ? END
WHERE path = ?
`, formatMemoryTime(usedAt), formatMemoryTime(usedAt), formatMemoryTime(usedAt), key); execErr != nil {
			err = execErr
			return err
		}
	}
	err = tx.Commit()
	return err
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemoryRecord(scanner memoryScanner) (*port.MemoryRecord, error) {
	var (
		id              string
		path            string
		content         string
		summary         string
		tagsRaw         string
		citationRaw     string
		stage           string
		status          string
		group           string
		workspace       string
		cwd             string
		gitBranch       string
		sourceKind      string
		sourceID        string
		sourcePath      string
		sourceUpdatedAt string
		usageCount      int
		lastUsedAt      string
		createdAt       string
		updatedAt       string
	)
	if err := scanner.Scan(&id, &path, &content, &summary, &tagsRaw, &citationRaw, &stage, &status, &group, &workspace, &cwd, &gitBranch, &sourceKind, &sourceID, &sourcePath, &sourceUpdatedAt, &usageCount, &lastUsedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	record := &port.MemoryRecord{
		ID:              id,
		Path:            normalizeMemoryPath(path),
		Content:         content,
		Summary:         summary,
		Stage:           port.MemoryStage(stage),
		Status:          port.MemoryStatus(status),
		Group:           normalizeMemoryPath(group),
		Workspace:       workspace,
		CWD:             cwd,
		GitBranch:       gitBranch,
		SourceKind:      sourceKind,
		SourceID:        sourceID,
		SourcePath:      sourcePath,
		SourceUpdatedAt: parseMemoryTime(sourceUpdatedAt),
		UsageCount:      usageCount,
		LastUsedAt:      parseMemoryTime(lastUsedAt),
		CreatedAt:       parseMemoryTime(createdAt),
		UpdatedAt:       parseMemoryTime(updatedAt),
	}
	_ = json.Unmarshal([]byte(tagsRaw), &record.Tags)
	_ = json.Unmarshal([]byte(citationRaw), &record.Citation)
	record.Tags = normalizeMemoryTags(record.Tags)
	record.Citation = normalizeMemoryCitation(record.Citation)
	if record.Stage == "" {
		record.Stage = port.MemoryStageManual
	}
	if record.Status == "" {
		record.Status = port.MemoryStatusActive
	}
	return record, nil
}

func formatMemoryTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseMemoryTime(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}
