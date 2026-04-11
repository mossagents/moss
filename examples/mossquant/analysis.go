package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/tool"
	"math"
	"strings"
)

// ─── Technical Analysis ─────────────────────────────

func registerAnalysisTools(reg tool.Registry, mkt *market) error {
	return reg.Register(tool.NewRawTool(analyzeMarketSpec, analyzeMarketHandler(mkt)))
}

var analyzeMarketSpec = tool.ToolSpec{
	Name: "analyze_market",
	Description: `Run technical analysis on one or more symbols.
Computes indicators from price history: Moving Averages, RSI, MACD, and Bollinger Bands.
Use this to identify trends, overbought/oversold conditions, and volatility.

Indicators:
- MA: Simple Moving Average (5-period and 20-period)
- RSI: Relative Strength Index (14-period), >70 overbought, <30 oversold
- MACD: Moving Average Convergence Divergence (12,26,9)
- BB: Bollinger Bands (20-period, 2 std dev)

Available symbols: AAPL, GOOGL, MSFT, AMZN, TSLA, NVDA, META, BTC, ETH, SPY.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"symbols": {
				"type": "array",
				"items": {"type": "string"},
				"description": "List of ticker symbols. Use [\"*\"] for all."
			},
			"indicators": {
				"type": "array",
				"items": {"type": "string", "enum": ["ma", "rsi", "macd", "bb"]},
				"description": "Which indicators to compute (default: all). Options: ma, rsi, macd, bb."
			}
		},
		"required": ["symbols"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"analysis"},
}

type analysisResult struct {
	Symbol     string           `json:"symbol"`
	Price      float64          `json:"price"`
	DataPoints int              `json:"data_points"`
	MA         *maResult        `json:"ma,omitempty"`
	RSI        *rsiResult       `json:"rsi,omitempty"`
	MACD       *macdResult      `json:"macd,omitempty"`
	BB         *bollingerResult `json:"bollinger_bands,omitempty"`
	Signal     string           `json:"signal"`
}

type maResult struct {
	MA5      float64 `json:"ma5"`
	MA20     float64 `json:"ma20"`
	Trend    string  `json:"trend"`
	CrossMsg string  `json:"cross,omitempty"`
}

type rsiResult struct {
	Value  float64 `json:"value"`
	Status string  `json:"status"`
}

type macdResult struct {
	MACD      float64 `json:"macd"`
	Signal    float64 `json:"signal"`
	Histogram float64 `json:"histogram"`
	Trend     string  `json:"trend"`
}

type bollingerResult struct {
	Upper  float64 `json:"upper"`
	Middle float64 `json:"middle"`
	Lower  float64 `json:"lower"`
	Width  float64 `json:"width_pct"`
	Pos    string  `json:"position"`
}

func analyzeMarketHandler(mkt *market) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Symbols    []string `json:"symbols"`
			Indicators []string `json:"indicators"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		mkt.mu.Lock()
		defer mkt.mu.Unlock()

		// Resolve symbols
		if len(params.Symbols) == 1 && params.Symbols[0] == "*" {
			params.Symbols = nil
			for sym := range mkt.prices {
				params.Symbols = append(params.Symbols, sym)
			}
		}

		// Resolve indicators (default: all)
		wantAll := len(params.Indicators) == 0
		want := make(map[string]bool)
		for _, ind := range params.Indicators {
			want[strings.ToLower(ind)] = true
		}

		var results []analysisResult
		for _, sym := range params.Symbols {
			sym = strings.ToUpper(sym)
			price, ok := mkt.prices[sym]
			if !ok {
				continue
			}
			hist := mkt.history[sym]
			if len(hist) < 5 {
				continue
			}

			r := analysisResult{
				Symbol:     sym,
				Price:      price,
				DataPoints: len(hist),
			}

			var signals []string

			if wantAll || want["ma"] {
				ma := computeMA(hist, price)
				r.MA = ma
				if ma.Trend == "bullish" {
					signals = append(signals, "MA:bullish")
				} else {
					signals = append(signals, "MA:bearish")
				}
			}

			if wantAll || want["rsi"] {
				if len(hist) >= 15 {
					rsi := computeRSI(hist)
					r.RSI = rsi
					switch rsi.Status {
					case "overbought":
						signals = append(signals, "RSI:overbought")
					case "oversold":
						signals = append(signals, "RSI:oversold")
					default:
						signals = append(signals, "RSI:neutral")
					}
				}
			}

			if wantAll || want["macd"] {
				if len(hist) >= 26 {
					macd := computeMACD(hist)
					r.MACD = macd
					if macd.Histogram > 0 {
						signals = append(signals, "MACD:bullish")
					} else {
						signals = append(signals, "MACD:bearish")
					}
				}
			}

			if wantAll || want["bb"] {
				if len(hist) >= 20 {
					bb := computeBollinger(hist, price)
					r.BB = bb
					signals = append(signals, "BB:"+bb.Pos)
				}
			}

			r.Signal = summarizeSignals(signals)
			results = append(results, r)
		}

		return json.Marshal(results)
	}
}

// ─── Indicator Computations ─────────────────────────

func computeMA(hist []float64, currentPrice float64) *maResult {
	ma5 := sma(hist, 5)
	ma20 := sma(hist, 20)

	trend := "bearish"
	if ma5 > ma20 {
		trend = "bullish"
	}

	var cross string
	if len(hist) >= 21 {
		prevMA5 := sma(hist[:len(hist)-1], 5)
		prevMA20 := sma(hist[:len(hist)-1], 20)
		if prevMA5 <= prevMA20 && ma5 > ma20 {
			cross = "golden_cross"
		} else if prevMA5 >= prevMA20 && ma5 < ma20 {
			cross = "death_cross"
		}
	}

	return &maResult{
		MA5:      round2(ma5),
		MA20:     round2(ma20),
		Trend:    trend,
		CrossMsg: cross,
	}
}

func computeRSI(hist []float64) *rsiResult {
	period := 14
	if len(hist) < period+1 {
		return &rsiResult{Value: 50, Status: "insufficient_data"}
	}

	// Use the last period+1 prices to compute changes
	data := hist[len(hist)-period-1:]
	var gains, losses float64
	for i := 1; i < len(data); i++ {
		change := data[i] - data[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	var rsi float64
	if avgLoss == 0 {
		rsi = 100
	} else {
		rs := avgGain / avgLoss
		rsi = 100 - 100/(1+rs)
	}
	rsi = round2(rsi)

	status := "neutral"
	if rsi >= 70 {
		status = "overbought"
	} else if rsi <= 30 {
		status = "oversold"
	}

	return &rsiResult{Value: rsi, Status: status}
}

func computeMACD(hist []float64) *macdResult {
	ema12 := ema(hist, 12)
	ema26 := ema(hist, 26)
	macdLine := ema12 - ema26

	// Signal line: 9-period EMA of MACD values
	// Approximate by computing MACD over a sliding window
	if len(hist) < 35 {
		// Not enough data for proper signal line
		return &macdResult{
			MACD:      round4(macdLine),
			Signal:    0,
			Histogram: round4(macdLine),
			Trend:     trendFromValue(macdLine),
		}
	}

	var macdSeries []float64
	for i := 26; i <= len(hist); i++ {
		sub := hist[:i]
		e12 := ema(sub, 12)
		e26 := ema(sub, 26)
		macdSeries = append(macdSeries, e12-e26)
	}

	signalLine := emaSeries(macdSeries, 9)
	histogram := macdLine - signalLine

	return &macdResult{
		MACD:      round4(macdLine),
		Signal:    round4(signalLine),
		Histogram: round4(histogram),
		Trend:     trendFromValue(histogram),
	}
}

func computeBollinger(hist []float64, currentPrice float64) *bollingerResult {
	period := 20
	if len(hist) < period {
		period = len(hist)
	}
	data := hist[len(hist)-period:]

	middle := sma(data, period)
	variance := 0.0
	for _, p := range data {
		diff := p - middle
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	upper := middle + 2*stdDev
	lower := middle - 2*stdDev
	width := 0.0
	if middle > 0 {
		width = (upper - lower) / middle * 100
	}

	pos := "middle"
	if currentPrice >= upper {
		pos = "above_upper"
	} else if currentPrice <= lower {
		pos = "below_lower"
	} else if currentPrice > middle+(upper-middle)*0.5 {
		pos = "upper_half"
	} else if currentPrice < middle-(middle-lower)*0.5 {
		pos = "lower_half"
	}

	return &bollingerResult{
		Upper:  round2(upper),
		Middle: round2(middle),
		Lower:  round2(lower),
		Width:  round2(width),
		Pos:    pos,
	}
}

// ─── Helpers ────────────────────────────────────────

func sma(data []float64, period int) float64 {
	if len(data) < period {
		period = len(data)
	}
	if period == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range data[len(data)-period:] {
		sum += v
	}
	return sum / float64(period)
}

func ema(data []float64, period int) float64 {
	if len(data) == 0 {
		return 0
	}
	if len(data) < period {
		return sma(data, len(data))
	}

	multiplier := 2.0 / float64(period+1)
	result := sma(data[:period], period) // seed with SMA
	for i := period; i < len(data); i++ {
		result = (data[i]-result)*multiplier + result
	}
	return result
}

func emaSeries(data []float64, period int) float64 {
	return ema(data, period)
}

func trendFromValue(v float64) string {
	if v > 0 {
		return "bullish"
	}
	return "bearish"
}

func summarizeSignals(signals []string) string {
	bullish, bearish := 0, 0
	for _, s := range signals {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[1] {
		case "bullish", "oversold", "below_lower", "lower_half":
			bullish++
		case "bearish", "overbought", "above_upper", "upper_half":
			bearish++
		}
	}

	switch {
	case bullish > bearish+1:
		return "strong_buy"
	case bullish > bearish:
		return "buy"
	case bearish > bullish+1:
		return "strong_sell"
	case bearish > bullish:
		return "sell"
	default:
		return "neutral"
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
