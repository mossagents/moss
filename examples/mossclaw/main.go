// mossclaw 是一个个人 AI 助理示例，对标 OpenClaw (openclaw.ai)。
//
// 演示如何用 moss kernel 构建具有丰富能力的个人 AI 助理：
//   - 网络访问工具：fetch_url（抓取网页内容）、extract_links（提取链接）
//   - 知识库：语义检索、文档摄入
//   - 定时任务调度
//   - Bootstrap 上下文（AGENTS.md / SOUL.md / TOOLS.md）
//   - 交互式 TUI 模式
//
// 用法:
//
//	go run . --provider openai --model gpt-4o
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/gateway"
	"github.com/mossagents/moss/kernel"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/providers/embedding"
	"github.com/mossagents/moss/scheduler"
	mosstui "github.com/mossagents/moss/contrib/tui"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

func main() {
	if err := appkit.InitializeApp("mossclaw", nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	var mode string
	flag.StringVar(&mode, "mode", "tui", "Run mode: tui | gateway (channel-based)")
	flags := appkit.ParseAppFlags()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	if err := run(ctx, flags, mode); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, flags *appkit.AppFlags, mode string) error {
	if err := prepareP2Resilience(flags.Workspace); err != nil {
		return err
	}
	if mode == "gateway" {
		return runGateway(ctx, flags)
	}

	return launchTUI(flags)

}

func prepareP2Resilience(workspace string) error {
	if workspace == "" {
		workspace = "."
	}
	_, err := gateway.ValidateRuntimeAssets(workspace, gateway.AssetModeBestEffort)
	if err != nil {
		return fmt.Errorf("validate runtime assets: %w", err)
	}
	_ = gateway.NewRetryBudget(8)
	_ = gateway.NewProfileRotator([]gateway.ModelProfile{
		{Name: "primary", Provider: "default"},
	})
	return nil
}

type mossclawRuntime struct {
	flags *appkit.AppFlags
	store session.SessionStore
	sched *scheduler.Scheduler
}

func launchTUI(flags *appkit.AppFlags) error {
	var activeRuntime *mossclawRuntime

	return mosstui.Run(mosstui.Config{
		Provider:  flags.Provider,
		Model:     flags.Model,
		Workspace: flags.Workspace,
		Trust:     flags.Trust,
		BaseURL:   flags.BaseURL,
		APIKey:    flags.APIKey,
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
			k, runtime, err := buildMiniclawKernel(context.Background(), runtimeFlags, io)
			if err != nil {
				return nil, err
			}
			activeRuntime = runtime
			return k, nil
		},
		AfterBoot: func(ctx context.Context, k *kernel.Kernel, io kernio.UserIO) error {
			if activeRuntime != nil {
				activeRuntime.startScheduler(ctx, k, io)
			}
			return nil
		},
		BuildSystemPrompt: buildSystemPrompt,
		BuildSessionConfig: func(workspace, trust, approvalMode, profile, systemPrompt string) session.SessionConfig {
			return session.SessionConfig{
				Goal:         "personal AI assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				Profile:      profile,
				SystemPrompt: systemPrompt,
				MaxSteps:     200,
			}
		},
	})
}

func runGateway(ctx context.Context, flags *appkit.AppFlags) error {
	userIO := kernio.NewConsoleIO()
	k, rt, err := buildMiniclawKernel(ctx, flags, userIO)
	if err != nil {
		return err
	}

	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)
	rt.startScheduler(ctx, k, userIO)

	modelName := flags.Model
	if modelName == "" {
		modelName = "(default)"
	}
	appkit.PrintBannerWithHint("mossclaw — Personal AI Assistant",
		map[string]string{
			"Provider":  flags.Provider,
			"Model":     modelName,
			"Workspace": flags.Workspace,
			"Mode":      "gateway",
			"Tools":     fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
		},
		"Ask me anything — I can search the web, manage files, schedule tasks, and more.",
	)

	return appkit.Serve(ctx, appkit.ServeConfig{
		Prompt:       "🐾 > ",
		SessionStore: rt.store,
		SystemPrompt: buildSystemPrompt(flags.Workspace, flags.Trust),
		DeliveryDir:  filepath.Join(appconfig.AppDir(), "delivery"),
		RouteScope:   "per-peer",
	}, k)
}

func buildMiniclawKernel(ctx context.Context, flags *appkit.AppFlags, io kernio.UserIO) (*kernel.Kernel, *mossclawRuntime, error) {
	storeDir := filepath.Join(appconfig.AppDir(), "sessions")
	store, err := session.NewFileStore(storeDir)
	if err != nil {
		return nil, nil, fmt.Errorf("session store: %w", err)
	}

	sched := scheduler.New()
	embedder := embedding.NewWithBaseURL(flags.APIKey, flags.BaseURL)
	knStore := runtime.NewMemoryKnowledgeStore()

	k, err := appkit.BuildKernelWithFeatures(ctx, flags, io,
		appkit.WithSessionStore(store),
		appkit.WithScheduling(sched),
		appkit.WithLoadedBootstrapContextWithTrust(flags.Workspace, "mossclaw", flags.Trust),
		appkit.WithKnowledge(knStore, embedder),
		appkit.AfterBuild(func(_ context.Context, built *kernel.Kernel) error {
			return registerWebTools(built)
		}),
	)
	if err != nil {
		return nil, nil, err
	}
	if flags.Trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command", "fetch_url"),
			builtins.DefaultAllow(),
		)
	}

	return k, &mossclawRuntime{flags: flags, store: store, sched: sched}, nil
}

func (r *mossclawRuntime) startScheduler(ctx context.Context, k *kernel.Kernel, io kernio.UserIO) {
	_ = runtime.StartScheduledRunner(ctx, runtime.ScheduledRunnerConfig{
		Kernel:       k,
		Scheduler:    r.sched,
		SessionStore: r.store,
		DefaultIO:    io,
		BuildSessionConfig: func(_ context.Context, job scheduler.Job) (session.SessionConfig, error) {
			return session.SessionConfig{
				Goal:         job.Goal,
				Mode:         "scheduled",
				TrustLevel:   r.flags.Trust,
				SystemPrompt: buildSystemPrompt(r.flags.Workspace, r.flags.Trust),
				MaxSteps:     30,
			}, nil
		},
		BeforeRun: func(jobCtx context.Context, job scheduler.Job) {
			_ = io.Send(jobCtx, kernio.OutputMessage{
				Type:    kernio.OutputProgress,
				Content: fmt.Sprintf("Scheduled task [%s] started: %s", job.ID, job.Goal),
			})
		},
		RunIO: func(_ context.Context, job scheduler.Job) kernio.UserIO {
			_ = job
			return runtime.NewScheduledCaptureIO()
		},
		OnCreateError: func(jobCtx context.Context, job scheduler.Job, err error) {
			_ = io.Send(jobCtx, kernio.OutputMessage{Type: kernio.OutputProgress, Content: fmt.Sprintf("Scheduled task [%s] failed to create session: %v", job.ID, err)})
		},
		OnRunError: func(jobCtx context.Context, job scheduler.Job, _ *session.Session, err error, _ kernio.UserIO) {
			_ = io.Send(jobCtx, kernio.OutputMessage{Type: kernio.OutputProgress, Content: fmt.Sprintf("Scheduled task [%s] failed: %v", job.ID, err)})
		},
		OnComplete: func(jobCtx context.Context, job scheduler.Job, _ *session.Session, result *loop.SessionResult, runIO kernio.UserIO) {
			summary := strings.TrimSpace(result.Output)
			if summary == "" {
				if capture, ok := runIO.(*runtime.ScheduledCaptureIO); ok {
					summary = strings.TrimSpace(capture.FinalText())
				}
			}
			if summary != "" {
				_ = io.Send(jobCtx, kernio.OutputMessage{
					Type:    kernio.OutputText,
					Content: fmt.Sprintf("⏰ Scheduled task [%s]\n%s", job.ID, summary),
				})
			}
			_ = io.Send(jobCtx, kernio.OutputMessage{
				Type:    kernio.OutputProgress,
				Content: fmt.Sprintf("Scheduled task [%s] done (%d steps)", job.ID, result.Steps),
			})
		},
	})
}

// ─── Web Tools ──────────────────────────────────────

func registerWebTools(k *kernel.Kernel) error {
	// fetch_url: 抓取网页内容，返回纯文本
	fetchSpec := tool.ToolSpec{
		Name: "fetch_url",
		Description: `Fetch the content of a web page and return it as plain text.
HTML tags are stripped. Use this to read web pages, documentation, articles, etc.
Supports http and https URLs. Returns up to 50000 characters of content.`,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {"type": "string", "description": "The URL to fetch (must start with http:// or https://)"},
				"max_length": {"type": "integer", "description": "Maximum content length to return (default: 50000)"}
			},
			"required": ["url"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"network"},
	}

	// extract_links: 提取网页中的所有链接
	linksSpec := tool.ToolSpec{
		Name: "extract_links",
		Description: `Fetch a web page and extract all hyperlinks (a href) from it.
Returns a JSON array of objects with "text" and "url" fields.
Useful for discovering pages to crawl, finding navigation structure, or building sitemaps.`,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url":    {"type": "string", "description": "The URL to extract links from"},
				"filter": {"type": "string", "description": "Optional substring filter: only return links whose URL contains this string"}
			},
			"required": ["url"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"network"},
	}

	if err := k.ToolRegistry().Register(tool.NewRawTool(fetchSpec, fetchURLHandler)); err != nil {
		return err
	}
	return k.ToolRegistry().Register(tool.NewRawTool(linksSpec, extractLinksHandler))
}

// HTTP client with timeout
var httpClient = &http.Client{Timeout: 30 * time.Second}

func fetchURLHandler(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var params struct {
		URL       string `json:"url"`
		MaxLength int    `json:"max_length"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return nil, fmt.Errorf("invalid URL: must start with http:// or https://")
	}

	if params.MaxLength <= 0 {
		params.MaxLength = 50000
	}

	body, err := doFetch(ctx, params.URL)
	if err != nil {
		return nil, err
	}

	text := htmlToText(body)
	if len(text) > params.MaxLength {
		text = text[:params.MaxLength] + "\n\n[content truncated]"
	}

	return json.Marshal(map[string]any{
		"url":    params.URL,
		"length": len(text),
		"text":   text,
	})
}

func extractLinksHandler(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var params struct {
		URL    string `json:"url"`
		Filter string `json:"filter"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return nil, fmt.Errorf("invalid URL: must start with http:// or https://")
	}

	body, err := doFetch(ctx, params.URL)
	if err != nil {
		return nil, err
	}

	links := extractHrefLinks(body, params.URL)
	if params.Filter != "" {
		var filtered []linkEntry
		for _, l := range links {
			if strings.Contains(l.URL, params.Filter) {
				filtered = append(filtered, l)
			}
		}
		links = filtered
	}

	return json.Marshal(map[string]any{
		"url":   params.URL,
		"count": len(links),
		"links": links,
	})
}

type linkEntry struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// doFetch 执行 HTTP GET 请求。
func doFetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "mossclaw/1.0 (moss personal assistant)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// 限制读取大小（10MB）
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(data), nil
}

// ─── HTML Processing (lightweight, no external deps) ─

// htmlToText 将 HTML 转为纯文本（轻量实现，无外部依赖）。
func htmlToText(html string) string {
	// 移除 script 和 style 块
	reScript := regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</\1>`)
	text := reScript.ReplaceAllString(html, "")

	// 将 br 和块级标签转换为换行
	reBlock := regexp.MustCompile(`(?i)<(br|p|div|h[1-6]|li|tr|blockquote|hr)[^>]*/?>`)
	text = reBlock.ReplaceAllString(text, "\n")
	reCloseBlock := regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|tr|blockquote|table|ul|ol)>`)
	text = reCloseBlock.ReplaceAllString(text, "\n")

	// 移除所有剩余 HTML 标签
	reTag := regexp.MustCompile(`<[^>]+>`)
	text = reTag.ReplaceAllString(text, "")

	// 解码常见 HTML 实体
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// 压缩连续空白行
	reBlank := regexp.MustCompile(`\n{3,}`)
	text = reBlank.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// extractHrefLinks 从 HTML 中提取所有 <a href="..."> 链接。
func extractHrefLinks(html, baseURL string) []linkEntry {
	reLink := regexp.MustCompile(`(?i)<a\s[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	matches := reLink.FindAllStringSubmatch(html, -1)

	var links []linkEntry
	seen := make(map[string]bool)

	for _, m := range matches {
		href := strings.TrimSpace(m[1])
		text := strings.TrimSpace(stripTags(m[2]))

		// 跳过锚点和 javascript
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
			continue
		}

		// 相对 URL → 绝对 URL（简易处理）
		if strings.HasPrefix(href, "/") {
			// 提取 base domain
			parts := strings.SplitN(baseURL, "/", 4)
			if len(parts) >= 3 {
				href = parts[0] + "//" + parts[2] + href
			}
		} else if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			// 相对路径
			if idx := strings.LastIndex(baseURL, "/"); idx > 8 {
				href = baseURL[:idx+1] + href
			}
		}

		if seen[href] {
			continue
		}
		seen[href] = true

		links = append(links, linkEntry{
			Text: truncate(text, 100),
			URL:  href,
		})
	}

	return links
}

var reAllTags = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string {
	return reAllTags.ReplaceAllString(s, "")
}

// ─── System Prompt ──────────────────────────────────

func buildSystemPrompt(workspace, trust string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	prompt := appconfig.RenderSystemPromptForTrust(workspace, trust, defaultSystemPromptTemplate, ctx)
	return prompt
}

// ─── Helpers ────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
