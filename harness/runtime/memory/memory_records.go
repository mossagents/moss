package memstore

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mossagents/moss/x/stringutil"
)

func NormalizeMemoryScope(scope MemoryScope, sessionID, userID string) MemoryScope {
	scope = MemoryScope(strings.TrimSpace(string(scope)))
	if scope != "" {
		return scope
	}
	if strings.TrimSpace(sessionID) != "" {
		return MemoryScopeSession
	}
	if strings.TrimSpace(userID) != "" {
		return MemoryScopeUser
	}
	return MemoryScopeRepo
}

func NormalizeMemoryIdentity(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
}

func NormalizeRepoID(value string) string {
	return NormalizePath(value)
}

func NormalizeMemoryKind(kind string) string {
	return strings.TrimSpace(kind)
}

func NormalizeMemoryFingerprint(fingerprint string) string {
	return strings.TrimSpace(fingerprint)
}

func EffectiveMemoryScope(record ExtendedMemoryRecord) MemoryScope {
	return NormalizeMemoryScope(record.Scope, record.SessionID, record.UserID)
}

func EffectiveMemoryConfidence(record ExtendedMemoryRecord) float64 {
	if record.Confidence > 0 {
		return record.Confidence
	}
	switch record.Stage {
	case MemoryStagePromoted:
		return 1.0
	case MemoryStageConsolidated:
		return 0.75
	case MemoryStageSnapshot:
		return 0.35
	default:
		return 0.9
	}
}

func EffectiveMemoryExpiry(record ExtendedMemoryRecord) time.Time {
	return record.ExpiresAt.UTC()
}

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
	record.Scope = MemoryScope(strings.TrimSpace(string(record.Scope)))
	record.SessionID = NormalizeMemoryIdentity(record.SessionID)
	record.RepoID = NormalizeRepoID(record.RepoID)
	record.UserID = NormalizeMemoryIdentity(record.UserID)
	record.Kind = NormalizeMemoryKind(record.Kind)
	record.Fingerprint = NormalizeMemoryFingerprint(record.Fingerprint)
	record.Group = NormalizePath(record.Group)
	record.Workspace = strings.TrimSpace(record.Workspace)
	record.CWD = strings.TrimSpace(record.CWD)
	record.GitBranch = strings.TrimSpace(record.GitBranch)
	record.SourceKind = strings.TrimSpace(record.SourceKind)
	record.SourceID = strings.TrimSpace(record.SourceID)
	record.SourcePath = strings.TrimSpace(strings.ReplaceAll(record.SourcePath, "\\", "/"))
	record.Tags = normalizeMemoryTags(record.Tags)
	record.Citation = normalizeMemoryCitation(record.Citation)
	if record.RepoID == "" {
		record.RepoID = NormalizeRepoID(record.Workspace)
	}
	if record.Kind == "" {
		record.Kind = metadataString(record.Metadata, metadataKeyMemoryKind)
		if record.Kind == "" {
			record.Kind = record.SourceKind
		}
	}
	if record.Fingerprint == "" {
		record.Fingerprint = metadataString(record.Metadata, metadataKeyMemoryFingerprint)
	}
	if record.Confidence <= 0 {
		record.Confidence = metadataFloat64(record.Metadata, metadataKeyMemoryConfidence)
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = metadataTime(record.Metadata, metadataKeyMemoryExpiresAt)
	}
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
		if record.Scope == "" {
			record.Scope = existing.Scope
		}
		if record.SessionID == "" {
			record.SessionID = existing.SessionID
		}
		if record.RepoID == "" {
			record.RepoID = existing.RepoID
		}
		if record.UserID == "" {
			record.UserID = existing.UserID
		}
		if record.Kind == "" {
			record.Kind = existing.Kind
		}
		if record.Fingerprint == "" {
			record.Fingerprint = existing.Fingerprint
		}
		if record.Confidence <= 0 {
			record.Confidence = existing.Confidence
		}
		if record.ExpiresAt.IsZero() {
			record.ExpiresAt = existing.ExpiresAt
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
	record.Scope = NormalizeMemoryScope(record.Scope, record.SessionID, record.UserID)
	if record.RepoID == "" {
		record.RepoID = NormalizeRepoID(record.Workspace)
	}
	if record.Fingerprint == "" {
		record.Fingerprint = record.Path
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
	if len(query.Scopes) > 0 {
		matched := false
		itemScope := EffectiveMemoryScope(item)
		for _, scope := range query.Scopes {
			if scope != "" && itemScope == scope {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if sessionID := NormalizeMemoryIdentity(query.SessionID); sessionID != "" && !strings.EqualFold(item.SessionID, sessionID) {
		return false
	}
	if repoID := NormalizeRepoID(query.RepoID); repoID != "" && !strings.EqualFold(item.RepoID, repoID) {
		return false
	}
	if userID := NormalizeMemoryIdentity(query.UserID); userID != "" && !strings.EqualFold(item.UserID, userID) {
		return false
	}
	if len(query.Kinds) > 0 {
		matched := false
		for _, kind := range query.Kinds {
			if kind != "" && strings.EqualFold(item.Kind, NormalizeMemoryKind(kind)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if fingerprint := NormalizeMemoryFingerprint(query.Fingerprint); fingerprint != "" && item.Fingerprint != fingerprint {
		return false
	}
	if query.MinConfidence > 0 && EffectiveMemoryConfidence(item) < query.MinConfidence {
		return false
	}
	if !query.NotExpiredAt.IsZero() {
		expiresAt := EffectiveMemoryExpiry(item)
		if !expiresAt.IsZero() && expiresAt.Before(query.NotExpiredAt.UTC()) {
			return false
		}
	}
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
	terms := memoryQueryTerms(needle)
	if len(terms) == 0 {
		return 0
	}
	score := 0
	checks := []struct {
		text   string
		weight int
	}{
		{item.Path, 8},
		{item.Kind, 7},
		{item.Fingerprint, 6},
		{item.RepoID, 5},
		{item.SessionID, 4},
		{item.UserID, 4},
		{item.Group, 5},
		{item.Summary, 6},
		{item.SourcePath, 4},
		{item.CWD, 3},
		{item.GitBranch, 2},
		{item.Content, 1},
	}
	for _, check := range checks {
		text := strings.ToLower(check.text)
		if text == "" {
			continue
		}
		for _, term := range terms {
			if strings.Contains(text, term) {
				score += check.weight
			}
		}
	}
	for _, tag := range item.Tags {
		text := strings.ToLower(tag)
		for _, term := range terms {
			if strings.Contains(text, term) {
				score += 3
			}
		}
	}
	return score
}

func memoryQueryTerms(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	seen := make(map[string]struct{}, 16)
	terms := make([]string, 0, 16)
	add := func(term string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			return
		}
		if _, ok := seen[term]; ok {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	add(query)
	for _, token := range strings.FieldsFunc(strings.ToLower(query), isMemoryQueryDelimiter) {
		if shouldIncludeMemoryQueryTerm(token) {
			add(token)
		}
	}
	for _, token := range extractMemoryQueryCJKTerms(query) {
		add(token)
	}
	return terms
}

func shouldIncludeMemoryQueryTerm(term string) bool {
	term = strings.TrimSpace(term)
	if term == "" {
		return false
	}
	runes := []rune(term)
	if len(runes) < 2 {
		return false
	}
	return true
}

func extractMemoryQueryCJKTerms(text string) []string {
	segments := make([][]rune, 0, 4)
	current := make([]rune, 0, len(text))
	flush := func() {
		if len(current) == 0 {
			return
		}
		segments = append(segments, append([]rune(nil), current...))
		current = current[:0]
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	out := make([]string, 0, 12)
	for _, segment := range segments {
		if len(segment) >= 2 && len(segment) <= 8 {
			out = append(out, string(segment))
		}
		for i := 0; i+1 < len(segment); i++ {
			out = append(out, string(segment[i:i+2]))
		}
	}
	return out
}

func isMemoryQueryDelimiter(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func sortMemoryRecords(records []ExtendedMemoryRecord, query ExtendedMemoryQuery) {
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	sortBy := query.SortBy
	if sortBy == "" {
		sortBy = MemorySortByScore
	}
	sort.Slice(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		switch sortBy {
		case MemorySortByUpdatedAt:
			if !left.UpdatedAt.Equal(right.UpdatedAt) {
				return left.UpdatedAt.After(right.UpdatedAt)
			}
			leftLastUsed := left.LastUsedAt.UTC()
			rightLastUsed := right.LastUsedAt.UTC()
			if !leftLastUsed.Equal(rightLastUsed) {
				return leftLastUsed.After(rightLastUsed)
			}
		case MemorySortByLastUsedAt:
			leftLastUsed := MemoryFreshness(left)
			rightLastUsed := MemoryFreshness(right)
			if !leftLastUsed.Equal(rightLastUsed) {
				return leftLastUsed.After(rightLastUsed)
			}
			if !left.UpdatedAt.Equal(right.UpdatedAt) {
				return left.UpdatedAt.After(right.UpdatedAt)
			}
		default:
			leftScore := memoryTextScore(left, needle)
			rightScore := memoryTextScore(right, needle)
			if leftScore != rightScore {
				return leftScore > rightScore
			}
			leftConfidence := EffectiveMemoryConfidence(left)
			rightConfidence := EffectiveMemoryConfidence(right)
			if leftConfidence != rightConfidence {
				return leftConfidence > rightConfidence
			}
			if left.UsageCount != right.UsageCount {
				return left.UsageCount > right.UsageCount
			}
			leftFresh := MemoryFreshness(left)
			rightFresh := MemoryFreshness(right)
			if !leftFresh.Equal(rightFresh) {
				return leftFresh.After(rightFresh)
			}
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

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok {
		return ""
	}
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func metadataFloat64(metadata map[string]any, key string) float64 {
	if len(metadata) == 0 {
		return 0
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return 0
	}
	switch value := raw.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case int32:
		return float64(value)
	case jsonNumberLike:
		parsed, _ := strconv.ParseFloat(value.String(), 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
		return parsed
	}
}

type jsonNumberLike interface {
	String() string
}

func metadataTime(metadata map[string]any, key string) time.Time {
	if len(metadata) == 0 {
		return time.Time{}
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return time.Time{}
	}
	switch value := raw.(type) {
	case string:
		return ParseMemoryTime(value)
	case time.Time:
		return value.UTC()
	default:
		return ParseMemoryTime(fmt.Sprint(value))
	}
}
