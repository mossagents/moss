// miniclaw 是一个智能 Web 爬虫 Agent 示例。
//
// 演示如何用 moss kernel 构建具有网络访问能力的 Agent：
//   - 自定义工具：fetch_url（抓取网页内容）、extract_links（提取链接）
//   - Agent 自主决定抓取策略：根据用户目标选择 URL、筛选内容、跟踪链接
//   - 结合内置工具将结果保存到本地文件
//   - 交互式 REPL 模式，支持多轮爬取
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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/mossagi/moss/adapters"
	"github.com/mossagi/moss/adapters/embedding"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/appkit"
	"github.com/mossagi/moss/kernel/knowledge"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/scheduler"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/kernel/tool"
	toolbuiltins "github.com/mossagi/moss/kernel/tool/builtins"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

func main() {
	skill.SetAppName("miniclaw")
	_ = skill.EnsureMossDir()

	provider := flag.String("provider", "openai", "LLM provider: claude|openai")
	model := flag.String("model", "", "Model name")
	workspace := flag.String("workspace", ".", "Workspace directory (for saving crawled data)")
	trust := flag.String("trust", "trusted", "Trust level: trusted|restricted")
	apiKey := flag.String("api-key", "", "API key (overrides env)")
	baseURL := flag.String("base-url", "", "API base URL")
	flag.Parse()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	if err := run(ctx, *provider, *model, *workspace, *trust, *apiKey, *baseURL); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, provider, model, workspace, trust, apiKey, baseURL string) error {
	llm, err := adapters.BuildLLM(provider, model, apiKey, baseURL)
	if err != nil {
		return err
	}

	sb, err := sandbox.NewLocal(workspace)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}

	// Session 持久化存储
	storeDir := filepath.Join(skill.MossDir(), "sessions")
	store, err := session.NewFileStore(storeDir)
	if err != nil {
		return fmt.Errorf("session store: %w", err)
	}

	// 定时调度器
	sched := scheduler.New()

	// 知识库：嵌入模型 + 内存向量存储
	embedder := embedding.NewWithBaseURL(apiKey, baseURL)
	knStore := knowledge.NewMemoryStore()

	userIO := port.NewConsoleIO()

	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(userIO),
		kernel.WithSessionStore(store),
		kernel.WithScheduler(sched),
		kernel.WithEmbedder(embedder),
	)

	if err := k.SetupWithDefaults(ctx, workspace, kernel.WithWarningWriter(os.Stderr)); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// 注册爬虫专用工具
	if err := registerCrawlTools(k); err != nil {
		return fmt.Errorf("register crawl tools: %w", err)
	}

	// 注册调度工具 (schedule_task, list_schedules, cancel_schedule)
	if err := toolbuiltins.RegisterScheduleTools(k.ToolRegistry(), sched); err != nil {
		return fmt.Errorf("register schedule tools: %w", err)
	}

	// 注册知识库工具 (ingest_document, knowledge_search, knowledge_list)
	if err := toolbuiltins.RegisterKnowledgeTools(k.ToolRegistry(), knStore, embedder); err != nil {
		return fmt.Errorf("register knowledge tools: %w", err)
	}

	if trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command", "fetch_url"),
			builtins.DefaultAllow(),
		)
	}

	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)

	// 启动调度器：当任务触发时创建新 Session 并执行
	sched.Start(ctx, func(jobCtx context.Context, job scheduler.Job) {
		fmt.Fprintf(os.Stdout, "\n⏰ Scheduled task [%s]: %s\n", job.ID, job.Goal)
		jobSess, err := k.NewSession(jobCtx, session.SessionConfig{
			Goal:         job.Goal,
			Mode:         "scheduled",
			TrustLevel:   trust,
			SystemPrompt: buildSystemPrompt(workspace),
			MaxSteps:     30,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ schedule session: %v\n", err)
			return
		}
		jobSess.AppendMessage(port.Message{Role: port.RoleUser, Content: job.Goal})
		result, err := k.Run(jobCtx, jobSess)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ❌ schedule run: %v\n", err)
			return
		}
		// 持久化完成的 session
		_ = store.Save(jobCtx, jobSess)
		fmt.Fprintf(os.Stdout, "  ✅ [%s] done (%d steps)\n\n", job.ID, result.Steps)
	})
	defer sched.Stop()

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "web crawling assistant",
		Mode:         "interactive",
		TrustLevel:   trust,
		SystemPrompt: buildSystemPrompt(workspace),
	})
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}

	modelName := model
	if modelName == "" {
		modelName = "(default)"
	}
	fmt.Println("╭──────────────────────────────────────╮")
	fmt.Println("│        miniclaw — Web Crawler         │")
	fmt.Println("╰──────────────────────────────────────╯")
	fmt.Printf("  Provider:  %s\n", provider)
	fmt.Printf("  Model:     %s\n", modelName)
	fmt.Printf("  Workspace: %s\n", workspace)
	fmt.Printf("  Tools:     %d loaded\n", len(k.ToolRegistry().List()))
	fmt.Println()
	fmt.Println("  Type a URL or describe what you want to crawl.")
	fmt.Println("  Type /help for commands, /exit to quit.")
	fmt.Println()

	return appkit.REPL(ctx, appkit.REPLConfig{
		Prompt:  "🕷 > ",
		AppName: "miniclaw",
	}, k, sess)
}

// ─── Crawl Tools ────────────────────────────────────

func registerCrawlTools(k *kernel.Kernel) error {
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

	if err := k.ToolRegistry().Register(fetchSpec, fetchURLHandler); err != nil {
		return err
	}
	return k.ToolRegistry().Register(linksSpec, extractLinksHandler)
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
	req.Header.Set("User-Agent", "miniclaw/1.0 (moss agent)")
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

func buildSystemPrompt(workspace string) string {
	osName := runtime.GOOS
	return skill.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, map[string]any{
		"OS":        osName,
		"Workspace": workspace,
	})
}

// ─── Helpers ────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
