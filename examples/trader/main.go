// trader 是一个模拟交易策略 Agent 示例。
//
// 演示如何用 moss kernel 构建有状态的 Agent 应用：
//   - 自定义工具：get_market_data（行情）、place_order（下单）、get_portfolio（持仓）、get_trade_history（历史）
//   - 内存模拟市场：随机价格波动，无需真实 API
//   - 事件系统：OnEvent 监听交易事件，自动记录日志
//   - Session.State：维护持仓和资金状态
//
// 用法:
//
//	go run . --provider openai --model gpt-4o --capital 100000
//	go run . --provider openai --model Qwen/Qwen3-8B --base-url http://localhost:8080/v1
package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
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

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

func main() {
	skill.SetAppName("trader")
	_ = skill.EnsureMossDir()

	provider := flag.String("provider", "openai", "LLM provider: claude|openai")
	model := flag.String("model", "", "Model name")
	workspace := flag.String("workspace", ".", "Workspace directory")
	apiKey := flag.String("api-key", "", "API key")
	baseURL := flag.String("base-url", "", "API base URL")
	capital := flag.Float64("capital", 100000, "Starting capital")
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

	if err := run(ctx, *provider, *model, *workspace, *apiKey, *baseURL, *capital); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// ─── Market Simulation ──────────────────────────────

// market 是一个内存模拟市场。
type market struct {
	mu        sync.Mutex
	prices    map[string]float64   // 当前价格
	history   map[string][]float64 // 价格历史
	portfolio map[string]float64   // 持仓数量
	cash      float64              // 可用资金
	trades    []tradeRecord        // 交易记录
}

type tradeRecord struct {
	Time   string  `json:"time"`
	Symbol string  `json:"symbol"`
	Side   string  `json:"side"` // buy/sell
	Qty    float64 `json:"qty"`
	Price  float64 `json:"price"`
	Total  float64 `json:"total"`
}

func newMarket(capital float64) *market {
	// 初始化模拟股票
	prices := map[string]float64{
		"AAPL":  185.50,
		"GOOGL": 175.20,
		"MSFT":  420.80,
		"AMZN":  185.60,
		"TSLA":  245.30,
		"NVDA":  880.50,
		"META":  505.75,
		"BTC":   67500.00,
		"ETH":   3450.00,
		"SPY":   520.40,
	}

	history := make(map[string][]float64)
	for sym, p := range prices {
		// 生成 20 天历史数据
		h := make([]float64, 20)
		h[19] = p
		for i := 18; i >= 0; i-- {
			change := 1.0 + (rand.Float64()-0.5)*0.04 // ±2% 日波动
			h[i] = h[i+1] / change
		}
		history[sym] = h
	}

	return &market{
		prices:    prices,
		history:   history,
		portfolio: make(map[string]float64),
		cash:      capital,
	}
}

// tick 模拟一次市场价格变动。
func (m *market) tick() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sym, p := range m.prices {
		change := 1.0 + (rand.Float64()-0.5)*0.03 // ±1.5% 变动
		newPrice := math.Round(p*change*100) / 100
		m.prices[sym] = newPrice
		m.history[sym] = append(m.history[sym], newPrice)
		if len(m.history[sym]) > 100 {
			m.history[sym] = m.history[sym][1:]
		}
	}
}

// ─── Main Run ───────────────────────────────────────

func run(ctx context.Context, provider, model, workspace, apiKey, baseURL string, capital float64) error {
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

	// 注册内置工具（file access 等，不需要 run_command）
	if err := k.SetupWithDefaults(ctx, workspace,
		kernel.WithWarningWriter(os.Stderr),
	); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// 创建模拟市场
	mkt := newMarket(capital)

	// 注册交易工具
	if err := registerTradeTools(k, mkt); err != nil {
		return fmt.Errorf("register trade tools: %w", err)
	}

	// 事件监听：记录交易操作
	k.OnEvent("tool.completed", func(e builtins.Event) {
		if data, ok := e.Data.(map[string]any); ok {
			if name, _ := data["tool"].(string); name == "place_order" {
				fmt.Fprintf(os.Stdout, "  📊 [event] Trade executed at %s\n", e.Timestamp.Format("15:04:05"))
			}
		}
	})

	// place_order 需要用户确认
	k.WithPolicy(
		builtins.RequireApprovalFor("place_order"),
		builtins.DefaultAllow(),
	)

	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         "trading assistant",
		Mode:         "interactive",
		TrustLevel:   "restricted",
		SystemPrompt: buildSystemPrompt(workspace, capital),
	})
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}

	// 启动市场模拟（每 5 秒 tick 一次）
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mkt.tick()
			}
		}
	}()

	modelName := model
	if modelName == "" {
		modelName = "(default)"
	}
	fmt.Println("╭──────────────────────────────────────╮")
	fmt.Println("│         trader — Trading Agent         │")
	fmt.Println("╰──────────────────────────────────────╯")
	fmt.Printf("  Provider:  %s\n", provider)
	fmt.Printf("  Model:     %s\n", modelName)
	fmt.Printf("  Capital:   $%.2f\n", capital)
	fmt.Printf("  Symbols:   %d available\n", len(mkt.prices))
	fmt.Printf("  Tools:     %d loaded\n", len(k.ToolRegistry().List()))
	fmt.Println()
	fmt.Println("  Market is live! Prices update every 5 seconds.")
	fmt.Println("  Type /help for commands, /exit to quit.")
	fmt.Println()

	return repl(ctx, k, sess)
}

// ─── REPL ───────────────────────────────────────────

func repl(ctx context.Context, k *kernel.Kernel, sess *session.Session) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("💰 > ")
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
		keep := 8
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
		fmt.Println("  /exit     Exit trader")
	default:
		fmt.Printf("Unknown command: %s (type /help)\n", cmd)
	}
	return false
}

// ─── Trade Tools ────────────────────────────────────

func registerTradeTools(k *kernel.Kernel, mkt *market) error {
	tools := []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{marketDataSpec, marketDataHandler(mkt)},
		{placeOrderSpec, placeOrderHandler(mkt)},
		{portfolioSpec, portfolioHandler(mkt)},
		{tradeHistorySpec, tradeHistoryHandler(mkt)},
	}

	for _, t := range tools {
		if err := k.ToolRegistry().Register(t.spec, t.handler); err != nil {
			return err
		}
	}
	return nil
}

// ── get_market_data ──

var marketDataSpec = tool.ToolSpec{
	Name: "get_market_data",
	Description: `Get current market data for one or more symbols.
Returns current price, daily change, and recent price history.
Available symbols: AAPL, GOOGL, MSFT, AMZN, TSLA, NVDA, META, BTC, ETH, SPY.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"symbols": {
				"type": "array",
				"items": {"type": "string"},
				"description": "List of ticker symbols (e.g., [\"AAPL\", \"GOOGL\"]). Use [\"*\"] for all."
			}
		},
		"required": ["symbols"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"market"},
}

type symbolData struct {
	Symbol  string    `json:"symbol"`
	Price   float64   `json:"price"`
	Change  float64   `json:"change_pct"`
	High    float64   `json:"high_20d"`
	Low     float64   `json:"low_20d"`
	History []float64 `json:"recent_5d"`
}

func marketDataHandler(mkt *market) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Symbols []string `json:"symbols"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		mkt.mu.Lock()
		defer mkt.mu.Unlock()

		// 展开 "*"
		if len(params.Symbols) == 1 && params.Symbols[0] == "*" {
			params.Symbols = nil
			for sym := range mkt.prices {
				params.Symbols = append(params.Symbols, sym)
			}
		}

		var result []symbolData
		for _, sym := range params.Symbols {
			sym = strings.ToUpper(sym)
			price, ok := mkt.prices[sym]
			if !ok {
				continue
			}

			hist := mkt.history[sym]
			var change float64
			if len(hist) >= 2 {
				prev := hist[len(hist)-2]
				if prev > 0 {
					change = math.Round((price-prev)/prev*10000) / 100
				}
			}

			high, low := price, price
			for _, p := range hist {
				if p > high {
					high = p
				}
				if p < low {
					low = p
				}
			}

			var recent []float64
			if len(hist) > 5 {
				recent = hist[len(hist)-5:]
			} else {
				recent = hist
			}

			result = append(result, symbolData{
				Symbol:  sym,
				Price:   price,
				Change:  change,
				High:    math.Round(high*100) / 100,
				Low:     math.Round(low*100) / 100,
				History: recent,
			})
		}

		return json.Marshal(result)
	}
}

// ── place_order ──

var placeOrderSpec = tool.ToolSpec{
	Name: "place_order",
	Description: `Place a buy or sell order for a given symbol.
Orders execute at the current market price immediately (market order).
Returns the executed trade details including total cost/proceeds.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"symbol": {"type": "string", "description": "Ticker symbol (e.g., AAPL)"},
			"side":   {"type": "string", "enum": ["buy", "sell"], "description": "Order side"},
			"qty":    {"type": "number", "description": "Quantity to buy/sell (supports fractional shares)"}
		},
		"required": ["symbol", "side", "qty"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"trading"},
}

func placeOrderHandler(mkt *market) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Symbol string  `json:"symbol"`
			Side   string  `json:"side"`
			Qty    float64 `json:"qty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		params.Symbol = strings.ToUpper(params.Symbol)
		if params.Qty <= 0 {
			return nil, fmt.Errorf("quantity must be positive")
		}

		mkt.mu.Lock()
		defer mkt.mu.Unlock()

		price, ok := mkt.prices[params.Symbol]
		if !ok {
			return nil, fmt.Errorf("unknown symbol: %s", params.Symbol)
		}

		total := math.Round(price*params.Qty*100) / 100

		switch params.Side {
		case "buy":
			if total > mkt.cash {
				return nil, fmt.Errorf("insufficient funds: need $%.2f, have $%.2f", total, mkt.cash)
			}
			mkt.cash -= total
			mkt.portfolio[params.Symbol] += params.Qty

		case "sell":
			held := mkt.portfolio[params.Symbol]
			if params.Qty > held {
				return nil, fmt.Errorf("insufficient shares: want %.4f, hold %.4f", params.Qty, held)
			}
			mkt.cash += total
			mkt.portfolio[params.Symbol] -= params.Qty
			if mkt.portfolio[params.Symbol] < 0.0001 {
				delete(mkt.portfolio, params.Symbol)
			}

		default:
			return nil, fmt.Errorf("invalid side: %s (must be buy or sell)", params.Side)
		}

		trade := tradeRecord{
			Time:   time.Now().Format("15:04:05"),
			Symbol: params.Symbol,
			Side:   params.Side,
			Qty:    params.Qty,
			Price:  price,
			Total:  total,
		}
		mkt.trades = append(mkt.trades, trade)

		return json.Marshal(map[string]any{
			"status":         "executed",
			"trade":          trade,
			"remaining_cash": math.Round(mkt.cash*100) / 100,
		})
	}
}

// ── get_portfolio ──

var portfolioSpec = tool.ToolSpec{
	Name: "get_portfolio",
	Description: `Get the current portfolio including cash balance, positions, and total value.
Shows each holding with current price, market value, and unrealized P&L.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"market"},
}

type positionInfo struct {
	Symbol      string  `json:"symbol"`
	Qty         float64 `json:"qty"`
	Price       float64 `json:"current_price"`
	MarketValue float64 `json:"market_value"`
}

func portfolioHandler(mkt *market) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		mkt.mu.Lock()
		defer mkt.mu.Unlock()

		var positions []positionInfo
		totalValue := mkt.cash

		for sym, qty := range mkt.portfolio {
			price := mkt.prices[sym]
			mv := math.Round(price*qty*100) / 100
			totalValue += mv
			positions = append(positions, positionInfo{
				Symbol:      sym,
				Qty:         qty,
				Price:       price,
				MarketValue: mv,
			})
		}

		return json.Marshal(map[string]any{
			"cash":        math.Round(mkt.cash*100) / 100,
			"positions":   positions,
			"total_value": math.Round(totalValue*100) / 100,
			"num_trades":  len(mkt.trades),
		})
	}
}

// ── get_trade_history ──

var tradeHistorySpec = tool.ToolSpec{
	Name: "get_trade_history",
	Description: `Get the history of all executed trades in this session.
Returns trades in chronological order with price, quantity, and total.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"symbol": {"type": "string", "description": "Filter by symbol (optional)"},
			"last_n": {"type": "integer", "description": "Return only the last N trades (default: all)"}
		}
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"market"},
}

func tradeHistoryHandler(mkt *market) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Symbol string `json:"symbol"`
			LastN  int    `json:"last_n"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		mkt.mu.Lock()
		defer mkt.mu.Unlock()

		trades := mkt.trades
		if params.Symbol != "" {
			sym := strings.ToUpper(params.Symbol)
			var filtered []tradeRecord
			for _, t := range trades {
				if t.Symbol == sym {
					filtered = append(filtered, t)
				}
			}
			trades = filtered
		}

		if params.LastN > 0 && len(trades) > params.LastN {
			trades = trades[len(trades)-params.LastN:]
		}

		return json.Marshal(map[string]any{
			"count":  len(trades),
			"trades": trades,
		})
	}
}

// ─── System Prompt ──────────────────────────────────

func buildSystemPrompt(workspace string, capital float64) string {
	osName := runtime.GOOS
	return skill.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, map[string]any{
		"OS":        osName,
		"Workspace": workspace,
		"Capital":   capital,
	})
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
			if len(content) > 300 {
				content = content[:300] + "..."
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

	default:
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		return port.InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}
