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
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/mossagi/moss/adapters/claude"
	adaptersopenai "github.com/mossagi/moss/adapters/openai"
	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/kernel/tool"
)

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\nBye!")
		cancel()
		os.Exit(0)
	}()

	if err := run(ctx, *provider, *model, *workspace, *trust, *apiKey, *baseURL); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, provider, model, workspace, trust, apiKey, baseURL string) error {
	llm, err := buildLLM(provider, model, apiKey, baseURL)
	if err != nil {
		return err
	}

	sb, err := sandbox.NewLocal(workspace)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}

	userIO := &consoleIO{writer: os.Stdout, reader: os.Stdin}

	k := kernel.New(
		kernel.WithLLM(llm),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(userIO),
	)

	if err := k.SetupWithDefaults(ctx, workspace, kernel.WithWarningWriter(os.Stderr)); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// 注册爬虫专用工具
	if err := registerCrawlTools(k); err != nil {
		return fmt.Errorf("register crawl tools: %w", err)
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

	return repl(ctx, k, sess)
}

// ─── REPL ───────────────────────────────────────────

func repl(ctx context.Context, k *kernel.Kernel, sess *session.Session) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("🕷 > ")
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			fmt.Println()
			return nil
		}
		if err != nil {
			return err
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			done := handleCommand(input, sess)
			if done {
				return nil
			}
			continue
		}

		sess.AppendMessage(port.Message{Role: port.RoleUser, Content: input})

		result, err := k.Run(ctx, sess)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n\n", err)
			continue
		}
		_ = result
		fmt.Println()
	}
}

func handleCommand(input string, sess *session.Session) bool {
	cmd := strings.ToLower(strings.Fields(input)[0])

	switch cmd {
	case "/exit", "/quit":
		fmt.Println("Bye!")
		return true
	case "/clear":
		var systemMsgs []port.Message
		for _, m := range sess.Messages {
			if m.Role == port.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			}
		}
		sess.Messages = systemMsgs
		sess.Budget.UsedSteps = 0
		sess.Budget.UsedTokens = 0
		fmt.Println("✓ Conversation cleared.")
	case "/compact":
		var systemMsgs, dialogMsgs []port.Message
		for _, m := range sess.Messages {
			if m.Role == port.RoleSystem {
				systemMsgs = append(systemMsgs, m)
			} else {
				dialogMsgs = append(dialogMsgs, m)
			}
		}
		keep := 6
		if len(dialogMsgs) > keep {
			dialogMsgs = dialogMsgs[len(dialogMsgs)-keep:]
		}
		sess.Messages = append(systemMsgs, dialogMsgs...)
		fmt.Printf("✓ Compacted to %d messages.\n", len(sess.Messages))
	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /help     Show this help")
		fmt.Println("  /clear    Clear conversation history")
		fmt.Println("  /compact  Keep only recent messages")
		fmt.Println("  /exit     Exit miniclaw")
	default:
		fmt.Printf("Unknown command: %s (type /help)\n", cmd)
	}
	return false
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
	return fmt.Sprintf(`You are miniclaw, an intelligent web crawling agent.

## Environment
- OS: %s
- Workspace: %s (for saving crawled data)

## Crawl Tools
- **fetch_url**: Fetch a web page and get its text content (HTML stripped). Supports up to 50000 chars.
- **extract_links**: Fetch a page and extract all hyperlinks with their text and URLs. Supports filtering.

## File Tools
- **read_file**: Read saved files
- **write_file**: Save crawled content to files
- **list_files**: List files in workspace
- **search_text**: Search within saved files

## Workflow
1. **Understand the goal**: What does the user want to extract or crawl?
2. **Explore**: Use extract_links to discover page structure, then fetch_url for specific pages.
3. **Extract**: Parse the relevant information from fetched content.
4. **Save**: Write results to files using write_file (e.g., Markdown, JSON, CSV).
5. **Report**: Summarize what was found and where it was saved.

## Rules
- Always fetch a page before trying to extract information from it.
- For sites with many pages, use extract_links first to discover the structure, then selectively fetch.
- Save important results to files — don't just display them.
- Be respectful: don't fetch too many pages at once.
- Clearly explain what you're doing at each step.
- If a URL fails, report the error and suggest alternatives.
- Use Markdown formatting in responses.
`, osName, workspace)
}

// ─── LLM Construction ───────────────────────────────

func buildLLM(provider, model, apiKey, baseURL string) (port.LLM, error) {
	switch strings.ToLower(provider) {
	case "claude", "anthropic":
		var opts []claude.Option
		if model != "" {
			opts = append(opts, claude.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return claude.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return claude.New("", opts...), nil

	case "openai":
		var opts []adaptersopenai.Option
		if model != "" {
			opts = append(opts, adaptersopenai.WithModel(model))
		}
		if baseURL != "" || apiKey != "" {
			return adaptersopenai.NewWithBaseURL(apiKey, baseURL, opts...), nil
		}
		return adaptersopenai.New("", opts...), nil

	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: claude, openai)", provider)
	}
}

// ─── Console UserIO ─────────────────────────────────

type consoleIO struct {
	writer io.Writer
	reader *os.File
}

func (c *consoleIO) Send(_ context.Context, msg port.OutputMessage) error {
	switch msg.Type {
	case port.OutputText:
		fmt.Fprintln(c.writer, msg.Content)
	case port.OutputStream:
		fmt.Fprint(c.writer, msg.Content)
	case port.OutputStreamEnd:
		fmt.Fprintln(c.writer)
	case port.OutputToolStart:
		fmt.Fprintf(c.writer, "🔧 %s\n", msg.Content)
	case port.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(c.writer, "❌ %s\n", msg.Content)
		} else {
			content := msg.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Fprintf(c.writer, "✅ %s\n", content)
		}
	}
	return nil
}

func (c *consoleIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	reader := bufio.NewReader(c.reader)

	switch req.Type {
	case port.InputConfirm:
		fmt.Fprintf(c.writer, "%s [y/N]: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		answer := strings.TrimSpace(strings.ToLower(line))
		return port.InputResponse{Approved: answer == "y" || answer == "yes"}, nil

	case port.InputSelect:
		for i, opt := range req.Options {
			fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt)
		}
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		var sel int
		fmt.Fscan(c.reader, &sel)
		return port.InputResponse{Selected: sel - 1}, nil

	default:
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		return port.InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}

// ─── Helpers ────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
