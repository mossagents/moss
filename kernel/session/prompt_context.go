package session

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mossagents/moss/kernel/port"
)

const promptContextStateKey = "prompt_context"

// PromptContextFragment describes a typed, prompt-visible context fragment.
type PromptContextFragment struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Role   port.Role `json:"role"`
	Title  string    `json:"title,omitempty"`
	Text   string    `json:"text"`
	Hash   string    `json:"hash,omitempty"`
	Tokens int       `json:"tokens,omitempty"`
}

// PromptContextState stores the managed prompt baseline and compaction state.
type PromptContextState struct {
	Version              int                     `json:"version"`
	PromptBudget         int                     `json:"prompt_budget,omitempty"`
	StartupBudget        int                     `json:"startup_budget,omitempty"`
	CompactedDialogCount int                     `json:"compacted_dialog_count,omitempty"`
	KeepRecent           int                     `json:"keep_recent,omitempty"`
	LastSnapshotID       string                  `json:"last_snapshot_id,omitempty"`
	LastSummary          string                  `json:"last_summary,omitempty"`
	BaselineFragments    []PromptContextFragment `json:"baseline_fragments,omitempty"`
	StartupFragments     []PromptContextFragment `json:"startup_fragments,omitempty"`
	DynamicFragments     []PromptContextFragment `json:"dynamic_fragments,omitempty"`
	FragmentHashes       map[string]string       `json:"fragment_hashes,omitempty"`
	LastFragmentDiff     []string                `json:"last_fragment_diff,omitempty"`
	LastPromptTokens     int                     `json:"last_prompt_tokens,omitempty"`
	LastPromptBuiltAt    time.Time               `json:"last_prompt_built_at,omitempty"`
}

func ReadPromptContextState(sess *Session) PromptContextState {
	if sess == nil {
		return PromptContextState{}
	}
	raw, ok := sess.GetState(promptContextStateKey)
	if !ok || raw == nil {
		return PromptContextState{}
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return PromptContextState{}
	}
	var st PromptContextState
	if err := json.Unmarshal(blob, &st); err != nil {
		return PromptContextState{}
	}
	return normalizePromptContextState(st)
}

func WritePromptContextState(sess *Session, st PromptContextState) {
	if sess == nil {
		return
	}
	sess.SetState(promptContextStateKey, normalizePromptContextState(st))
}

func PromptMessages(sess *Session) []port.Message {
	if sess == nil {
		return nil
	}
	st := ReadPromptContextState(sess)
	if st.Version == 0 {
		return append([]port.Message(nil), sess.Messages...)
	}
	return BuildPromptMessages(sess.Messages, st)
}

func BuildPromptMessages(messages []port.Message, st PromptContextState) []port.Message {
	st = normalizePromptContextState(st)
	if st.Version == 0 {
		return append([]port.Message(nil), messages...)
	}
	fragments := append(append(append([]PromptContextFragment(nil), st.BaselineFragments...), st.StartupFragments...), st.DynamicFragments...)
	out := make([]port.Message, 0, len(messages)+len(fragments))
	remaining := st.PromptBudget
	for _, fragment := range fragments {
		msg := fragmentMessage(fragment)
		if msg == nil {
			continue
		}
		out = append(out, *msg)
		if remaining > 0 {
			remaining -= EstimateMessageTokens(*msg)
		}
	}
	dialog := dialogMessagesAfter(messages, st.CompactedDialogCount)
	if st.PromptBudget <= 0 {
		return append(out, dialog...)
	}
	if remaining <= 0 {
		return out
	}
	selected := make([]port.Message, 0, len(dialog))
	used := 0
	for i := len(dialog) - 1; i >= 0; i-- {
		cost := EstimateMessageTokens(dialog[i])
		if len(selected) > 0 && used+cost > remaining {
			break
		}
		used += cost
		selected = append(selected, dialog[i])
	}
	for i := len(selected) - 1; i >= 0; i-- {
		out = append(out, selected[i])
	}
	return out
}

func EstimateMessagesTokens(messages []port.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

func EstimateMessageTokens(msg port.Message) int {
	total := 4
	if len(msg.ToolCalls) > 0 {
		total += 12 * len(msg.ToolCalls)
	}
	if len(msg.ToolResults) > 0 {
		total += 8 * len(msg.ToolResults)
	}
	for _, part := range msg.ContentParts {
		switch part.Type {
		case port.ContentPartText, port.ContentPartReasoning:
			total += EstimateTextTokens(part.Text)
		default:
			total += 32
		}
	}
	return total
}

func EstimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	runes := utf8.RuneCountInString(text)
	lines := strings.Count(text, "\n") + 1
	return maxInt(1, (runes+3)/4+lines)
}

func NewPromptContextFragment(id, kind string, role port.Role, title, text string) PromptContextFragment {
	text = strings.TrimSpace(text)
	if text == "" {
		return PromptContextFragment{}
	}
	if strings.TrimSpace(id) == "" {
		id = kind + ":" + fragmentHash(kind+"\n"+title+"\n"+text)
	}
	if role == "" {
		role = port.RoleSystem
	}
	return PromptContextFragment{
		ID:     strings.TrimSpace(id),
		Kind:   strings.TrimSpace(kind),
		Role:   role,
		Title:  strings.TrimSpace(title),
		Text:   text,
		Hash:   fragmentHash(kind + "\n" + title + "\n" + text),
		Tokens: EstimateTextTokens(text),
	}
}

func FlattenPromptContextFragments(st PromptContextState) []PromptContextFragment {
	fragments := make([]PromptContextFragment, 0, len(st.BaselineFragments)+len(st.StartupFragments)+len(st.DynamicFragments))
	fragments = append(fragments, st.BaselineFragments...)
	fragments = append(fragments, st.StartupFragments...)
	fragments = append(fragments, st.DynamicFragments...)
	return fragments
}

func ComputePromptFragmentDiff(previous map[string]string, fragments []PromptContextFragment) ([]string, map[string]string) {
	current := make(map[string]string, len(fragments))
	changed := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		if strings.TrimSpace(fragment.ID) == "" || strings.TrimSpace(fragment.Hash) == "" {
			continue
		}
		current[fragment.ID] = fragment.Hash
		if previous == nil || previous[fragment.ID] != fragment.Hash {
			changed = append(changed, fragment.ID)
		}
	}
	for id := range previous {
		if _, ok := current[id]; !ok {
			changed = append(changed, id)
		}
	}
	return changed, current
}

func normalizePromptContextState(st PromptContextState) PromptContextState {
	if st.Version == 0 && (len(st.BaselineFragments) > 0 || len(st.StartupFragments) > 0 || len(st.DynamicFragments) > 0 || st.CompactedDialogCount > 0) {
		st.Version = 1
	}
	st.BaselineFragments = normalizePromptFragments(st.BaselineFragments)
	st.StartupFragments = normalizePromptFragments(st.StartupFragments)
	st.DynamicFragments = normalizePromptFragments(st.DynamicFragments)
	if st.FragmentHashes == nil && len(FlattenPromptContextFragments(st)) > 0 {
		_, st.FragmentHashes = ComputePromptFragmentDiff(nil, FlattenPromptContextFragments(st))
	}
	if st.CompactedDialogCount < 0 {
		st.CompactedDialogCount = 0
	}
	if st.KeepRecent < 0 {
		st.KeepRecent = 0
	}
	return st
}

func normalizePromptFragments(fragments []PromptContextFragment) []PromptContextFragment {
	if len(fragments) == 0 {
		return nil
	}
	out := make([]PromptContextFragment, 0, len(fragments))
	seen := make(map[string]struct{}, len(fragments))
	for _, fragment := range fragments {
		normalized := NewPromptContextFragment(fragment.ID, fragment.Kind, fragment.Role, fragment.Title, fragment.Text)
		if strings.TrimSpace(normalized.ID) == "" || strings.TrimSpace(normalized.Text) == "" {
			continue
		}
		if _, ok := seen[normalized.ID]; ok {
			continue
		}
		seen[normalized.ID] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func fragmentMessage(fragment PromptContextFragment) *port.Message {
	if strings.TrimSpace(fragment.Text) == "" {
		return nil
	}
	role := fragment.Role
	if role == "" {
		role = port.RoleSystem
	}
	return &port.Message{
		Role:         role,
		ContentParts: []port.ContentPart{port.TextPart(fragment.Text)},
	}
}

func dialogMessagesAfter(messages []port.Message, skipDialog int) []port.Message {
	if skipDialog <= 0 {
		return append([]port.Message(nil), filterDialogMessages(messages)...)
	}
	dialogSeen := 0
	out := make([]port.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == port.RoleSystem {
			continue
		}
		dialogSeen++
		if dialogSeen <= skipDialog {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func filterDialogMessages(messages []port.Message) []port.Message {
	out := make([]port.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == port.RoleSystem {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func fragmentHash(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func FormatPromptContextFragment(prefix, body string) string {
	prefix = strings.TrimSpace(prefix)
	body = strings.TrimSpace(body)
	switch {
	case prefix == "" && body == "":
		return ""
	case prefix == "":
		return body
	case body == "":
		return prefix
	default:
		return fmt.Sprintf("<%s>\n%s\n</%s>", prefix, body, prefix)
	}
}
