package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/tool"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// ─── Market Simulation ──────────────────────────────

type market struct {
	mu        sync.Mutex
	prices    map[string]float64
	history   map[string][]float64
	portfolio map[string]float64
	cash      float64
	trades    []tradeRecord
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
		h := make([]float64, 20)
		h[19] = p
		for i := 18; i >= 0; i-- {
			change := 1.0 + (rand.Float64()-0.5)*0.04 // ±2% daily
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

func (m *market) tick() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sym, p := range m.prices {
		change := 1.0 + (rand.Float64()-0.5)*0.03 // ±1.5%
		newPrice := math.Round(p*change*100) / 100
		m.prices[sym] = newPrice
		m.history[sym] = append(m.history[sym], newPrice)
		if len(m.history[sym]) > 100 {
			m.history[sym] = m.history[sym][1:]
		}
	}
}

// ─── Trade Tools ────────────────────────────────────

func registerTradeTools(reg tool.Registry, mkt *market) error {
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
		if err := reg.Register(tool.NewRawTool(t.spec, t.handler)); err != nil {
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
