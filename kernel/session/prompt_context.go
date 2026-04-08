package session

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mdl "github.com/mossagents/moss/kernel/model"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const promptContextStateKey = "prompt_context"

// PromptContextFragment describes a typed, prompt-visible context fragment.
type PromptContextFragment struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Role   mdl.Role `json:"role"`
	Title  string   `json:"title,omitempty"`
	Text   string   `json:"text"`
	Hash   string   `json:"hash,omitempty"`
	Tokens int      `json:"tokens,omitempty"`
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

func PromptMessages(sess *Session) []mdl.Message {
	if sess == nil {
		return nil
	}
	st := ReadPromptContextState(sess)
	msgs := sess.CopyMessages()
	if st.Version == 0 {
		return lightweightChatPromptMessages(msgs)
	}
	return BuildPromptMessages(msgs, st)
}

func BuildPromptMessages(messages []mdl.Message, st PromptContextState) []mdl.Message {
	st = normalizePromptContextState(st)
	if st.Version == 0 {
		return lightweightChatPromptMessages(messages)
	}
	fragments := append(append(append([]PromptContextFragment(nil), st.BaselineFragments...), st.StartupFragments...), st.DynamicFragments...)
	out := make([]mdl.Message, 0, len(messages)+len(fragments))
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
	if LatestUserTurnIsLightweightChat(dialog) {
		return append(out, latestUserTurnMessages(dialog)...)
	}
	if st.PromptBudget <= 0 {
		return append(out, dialog...)
	}
	pinnedStart := latestUserMessageIndex(dialog)
	pinned := []mdl.Message(nil)
	if pinnedStart >= 0 {
		pinned = append([]mdl.Message(nil), dialog[pinnedStart:]...)
		dialog = dialog[:pinnedStart]
	}
	selected := make([]mdl.Message, 0, len(dialog)+len(pinned))
	if remaining > 0 {
		earlier := selectDialogTailWithinBudget(dialog, maxInt(0, remaining-EstimateMessagesTokens(pinned)))
		selected = append(selected, earlier...)
	}
	selected = append(selected, pinned...)
	if len(selected) == 0 && len(pinned) == 0 && remaining > 0 {
		selected = selectDialogTailWithinBudget(dialog, remaining)
	}
	out = append(out, selected...)
	return out
}

func EstimateMessagesTokens(messages []mdl.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

func EstimateMessageTokens(msg mdl.Message) int {
	total := 4
	if len(msg.ToolCalls) > 0 {
		total += 12 * len(msg.ToolCalls)
	}
	if len(msg.ToolResults) > 0 {
		total += 8 * len(msg.ToolResults)
	}
	for _, part := range msg.ContentParts {
		switch part.Type {
		case mdl.ContentPartText, mdl.ContentPartReasoning:
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

func NewPromptContextFragment(id, kind string, role mdl.Role, title, text string) PromptContextFragment {
	text = strings.TrimSpace(text)
	if text == "" {
		return PromptContextFragment{}
	}
	if strings.TrimSpace(id) == "" {
		id = kind + ":" + fragmentHash(kind+"\n"+title+"\n"+text)
	}
	if role == "" {
		role = mdl.RoleSystem
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

func fragmentMessage(fragment PromptContextFragment) *mdl.Message {
	if strings.TrimSpace(fragment.Text) == "" {
		return nil
	}
	role := fragment.Role
	if role == "" {
		role = mdl.RoleSystem
	}
	return &mdl.Message{
		Role:         role,
		ContentParts: []mdl.ContentPart{mdl.TextPart(fragment.Text)},
	}
}

func dialogMessagesAfter(messages []mdl.Message, skipDialog int) []mdl.Message {
	if skipDialog <= 0 {
		return append([]mdl.Message(nil), filterDialogMessages(messages)...)
	}
	dialogSeen := 0
	out := make([]mdl.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == mdl.RoleSystem {
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

func filterDialogMessages(messages []mdl.Message) []mdl.Message {
	out := make([]mdl.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == mdl.RoleSystem {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func latestUserMessageIndex(messages []mdl.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == mdl.RoleUser {
			return i
		}
	}
	return -1
}

func latestUserTurnMessages(messages []mdl.Message) []mdl.Message {
	idx := latestUserMessageIndex(messages)
	if idx < 0 {
		return append([]mdl.Message(nil), messages...)
	}
	return append([]mdl.Message(nil), messages[idx:]...)
}

func LatestUserTurnIsLightweightChat(messages []mdl.Message) bool {
	idx := latestUserMessageIndex(messages)
	if idx < 0 {
		return false
	}
	text := normalizeLightweightChatText(mdl.ContentPartsToPlainText(messages[idx].ContentParts))
	if text == "" {
		return false
	}
	_, ok := lightweightChatInputs[text]
	return ok
}

var lightweightChatInputs = map[string]struct{}{
	"hi":       {},
	"hello":    {},
	"hey":      {},
	"你好":       {},
	"您好":       {},
	"嗨":        {},
	"哈喽":       {},
	"早上好":      {},
	"下午好":      {},
	"晚上好":      {},
	"早安":       {},
	"晚安":       {},
	"thanks":   {},
	"thankyou": {},
	"谢谢":       {},
	"多谢":       {},
}

func normalizeLightweightChatText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case unicode.IsSpace(r), unicode.IsPunct(r), unicode.IsSymbol(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func lightweightChatPromptMessages(messages []mdl.Message) []mdl.Message {
	dialog := filterDialogMessages(messages)
	if LatestUserTurnIsLightweightChat(dialog) {
		out := make([]mdl.Message, 0, len(messages))
		for _, msg := range messages {
			if msg.Role == mdl.RoleSystem {
				out = append(out, msg)
			}
		}
		out = append(out, latestUserTurnMessages(dialog)...)
		return out
	}
	return append([]mdl.Message(nil), messages...)
}

func selectDialogTailWithinBudget(messages []mdl.Message, budget int) []mdl.Message {
	if len(messages) == 0 {
		return nil
	}
	if budget <= 0 {
		return nil
	}
	selected := make([]mdl.Message, 0, len(messages))
	used := 0
	for i := len(messages) - 1; i >= 0; i-- {
		cost := EstimateMessageTokens(messages[i])
		if len(selected) > 0 && used+cost > budget {
			break
		}
		used += cost
		selected = append(selected, messages[i])
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
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
