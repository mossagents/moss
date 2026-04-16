package memstore

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/harness/stringutil"
)

func NormalizePath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	path = strings.ReplaceAll(path, "\\", "/")
	if path == "." {
		return ""
	}
	return path
}

func summarizeMemoryContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	summaryParts := make([]string, 0, 3)
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		line = strings.Trim(line, "#-*` ")
		if line == "" {
			continue
		}
		summaryParts = append(summaryParts, line)
		if len(summaryParts) == 3 {
			break
		}
	}
	summary := strings.Join(summaryParts, " | ")
	if summary == "" {
		summary = strings.Join(strings.Fields(content), " ")
	}
	const maxLen = 220
	if len(summary) <= maxLen {
		return summary
	}
	return strings.TrimSpace(summary[:maxLen]) + "..."
}

func normalizeMemoryTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func normalizeMemoryCitation(citation MemoryCitation) MemoryCitation {
	out := MemoryCitation{
		Entries:     make([]MemoryCitationEntry, 0, len(citation.Entries)),
		MemoryPaths: DedupeStrings(citation.MemoryPaths),
		RolloutIDs:  DedupeStrings(citation.RolloutIDs),
	}
	for _, entry := range citation.Entries {
		entry.Path = strings.TrimSpace(strings.ReplaceAll(entry.Path, "\\", "/"))
		entry.Note = strings.TrimSpace(entry.Note)
		if entry.Path == "" {
			continue
		}
		out.Entries = append(out.Entries, entry)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		if out.Entries[i].Path == out.Entries[j].Path {
			if out.Entries[i].LineStart == out.Entries[j].LineStart {
				return out.Entries[i].LineEnd < out.Entries[j].LineEnd
			}
			return out.Entries[i].LineStart < out.Entries[j].LineStart
		}
		return out.Entries[i].Path < out.Entries[j].Path
	})
	return out
}

func normalizeMemoryRecord(record ExtendedMemoryRecord, existing *ExtendedMemoryRecord, now time.Time) ExtendedMemoryRecord {
	record.Path = NormalizePath(record.Path)
	record.Group = NormalizePath(record.Group)
	record.Workspace = strings.TrimSpace(record.Workspace)
	record.CWD = strings.TrimSpace(record.CWD)
	record.GitBranch = strings.TrimSpace(record.GitBranch)
	record.SourceKind = strings.TrimSpace(record.SourceKind)
	record.SourceID = strings.TrimSpace(record.SourceID)
	record.SourcePath = strings.TrimSpace(strings.ReplaceAll(record.SourcePath, "\\", "/"))
	record.Tags = normalizeMemoryTags(record.Tags)
	record.Citation = normalizeMemoryCitation(record.Citation)
	if strings.TrimSpace(record.Summary) == "" {
		record.Summary = summarizeMemoryContent(record.Content)
	}
	if existing != nil {
		record.ID = stringutil.FirstNonEmpty(record.ID, existing.ID)
		if record.CreatedAt.IsZero() {
			record.CreatedAt = existing.CreatedAt
		}
		record.Stage = MemoryStage(stringutil.FirstNonEmpty(string(record.Stage), string(existing.Stage)))
		record.Status = MemoryStatus(stringutil.FirstNonEmpty(string(record.Status), string(existing.Status)))
		if record.Group == "" {
			record.Group = existing.Group
		}
		if record.Workspace == "" {
			record.Workspace = existing.Workspace
		}
		if record.CWD == "" {
			record.CWD = existing.CWD
		}
		if record.GitBranch == "" {
			record.GitBranch = existing.GitBranch
		}
		if record.SourceKind == "" {
			record.SourceKind = existing.SourceKind
		}
		if record.SourceID == "" {
			record.SourceID = existing.SourceID
		}
		if record.SourcePath == "" {
			record.SourcePath = existing.SourcePath
		}
		if record.SourceUpdatedAt.IsZero() {
			record.SourceUpdatedAt = existing.SourceUpdatedAt
		}
		if record.UsageCount == 0 {
			record.UsageCount = existing.UsageCount
		}
		if record.LastUsedAt.IsZero() {
			record.LastUsedAt = existing.LastUsedAt
		}
		if len(record.Citation.Entries) == 0 && len(record.Citation.MemoryPaths) == 0 && len(record.Citation.RolloutIDs) == 0 {
			record.Citation = existing.Citation
		}
		if len(record.Tags) == 0 {
			record.Tags = append([]string(nil), existing.Tags...)
		}
	}
	if record.Stage == "" {
		record.Stage = MemoryStageManual
	}
	if record.Status == "" {
		record.Status = MemoryStatusActive
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now.UTC()
	}
	record.UpdatedAt = now.UTC()
	return record
}

func DedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func memoryMatchesQuery(item ExtendedMemoryRecord, query ExtendedMemoryQuery) bool {
	if len(query.Tags) > 0 {
		expected := make(map[string]struct{}, len(query.Tags))
		for _, tag := range query.Tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag != "" {
				expected[tag] = struct{}{}
			}
		}
		if len(expected) > 0 {
			matched := false
			for _, tag := range item.Tags {
				if _, ok := expected[strings.ToLower(strings.TrimSpace(tag))]; ok {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}
	if len(query.Stages) > 0 {
		matched := false
		for _, stage := range query.Stages {
			if stage != "" && item.Stage == stage {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(query.Statuses) > 0 {
		matched := false
		for _, status := range query.Statuses {
			if status != "" && item.Status == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if workspace := strings.TrimSpace(query.Workspace); workspace != "" && !strings.EqualFold(strings.TrimSpace(item.Workspace), workspace) {
		return false
	}
	if group := NormalizePath(query.Group); group != "" && item.Group != group {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	if needle == "" {
		return true
	}
	return memoryTextScore(item, needle) > 0
}

func memoryTextScore(item ExtendedMemoryRecord, needle string) int {
	if needle == "" {
		return 0
	}
	score := 0
	checks := []struct {
		text   string
		weight int
	}{
		{item.Path, 8},
		{item.Group, 5},
		{item.Summary, 6},
		{item.SourcePath, 4},
		{item.CWD, 3},
		{item.GitBranch, 2},
		{item.Content, 1},
	}
	for _, check := range checks {
		text := strings.ToLower(check.text)
		if text == "" || !strings.Contains(text, needle) {
			continue
		}
		score += check.weight
	}
	for _, tag := range item.Tags {
		if strings.Contains(strings.ToLower(tag), needle) {
			score += 3
		}
	}
	return score
}

func sortMemoryRecords(records []ExtendedMemoryRecord, query ExtendedMemoryQuery) {
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	sort.Slice(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		leftScore := memoryTextScore(left, needle)
		rightScore := memoryTextScore(right, needle)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.UsageCount != right.UsageCount {
			return left.UsageCount > right.UsageCount
		}
		leftFresh := MemoryFreshness(left)
		rightFresh := MemoryFreshness(right)
		if !leftFresh.Equal(rightFresh) {
			return leftFresh.After(rightFresh)
		}
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.Path < right.Path
	})
}

func trimMemoryRecords(records []ExtendedMemoryRecord, limit int) []ExtendedMemoryRecord {
	if limit > 0 && len(records) > limit {
		return records[:limit]
	}
	return records
}

func MemoryFreshness(item ExtendedMemoryRecord) time.Time {
	switch {
	case !item.LastUsedAt.IsZero():
		return item.LastUsedAt.UTC()
	case !item.SourceUpdatedAt.IsZero():
		return item.SourceUpdatedAt.UTC()
	case !item.UpdatedAt.IsZero():
		return item.UpdatedAt.UTC()
	default:
		return item.CreatedAt.UTC()
	}
}

func BumpMemoryUsage(record ExtendedMemoryRecord, usedAt time.Time) ExtendedMemoryRecord {
	if usedAt.IsZero() {
		usedAt = time.Now().UTC()
	}
	record.UsageCount++
	record.LastUsedAt = usedAt.UTC()
	return record
}
