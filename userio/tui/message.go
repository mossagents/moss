package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// msgKind 区分消息类型。
type msgKind int

const (
	msgUser msgKind = iota
	msgAssistant
	msgSystem
	msgToolStart
	msgToolResult
	msgToolError
	msgProgress
	msgError
)

// chatMessage 表示聊天区域中的一条消息。
type chatMessage struct {
	kind    msgKind
	content string
	meta    map[string]any
}

var markdownRendererCache sync.Map // map[int]*glamour.TermRenderer

// renderMessage 渲染单条消息为带样式的字符串。
func renderMessage(m chatMessage, width int) string {
	maxContent := width - 4
	if maxContent < 20 {
		maxContent = 20
	}

	switch m.kind {
	case msgUser:
		label := userLabelStyle.Render("You")
		return fmt.Sprintf("\n%s\n%s", label, renderMarkdown(m.content, maxContent))

	case msgAssistant:
		label := assistantLabelStyle.Render("🤖 moss")
		return fmt.Sprintf("\n%s\n%s", label, renderMarkdown(m.content, maxContent))

	case msgSystem:
		return systemStyle.Render(renderSystemMessage(m.content, maxContent))

	case msgToolStart:
		return renderToolStartMessage(m, maxContent)

	case msgToolResult:
		return renderToolResultMessage(m, maxContent)

	case msgToolError:
		if m.meta == nil {
			m.meta = map[string]any{}
		}
		m.meta["is_error"] = true
		return renderToolResultMessage(m, maxContent)

	case msgProgress:
		return progressStyle.Render(fmt.Sprintf("  ⏳ %s", m.content))

	case msgError:
		return errorStyle.Render(fmt.Sprintf("Error: %s", m.content))

	default:
		return m.content
	}
}

func renderMarkdown(content string, width int) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	if shouldWrapAsPlainText(content) {
		return wrapText(content, width)
	}
	renderer, err := markdownRenderer(width)
	if err != nil {
		return wrapText(content, width)
	}
	out, err := renderer.Render(content)
	if err != nil {
		return wrapText(content, width)
	}
	return strings.TrimRight(out, "\n")
}

func renderSystemMessage(content string, width int) string {
	wrapped := wrapText(content, width)
	if wrapped == "" {
		return "\n  ● "
	}
	lines := strings.Split(wrapped, "\n")
	var b strings.Builder
	b.WriteString("\n  ● ")
	b.WriteString(lines[0])
	for _, line := range lines[1:] {
		b.WriteString("\n    ")
		b.WriteString(line)
	}
	return b.String()
}

func shouldWrapAsPlainText(content string) bool {
	if !strings.Contains(content, "\n") {
		return false
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#"),
			strings.HasPrefix(line, "- "),
			strings.HasPrefix(line, "* "),
			strings.HasPrefix(line, "> "),
			strings.HasPrefix(line, "```"),
			isOrderedMarkdownLine(line),
			strings.Contains(line, "|"):
			return false
		}
	}
	return true
}

func isOrderedMarkdownLine(line string) bool {
	dot := strings.IndexByte(line, '.')
	if dot <= 0 || dot >= len(line)-1 {
		return false
	}
	if line[dot+1] != ' ' {
		return false
	}
	for _, r := range line[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func markdownRenderer(width int) (*glamour.TermRenderer, error) {
	if v, ok := markdownRendererCache.Load(width); ok {
		return v.(*glamour.TermRenderer), nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	markdownRendererCache.Store(width, r)
	return r, nil
}

// isToolMsg 判断消息是否属于可折叠的工具类别。
func isToolMsg(kind msgKind) bool {
	return kind == msgToolStart || kind == msgToolResult
}

// renderAllMessages 渲染所有消息为单个可滚动字符串。
// 当 toolCollapsed 为 true 时，连续的工具消息会折叠为一行摘要。
func renderAllMessages(messages []chatMessage, width int, toolCollapsed bool) string {
	if len(messages) == 0 {
		return mutedStyle.Render("\n  Type a message to start...\n")
	}
	var b strings.Builder
	i := 0
	for i < len(messages) {
		m := messages[i]
		if toolCollapsed && isToolMsg(m.kind) {
			// 计算连续工具消息数量
			msgCount := 0
			callCount := 0
			for j := i; j < len(messages) && isToolMsg(messages[j].kind); j++ {
				msgCount++
				if messages[j].kind == msgToolStart {
					callCount++
				}
			}
			if callCount == 0 {
				callCount = max(1, msgCount/2)
			}
			b.WriteString(collapsedToolStyle.Render(
				fmt.Sprintf("  ▶ %d tool calls (Ctrl+O to expand)", callCount)))
			b.WriteString("\n")
			i += msgCount
		} else {
			b.WriteString(renderMessage(m, width))
			b.WriteString("\n")
			i++
		}
	}
	return b.String()
}

func renderToolStartMessage(m chatMessage, width int) string {
	toolName := toolMetaString(m, "tool", m.content)
	lines := []string{toolLabelStyle.Render(fmt.Sprintf("  🔧 %s", toolName))}
	var metaParts []string
	if risk := toolMetaString(m, "risk", ""); risk != "" {
		metaParts = append(metaParts, "risk="+risk)
	}
	if callID := toolMetaString(m, "call_id", ""); callID != "" {
		metaParts = append(metaParts, "id="+callID)
	}
	if len(metaParts) > 0 {
		lines = append(lines, mutedStyle.Render("     "+strings.Join(metaParts, " · ")))
	}
	if args := toolMetaString(m, "args_preview", ""); args != "" {
		lines = append(lines, mutedStyle.Render("     args"))
		lines = append(lines, mutedStyle.Render(indentBlock(renderToolSnippet(args, max(20, width-5)), "       ")))
	}
	return strings.Join(lines, "\n")
}

func renderToolResultMessage(m chatMessage, width int) string {
	isErr, _ := m.meta["is_error"].(bool)
	style := toolResultStyle
	icon := "✅"
	if isErr {
		style = toolErrorStyle
		icon = "❌"
	}
	toolName := toolMetaString(m, "tool", "tool")
	header := fmt.Sprintf("  %s %s", icon, toolName)
	if dur := toolMetaDuration(m.meta["duration_ms"]); dur > 0 {
		header += fmt.Sprintf(" · %dms", dur)
	}
	lines := []string{style.Render(header)}
	body := renderToolBody(m.content, max(20, width-5))
	if body.summary != "" {
		lines = append(lines, mutedStyle.Render("     "+body.summary))
	}
	if body.content != "" {
		lines = append(lines, indentBlock(body.content, "     "))
	}
	return strings.Join(lines, "\n")
}

type renderedToolBody struct {
	summary string
	content string
}

func renderToolBody(content string, width int) renderedToolBody {
	text := strings.TrimSpace(content)
	if text == "" {
		return renderedToolBody{}
	}

	if value, ok := parseJSONObject(text); ok {
		return renderedToolBody{
			summary: "JSON object",
			content: truncateToolBlock(formatIndentedJSON(value), 14, 900),
		}
	}

	if values, ok := parseJSONArray(text); ok {
		return renderToolJSONArray(values, width)
	}

	text = truncateToolBlock(text, 14, 900)
	if looksMarkdown(text) {
		return renderedToolBody{content: renderMarkdown(text, width)}
	}
	return renderedToolBody{content: wrapText(text, width)}
}

func renderToolSnippet(content string, width int) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if value, ok := parseJSONObject(trimmed); ok {
		return truncateToolBlock(formatIndentedJSON(value), 8, 400)
	}
	if values, ok := parseJSONArray(trimmed); ok {
		body := renderToolJSONArray(values, width)
		if body.summary == "" {
			return body.content
		}
		if body.content == "" {
			return body.summary
		}
		return body.summary + "\n" + body.content
	}
	return wrapText(truncateToolBlock(trimmed, 8, 400), width)
}

func looksMarkdown(content string) bool {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#"),
			strings.HasPrefix(line, "- "),
			strings.HasPrefix(line, "* "),
			strings.HasPrefix(line, "> "),
			strings.HasPrefix(line, "```"),
			isOrderedMarkdownLine(line),
			strings.Contains(line, "|"):
			return true
		}
	}
	return false
}

func parseJSONObject(content string) (map[string]any, bool) {
	var value map[string]any
	if err := json.Unmarshal([]byte(content), &value); err != nil || value == nil {
		return nil, false
	}
	return value, true
}

func parseJSONArray(content string) ([]any, bool) {
	var value []any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return nil, false
	}
	return value, true
}

func renderToolJSONArray(values []any, width int) renderedToolBody {
	summary := fmt.Sprintf("JSON array · %d items", len(values))
	if len(values) == 0 {
		return renderedToolBody{summary: summary, content: "[]"}
	}
	limit := min(3, len(values))
	lines := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		lines = append(lines, wrapText(fmt.Sprintf("%d. %s", i+1, compactJSON(values[i])), width))
	}
	if len(values) > limit {
		lines = append(lines, fmt.Sprintf("... %d more items", len(values)-limit))
	}
	return renderedToolBody{
		summary: summary,
		content: strings.Join(lines, "\n"),
	}
}

func formatIndentedJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return string(raw)
	}
	return out.String()
}

func truncateToolBlock(content string, maxLines, maxChars int) string {
	if maxChars > 0 && len(content) > maxChars {
		content = content[:maxChars] + "..."
	}
	lines := strings.Split(content, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... %d more lines", len(lines)-maxLines))
	}
	return strings.Join(lines, "\n")
}

func indentBlock(content, indent string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

func toolMetaString(m chatMessage, key, fallback string) string {
	return metaString(m.meta, key, fallback)
}

func metaString(meta map[string]any, key, fallback string) string {
	if meta == nil {
		return fallback
	}
	value, _ := meta[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func toolMetaDuration(v any) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

// wrapText 简单的文本换行。
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var result strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}
		for len(line) > width {
			// 找到最后一个空格以在单词边界处换行
			idx := strings.LastIndex(line[:width], " ")
			if idx <= 0 {
				idx = width
			}
			result.WriteString(line[:idx])
			result.WriteString("\n")
			line = strings.TrimLeft(line[idx:], " ")
		}
		if len(line) > 0 {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}
	return strings.TrimRight(result.String(), "\n")
}
