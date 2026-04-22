package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/patterns"
	"github.com/mossagents/moss/kernel/session"
)

// Finding captures one agent's research output for a given round.
type Finding struct {
	AgentID  int
	Persona  string
	Round    int
	Question string
	Content  string
}

// newEvent is a convenience constructor for a custom agent event.
func newEvent(id, author, text string) *session.Event {
	return &session.Event{
		ID:        id,
		Author:    author,
		Type:      session.EventTypeCustom,
		Timestamp: time.Now(),
		Content: &model.Message{
			Role:         model.RoleAssistant,
			ContentParts: []model.ContentPart{model.TextPart(text)},
		},
	}
}

// collectLLMText drains a GenerateContent iterator and returns the accumulated text.
func collectLLMText(ctx context.Context, llm model.LLM, req model.CompletionRequest) (string, error) {
	var sb strings.Builder
	for chunk, err := range llm.GenerateContent(ctx, req) {
		if err != nil {
			return sb.String(), err
		}
		if chunk.Type == model.StreamChunkTextDelta {
			sb.WriteString(chunk.Content)
		}
	}
	return sb.String(), nil
}

// ─── PersonaWorkerAgent ───────────────────────────────────────────────────────

// PersonaWorkerAgent is one member of the swarm. It calls the LLM directly
// (no nested RunChild) so it consumes exactly one active-agent slot.
// Each worker has a fixed persona and researches a specific sub-question.
// In rounds > 1 it also receives all prior findings to enable brainstorming.
type PersonaWorkerAgent struct {
	id           int
	persona      Persona
	subQuestion  string
	round        int
	prevFindings []Finding // findings from all previous rounds
	llm          model.LLM
	out          []Finding // pre-allocated slice; writes to out[id]
}

func (w *PersonaWorkerAgent) Name() string {
	return fmt.Sprintf("worker-%03d-%s", w.id, w.persona.Role)
}

func (w *PersonaWorkerAgent) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		userPrompt := w.buildUserPrompt()

		text, err := collectLLMText(ctx.Context, w.llm, model.CompletionRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart(w.persona.SystemPrompt)}},
				{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(userPrompt)}},
			},
		})
		if err != nil {
			// Do not propagate error upward – record a placeholder finding instead.
			text = fmt.Sprintf("（研究暂时中断: %v）", err)
		}

		w.out[w.id] = Finding{
			AgentID:  w.id,
			Persona:  w.persona.Name,
			Round:    w.round,
			Question: w.subQuestion,
			Content:  text,
		}

		label := fmt.Sprintf("[R%d|%s]", w.round, w.persona.Name)
		yield(newEvent(
			fmt.Sprintf("w%03d-r%d", w.id, w.round),
			label,
			text,
		), nil)
	}
}

func (w *PersonaWorkerAgent) buildUserPrompt() string {
	if w.round == 1 || len(w.prevFindings) == 0 {
		return fmt.Sprintf(`你正在参与一项多 Agent 协作研究项目。

**你的研究子问题：**
%s

**任务：**
请以你的专业视角（%s）对该子问题进行深入分析，提供：
1. 核心洞察（2–3条最重要的发现）
2. 关键论点及支撑依据
3. 你认为该领域最值得关注的争议点或未解问题

请给出简洁但有深度的分析（约150–250字）。`,
			w.subQuestion, w.persona.Name)
	}

	// Build digest of previous findings (truncated for context efficiency).
	var digest strings.Builder
	digest.WriteString("**其他研究者的发现（第")
	digest.WriteString(fmt.Sprintf("%d", w.round-1))
	digest.WriteString("轮）：**\n\n")
	for i, f := range w.prevFindings {
		if f.Content == "" {
			continue
		}
		snip := f.Content
		if len([]rune(snip)) > 200 {
			runes := []rune(snip)
			snip = string(runes[:200]) + "…"
		}
		digest.WriteString(fmt.Sprintf("**[%s | %s]** %s\n\n", f.Persona, f.Question, snip))
		if i >= 14 {
			digest.WriteString(fmt.Sprintf("（另有 %d 条发现略去）\n", len(w.prevFindings)-15))
			break
		}
	}

	return fmt.Sprintf(`你正在参与一项多 Agent 协作研究项目的第 %d 轮讨论。

%s

---

**你的研究子问题（本轮继续研究）：**
%s

**任务：**
请以你的专业视角（%s）阅读以上其他研究者的发现，然后：
1. 选出 1–2 条你认为存在问题或可补充的发现，进行建设性的质疑或延伸
2. 提出本轮你新的核心洞察（视角应与上轮有所演进）
3. 指出跨不同视角发现之间的联系或矛盾

请保持简洁（约150–250字），直接进入实质内容。`,
		w.round, digest.String(), w.subQuestion, w.persona.Name)
}

// ─── ResearchSwarm ────────────────────────────────────────────────────────────

// ResearchSwarm is the top-level orchestrator agent.
// It drives the full pipeline: decompose → multi-round research → synthesize.
type ResearchSwarm struct {
	topic  string
	agents int
	rounds int
	batch  int
	llm    model.LLM
}

func (s *ResearchSwarm) Name() string { return "research-swarm" }

func (s *ResearchSwarm) Run(ctx *kernel.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		// ── 1. Problem decomposition ──────────────────────────────────────────
		fmt.Printf("\n🔍  正在分解研究主题...\n")
		questions, err := s.decompose(ctx.Context)
		if err != nil {
			yield(nil, fmt.Errorf("问题分解失败: %w", err))
			return
		}
		fmt.Printf("    ✓ 已生成 %d 个子问题\n", len(questions))
		for i, q := range questions {
			fmt.Printf("      [%02d] %s\n", i+1, q)
		}

		// ── 2. Multi-round research ───────────────────────────────────────────
		var allFindings []Finding

		for round := 1; round <= s.rounds; round++ {
			fmt.Printf("\n🔬  第 %d/%d 轮研究（%d 个 Agent 并行）...\n",
				round, s.rounds, s.agents)

			roundFindings := make([]Finding, s.agents)
			workers := make([]kernel.Agent, s.agents)
			for i := 0; i < s.agents; i++ {
				p := personas[i%len(personas)]
				workers[i] = &PersonaWorkerAgent{
					id:           i,
					persona:      p,
					subQuestion:  questions[i%len(questions)],
					round:        round,
					prevFindings: allFindings,
					llm:          s.llm,
					out:          roundFindings,
				}
			}

			// Run workers in batches to respect maxActiveAgents=16.
			// Peak slots: 1 (ParallelAgent) + batch = at most 11.
			for bIdx := 0; bIdx*s.batch < s.agents; bIdx++ {
				lo := bIdx * s.batch
				hi := lo + s.batch
				if hi > s.agents {
					hi = s.agents
				}

				par := &patterns.ParallelAgent{
					AgentName: fmt.Sprintf("par-r%d-b%d", round, bIdx),
					Agents:    workers[lo:hi],
				}

				for event, err := range ctx.RunChild(par, kernel.ChildRunConfig{
					Branch:                 fmt.Sprintf("swarm.r%d.b%d", round, bIdx),
					DisableMaterialization: true,
				}) {
					if err != nil {
						// Non-fatal batch error: log and skip this batch.
						fmt.Printf("  ⚠  批次 %d 错误（已跳过）: %v\n", bIdx, err)
						break
					}
					if event == nil || event.Content == nil {
						continue
					}
					if !yield(event, nil) {
						return
					}
				}
			}

			allFindings = append(allFindings, roundFindings...)
			validFindings := countValid(roundFindings)
			fmt.Printf("  ✓ 第 %d 轮完成，%d 条发现（累计 %d 条）\n",
				round, validFindings, countValid(allFindings))
		}

		// ── 3. Synthesis ──────────────────────────────────────────────────────
		fmt.Printf("\n📊  正在综合 %d 条研究发现...\n", countValid(allFindings))
		report, err := s.synthesize(ctx.Context, allFindings)
		if err != nil {
			yield(nil, fmt.Errorf("综合报告生成失败: %w", err))
			return
		}
		yield(newEvent("synthesis-report", "synthesis", report), nil)
	}
}

// decompose calls the LLM to break the research topic into N sub-questions.
func (s *ResearchSwarm) decompose(ctx context.Context) ([]string, error) {
	prompt := fmt.Sprintf(`你是一名研究协调员。请将以下研究主题分解为恰好 %d 个具体子问题，
每个子问题应从不同维度切入（例如技术、经济、社会、伦理、历史、政策等），
使得各子问题合在一起能够全面覆盖该主题的各个核心方面。

研究主题：%s

请以 JSON 数组格式输出，每个元素是一个子问题字符串（不需要编号）：
["子问题1", "子问题2", ...]

只输出 JSON 数组，不要其他文字。`, s.agents, s.topic)

	text, err := collectLLMText(ctx, s.llm, model.CompletionRequest{
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(prompt)}},
		},
	})
	if err != nil {
		return s.fallbackQuestions(), nil // graceful degradation
	}

	questions, parseErr := parseJSONStringArray(text)
	if parseErr != nil || len(questions) == 0 {
		// Fall back to line-by-line parsing.
		questions = parseLines(text)
	}

	// Ensure we have exactly s.agents questions.
	for len(questions) < s.agents {
		questions = append(questions, fmt.Sprintf("%s 的补充研究维度 #%d", s.topic, len(questions)+1))
	}
	return questions[:s.agents], nil
}

// synthesize calls the LLM to produce a structured research report.
func (s *ResearchSwarm) synthesize(ctx context.Context, findings []Finding) (string, error) {
	var digest strings.Builder
	for _, f := range findings {
		if f.Content == "" {
			continue
		}
		digest.WriteString(fmt.Sprintf("### [R%d | %s]\n**子问题：** %s\n\n%s\n\n---\n\n",
			f.Round, f.Persona, f.Question, f.Content))
	}

	prompt := fmt.Sprintf(`你是一名高级研究综合专家。以下是 %d 个具有不同专业背景的研究者，
经过 %d 轮讨论后产出的所有研究发现：

---

%s

---

请根据以上所有发现，撰写一份结构完整的综合研究报告（使用 Markdown 格式），包含：

1. **执行摘要**（4–6句话，概括最核心的发现）
2. **主要共识**（各方普遍认可的核心观点）
3. **争议与分歧**（不同视角之间的关键分歧，及其背后的原因）
4. **综合结论与建议**（整合多方视角后的综合判断和行动建议）
5. **未来研究方向**（本次讨论揭示的重要待探索议题）

报告应整合各方视角，保持客观中立，字数约 600–900 字。`,
		s.agents, s.rounds, digest.String())

	text, err := collectLLMText(ctx, s.llm, model.CompletionRequest{
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(prompt)}},
		},
	})
	if err != nil {
		return fmt.Sprintf("（综合报告生成失败: %v）\n\n原始发现已收集 %d 条。", err, len(findings)), nil
	}
	return text, nil
}

// fallbackQuestions generates simple sub-questions without LLM.
func (s *ResearchSwarm) fallbackQuestions() []string {
	dimensions := []string{
		"技术可行性与实现路径", "经济影响与商业模式",
		"社会文化影响", "伦理与隐私风险",
		"政策与监管框架", "历史先例与类比案例",
		"短期挑战与障碍", "长期趋势与未来图景",
		"国际竞争与合作格局", "用户体验与包容性设计",
	}
	out := make([]string, 0, s.agents)
	for _, d := range dimensions {
		if len(out) >= s.agents {
			break
		}
		out = append(out, fmt.Sprintf("从【%s】角度分析：%s", d, s.topic))
	}
	for len(out) < s.agents {
		out = append(out, fmt.Sprintf("%s 的补充分析视角 #%d", s.topic, len(out)+1))
	}
	return out
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func parseJSONStringArray(text string) ([]string, error) {
	// Find the first '[' ... ']' block.
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found")
	}
	var arr []string
	if err := json.Unmarshal([]byte(text[start:end+1]), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func parseLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		// Strip common list markers.
		line = strings.TrimLeft(line, "0123456789.-*• ")
		line = strings.Trim(line, `"`)
		if len([]rune(line)) > 8 {
			out = append(out, line)
		}
	}
	return out
}

func countValid(findings []Finding) int {
	n := 0
	for _, f := range findings {
		if f.Content != "" {
			n++
		}
	}
	return n
}
