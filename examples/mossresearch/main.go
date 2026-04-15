package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
	mosstui "github.com/mossagents/moss/contrib/tui"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/runtime/thinking"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

const appName = "mossresearch"
const outputDirName = ".mossresearch"

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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
	fs.StringVar(&cfg.prompt, "prompt", "", "Run one-shot deep research for a single request")
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
	cfg.flags.MergeEnv("MOSSRESEARCH", "MOSS")
	cfg.flags.ApplyDefaults()
	return cfg
}

func printUsage() {
	fmt.Print(`mossresearch — deep research assistant

Usage:
  mossresearch [flags]

Flags:
  --prompt, -p  Run one-shot deep research for a single request
  --provider    LLM provider: claude|openai-completions|openai-responses|gemini
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
		BuildKernel: func(wsDir, trust, approvalMode, profile, provider, model, apiKey, baseURL string, io kernio.UserIO) (*kernel.Kernel, error) {
			runtimeFlags := &appkit.AppFlags{
				Provider:  provider,
				Name:      provider,
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
				Goal:         "interactive deep research assistant",
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
	userIO := kernio.NewConsoleIO()
	k, err := buildKernel(ctx, cfg.flags, userIO)
	if err != nil {
		return err
	}
	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)
	if err := writeResearchRequest(cfg.flags.Workspace, cfg.prompt); err != nil {
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
			"Prompt":    cfg.prompt,
		},
		"Uses deepagent defaults plus focused research tools and a delegated researcher agent.",
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
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(cfg.prompt)}}
	sess.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent(appName),
		UserContent: &userMsg,
	})
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	if err := ensureFinalReport(cfg.flags.Workspace, result.Output); err != nil {
		return fmt.Errorf("write final report: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Research completed (session: %s, steps: %d, tokens: %d)\n", sess.ID, result.Steps, result.TokensUsed.TotalTokens)
	reportPath := finalReportPath(cfg.flags.Workspace)
	fmt.Printf("📄 Report path: %s\n", reportPath)
	if strings.TrimSpace(result.Output) != "" {
		fmt.Printf("\n%s\n", result.Output)
	}
	return nil
}

func buildKernel(ctx context.Context, flags *appkit.AppFlags, io kernio.UserIO) (*kernel.Kernel, error) {
	deepCfg := appkit.DeepAgentDefaults()
	deepCfg.AppName = appName
	deepCfg.GeneralPurposeName = "research-generalist"
	deepCfg.GeneralPurposePrompt = "You are a general-purpose delegated assistant helping a deep research orchestrator. Complete delegated tasks thoroughly, cite evidence when possible, and return concise findings."
	deepCfg.GeneralPurposeDesc = "General-purpose delegated assistant for research-adjacent tasks."
	deepCfg.AdditionalFeatures = []harness.Feature{
		harness.FeatureFunc{FeatureName: "research-tools", InstallFunc: func(_ context.Context, h *harness.Harness) error {
			if err := registerResearchTools(h.Kernel().ToolRegistry()); err != nil {
				return err
			}
			return registerResearchAgents(h.Kernel(), flags)
		}},
	}
	return appkit.BuildDeepAgent(ctx, flags, io, &deepCfg)
}

func buildSystemPrompt(workspace, trust string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	return appconfig.RenderSystemPromptForTrust(workspace, trust, defaultSystemPromptTemplate, ctx)
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
	return thinking.RegisterThinkTool(reg,
		thinking.WithThinkToolDescription("Record a short research reflection about what was found, what is missing, and what to do next."),
		thinking.WithThinkToolCapabilities("thinking", "research"),
	)
}

func registerResearchAgents(k *kernel.Kernel, flags *appkit.AppFlags) error {
	researcherPrompt := strings.TrimSpace(fmt.Sprintf(`
You are a focused research sub-agent. Today's date is %s.

Your role is to gather evidence for the orchestrator, not to write the final polished report.

Available research tools:
- think_tool: reflect briefly after each search or read step

Research rules:
1. Start broad, then narrow only if needed.
2. Use think_tool after each pass to assess what is still missing.
3. Prefer authoritative or primary sources when possible.
4. Stop after you have enough evidence to answer confidently.
5. Return findings with inline citations and a final '### Sources' section.

Suggested budgets:
- Simple questions: 2-3 search steps.
- Complex questions: up to 5 search steps.
- Avoid redundant searches that repeat the same evidence.
`, time.Now().Format("2006-01-02")))

	return harness.RegisterSubagent(k, harness.SubagentConfig{
		Name:         "researcher",
		Description:  "Focused research agent for gathering cited findings from available tools and project context.",
		SystemPrompt: researcherPrompt,
		Tools:        []string{"think_tool", "read_file", "web_fetch"},
		MaxSteps:     20,
		TrustLevel:   flags.Trust,
	})
}

