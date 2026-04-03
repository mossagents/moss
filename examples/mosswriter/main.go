package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

const appName = "mosswriter"

type config struct {
	flags  *appkit.AppFlags
	prompt string
}

func main() {
	if err := appkit.InitializeApp(appName, nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg := parseFlags()
	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	if strings.TrimSpace(cfg.prompt) != "" {
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
	fs.StringVar(&cfg.prompt, "prompt", "", "Run one-shot content creation for a single request")
	fs.StringVar(&cfg.prompt, "p", cfg.prompt, "Shorthand for --prompt")
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
  --prompt, -p  Run one-shot content creation for a single request
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
		BuildKernel: func(wsDir, trust, approvalMode, profile, apiType, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
			runtimeFlags := &appkit.AppFlags{
				APIType:   apiType,
				Provider:  apiType,
				Name:      apiType,
				Model:     model,
				Workspace: wsDir,
				Trust:     trust,
				Profile:   profile,
				APIKey:    apiKey,
				BaseURL:   baseURL,
			}
			return buildKernel(context.Background(), runtimeFlags, io)
		},
		BuildSystemPrompt: buildSystemPrompt,
		BuildSessionConfig: func(workspace, trust, approvalMode, profile, systemPrompt string) session.SessionConfig {
			return session.SessionConfig{
				Goal:         "interactive content writing assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				Profile:      profile,
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
			"Prompt":    cfg.prompt,
		},
		"Uses deepagent defaults plus content-writing skills, delegated subagents, and writing utilities.",
	)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         cfg.prompt,
		Mode:         "oneshot",
		TrustLevel:   cfg.flags.Trust,
		SystemPrompt: buildSystemPrompt(cfg.flags.Workspace, cfg.flags.Trust),
		MaxSteps:     120,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: cfg.prompt})

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
			return runtime.LoadSubagentsFromYAML(k, filepath.Join(flags.Workspace, "subagents.yaml"))
		}),
	}
	return deepagent.BuildKernel(ctx, flags, io, &deepCfg)
}

func buildSystemPrompt(workspace, trust string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	return appconfig.RenderSystemPromptForTrust(workspace, trust, defaultSystemPromptTemplate, ctx)
}

func registerWriterTools(reg tool.Registry) error {
	for _, entry := range []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{
			runtime.NewThinkToolSpec(
				runtime.WithThinkToolDescription("Record a short reflection about writing, research gaps, or next steps."),
				runtime.WithThinkToolCapabilities("thinking", "writing"),
			),
			runtime.NewThinkToolHandler("think_tool"),
		},
		{makeSlugSpec, makeSlugHandler()},
		{generateImageBriefSpec, generateImageBriefHandler()},
	} {
		if err := reg.Register(entry.spec, entry.handler); err != nil {
			return err
		}
	}
	return nil
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
