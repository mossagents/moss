package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	hswarm "github.com/mossagents/moss/harness/swarm"
	"github.com/mossagents/moss/kernel"
	kio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
)

const maxExpertWorkers = 5

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

type expertQuestion struct {
	Slug     string `json:"slug"`
	Question string `json:"question"`
}

// sendMessageToSwarm routes a user message to the expert swarm pipeline.
// Returns immediately; the pipeline runs in a background goroutine.
func (s *ChatService) sendMessageToSwarm(sw *hswarm.Runtime, rootSess *session.Session, content string, breadth int, depth string, outputLength string) error {
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
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(content)}}
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

		output, err := s.runExpertSwarm(ctx, sw, rootSessID, content, historyMsgs, breadth, depth, outputLength)
		if err != nil {
			if ctx.Err() != nil {
				emitEvent("chat:cancelled", map[string]any{"message": "已取消", "session_id": rootSessID})
				return
			}
			emitEvent("chat:error", map[string]any{"message": err.Error(), "session_id": rootSessID})
			return
		}

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
// When append is true, content is appended to the existing thinking text.
// When append is false, content replaces the thinking text.
func emitThinking(sessionID, content string, append bool) {
	emitEvent("chat:thinking", map[string]any{
		"content":    content,
		"append":     append,
		"session_id": sessionID,
	})
}

// runExpertSwarm runs the Planner → Workers → Synthesizer pipeline for expert mode.
func (s *ChatService) runExpertSwarm(ctx context.Context, sw *hswarm.Runtime, rootSessID, goal string, history []model.Message, breadth int, depth string, outputLength string) (string, error) {
	k := s.k
	numWorkers := breadth
	if numWorkers <= 0 {
		numWorkers = s.cfg.workers
	}
	if numWorkers <= 0 || numWorkers > maxExpertWorkers {
		numWorkers = 3
	}
	maxSteps := depthMaxSteps(depth)
	historyCtx := buildHistoryContext(history)

	// Phase 1: Plan sub-questions via a direct LLM call.
	emitThinking(rootSessID, "🧠 规划研究方向...\n\n", true)
	questions, err := expertPlanQuestions(ctx, k, goal, historyCtx, numWorkers)
	if err != nil {
		return "", fmt.Errorf("规划阶段失败: %w", err)
	}
	if len(questions) == 0 {
		return "", fmt.Errorf("规划器未返回任何子问题")
	}

	// Show initial research plan (all pending).
	emitThinking(rootSessID, expertBuildPlanText(questions, nil), false)

	// Phase 2: Run workers concurrently; update plan on each completion.
	var progressMu sync.Mutex
	completed := map[int]bool{}
	onWorkerDone := func(idx int) {
		progressMu.Lock()
		completed[idx] = true
		text := expertBuildPlanText(questions, completed)
		progressMu.Unlock()
		emitThinking(rootSessID, text, false)
	}
	findings, err := expertRunWorkers(ctx, k, sw, s.store, rootSessID, goal, questions, maxSteps, onWorkerDone)
	if err != nil {
		return "", fmt.Errorf("调研阶段失败: %w", err)
	}

	// Seal the research-thinking bubble, then reset so synthesizer gets its own bubble.
	emitEvent("chat:stream_end", map[string]any{"session_id": rootSessID})
	emitEvent("chat:reset_stream", map[string]any{"session_id": rootSessID})

	// Phase 3: Synthesize findings (streams directly via wailsIO → chat:stream events).
	result, err := expertSynthesize(ctx, k, sw, s.store, s.wailsIO, rootSessID, goal, historyCtx, outputLength, findings)
	if err != nil {
		return "", fmt.Errorf("综合阶段失败: %w", err)
	}
	return result, nil
}

// expertBuildPlanText formats the research questions with per-item completion indicators (plain text).
func expertBuildPlanText(questions []expertQuestion, completed map[int]bool) string {
	var sb strings.Builder
	sb.WriteString("研究方向\n\n")
	for i, q := range questions {
		if completed[i] {
			fmt.Fprintf(&sb, "  ✅ %s\n", q.Question)
		} else {
			fmt.Fprintf(&sb, "  ⏳ %s\n", q.Question)
		}
	}
	return sb.String()
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

// expertPlanQuestions calls the LLM directly to produce N sub-questions as JSON.
func expertPlanQuestions(ctx context.Context, k *kernel.Kernel, goal, historyCtx string, numWorkers int) ([]expertQuestion, error) {
	systemPrompt := "You are a research planner. Return valid JSON only, no markdown."

	var historySection string
	if historyCtx != "" {
		historySection = fmt.Sprintf("\nPrevious conversation:\n%s\n", historyCtx)
	}

	userPrompt := fmt.Sprintf(
		`Return a JSON array with exactly %d objects. Each object must have "slug" (short kebab-case identifier) and "question" (research question string).
%s
Current goal: %s
As of: %s

Cover distinct research angles that together address the current goal. If there is prior conversation, focus on what's new, complementary, or corrective — avoid repeating already-covered angles. Return valid JSON array only.`,
		numWorkers, historySection, goal, time.Now().UTC().Format(time.RFC3339),
	)

	text, err := expertCompleteText(ctx, k.LLM(), systemPrompt, userPrompt)
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

	var questions []expertQuestion
	if err := json.Unmarshal([]byte(text), &questions); err != nil {
		return nil, fmt.Errorf("规划器返回无效 JSON (%w): %s", err, text)
	}
	return questions, nil
}

// expertRunWorkers runs each research question in its own sub-session, concurrently.
func expertRunWorkers(
	ctx context.Context,
	k *kernel.Kernel,
	sw *hswarm.Runtime,
	store session.SessionStore,
	rootSessID, goal string,
	questions []expertQuestion,
	maxSteps int,
	onComplete func(idx int),
) ([]string, error) {
	type workerResult struct {
		idx    int
		output string
		err    error
	}
	results := make([]workerResult, len(questions))
	var wg sync.WaitGroup
	wg.Add(len(questions))
	for i, q := range questions {
		i, q := i, q
		go func() {
			defer wg.Done()
			out, err := expertRunWorker(ctx, k, sw, store, rootSessID, goal, q.Question, maxSteps)
			results[i] = workerResult{idx: i, output: out, err: err}
			if onComplete != nil {
				onComplete(i)
			}
		}()
	}
	wg.Wait()

	var findings []string
	for _, r := range results {
		if r.err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			slog.Warn("expert worker failed", slog.Any("error", r.err))
			findings = append(findings, fmt.Sprintf("[调研失败: %v]", r.err))
			continue
		}
		findings = append(findings, r.output)
	}
	return findings, nil
}

// expertRunWorker runs a single research question in a tagged sub-session.
func expertRunWorker(
	ctx context.Context,
	k *kernel.Kernel,
	sw *hswarm.Runtime,
	store session.SessionStore,
	rootSessID, goal, question string,
	maxSteps int,
) (string, error) {
	if maxSteps <= 0 {
		maxSteps = 30
	}
	thread, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     question,
		Mode:     "swarm-worker",
		MaxSteps: maxSteps,
	})
	if err != nil {
		return "", fmt.Errorf("创建调研子会话失败: %w", err)
	}
	session.SetThreadParent(thread, rootSessID)
	session.SetThreadRole(thread, string(kswarm.RoleWorker))

	if sw != nil {
		if spec, ok := sw.Role(kswarm.RoleWorker); ok {
			sysPrompt := expertComposeSysPrompt(spec)
			thread.Config.SystemPrompt = sysPrompt
			thread.UpdateSystemPrompt(sysPrompt)
		}
	}

	prompt := fmt.Sprintf(
		"Overall goal: %s\n\nYour assigned research question: %s\n\nResearch this question thoroughly and provide a detailed, evidence-backed answer.",
		goal, question,
	)
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(prompt)}}
	thread.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     thread,
		Agent:       k.BuildLLMAgent("expert-worker"),
		UserContent: &userMsg,
		IO:          discardIO{}, // suppress worker output from chat UI
	})
	if err != nil {
		return "", err
	}
	if store != nil {
		if saveErr := store.Save(ctx, thread); saveErr != nil {
			slog.Debug("save worker thread failed", slog.Any("error", saveErr))
		}
	}
	return strings.TrimSpace(result.Output), nil
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

// expertSynthesize combines worker findings into a final answer.
// The synthesizer's output is streamed to the frontend via wailsIO.
func expertSynthesize(
	ctx context.Context,
	k *kernel.Kernel,
	sw *hswarm.Runtime,
	store session.SessionStore,
	userIO *WailsUserIO,
	rootSessID, goal, historyCtx, outputLength string,
	findings []string,
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
			sysPrompt := expertComposeSysPrompt(spec)
			thread.Config.SystemPrompt = sysPrompt
			thread.UpdateSystemPrompt(sysPrompt)
		}
	}

	var historySection string
	if historyCtx != "" {
		historySection = fmt.Sprintf("\nPrevious conversation (for continuity):\n%s\n", historyCtx)
	}

	prompt := fmt.Sprintf(
		"Goal: %s\nAs of: %s\n%s\nWorker findings:\n%s\n\nBased on the above findings, write a comprehensive, well-structured answer in markdown. If there is prior conversation, build on it — address corrections, additions, or follow-ups as appropriate.\n\nOutput length guidance: %s",
		goal,
		time.Now().UTC().Format(time.RFC3339),
		historySection,
		strings.Join(findings, "\n\n---\n\n"),
		outputLengthInstruction(outputLength),
	)
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(prompt)}}
	thread.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     thread,
		Agent:       k.BuildLLMAgent("expert-synthesizer"),
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

// expertComposeSysPrompt composes a role-specific system prompt with time context.
func expertComposeSysPrompt(spec hswarm.RoleSpec) string {
	return strings.TrimSpace(fmt.Sprintf("%s\n\nCurrent reference time: %s",
		spec.SystemPrompt, time.Now().UTC().Format(time.RFC3339)))
}

// expertCompleteText calls the LLM with a simple two-message prompt and collects the response.
func expertCompleteText(ctx context.Context, llm model.LLM, systemPrompt, userPrompt string) (string, error) {
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
