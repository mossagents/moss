package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appconfig "github.com/mossagents/moss/harness/config"
	hswarm "github.com/mossagents/moss/harness/swarm"
	"github.com/mossagents/moss/kernel"
	kio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
)

const maxSwarmWorkers = 5

// depthMaxSteps maps a depth label to a per-worker MaxSteps budget.
func depthMaxSteps(depth string) int {
	switch depth {
	case "fast":
		return 10
	case "deep":
		return 60
	default: // "standard" or unset
		return 30
	}
}

// discardIO is a silent io.UserIO that drops all output and auto-approves asks.
// Used for swarm worker sub-agents to avoid polluting the chat UI.
type discardIO struct{}

func (discardIO) Send(_ context.Context, _ kio.OutputMessage) error { return nil }
func (discardIO) Ask(_ context.Context, _ kio.InputRequest) (kio.InputResponse, error) {
	return kio.InputResponse{Approved: true}, nil
}

type swarmQuestion struct {
	Slug     string `json:"slug"`
	Question string `json:"question"`
}

// workerFinding is the structured output from a single research worker.
type workerFinding struct {
	Slug       string   `json:"slug"`
	Question   string   `json:"question"`
	Finding    string   `json:"finding"`
	Confidence string   `json:"confidence"` // "high", "medium", "low"
	Gaps       []string `json:"gaps"`
	Status     string   `json:"status"` // "done", "partial", "failed"
	Error      string   `json:"error,omitempty"`
}

// sendMessageToSwarm routes a user message to the swarm pipeline.
// Returns immediately; the pipeline runs in a background goroutine.
// userParts is the full multimodal content for the user message; nil falls
// back to a single text part from content. The swarm planner always uses the
// plain-text content string as its goal regardless of userParts.
func (s *ChatService) sendMessageToSwarm(sw *hswarm.Runtime, rootSess *session.Session, content string, userParts []model.ContentPart, breadth int, depth string, outputLength string) error {
	if rootSess == nil || s.k == nil {
		return fmt.Errorf("service not initialized")
	}

	s.mu.Lock()
	if _, running := s.activeRuns[rootSess.ID]; running {
		s.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	rootSessID := rootSess.ID
	s.mu.Unlock()

	// Capture history (previous turns) BEFORE appending the current message.
	historyMsgs := rootSess.CopyMessages()

	// Append user message to root session immediately.
	if userParts == nil {
		userParts = []model.ContentPart{model.TextPart(content)}
	}
	userMsg := model.Message{Role: model.RoleUser, ContentParts: userParts}
	rootSess.AppendMessage(userMsg)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				emitEvent("chat:error", map[string]any{
					"message":    fmt.Sprintf("内部错误: %v", r),
					"session_id": rootSessID,
				})
			}
			if err := s.persistSession(rootSess); err != nil {
				slog.Debug("persist root session after swarm failed", slog.Any("error", err))
			}
			s.mu.Lock()
			delete(s.activeRuns, rootSessID)
			s.mu.Unlock()
			s.emitDashboard()
		}()

		ctx, cancel := context.WithCancel(context.Background())
		ctx = context.WithValue(ctx, sessionIDKey{}, rootSessID)
		s.mu.Lock()
		s.activeRuns[rootSessID] = cancel
		s.mu.Unlock()
		defer cancel()

		output, err := s.runSwarmPipeline(ctx, sw, rootSessID, content, historyMsgs, breadth, depth, outputLength)
		if err != nil {
			if ctx.Err() != nil {
				emitEvent("chat:cancelled", map[string]any{"message": "已取消", "session_id": rootSessID})
				return
			}
			emitEvent("chat:error", map[string]any{"message": err.Error(), "session_id": rootSessID})
			return
		}

		saveSwarmOutput(content, output)

		// Append synthesized answer to root session for persistence.
		assistantMsg := model.Message{
			Role:         model.RoleAssistant,
			ContentParts: []model.ContentPart{model.TextPart(output)},
		}
		rootSess.AppendMessage(assistantMsg)

		go s.maybeGenerateTitle(rootSess)

		emitEvent("chat:done", map[string]any{
			"session_id":  rootSessID,
			"steps":       1,
			"tokens_used": 0,
			"output":      output,
		})
	}()

	return nil
}

// emitThinking sends a research-step update to the frontend's thinking block.
// When appendMode is true the content is appended; when false it replaces.
func emitThinking(sessionID, content string, appendMode bool) {
	emitEvent("chat:thinking", map[string]any{
		"content":    content,
		"append":     appendMode,
		"session_id": sessionID,
	})
}

// workerStatusIcon returns a display icon for a worker completion status.
func workerStatusIcon(status string) string {
	switch status {
	case "done":
		return "✅"
	case "partial":
		return "🔶"
	case "failed":
		return "❌"
	default:
		return "⏳"
	}
}

// saveSwarmOutput persists the synthesized swarm-mode answer to ~/.mosswork/.
// Filename format: swarm-YYYYMMDD-HHMMSS.md
// Errors are logged but never propagated — saving is best-effort.
func saveSwarmOutput(goal, output string) {
	dir := appconfig.AppDir()
	if dir == "" {
		slog.Warn("saveSwarmOutput: cannot determine app dir")
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Warn("saveSwarmOutput: create dir failed", slog.Any("error", err))
		return
	}
	ts := time.Now().Format("20060102-150405")
	name := filepath.Join(dir, "swarm-"+ts+".md")
	content := "# " + goal + "\n\n" + output + "\n"
	if err := os.WriteFile(name, []byte(content), 0600); err != nil {
		slog.Warn("saveSwarmOutput: write failed", slog.String("path", name), slog.Any("error", err))
		return
	}
	slog.Debug("swarm output saved", slog.String("path", name))
}

// confidenceBadge returns a compact Chinese label for a confidence level.
func confidenceBadge(c string) string {
	switch c {
	case "high":
		return "〔高〕"
	case "medium":
		return "〔中〕"
	case "low":
		return "〔低〕"
	default:
		return ""
	}
}

// swarmBuildPlanText formats the research questions with per-item status and confidence.
// findings may be nil (returns pending state for all items).
func swarmBuildPlanText(questions []swarmQuestion, findings map[int]workerFinding) string {
	var sb strings.Builder
	sb.WriteString("研究方向\n\n")
	for i, q := range questions {
		f := findings[i] // zero value if nil map or missing key
		fmt.Fprintf(&sb, "  %s %s%s\n", workerStatusIcon(f.Status), confidenceBadge(f.Confidence), q.Question)
	}
	return sb.String()
}

// buildFindingsSummaryText appends a short per-worker excerpt section to the thinking panel.
func buildFindingsSummaryText(findings []workerFinding) string {
	var sb strings.Builder
	sb.WriteString("\n─────────────────────\n\n调研摘要\n\n")
	for _, f := range findings {
		if f.Status == "failed" || f.Finding == "" {
			if f.Error != "" {
				fmt.Fprintf(&sb, "%s（失败）: %s\n\n", f.Question, f.Error)
			} else {
				fmt.Fprintf(&sb, "%s（失败）\n\n", f.Question)
			}
			continue
		}
		fmt.Fprintf(&sb, "%s（置信度：%s）\n", f.Question, confidenceLabel(f.Confidence))
		excerpt := strings.TrimSpace(f.Finding)
		if nl := strings.IndexByte(excerpt, '\n'); nl >= 0 && nl < 80 {
			excerpt = excerpt[:nl]
		} else if runes := []rune(excerpt); len(runes) > 80 {
			excerpt = string(runes[:80]) + "..."
		}
		sb.WriteString(excerpt)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// runSwarmPipeline runs the Planner → Workers → Synthesizer → Reviewer pipeline for swarm mode.
func (s *ChatService) runSwarmPipeline(ctx context.Context, sw *hswarm.Runtime, rootSessID, goal string, history []model.Message, breadth int, depth string, outputLength string) (string, error) {
	k := s.k
	numWorkers := breadth
	if numWorkers <= 0 {
		numWorkers = s.cfg.workers
	}
	if numWorkers <= 0 || numWorkers > maxSwarmWorkers {
		numWorkers = 3
	}
	maxSteps := depthMaxSteps(depth)
	historyCtx := buildHistoryContext(history)

	// Phase 1: Plan sub-questions via a direct LLM call.
	emitThinking(rootSessID, "🧠 规划研究方向...\n\n", true)
	questions, err := swarmPlanQuestions(ctx, k, sw, goal, historyCtx, numWorkers)
	if err != nil {
		return "", fmt.Errorf("规划阶段失败: %w", err)
	}
	if len(questions) == 0 {
		return "", fmt.Errorf("规划器未返回任何子问题")
	}

	// Show initial research plan (all pending).
	emitThinking(rootSessID, swarmBuildPlanText(questions, nil), false)

	// Phase 2: Run workers concurrently; update plan on each completion.
	var progressMu sync.Mutex
	progressFindings := map[int]workerFinding{}
	onWorkerComplete := func(idx int, f workerFinding) {
		progressMu.Lock()
		progressFindings[idx] = f
		text := swarmBuildPlanText(questions, progressFindings)
		// Emit inside the lock to prevent out-of-order snapshots.
		emitThinking(rootSessID, text, false)
		progressMu.Unlock()
	}
	findings, err := swarmRunWorkers(ctx, k, sw, s.store, rootSessID, goal, questions, maxSteps, onWorkerComplete)
	if err != nil {
		return "", fmt.Errorf("调研阶段失败: %w", err)
	}

	// Count results and build final plan from authoritative findings (not callback state).
	done, partial, failed := 0, 0, 0
	for _, f := range findings {
		switch f.Status {
		case "done":
			done++
		case "partial":
			partial++
		default:
			failed++
		}
	}
	finalMap := make(map[int]workerFinding, len(findings))
	for i, f := range findings {
		finalMap[i] = f
	}
	planText := swarmBuildPlanText(questions, finalMap)
	summary := fmt.Sprintf("\n完成 %d，部分 %d，失败 %d\n", done, partial, failed)
	emitThinking(rootSessID, planText+summary+buildFindingsSummaryText(findings), false)

	if done+partial == 0 {
		return "", fmt.Errorf("所有调研方向均失败，无法生成报告")
	}

	// Seal the research-thinking bubble, then reset so synthesizer gets its own bubble.
	emitEvent("chat:stream_end", map[string]any{"session_id": rootSessID})
	emitEvent("chat:reset_stream", map[string]any{"session_id": rootSessID})

	// Phase 3: Synthesize findings (streams directly via wailsIO → chat:stream events).
	result, err := swarmSynthesize(ctx, k, sw, s.store, s.wailsIO, rootSessID, goal, historyCtx, outputLength, findings)
	if err != nil {
		return "", fmt.Errorf("综合阶段失败: %w", err)
	}

	// Phase 4: Quality review — appends caveats to the thinking panel if issues are found.
	swarmReview(ctx, k, sw, rootSessID, goal, result)

	return result, nil
}

// buildHistoryContext converts prior session messages into a compact text block
// that planners and synthesizers can use as conversation context.
func buildHistoryContext(msgs []model.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Role != model.RoleUser && m.Role != model.RoleAssistant {
			continue
		}
		text := model.ContentPartsToPlainText(m.ContentParts)
		if text == "" {
			continue
		}
		role := "User"
		if m.Role == model.RoleAssistant {
			role = "Assistant"
		}
		fmt.Fprintf(&sb, "%s: %s\n\n", role, text)
	}
	return strings.TrimSpace(sb.String())
}

// swarmPlannerSysPrompt returns the system prompt for the planner phase.
// Uses the configured role spec when available, otherwise falls back to a built-in prompt.
func swarmPlannerSysPrompt(sw *hswarm.Runtime) string {
	if sw != nil {
		if spec, ok := sw.Role(kswarm.RolePlanner); ok && spec.SystemPrompt != "" {
			return swarmComposeSysPrompt(spec)
		}
	}
	return fmt.Sprintf("You are a research planner. Return valid JSON only, no markdown. Current time: %s", time.Now().UTC().Format(time.RFC3339))
}

// swarmPlanQuestions calls the LLM directly to produce N sub-questions as JSON.
func swarmPlanQuestions(ctx context.Context, k *kernel.Kernel, sw *hswarm.Runtime, goal, historyCtx string, numWorkers int) ([]swarmQuestion, error) {
	systemPrompt := swarmPlannerSysPrompt(sw)

	var historySection string
	if historyCtx != "" {
		historySection = fmt.Sprintf("\nPrevious conversation:\n%s\n", historyCtx)
	}

	userPrompt := fmt.Sprintf(
		`Return a JSON array with exactly %d objects. Each object must have "slug" (short kebab-case identifier, unique) and "question" (research question string).
%s
Current goal: %s
As of: %s

Cover distinct, non-overlapping research angles that together fully address the goal. If there is prior conversation, focus on new, complementary, or corrective angles — avoid repeating already-covered content. Return valid JSON array only.`,
		numWorkers, historySection, goal, time.Now().UTC().Format(time.RFC3339),
	)

	text, err := swarmCompleteText(ctx, k.LLM(), systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}
	// Strip markdown code fences if present.
	text = strings.TrimSpace(text)
	if idx := strings.Index(text, "["); idx > 0 {
		text = text[idx:]
	}
	if idx := strings.LastIndex(text, "]"); idx >= 0 && idx < len(text)-1 {
		text = text[:idx+1]
	}

	var questions []swarmQuestion
	if err := json.Unmarshal([]byte(text), &questions); err != nil {
		return nil, fmt.Errorf("规划器返回无效 JSON (%w): %s", err, text)
	}

	// Deduplicate by slug and filter empty questions.
	seen := make(map[string]struct{}, len(questions))
	deduped := questions[:0]
	for _, q := range questions {
		q.Slug = strings.TrimSpace(q.Slug)
		q.Question = strings.TrimSpace(q.Question)
		if q.Question == "" || q.Slug == "" {
			continue
		}
		if _, ok := seen[q.Slug]; ok {
			continue
		}
		seen[q.Slug] = struct{}{}
		deduped = append(deduped, q)
	}
	return deduped, nil
}

// swarmRunWorkers runs each research question in its own sub-session, concurrently.
func swarmRunWorkers(
	ctx context.Context,
	k *kernel.Kernel,
	sw *hswarm.Runtime,
	store session.SessionStore,
	rootSessID, goal string,
	questions []swarmQuestion,
	maxSteps int,
	onComplete func(idx int, f workerFinding),
) ([]workerFinding, error) {
	results := make([]workerFinding, len(questions))
	var wg sync.WaitGroup
	wg.Add(len(questions))
	for i, q := range questions {
		i, q := i, q
		go func() {
			defer wg.Done()
			finding := swarmRunWorker(ctx, k, sw, store, rootSessID, goal, q, maxSteps)
			results[i] = finding
			if onComplete != nil {
				onComplete(i, finding)
			}
		}()
	}
	wg.Wait()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return results, nil
}

// swarmRunWorker runs a single research question in a tagged sub-session.
func swarmRunWorker(
	ctx context.Context,
	k *kernel.Kernel,
	sw *hswarm.Runtime,
	store session.SessionStore,
	rootSessID, goal string,
	question swarmQuestion,
	maxSteps int,
) workerFinding {
	if maxSteps <= 0 {
		maxSteps = 30
	}
	thread, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     question.Question,
		Mode:     "swarm-worker",
		MaxSteps: maxSteps,
	})
	if err != nil {
		return workerFinding{
			Slug:     question.Slug,
			Question: question.Question,
			Status:   "failed",
			Error:    fmt.Sprintf("创建调研子会话失败: %v", err),
		}
	}
	session.SetThreadParent(thread, rootSessID)
	session.SetThreadRole(thread, string(kswarm.RoleWorker))

	if sw != nil {
		if spec, ok := sw.Role(kswarm.RoleWorker); ok {
			sysPrompt := swarmComposeSysPrompt(spec)
			thread.Config.SystemPrompt = sysPrompt
			thread.UpdateSystemPrompt(sysPrompt)
		}
	}

	prompt := fmt.Sprintf(
		"Overall goal: %s\n\nYour assigned research question: %s\n\nResearch this question thoroughly. Use any available tools (for example, use http_request to fetch web pages or query search APIs) to gather relevant, up-to-date information before answering. After collecting sufficient evidence, output your findings as your final message using this EXACT format:\n\n<findings>\n{\"finding\": \"<comprehensive answer to the research question>\", \"confidence\": \"high|medium|low\", \"gaps\": [\"<aspect not fully covered>\"]}\n</findings>\n\nConfidence guide: \"high\" = well-established facts with strong evidence; \"medium\" = reasonable basis with some uncertainty; \"low\" = speculative or limited evidence. Do not output anything after the </findings> tag.",
		goal, question.Question,
	)
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(prompt)}}
	thread.AppendMessage(userMsg)

	result, runErr := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     thread,
		Agent:       k.BuildLLMAgent("swarm-worker"),
		UserContent: &userMsg,
		IO:          discardIO{},
	})
	if store != nil {
		if saveErr := store.Save(ctx, thread); saveErr != nil {
			slog.Debug("save worker thread failed", slog.Any("error", saveErr))
		}
	}
	if runErr != nil {
		return workerFinding{
			Slug:     question.Slug,
			Question: question.Question,
			Status:   "failed",
			Error:    runErr.Error(),
		}
	}

	return extractWorkerFinding(strings.TrimSpace(result.Output), question)
}

// extractWorkerFinding parses a structured finding from worker output.
// It tries sentinel format (<findings>…</findings>) first, then bare JSON extraction,
// and finally falls back to treating the full output as a plain text partial finding.
func extractWorkerFinding(text string, q swarmQuestion) workerFinding {
	if text == "" {
		return workerFinding{Slug: q.Slug, Question: q.Question, Status: "failed", Error: "empty output"}
	}

	// Primary: extract JSON from <findings>…</findings> sentinel block.
	if si := strings.Index(text, "<findings>"); si >= 0 {
		rest := text[si+len("<findings>"):]
		if ei := strings.Index(rest, "</findings>"); ei >= 0 {
			if f, ok := parseWorkerFindingJSON(strings.TrimSpace(rest[:ei]), q); ok {
				return f
			}
		}
	}

	// Fallback: find the first complete {…} block in the output.
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			if f, ok := parseWorkerFindingJSON(text[start:end+1], q); ok {
				return f
			}
		}
	}

	// Last resort: treat raw output as a plain text partial finding.
	return workerFinding{
		Slug:       q.Slug,
		Question:   q.Question,
		Finding:    text,
		Confidence: "medium",
		Status:     "partial",
	}
}

// parseWorkerFindingJSON attempts to unmarshal a JSON string into a workerFinding.
func parseWorkerFindingJSON(jsonStr string, q swarmQuestion) (workerFinding, bool) {
	var raw struct {
		Finding    string   `json:"finding"`
		Confidence string   `json:"confidence"`
		Gaps       []string `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil || raw.Finding == "" {
		return workerFinding{}, false
	}
	confidence := raw.Confidence
	if confidence != "high" && confidence != "low" {
		confidence = "medium"
	}
	return workerFinding{
		Slug:       q.Slug,
		Question:   q.Question,
		Finding:    raw.Finding,
		Confidence: confidence,
		Gaps:       raw.Gaps,
		Status:     "done",
	}, true
}

// confidenceLabel translates a confidence value to a display label.
func confidenceLabel(c string) string {
	switch c {
	case "high":
		return "高"
	case "low":
		return "低"
	default:
		return "中"
	}
}

// buildFindingsContext formats structured findings into a synthesizer prompt context.
// Returns the formatted context string and a list of notes for failed/missing directions.
func buildFindingsContext(findings []workerFinding) (string, []string) {
	var sb strings.Builder
	var gapNotes []string
	first := true
	for _, f := range findings {
		if f.Status == "failed" || f.Finding == "" {
			gapNotes = append(gapNotes, fmt.Sprintf("研究方向「%s」未完成（%s）", f.Question, f.Error))
			continue
		}
		if !first {
			sb.WriteString("\n\n---\n\n")
		}
		first = false
		fmt.Fprintf(&sb, "### %s\n**置信度**: %s\n\n%s", f.Question, confidenceLabel(f.Confidence), f.Finding)
		if len(f.Gaps) > 0 {
			sb.WriteString("\n\n**未解决问题**:\n")
			for _, g := range f.Gaps {
				fmt.Fprintf(&sb, "- %s\n", g)
			}
		}
	}
	return sb.String(), gapNotes
}

// outputLengthInstruction maps a length key to a natural-language writing instruction.
func outputLengthInstruction(length string) string {
	switch length {
	case "brief":
		return "Write a concise summary (around 300–500 words). Use bullet points where helpful. Avoid repetition."
	case "detailed":
		return "Write a detailed report (around 2000–3000 words). Use headings, sub-sections, and examples."
	case "comprehensive":
		return "Write a comprehensive, in-depth analysis with no length limit. Cover every aspect thoroughly with rich detail, examples, and citations."
	default: // "standard"
		return "Write a well-structured answer (around 800–1200 words). Use headings and bullet points as appropriate."
	}
}

// swarmSynthesize combines worker findings into a final answer.
// The synthesizer's output is streamed to the frontend via wailsIO.
func swarmSynthesize(
	ctx context.Context,
	k *kernel.Kernel,
	sw *hswarm.Runtime,
	store session.SessionStore,
	userIO *WailsUserIO,
	rootSessID, goal, historyCtx, outputLength string,
	findings []workerFinding,
) (string, error) {
	thread, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     "synthesize: " + goal,
		Mode:     "swarm-synthesizer",
		MaxSteps: 30,
	})
	if err != nil {
		return "", fmt.Errorf("创建综合子会话失败: %w", err)
	}
	session.SetThreadParent(thread, rootSessID)
	session.SetThreadRole(thread, string(kswarm.RoleSynthesizer))

	if sw != nil {
		if spec, ok := sw.Role(kswarm.RoleSynthesizer); ok {
			sysPrompt := swarmComposeSysPrompt(spec)
			thread.Config.SystemPrompt = sysPrompt
			thread.UpdateSystemPrompt(sysPrompt)
		}
	}

	findingsContext, gapNotes := buildFindingsContext(findings)
	var historySection string
	if historyCtx != "" {
		historySection = fmt.Sprintf("\nPrevious conversation (for continuity):\n%s\n", historyCtx)
	}
	var gapSection string
	if len(gapNotes) > 0 {
		gapSection = "\n\nResearch gaps (these directions could not be fully investigated):\n"
		for _, note := range gapNotes {
			gapSection += "- " + note + "\n"
		}
	}

	prompt := fmt.Sprintf(
		"Goal: %s\nAs of: %s\n%s\nWorker findings:\n%s%s\n\nBased on the above findings, write a comprehensive, well-structured answer in markdown. If there is prior conversation, build on it — address corrections, additions, or follow-ups as appropriate. Where confidence is low or gaps exist, acknowledge them explicitly.\n\nOutput length guidance: %s",
		goal,
		time.Now().UTC().Format(time.RFC3339),
		historySection,
		findingsContext,
		gapSection,
		outputLengthInstruction(outputLength),
	)
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(prompt)}}
	thread.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     thread,
		Agent:       k.BuildLLMAgent("swarm-synthesizer"),
		UserContent: &userMsg,
		IO:          userIO, // stream synthesizer output to frontend
	})
	if err != nil {
		return "", err
	}
	if store != nil {
		if saveErr := store.Save(ctx, thread); saveErr != nil {
			slog.Debug("save synthesizer thread failed", slog.Any("error", saveErr))
		}
	}
	return strings.TrimSpace(result.Output), nil
}

// swarmReview performs a lightweight quality check on the synthesized output.
// If the reviewer flags issues, a note is appended to the thinking panel.
// Failures are silently discarded — the review is advisory only.
func swarmReview(ctx context.Context, k *kernel.Kernel, sw *hswarm.Runtime, rootSessID, goal, draft string) {
	sysPrompt := "You are a critical research reviewer. Be concise and direct."
	if sw != nil {
		if spec, ok := sw.Role(kswarm.RoleReviewer); ok && spec.SystemPrompt != "" {
			sysPrompt = swarmComposeSysPrompt(spec)
		}
	}
	userPrompt := fmt.Sprintf(
		"Review the following research answer for significant quality issues.\n\nGoal: %s\n\nAnswer:\n%s\n\nIf the answer is generally sound, respond with exactly: APPROVED\nIf there are significant issues (unsupported claims, critical missing angles, overconfident conclusions), respond with a brief bulleted list of issues (3 max). Do not rewrite the answer.",
		goal, draft,
	)
	review, err := swarmCompleteText(ctx, k.LLM(), sysPrompt, userPrompt)
	if err != nil {
		return
	}
	review = strings.TrimSpace(review)
	if strings.EqualFold(review, "APPROVED") || strings.HasPrefix(strings.ToUpper(review), "APPROVED") {
		return
	}
	emitThinking(rootSessID, "\n\n---\n\n🔍 **审查说明**\n\n"+review, true)
}

// swarmComposeSysPrompt composes a role-specific system prompt with time context.
func swarmComposeSysPrompt(spec hswarm.RoleSpec) string {
	return strings.TrimSpace(fmt.Sprintf("%s\n\nCurrent reference time: %s",
		spec.SystemPrompt, time.Now().UTC().Format(time.RFC3339)))
}

// swarmCompleteText calls the LLM with a simple two-message prompt and collects the response.
func swarmCompleteText(ctx context.Context, llm model.LLM, systemPrompt, userPrompt string) (string, error) {
	var sb strings.Builder
	for chunk, err := range llm.GenerateContent(ctx, model.CompletionRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart(systemPrompt)}},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(userPrompt)}},
		},
	}) {
		if err != nil {
			return sb.String(), err
		}
		if chunk.Type == model.StreamChunkTextDelta {
			sb.WriteString(chunk.Content)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}
