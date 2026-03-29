package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/presets/deepagent"
	mosstui "github.com/mossagents/moss/userio/tui"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

const appName = "mossresearch"
const outputDirName = ".mossresearch"

type config struct {
	flags *appkit.AppFlags
	goal  string
}

type jinaSearchParams struct {
	Query string `json:"query"`
	Count int    `json:"count"`
	GL    string `json:"gl"`
	HL    string `json:"hl"`
}

type jinaReaderParams struct {
	URL            string `json:"url"`
	TargetSelector string `json:"target_selector"`
	RemoveSelector string `json:"remove_selector"`
	TokenBudget    int    `json:"token_budget"`
}

func main() {
	appconfig.SetAppName(appName)
	_ = appconfig.EnsureAppDir()

	cfg := parseFlags()
	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	if strings.TrimSpace(cfg.goal) != "" {
		if err := runOneShot(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := launchTUI(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *config {
	cfg := &config{flags: &appkit.AppFlags{}}
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	fs.StringVar(&cfg.goal, "goal", "", "Run one-shot deep research for a single request")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			printUsage()
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg.flags.MergeGlobalConfig()
	cfg.flags.MergeEnv("MOSSRESEARCH", "MOSS")
	cfg.flags.ApplyDefaults()
	return cfg
}

func printUsage() {
	fmt.Print(`mossresearch — deep research assistant

Usage:
  mossresearch [flags]

Flags:
  --goal        Run one-shot deep research for a single request
  --provider    LLM provider: claude|openai
  --model       Model name
  --workspace   Workspace directory (default: ".")
  --trust       Trust level: trusted|restricted
  --api-key     API key
  --base-url    API base URL
`)
}

func launchTUI(cfg *config) error {
	flags := cfg.flags
	return mosstui.Run(mosstui.Config{
		Provider:        flags.Provider,
		Model:           flags.Model,
		Workspace:       flags.Workspace,
		Trust:           flags.Trust,
		SessionStoreDir: filepath.Join(appconfig.AppDir(), "sessions"),
		BaseURL:         flags.BaseURL,
		APIKey:          flags.APIKey,
		BuildKernel: func(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
			runtimeFlags := &appkit.AppFlags{
				Provider:  provider,
				Model:     model,
				Workspace: wsDir,
				Trust:     trust,
				APIKey:    apiKey,
				BaseURL:   baseURL,
			}
			return buildKernel(context.Background(), runtimeFlags, io)
		},
		BuildSystemPrompt: buildSystemPrompt,
		BuildSessionConfig: func(workspace, trust, systemPrompt string) session.SessionConfig {
			return session.SessionConfig{
				Goal:         "interactive deep research assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				SystemPrompt: systemPrompt,
				MaxSteps:     200,
			}
		},
	})
}

func runOneShot(ctx context.Context, cfg *config) error {
	userIO := port.NewConsoleIO()
	k, err := buildKernel(ctx, cfg.flags, userIO)
	if err != nil {
		return err
	}
	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)
	if err := writeResearchRequest(cfg.flags.Workspace, cfg.goal); err != nil {
		return fmt.Errorf("write research request: %w", err)
	}

	modelName := cfg.flags.Model
	if modelName == "" {
		modelName = "(default)"
	}
	appkit.PrintBannerWithHint("mossresearch — Deep Research Assistant",
		map[string]string{
			"Provider":  cfg.flags.Provider,
			"Model":     modelName,
			"Workspace": cfg.flags.Workspace,
			"Mode":      "one-shot",
			"Trust":     cfg.flags.Trust,
			"Tools":     fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
			"Goal":      cfg.goal,
		},
		"Uses deepagent defaults plus focused research tools and a delegated researcher agent.",
	)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         cfg.goal,
		Mode:         "oneshot",
		TrustLevel:   cfg.flags.Trust,
		SystemPrompt: buildSystemPrompt(cfg.flags.Workspace),
		MaxSteps:     120,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: cfg.goal})

	result, err := k.Run(ctx, sess)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	if err := ensureFinalReport(cfg.flags.Workspace, result.Output); err != nil {
		return fmt.Errorf("write final report: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Research completed (session: %s, steps: %d, tokens: %d)\n", result.SessionID, result.Steps, result.TokensUsed.TotalTokens)
	reportPath := finalReportPath(cfg.flags.Workspace)
	fmt.Printf("📄 Report path: %s\n", reportPath)
	if strings.TrimSpace(result.Output) != "" {
		fmt.Printf("\n%s\n", result.Output)
	}
	return nil
}

func buildKernel(ctx context.Context, flags *appkit.AppFlags, io port.UserIO) (*kernel.Kernel, error) {
	deepCfg := deepagent.DefaultConfig()
	deepCfg.AppName = appName
	deepCfg.GeneralPurposeName = "research-generalist"
	deepCfg.GeneralPurposePrompt = "You are a general-purpose delegated assistant helping a deep research orchestrator. Complete delegated tasks thoroughly, cite evidence when possible, and return concise findings."
	deepCfg.GeneralPurposeDesc = "General-purpose delegated assistant for research-adjacent tasks."
	deepCfg.AdditionalAppExtensions = []appkit.Extension{
		appkit.AfterBuild(func(_ context.Context, k *kernel.Kernel) error {
			if err := registerResearchTools(k.ToolRegistry()); err != nil {
				return err
			}
			return registerResearchAgents(k, flags)
		}),
	}
	return deepagent.BuildKernel(ctx, flags, io, &deepCfg)
}

func buildSystemPrompt(workspace string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	return appconfig.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, ctx)
}

func researchOutputDir(workspace string) string {
	return filepath.Join(workspace, outputDirName)
}

func researchRequestPath(workspace string) string {
	return filepath.Join(researchOutputDir(workspace), "research_request.md")
}

func finalReportPath(workspace string) string {
	return filepath.Join(researchOutputDir(workspace), "final_report.md")
}

func ensureResearchOutputDir(workspace string) error {
	return os.MkdirAll(researchOutputDir(workspace), 0o755)
}

func writeResearchRequest(workspace, goal string) error {
	if err := ensureResearchOutputDir(workspace); err != nil {
		return err
	}
	content := strings.TrimSpace(goal)
	if content == "" {
		content = "(empty research request)"
	}
	return os.WriteFile(researchRequestPath(workspace), []byte(content+"\n"), 0o644)
}

func ensureFinalReport(workspace, output string) error {
	if err := ensureResearchOutputDir(workspace); err != nil {
		return err
	}
	reportPath := finalReportPath(workspace)
	if data, err := os.ReadFile(reportPath); err == nil && strings.TrimSpace(string(data)) != "" {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := strings.TrimSpace(output)
	if content == "" {
		content = "Research completed, but no final textual report was returned by the model. Review the session transcript and supporting files for details."
	}
	return os.WriteFile(reportPath, []byte(content+"\n"), 0o644)
}

func registerResearchTools(reg tool.Registry) error {
	if err := reg.Register(jinaSearchSpec, jinaSearchHandler()); err != nil {
		return fmt.Errorf("register jina_search: %w", err)
	}
	if err := reg.Register(jinaReaderSpec, jinaReaderHandler()); err != nil {
		return fmt.Errorf("register jina_reader: %w", err)
	}
	thinkSpec := tool.ToolSpec{
		Name:        "think_tool",
		Description: "Record a short research reflection about what was found, what is missing, and what to do next.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"thought": {"type": "string", "description": "Research reflection or next-step note"}
			},
			"required": ["thought"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"thinking", "research"},
	}
	thinkHandler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Thought string `json:"thought"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse think_tool input: %w", err)
		}
		return json.Marshal(map[string]any{
			"recorded":    true,
			"thought":     strings.TrimSpace(params.Thought),
			"recorded_at": time.Now().Format(time.RFC3339),
		})
	}
	if err := reg.Register(thinkSpec, thinkHandler); err != nil {
		return fmt.Errorf("register think_tool: %w", err)
	}
	return nil
}

var jinaSearchSpec = tool.ToolSpec{
	Name:        "jina_search",
	Description: "Search the web via Jina Search and return extracted result content.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"query":{"type":"string","description":"search query"},
			"count":{"type":"integer","description":"result count (1-20)"},
			"gl":{"type":"string","description":"country code, e.g. us/cn"},
			"hl":{"type":"string","description":"language code, e.g. en/zh-CN"}
		},
		"required":["query"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"search", "web", "research"},
}

func jinaSearchHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params jinaSearchParams
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if strings.TrimSpace(params.Query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		if params.Count <= 0 || params.Count > 20 {
			params.Count = 5
		}

		req, err := newJinaSearchRequest(ctx, params)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("jina search %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return unwrapJinaPayload(body)
	}
}

var jinaReaderSpec = tool.ToolSpec{
	Name:        "jina_reader",
	Description: "Read a webpage via Jina Reader and return extracted content.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"url":{"type":"string","description":"target page url"},
			"target_selector":{"type":"string","description":"optional CSS selector to focus on"},
			"remove_selector":{"type":"string","description":"optional CSS selector to remove"},
			"token_budget":{"type":"integer","description":"optional max token budget"}
		},
		"required":["url"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"reader", "web", "research"},
}

func jinaReaderHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params jinaReaderParams
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if strings.TrimSpace(params.URL) == "" {
			return nil, fmt.Errorf("url is required")
		}

		req, err := newJinaReaderRequest(ctx, params)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("jina reader %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return unwrapJinaPayload(body)
	}
}

func unwrapJinaPayload(body []byte) (json.RawMessage, error) {
	var envelope struct {
		Code   int             `json:"code"`
		Status int             `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		return envelope.Data, nil
	}
	return body, nil
}

func newJinaSearchRequest(ctx context.Context, params jinaSearchParams) (*http.Request, error) {
	endpoint := "https://s.jina.ai/" + url.QueryEscape(params.Query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Retain-Images", "none")
	if key := os.Getenv("JINA_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	q := req.URL.Query()
	if params.GL != "" {
		q.Set("gl", params.GL)
	}
	if params.HL != "" {
		q.Set("hl", params.HL)
	}
	q.Set("count", fmt.Sprintf("%d", params.Count))
	req.URL.RawQuery = q.Encode()
	return req, nil
}

func newJinaReaderRequest(ctx context.Context, params jinaReaderParams) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://r.jina.ai/"+url.PathEscape(params.URL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Retain-Images", "none")
	if key := os.Getenv("JINA_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if params.TargetSelector != "" {
		req.Header.Set("X-Target-Selector", params.TargetSelector)
	}
	if params.RemoveSelector != "" {
		req.Header.Set("X-Remove-Selector", params.RemoveSelector)
	}
	if params.TokenBudget > 0 {
		req.Header.Set("X-Token-Budget", fmt.Sprintf("%d", params.TokenBudget))
	}
	return req, nil
}

func registerResearchAgents(k *kernel.Kernel, flags *appkit.AppFlags) error {
	researcherPrompt := strings.TrimSpace(fmt.Sprintf(`
You are a focused research sub-agent. Today's date is %s.

Your role is to gather evidence for the orchestrator, not to write the final polished report.

Available research tools:
- jina_search: search for candidate sources
- jina_reader: read and extract webpage content
- think_tool: reflect briefly after each search or read step

Research rules:
1. Start broad, then narrow only if needed.
2. Use think_tool after each search pass to assess what is still missing.
3. Prefer authoritative or primary sources when possible.
4. Stop after you have enough evidence to answer confidently.
5. Return findings with inline citations and a final '### Sources' section.

Suggested budgets:
- Simple questions: 2-3 search steps.
- Complex questions: up to 5 search steps.
- Avoid redundant searches that repeat the same evidence.
`, time.Now().Format("2006-01-02")))

	reg := runtime.AgentRegistry(k)
	return reg.Register(agent.AgentConfig{
		Name:         "researcher",
		Description:  "Focused web research agent for gathering cited findings from web sources.",
		SystemPrompt: researcherPrompt,
		Tools:        []string{"jina_search", "jina_reader", "think_tool"},
		MaxSteps:     20,
		TrustLevel:   flags.Trust,
	})
}
