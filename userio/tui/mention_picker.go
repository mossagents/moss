package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
)

type mentionCandidate struct {
	Label string
	Path  string
}

type mentionPickerState struct {
	candidates   []mentionCandidate
	query        string
	replaceToken string
	list         *selectionListState
}

func newMentionPickerState(workspace, query, replaceToken string) *mentionPickerState {
	candidates := listMentionCandidates(workspace, query, 200)
	items := make([]selectionListItem, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, selectionListItem{
			Key:    candidate.Path,
			Title:  candidate.Label,
			Detail: detectAttachmentKind(candidate.Path),
		})
	}
	return &mentionPickerState{
		candidates:   candidates,
		query:        strings.TrimSpace(query),
		replaceToken: replaceToken,
		list: &selectionListState{
			Title:        "Mention files",
			Footer:       "↑↓ choose • Enter attach • Esc close",
			EmptyMessage: "No files matched the current query.",
			Message:      "Select a file to add it as a structured attachment.",
			Items:        items,
		},
	}
}

// fileIndexEntry 是某个工作区下的文件列表缓存。
type fileIndexEntry struct {
	once  sync.Once
	paths []string // 相对于 workspace 的路径
}

// fileIndexStore 以 workspace 绝对路径为键缓存文件列表。
var fileIndexStore sync.Map // map[string]*fileIndexEntry

// ensureFileIndex 懒初始化指定工作区的文件列表（最多 fileIndexMaxSize 条）。
const fileIndexMaxSize = 5000

// skippedDirs 在文件索引构建时跳过的目录名。
var skippedDirs = map[string]bool{
	".git": true, ".moss": true, ".mosscode": true,
	"node_modules": true, "vendor": true, ".terraform": true,
	"__pycache__": true, ".venv": true, "venv": true, "dist": true,
	"build": true, "target": true, ".cache": true,
}

func ensureFileIndex(workspace string) []string {
	v, _ := fileIndexStore.LoadOrStore(workspace, &fileIndexEntry{})
	entry := v.(*fileIndexEntry)
	entry.once.Do(func() {
		paths := make([]string, 0, 512)
		_ = filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skippedDirs[strings.ToLower(d.Name())] {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(workspace, path)
			if relErr != nil {
				rel = path
			}
			paths = append(paths, filepath.Clean(rel))
			if len(paths) >= fileIndexMaxSize {
				return fs.SkipAll
			}
			return nil
		})
		sort.Strings(paths)
		entry.paths = paths
	})
	return entry.paths
}

// invalidateFileIndex 清除指定工作区的文件索引缓存（如文件变化后调用）。
func invalidateFileIndex(workspace string) {
	fileIndexStore.Delete(workspace)
}

// listMentionCandidates 使用缓存的文件索引和 fuzzy 过滤返回候选文件列表。
func listMentionCandidates(workspace, query string, limit int) []mentionCandidate {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	index := ensureFileIndex(workspace)
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]mentionCandidate, 0, min(32, limit))
	for _, rel := range index {
		if query != "" && !fuzzyContainsStr(strings.ToLower(rel), query) {
			continue
		}
		absPath := filepath.Join(workspace, rel)
		out = append(out, mentionCandidate{Label: rel, Path: absPath})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func detectAttachmentKind(path string) string {
	if !isMediaPath(path) {
		return "file"
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".wav", ".mp3", ".mpeg", ".m4a", ".ogg", ".flac":
		return "audio"
	default:
		return "video"
	}
}

func (m *chatModel) appendPendingAttachment(item composerAttachment) {
	for _, current := range m.pendingAttachments {
		if current.Key == item.Key {
			return
		}
	}
	m.pendingAttachments = append(m.pendingAttachments, item)
}

func (m chatModel) openMentionPicker(query, replaceToken string) (chatModel, tea.Cmd) {
	if !m.experimentalEnabled(product.ExperimentalComposerMentions) {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Composer mentions are disabled. Use /experimental enable composer-mentions to turn them back on."})
		m.refreshViewport()
		return m, nil
	}
	m.mentionPicker = newMentionPickerState(m.workspace, query, replaceToken)
	m.openMentionOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) openMentionPickerFromComposer() (chatModel, bool) {
	if !m.experimentalEnabled(product.ExperimentalComposerMentions) {
		return m, false
	}
	value := strings.TrimSpace(m.textarea.Value())
	if value == "" {
		return m, false
	}
	token := lastMentionToken(value)
	if token == "" {
		return m, false
	}
	updated, _ := m.openMentionPicker(strings.TrimPrefix(token, "@"), token)
	return updated, true
}

func (m chatModel) handleMentionPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.mentionPicker == nil || m.mentionPicker.list == nil || len(m.mentionPicker.candidates) == 0 {
		return m.closeMentionOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.mentionPicker.list.Move(-1)
	case "down":
		m.mentionPicker.list.Move(1)
	case "enter":
		idx := m.mentionPicker.list.SelectedIndex()
		if idx >= 0 {
			candidate := m.mentionPicker.candidates[idx]
			draft, err := buildAttachmentDraft(m.workspace, candidate.Path)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: err.Error()})
				m.refreshViewport()
				return m, nil
			}
			m.appendPendingAttachment(draft)
			if token := strings.TrimSpace(m.mentionPicker.replaceToken); token != "" {
				value := m.textarea.Value()
				if pos := strings.LastIndex(value, token); pos >= 0 {
					value = strings.TrimSpace(value[:pos] + value[pos+len(token):])
					m.textarea.SetValue(value)
					m.adjustInputHeight()
				}
			}
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Attached %s to the composer.", draft.Label)})
			return m.closeMentionOverlay(), nil
		}
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderMentionPicker(width int) string {
	if m.mentionPicker == nil || m.mentionPicker.list == nil {
		return ""
	}
	if idx := m.mentionPicker.list.SelectedIndex(); idx >= 0 && idx < len(m.mentionPicker.candidates) {
		candidate := m.mentionPicker.candidates[idx]
		m.mentionPicker.list.Message = fmt.Sprintf("%s\n\nType: %s", candidate.Path, detectAttachmentKind(candidate.Path))
	}
	return renderSelectionListDialog(width, m.mentionPicker.list)
}

func lastMentionToken(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	last := strings.TrimSpace(fields[len(fields)-1])
	if strings.HasPrefix(last, "@") && len(last) > 1 {
		return last
	}
	return ""
}
