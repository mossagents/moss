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
	"regexp"
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
	"gopkg.in/yaml.v3"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

const appName = "mosswriter"

type config struct {
	flags *appkit.AppFlags
	goal  string
}

type subagentFileConfig struct {
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxSteps     int      `yaml:"max_steps"`
	TrustLevel   string   `yaml:"trust_level"`
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
	fs.StringVar(&cfg.goal, "goal", "", "Run one-shot content creation for a single request")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			printUsage()
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg.flags.MergeGlobalConfig()
	cfg.flags.MergeEnv("MOSSWRITER", "MOSS")
	cfg.flags.ApplyDefaults()
	return cfg
}

func printUsage() {
	fmt.Print(`mosswriter — content builder agent

Usage:
  mosswriter [flags]

Flags:
  --goal        Run one-shot content creation for a single request
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
				Goal:         "interactive content writing assistant",
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

	modelName := cfg.flags.Model
	if modelName == "" {
		modelName = "(default)"
	}
	appkit.PrintBannerWithHint("mosswriter — Content Builder Agent",
		map[string]string{
			"Provider":  cfg.flags.Provider,
			"Model":     modelName,
			"Workspace": cfg.flags.Workspace,
			"Mode":      "one-shot",
			"Trust":     cfg.flags.Trust,
			"Tools":     fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
			"Goal":      cfg.goal,
		},
		"Uses deepagent defaults plus content-writing skills, delegated subagents, and writing utilities.",
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

	fmt.Println()
	fmt.Printf("✅ Content created (session: %s, steps: %d, tokens: %d)\n", result.SessionID, result.Steps, result.TokensUsed.TotalTokens)
	fmt.Printf("📁 Output root: %s\n", filepath.Join(cfg.flags.Workspace, ".mosswriter"))
	if strings.TrimSpace(result.Output) != "" {
		fmt.Printf("\n%s\n", result.Output)
	}
	return nil
}

func buildKernel(ctx context.Context, flags *appkit.AppFlags, io port.UserIO) (*kernel.Kernel, error) {
	deepCfg := deepagent.DefaultConfig()
	deepCfg.AppName = appName
	deepCfg.GeneralPurposeName = "content-generalist"
	deepCfg.GeneralPurposePrompt = "You are a general-purpose delegated assistant helping a content creation workflow. Complete delegated tasks thoroughly and return concise, useful results."
	deepCfg.GeneralPurposeDesc = "General-purpose delegated assistant for content workflows."
	deepCfg.AdditionalAppExtensions = []appkit.Extension{
		appkit.AfterBuild(func(_ context.Context, k *kernel.Kernel) error {
			if err := registerWriterTools(k.ToolRegistry()); err != nil {
				return err
			}
			return loadSubagents(k, filepath.Join(flags.Workspace, "subagents.yaml"))
		}),
	}
	return deepagent.BuildKernel(ctx, flags, io, &deepCfg)
}

func buildSystemPrompt(workspace string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	return appconfig.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, ctx)
}

func loadSubagents(k *kernel.Kernel, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read subagents file: %w", err)
	}

	var defs map[string]subagentFileConfig
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return fmt.Errorf("parse subagents file: %w", err)
	}

	reg := runtime.AgentRegistry(k)
	for name, def := range defs {
		cfg := agent.AgentConfig{
			Name:         name,
			Description:  def.Description,
			SystemPrompt: strings.TrimSpace(def.SystemPrompt),
			Tools:        def.Tools,
			MaxSteps:     def.MaxSteps,
			TrustLevel:   def.TrustLevel,
		}
		if _, exists := reg.Get(cfg.Name); exists {
			continue
		}
		if err := reg.Register(cfg); err != nil {
			return fmt.Errorf("register subagent %s: %w", name, err)
		}
	}
	return nil
}

func registerWriterTools(reg tool.Registry) error {
	for _, entry := range []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{jinaSearchSpec, jinaSearchHandler()},
		{jinaReaderSpec, jinaReaderHandler()},
		{thinkToolSpec, thinkToolHandler()},
		{makeSlugSpec, makeSlugHandler()},
		{generateImageBriefSpec, generateImageBriefHandler()},
	} {
		if err := reg.Register(entry.spec, entry.handler); err != nil {
			return err
		}
	}
	return nil
}

var thinkToolSpec = tool.ToolSpec{
	Name:         "think_tool",
	Description:  "Record a short reflection about writing, research gaps, or next steps.",
	InputSchema:  json.RawMessage(`{"type":"object","properties":{"thought":{"type":"string"}},"required":["thought"]}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"thinking", "writing"},
}

func thinkToolHandler() tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
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
}

var makeSlugSpec = tool.ToolSpec{
	Name:         "make_slug",
	Description:  "Create a filesystem-safe slug from a title or topic.",
	InputSchema:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"writing", "filesystem"},
}

func makeSlugHandler() tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse make_slug input: %w", err)
		}
		slug := slugify(params.Text)
		return json.Marshal(map[string]string{"slug": slug})
	}
}

var generateImageBriefSpec = tool.ToolSpec{
	Name:        "generate_image_brief",
	Description: "Generate a concise but specific image brief for a cover image or social card.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"title":{"type":"string"},
			"content_type":{"type":"string","description":"blog, linkedin, twitter, etc."},
			"theme":{"type":"string","description":"optional visual theme or direction"},
			"audience":{"type":"string","description":"optional target audience"}
		},
		"required":["title","content_type"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"writing", "creative"},
}

func generateImageBriefHandler() tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Title       string `json:"title"`
			ContentType string `json:"content_type"`
			Theme       string `json:"theme"`
			Audience    string `json:"audience"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse generate_image_brief input: %w", err)
		}
		brief := fmt.Sprintf(
			"Create a %s visual for the title %q. Focus on a clean, modern composition. Theme: %s. Audience: %s. Prefer strong focal imagery, minimal clutter, and visuals that reinforce the core idea without generic AI aesthetics.",
			strings.TrimSpace(params.ContentType),
			strings.TrimSpace(params.Title),
			valueOr(params.Theme, "editorial technology / professional insight"),
			valueOr(params.Audience, "technical and professional readers"),
		)
		return json.Marshal(map[string]string{
			"brief": brief,
		})
	}
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
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Query string `json:"query"`
			Count int    `json:"count"`
			GL    string `json:"gl"`
			HL    string `json:"hl"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if strings.TrimSpace(params.Query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		if params.Count <= 0 || params.Count > 20 {
			params.Count = 5
		}

		endpoint := "https://s.jina.ai/" + url.QueryEscape(params.Query)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
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
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			URL            string `json:"url"`
			TargetSelector string `json:"target_selector"`
			RemoveSelector string `json:"remove_selector"`
			TokenBudget    int    `json:"token_budget"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if strings.TrimSpace(params.URL) == "" {
			return nil, fmt.Errorf("url is required")
		}

		req, err := http.NewRequest(http.MethodGet, "https://r.jina.ai/"+params.URL, nil)
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
	return json.Marshal(string(body))
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "untitled"
	}
	return s
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
