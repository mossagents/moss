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

	// Build a digest of ALL findings from previous rounds, attributed by round.
	// Each entry is truncated to 200 runes to keep context size manageable.
	const maxShown = 20
	const snippetRunes = 200

	var digest strings.Builder
	digest.WriteString(fmt.Sprintf("**前 %d 轮的研究发现（共 %d 条）：**\n\n",
		w.round-1, countValid(w.prevFindings)))

	shown := 0
	for _, f := range w.prevFindings {
		if f.Content == "" {
			continue
		}
		snip := f.Content
		if runes := []rune(snip); len(runes) > snippetRunes {
			snip = string(runes[:snippetRunes]) + "…"
		}
		digest.WriteString(fmt.Sprintf("**[第%d轮 | %s | %s]**\n%s\n\n",
			f.Round, f.Persona, f.Question, snip))
		shown++
		if shown >= maxShown {
			if remaining := countValid(w.prevFindings) - shown; remaining > 0 {
				digest.WriteString(fmt.Sprintf("（另有 %d 条发现略去）\n\n", remaining))
			}
			break
		}
	}

	return fmt.Sprintf(`你正在参与一项多 Agent 协作研究项目的第 %d 轮讨论。

%s
---

**你的研究子问题（本轮继续研究）：**
%s

**任务：**
请以你的专业视角（%s）仔细阅读以上所有前轮发现，然后：
1. 选出 1–2 条你认为存在问题或可补充的发现，进行建设性的质疑或延伸
2. 提出本轮你新的核心洞察（视角应与前轮有所演进，避免重复）
3. 指出跨不同视角、不同轮次发现之间的联系或矛盾

请保持简洁（约150–250字），直接进入实质内容。`,
		w.round, digest.String(), w.subQuestion, w.persona.Name)
}




// ─── ResearchSwarm ────────────────────────────────────────────────────────────

// ResearchSwarm is the top-level orchestrator agent.
// It drives the full pipeline: decompose → multi-round research → synthesize.
type ResearchSwarm struct {
	topic     string
	agents    int
	rounds    int
	batch     int
	personasN int // 0 = use built-in fallback; >0 = generate via LLM
	llm       model.LLM
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
		currentPersonas := personas // built-in fallback; replaced each round on success

		for round := 1; round <= s.rounds; round++ {
			// Re-generate personas before each round.
			// Round 1: tailored to the topic.
			// Round 2+: informed by previous findings to fill research gaps.
			if s.personasN > 0 {
				label := "第 1 轮初始人设"
				if round > 1 {
					label = fmt.Sprintf("第 %d 轮（基于前轮发现补全视角）", round)
				}
				fmt.Printf("\n🎭  生成专家人设 — %s（%d 个）...\n", label, s.personasN)
				generated, err := s.generatePersonas(ctx.Context, round, allFindings)
				if err != nil || len(generated) == 0 {
					fallbackDesc := "内置预设"
					if round > 1 {
						fallbackDesc = "上轮人设"
					}
					fmt.Printf("    ⚠  生成失败（%v），沿用%s\n", err, fallbackDesc)
				} else {
					currentPersonas = generated
					names := make([]string, len(currentPersonas))
					for i, p := range currentPersonas {
						names[i] = p.Name
					}
					fmt.Printf("    ✓ %s\n", strings.Join(names, " / "))
				}
			}

			fmt.Printf("\n🔬  第 %d/%d 轮研究（%d 个 Agent 并行）...\n",
				round, s.rounds, s.agents)

			roundFindings := make([]Finding, s.agents)
			workers := make([]kernel.Agent, s.agents)
			for i := 0; i < s.agents; i++ {
				p := currentPersonas[i%len(currentPersonas)]
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
// Persona names are intentionally stripped from the digest so the final report
// only surfaces viewpoints and arguments, not internal agent identities.
func (s *ResearchSwarm) synthesize(ctx context.Context, findings []Finding) (string, error) {
	var digest strings.Builder
	seq := 0
	for _, f := range findings {
		if f.Content == "" {
			continue
		}
		seq++
		// Use neutral "视角N" labels — persona names are internal implementation
		// details and must not appear in the reader-facing report.
		digest.WriteString(fmt.Sprintf("### [第%d轮 · 视角%d]\n**研究子问题：** %s\n\n%s\n\n---\n\n",
			f.Round, seq, f.Question, f.Content))
	}

	prompt := fmt.Sprintf(`你是一名高级研究综合专家。以下是经过 %d 轮多视角讨论后产出的 %d 条研究发现，
每条发现标注了所属轮次和研究子问题：

---

%s

---

请根据以上所有发现，撰写一份结构完整的综合研究报告（使用 Markdown 格式），包含：

1. **执行摘要**（4–6句话，概括最核心的发现）
2. **主要共识**（各方普遍认可的核心观点）
3. **争议与分歧**（不同视角之间的关键分歧，及其背后的原因）
4. **综合结论与建议**（整合多方视角后的综合判断和行动建议）
5. **未来研究方向**（本次讨论揭示的重要待探索议题）

**重要写作要求：**
- 报告中不得提及任何研究者名称或角色标签（如"视角1"、"第2轮"等内部编号也不应出现在正文中）
- 引用观点时使用中性表述，例如：「有观点认为」「另一角度指出」「技术层面的分析显示」「从政策视角看」等
- 不同的观点和分歧应以论点本身呈现，而非以来源标注

报告应整合各方视角，保持客观中立，字数约 600–900 字。`,
		s.rounds, seq, digest.String())

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

// ─── Dynamic persona generation ───────────────────────────────────────────────

// generatePersonas calls the LLM to produce personas tailored to the research topic.
// Round 1: personas are designed for initial topic exploration.
// Round 2+: personas are chosen to fill gaps identified in previous findings.
func (s *ResearchSwarm) generatePersonas(ctx context.Context, round int, prevFindings []Finding) ([]Persona, error) {
	var prompt string
	if round == 1 || len(prevFindings) == 0 {
		prompt = fmt.Sprintf(`你是一名跨学科研究团队设计专家。
请根据以下研究主题，设计 %d 个最适合深度研究该主题的专家角色。

要求：
- 每个角色拥有独特的专业背景和认知视角，彼此形成互补
- 角色应专门针对该主题定制（而非泛化的通用角色）
- 角色之间视角差异越大越好，覆盖技术、社会、经济、伦理、政策、文化等维度
- system_prompt 须描述该专家的专业背景、思维方式、研究风格，
  以及在多方讨论中会如何质疑他人、提供独特洞察（4–6句中文）

研究主题：%s

请以 JSON 数组格式输出，每个元素包含：
- "name": 角色中文名称（2–6字，如"量子物理学家"、"教育政策顾问"）
- "role": 英文标识符（小写字母和连字符，如"quantum-physicist"）
- "system_prompt": 该专家的系统提示词（中文）

只输出 JSON 数组，不要其他文字。`, s.personasN, s.topic)
	} else {
		digest := buildFindingsDigest(prevFindings)
		prompt = fmt.Sprintf(`你是一名跨学科研究团队设计专家，正在为多轮研究项目设计第 %d 轮的专家团队。

研究主题：%s

前几轮已产出的研究发现摘要：
%s
---

请根据以上发现，为第 %d 轮设计 %d 个专家角色，这些角色应能：
- 填补前几轮研究中缺失或浅显的视角
- 深化仍有争议或尚未充分探索的领域
- 从新的维度挑战或强化现有结论
- 与前几轮的专家视角形成明显差异和互补

要求：
- system_prompt 须描述该专家的专业背景、思维方式、研究风格（4–6句中文）
- 角色应针对本研究当前阶段的实际需求定制

请以 JSON 数组格式输出，每个元素包含：
- "name": 角色中文名称（2–6字）
- "role": 英文标识符（小写字母和连字符）
- "system_prompt": 该专家的系统提示词（中文）

只输出 JSON 数组，不要其他文字。`, round, s.topic, digest, round, s.personasN)
	}

	text, err := collectLLMText(ctx, s.llm, model.CompletionRequest{
		Messages: []model.Message{
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(prompt)}},
		},
	})
	if err != nil {
		return nil, err
	}
	return parseJSONPersonas(text)
}

// buildFindingsDigest builds a concise summary of findings for the persona generation prompt.
func buildFindingsDigest(findings []Finding) string {
	const maxPerFinding = 150
	const maxItems = 10

	var sb strings.Builder
	shown := 0
	for _, f := range findings {
		if f.Content == "" {
			continue
		}
		snip := f.Content
		if runes := []rune(snip); len(runes) > maxPerFinding {
			snip = string(runes[:maxPerFinding]) + "…"
		}
		sb.WriteString(fmt.Sprintf("[第%d轮 | %s | %s]\n%s\n\n", f.Round, f.Persona, f.Question, snip))
		shown++
		if shown >= maxItems {
			if remaining := len(findings) - shown; remaining > 0 {
				sb.WriteString(fmt.Sprintf("（另有 %d 条发现略去）\n", remaining))
			}
			break
		}
	}
	return sb.String()
}

// parseJSONPersonas extracts a []Persona from an LLM response containing a JSON array.
func parseJSONPersonas(text string) ([]Persona, error) {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array in response")
	}

	var raw []struct {
		Name         string `json:"name"`
		Role         string `json:"role"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	out := make([]Persona, 0, len(raw))
	for _, r := range raw {
		if r.Name == "" || r.SystemPrompt == "" {
			continue
		}
		slug := sanitizeSlug(r.Role)
		if slug == "" {
			slug = "expert"
		}
		out = append(out, Persona{
			Name:         r.Name,
			Role:         slug,
			SystemPrompt: r.SystemPrompt,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid personas parsed")
	}
	return out, nil
}

// sanitizeSlug converts an arbitrary string to a lowercase hyphen-separated slug.
func sanitizeSlug(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			sb.WriteRune(r)
		case r == ' ' || r == '_':
			sb.WriteRune('-')
		}
	}
	return strings.Trim(sb.String(), "-")
}
