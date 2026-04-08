package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
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
	msgReasoning
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
		inner := renderRoleMessage(m, maxContent, func(s string) string { return userLabelStyle.Render(s) }, false)
		// 用背景色将用户消息高亮，与 assistant 消息产生清晰的视觉分区。
		// 追加一个空行作为视觉间距（对齐 Codex CLI 风格）。
		return userMessageStyle.Width(width).Render(inner)

	case msgAssistant:
		return renderRoleMessage(m, maxContent, func(s string) string { return assistantLabelStyle.Render(s) }, true)

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
		return renderProgressMessage(m, maxContent)

	case msgReasoning:
		return renderReasoningMessage(m, maxContent)

	case msgError:
		return errorStyle.Render(fmt.Sprintf("Error: %s", m.content))

	default:
		return m.content
	}
}

func renderProgressMessage(m chatMessage, width int) string {
	phase := ""
	status := ""
	if m.meta != nil {
		phase, _ = m.meta["phase"].(string)
		status, _ = m.meta["status"].(string)
	}
	label := strings.TrimSpace(progressPhaseLabel(phase))
	if label == "" {
		label = strings.ToLower(strings.TrimSpace(progressStatusLabel(status)))
	}
	if label == "" {
		label = "progress"
	}
	text := strings.TrimSpace(m.content)
	line := label
	if text != "" {
		line += "  " + text
	}
	wrapped := wrapText(line, width)
	lines := strings.Split(wrapped, "\n")
	var b strings.Builder
	b.WriteString("  ◦ ")
	b.WriteString(lines[0])
	for _, line := range lines[1:] {
		b.WriteString("\n    ")
		b.WriteString(line)
	}
	return halfMutedStyle.Render(b.String())
}

func renderReasoningMessage(m chatMessage, width int) string {
	body := strings.Join(strings.Fields(strings.TrimSpace(m.content)), " ")
	if body == "" {
		return halfMutedStyle.Render("  ◦ thinking")
	}
	line := "thinking  " + body
	wrapped := wrapText(line, width)
	lines := strings.Split(wrapped, "\n")
	var b strings.Builder
	b.WriteString("  ◦ ")
	b.WriteString(lines[0])
	for _, line := range lines[1:] {
		b.WriteString("\n    ")
		b.WriteString(line)
	}
	return halfMutedStyle.Render(b.String())
}

func renderRoleMessage(m chatMessage, width int, dotRenderer func(string) string, showTimestamp bool) string {
	if isMedia, _ := m.meta["is_media"].(bool); isMedia {
		kind, _ := m.meta["media_kind"].(string)
		if p, ok := m.meta["media_path"].(string); ok && strings.TrimSpace(p) != "" {
			hint := `(use /media open to view)`
			if strings.TrimSpace(kind) == "image" {
				hint = `(use /image open or /media open to view)`
			}
			return "  " + dotRenderer("●") + " " + wrapText("Generated "+strings.TrimSpace(kind)+": "+p+" "+hint, width)
		}
	}
	body := renderMarkdown(m.content, width)
	body = strings.TrimLeft(body, "\n")
	if strings.TrimSpace(body) == "" {
		return "  " + dotRenderer("●")
	}
	lines := strings.Split(body, "\n")
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return "  " + dotRenderer("●")
	}
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(dotRenderer("●"))
	b.WriteString(" ")
	b.WriteString(lines[0])
	if showTimestamp {
		if ts := formatMessageTimestamp(m.meta); ts != "" {
			b.WriteString(" ")
			b.WriteString(mutedStyle.Render(ts))
		}
	}
	for _, line := range lines[1:] {
		b.WriteString("\n    ")
		b.WriteString(line)
	}
	return b.String()
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
	// "Run summary: | key=val | ..." → compact "✓ completed · N step · N tokens"
	content = compactRunSummary(content)
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

// compactRunSummary 将冗长的 "Run summary: | status=completed | steps=1 | ..."
// 转换为紧凑单行格式，如 "✓ completed · 1 step · 51645 tokens"。
// 非 Run summary 内容原样返回。
func compactRunSummary(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "Run summary:") {
		return s
	}
	rest := strings.TrimPrefix(s, "Run summary:")
	kvs := make(map[string]string, 8)
	for _, part := range strings.Split(rest, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.IndexByte(part, '=')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(part[:idx])
		v := strings.TrimSpace(part[idx+1:])
		kvs[k] = v
	}
	var parts []string
	if status := kvs["status"]; status == "completed" {
		parts = append(parts, "✓ "+status)
	} else if status != "" {
		parts = append(parts, status)
	}
	if steps := kvs["steps"]; steps != "" && steps != "0" && steps != "n/a" {
		word := "steps"
		if steps == "1" {
			word = "step"
		}
		parts = append(parts, steps+" "+word)
	}
	if tokens := kvs["tokens"]; tokens != "" && tokens != "0" && tokens != "n/a" {
		parts = append(parts, tokens+" tokens")
	}
	if cost := kvs["cost"]; cost != "" && cost != "n/a" {
		parts = append(parts, "cost "+cost)
	}
	if len(parts) == 0 {
		return s
	}
	return strings.Join(parts, "  ·  ")
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
// 当 toolCollapsed 为 true 时，每个 tool call 默认只显示摘要；展开后显示详细 body。
func renderAllMessages(messages []chatMessage, width int, toolCollapsed bool) string {
	if len(messages) == 0 {
		return mutedStyle.Render("\n  Type a message to start...\n")
	}
	var b strings.Builder
	i := 0
	for i < len(messages) {
		m := messages[i]
		if m.kind == msgToolStart {
			if startShouldRenderAtResult(messages, i) {
				i++
				continue
			}
			rendered, consumed := renderToolCallMessages(messages, i, width, toolCollapsed)
			b.WriteString(rendered)
			b.WriteString("\n")
			i += consumed
		} else if m.kind == msgToolResult || m.kind == msgToolError {
			startIdx := findMatchingToolStartBefore(messages, i)
			if startIdx >= 0 {
				b.WriteString(renderToolCall(&messages[startIdx], &m, width, toolCollapsed))
			} else {
				b.WriteString(renderToolCall(nil, &m, width, toolCollapsed))
			}
			b.WriteString("\n")
			i++
		} else {
			b.WriteString(renderMessage(m, width))
			b.WriteString("\n")
			i++
		}
	}
	return b.String()
}

func startShouldRenderAtResult(messages []chatMessage, startIdx int) bool {
	if startIdx < 0 || startIdx >= len(messages) {
		return false
	}
	start := messages[startIdx]
	if start.kind != msgToolStart {
		return false
	}
	if start.meta == nil {
		return false
	}
	if _, done := start.meta["completed_at"]; !done {
		return false
	}
	return findMatchingToolResultAfter(messages, startIdx) >= 0
}

func renderToolCallMessages(messages []chatMessage, start int, width int, compact bool) (string, int) {
	msg := messages[start]
	if msg.kind != msgToolStart {
		return renderMessage(msg, width), 1
	}
	if start+1 < len(messages) && isToolCompletionForStart(msg, messages[start+1]) {
		return renderToolCall(&msg, &messages[start+1], width, compact), 2
	}
	return renderToolCall(&msg, nil, width, compact), 1
}

func isToolCompletionForStart(start, next chatMessage) bool {
	if next.kind != msgToolResult && next.kind != msgToolError {
		return false
	}
	startCall := toolMetaString(start, "call_id", "")
	nextCall := toolMetaString(next, "call_id", "")
	if startCall != "" && nextCall != "" {
		return startCall == nextCall
	}
	return toolMetaString(start, "tool", start.content) == toolMetaString(next, "tool", next.content)
}

func findMatchingToolResultAfter(messages []chatMessage, startIdx int) int {
	if startIdx < 0 || startIdx >= len(messages) {
		return -1
	}
	start := messages[startIdx]
	for i := startIdx + 1; i < len(messages); i++ {
		if isToolCompletionForStart(start, messages[i]) {
			return i
		}
	}
	return -1
}

func findMatchingToolStartBefore(messages []chatMessage, resultIdx int) int {
	if resultIdx < 0 || resultIdx >= len(messages) {
		return -1
	}
	result := messages[resultIdx]
	if result.kind != msgToolResult && result.kind != msgToolError {
		return -1
	}
	for i := resultIdx - 1; i >= 0; i-- {
		if messages[i].kind != msgToolStart {
			continue
		}
		if isToolCompletionForStart(messages[i], result) {
			return i
		}
	}
	return -1
}

func renderToolStartMessage(m chatMessage, width int) string {
	return renderToolCall(&m, nil, width, false)
}

func renderToolResultMessage(m chatMessage, width int) string {
	return renderToolCall(nil, &m, width, false)
}

func renderShellToolResult(toolName, content string, width int) []string {
	if toolName != "run_command" && toolName != "powershell" {
		return nil
	}
	type shellResult struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	var result shellResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &result); err != nil {
		return nil
	}
	lines := []string{halfMutedStyle.Render(fmt.Sprintf("    exit=%d", result.ExitCode))}
	out := firstNonEmptyLine(result.Stdout)
	errLine := firstNonEmptyLine(result.Stderr)
	if out != "" {
		lines = append(lines, "", renderToolBodyBlock(baseStyle, wrapText("stdout: "+truncateToolBlock(out, 1, 160), width)))
	}
	if errLine != "" {
		lines = append(lines, "", renderToolBodyBlock(baseStyle, wrapText("stderr: "+truncateToolBlock(errLine, 1, 160), width)))
	}
	return lines
}

func renderToolCall(start *chatMessage, result *chatMessage, width int, compact bool) string {
	toolName := "tool"
	if start != nil {
		toolName = toolMetaString(*start, "tool", start.content)
	} else if result != nil {
		toolName = toolMetaString(*result, "tool", result.content)
	}
	if toolName == "run_command" || toolName == "powershell" {
		return renderShellToolCall(start, result, width, compact)
	}

	style := toolLabelStyle
	icon := toolPendingIcon()
	suffix := ""
	summary := ""
	if start != nil {
		summary = summarizeToolParams(toolName, toolMetaString(*start, "args_preview", ""), width)
		suffix = formatToolRunningElapsed(start.meta)
	}
	if result != nil {
		style = toolResultStyle
		icon = toolSuccessIcon()
		if result.kind == msgToolError {
			style = toolErrorStyle
			icon = toolErrorIcon()
		}
		suffix = formatToolDuration(result.meta["duration_ms"])
	}
	header := renderToolHeaderLine(style, icon, toolPrettyName(toolName), summary, suffix, width)
	if compact {
		return header
	}
	parts := []string{header}
	if start != nil {
		if body := renderToolSnippet(toolMetaString(*start, "args_preview", ""), max(20, width-5)); body != "" && strings.TrimSpace(body) != strings.TrimSpace(summary) {
			parts = append(parts, "", halfMutedStyle.Render("    input"), renderToolBodyBlock(mutedStyle, body))
		}
	}
	if result != nil {
		body := renderToolBody(toolName, result.content, max(20, width-5))
		if body.summary != "" {
			parts = append(parts, "", halfMutedStyle.Render("    result · "+body.summary))
		} else {
			parts = append(parts, "", halfMutedStyle.Render("    result"))
		}
		if body.content != "" {
			parts = append(parts, renderToolBodyBlock(baseStyle, body.content))
		}
	}
	return strings.Join(parts, "\n")
}

func renderShellToolCall(start *chatMessage, result *chatMessage, width int, compact bool) string {
	toolName := "run_command"
	if start != nil {
		toolName = toolMetaString(*start, "tool", start.content)
	} else if result != nil {
		toolName = toolMetaString(*result, "tool", result.content)
	}
	type shellArgs struct {
		Description string   `json:"description"`
		Command     string   `json:"command"`
		Args        []string `json:"args"`
	}
	var args shellArgs
	if start != nil {
		_ = json.Unmarshal([]byte(toolMetaString(*start, "args_preview", "")), &args)
	}
	cmd := strings.TrimSpace(args.Command)
	if cmd == "" && len(args.Args) > 0 {
		cmd = strings.Join(args.Args, " ")
	} else if len(args.Args) > 0 {
		cmd = cmd + " " + strings.Join(args.Args, " ")
	}
	title := "Bash"
	if toolName == "powershell" {
		title = "PowerShell"
	}
	style := toolLabelStyle
	icon := toolPendingIcon()
	suffix := ""
	if start != nil {
		suffix = formatToolRunningElapsed(start.meta)
	}
	if result != nil {
		style = toolResultStyle
		icon = toolSuccessIcon()
		if result.kind == msgToolError {
			style = toolErrorStyle
			icon = toolErrorIcon()
		}
		suffix = formatToolDuration(result.meta["duration_ms"])
	}
	header := renderToolHeaderLine(style, icon, title, truncateDisplayWidth(strings.TrimSpace(cmd), max(20, width-12)), suffix, width)
	if compact {
		return header
	}
	parts := []string{header}
	if desc := strings.TrimSpace(args.Description); desc != "" {
		parts = append(parts, "", halfMutedStyle.Render("    task"), renderToolBodyBlock(mutedStyle, desc))
	}
	if strings.TrimSpace(cmd) != "" {
		parts = append(parts, "", halfMutedStyle.Render("    command"), renderToolBodyBlock(mutedStyle, truncateToolBlock(strings.TrimSpace(cmd), 3, 200)))
	}
	if result != nil {
		lines := renderShellToolResult(toolName, result.content, max(20, width-5))
		if len(lines) > 0 {
			parts = append(parts, "", halfMutedStyle.Render("    result"))
			parts = append(parts, lines...)
		} else if strings.TrimSpace(result.content) != "" {
			parts = append(parts, "", halfMutedStyle.Render("    result"), renderToolBodyBlock(baseStyle, renderToolBody(toolName, result.content, max(20, width-5)).content))
		}
	}
	return strings.Join(parts, "\n")
}

func renderToolHeaderLine(style lipgloss.Style, icon, name, summary, suffix string, width int) string {
	parts := []string{icon, name}
	if strings.TrimSpace(summary) != "" {
		parts = append(parts, truncateDisplayWidth(summary, max(16, width-12)))
	}
	header := "  " + strings.Join(parts, " ")
	if strings.TrimSpace(suffix) != "" {
		header += halfMutedStyle.Render(" · " + suffix)
	}
	return style.Render(header)
}

func renderToolBodyBlock(style lipgloss.Style, content string) string {
	return style.Render(indentBlock(content, "    │ "))
}

func toolPendingIcon() string { return "●" }
func toolSuccessIcon() string { return "✓" }
func toolErrorIcon() string   { return "✕" }

func toolPrettyName(name string) string {
	switch strings.TrimSpace(name) {
	case "run_command":
		return "Bash"
	case "powershell":
		return "PowerShell"
	}
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	fields := strings.Fields(name)
	for i, field := range fields {
		fields[i] = titleCaseWord(field)
	}
	if len(fields) == 0 {
		return "Tool"
	}
	return strings.Join(fields, " ")
}

func summarizeToolParams(toolName, argsPreview string, width int) string {
	argsPreview = strings.TrimSpace(argsPreview)
	if argsPreview == "" {
		return ""
	}
	if toolName == "run_command" || toolName == "powershell" {
		type shellArgs struct {
			Description string   `json:"description"`
			Command     string   `json:"command"`
			Args        []string `json:"args"`
		}
		var args shellArgs
		if err := json.Unmarshal([]byte(argsPreview), &args); err == nil {
			cmd := strings.TrimSpace(args.Command)
			if cmd == "" && len(args.Args) > 0 {
				cmd = strings.Join(args.Args, " ")
			} else if len(args.Args) > 0 {
				cmd += " " + strings.Join(args.Args, " ")
			}
			if strings.TrimSpace(cmd) != "" {
				return truncateDisplayWidth(strings.TrimSpace(cmd), max(16, width-12))
			}
			return truncateDisplayWidth(strings.TrimSpace(args.Description), max(16, width-12))
		}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(argsPreview), &obj); err != nil || len(obj) == 0 {
		return truncateDisplayWidth(strings.ReplaceAll(argsPreview, "\n", " "), max(16, width-12))
	}
	return truncateDisplayWidth(primaryToolSummary(obj), max(16, width-12))
}

func primaryToolSummary(obj map[string]any) string {
	preferredKeys := []string{
		"description",
		"prompt",
		"question",
		"query",
		"url",
		"path",
		"command",
		"pattern",
		"name",
		"goal",
		"resource_id",
		"session_id",
		"model",
		"skill",
		"intent",
	}
	for _, key := range preferredKeys {
		if value, ok := obj[key]; ok {
			if summary := naturalToolValue(value); summary != "" {
				return summary
			}
		}
	}
	if len(obj) == 1 {
		for _, value := range obj {
			return naturalToolValue(value)
		}
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if summary := naturalToolValue(obj[key]); summary != "" {
			return summary
		}
	}
	return ""
}

func naturalToolValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		if len(v) == 0 {
			return ""
		}
		return naturalToolValue(v[0])
	default:
		return summarizeToolValue(value)
	}
}

func summarizeToolValue(value any) string {
	switch v := value.(type) {
	case string:
		return truncateDisplayWidth(strings.TrimSpace(v), 32)
	case float64, bool, int, int64:
		return fmt.Sprintf("%v", v)
	case []any, map[string]any:
		return compactJSON(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatToolDuration(v any) string {
	if dur := toolMetaDuration(v); dur > 0 {
		return fmt.Sprintf("%dms", dur)
	}
	return ""
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func formatMessageTimestamp(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	raw, ok := meta["timestamp"]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case time.Time:
		if v.IsZero() {
			return ""
		}
		return v.Local().Format("15:04:05")
	case string:
		if strings.TrimSpace(v) == "" {
			return ""
		}
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return ts.Local().Format("15:04:05")
		}
		return v
	default:
		return ""
	}
}

func formatToolRunningElapsed(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	startRaw, ok := meta["started_at"]
	if !ok {
		return ""
	}
	if _, done := meta["completed_at"]; done {
		return ""
	}
	var started time.Time
	switch v := startRaw.(type) {
	case time.Time:
		started = v
	case string:
		ts, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return ""
		}
		started = ts
	default:
		return ""
	}
	if started.IsZero() {
		return ""
	}
	return formatElapsed(started, time.Now())
}

type renderedToolBody struct {
	summary string
	content string
}

func renderToolBody(toolName, content string, width int) renderedToolBody {
	text := strings.TrimSpace(content)
	if text == "" {
		return renderedToolBody{}
	}
	if decoded, ok := parseJSONString(text); ok {
		text = decoded
	}

	if toolName == "read_file" || toolName == "view" {
		return renderedToolBody{content: wrapText(truncateToolBlock(text, 24, 1600), width)}
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

func parseJSONString(content string) (string, bool) {
	var value string
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return "", false
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
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
		if s, ok := values[i].(string); ok {
			lines = append(lines, wrapText(fmt.Sprintf("%d. %s", i+1, s), width))
			continue
		}
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
	var wrapped []string
	for _, line := range strings.Split(s, "\n") {
		wrapped = append(wrapped, wrapTextLine(line, width)...)
	}
	return strings.Join(wrapped, "\n")
}

func wrapTextLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	runes := []rune(line)
	lines := make([]string, 0, 4)
	start := 0

	for start < len(runes) {
		end := start
		currentWidth := 0
		lastSpace := -1

		for end < len(runes) {
			rw := runewidth.RuneWidth(runes[end])
			if rw < 0 {
				rw = 0
			}
			if currentWidth > 0 && currentWidth+rw > width {
				break
			}
			if currentWidth == 0 && rw > width {
				end++
				break
			}
			currentWidth += rw
			if unicode.IsSpace(runes[end]) {
				lastSpace = end
			}
			end++
		}

		if end >= len(runes) {
			lines = append(lines, string(runes[start:]))
			break
		}

		if lastSpace >= start {
			segment := strings.TrimRightFunc(string(runes[start:lastSpace]), unicode.IsSpace)
			if segment != "" {
				lines = append(lines, segment)
				start = lastSpace + 1
				for start < len(runes) && unicode.IsSpace(runes[start]) {
					start++
				}
				continue
			}
		}

		if end == start {
			end++
		}
		lines = append(lines, string(runes[start:end]))
		start = end
	}

	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
