package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/tool"
	"io"
	"net/http"
	"net/url"
	"time"
)

var WeatherSpec = tool.ToolSpec{
	Name:        "get_weather",
	Description: "查询指定城市的实时天气信息，返回温度、天气状况、湿度、风速等数据",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {"type": "string", "description": "城市名称，例如 Beijing、Shanghai、Tokyo"},
			"lang": {"type": "string", "description": "返回语言，例如 zh（中文）、en（英文），默认 zh"}
		},
		"required": ["city"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"weather"},
}

func WeatherHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			City string `json:"city"`
			Lang string `json:"lang"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if args.Lang == "" {
			args.Lang = "zh"
		}

		apiURL := fmt.Sprintf("https://wttr.in/%s?format=j1&lang=%s",
			url.PathEscape(args.City), url.QueryEscape(args.Lang))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", "moss-weather-tool")

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("weather request failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("weather API returned %d: %s", resp.StatusCode, string(body))
		}

		var wttr wttrResponse
		if err := json.Unmarshal(body, &wttr); err != nil {
			return nil, fmt.Errorf("parse weather data: %w", err)
		}

		return json.Marshal(wttr.toResult(args.City, args.Lang))
	}
}

// RegisterWeather 便捷注册方法。
func RegisterWeather(reg tool.Registry) {
	_ = reg.Register(tool.NewRawTool(WeatherSpec, WeatherHandler()))
}

// ── wttr.in JSON 响应结构 ────────────────────────────

type wttrResponse struct {
	CurrentCondition []wttrCurrent `json:"current_condition"`
	NearestArea      []wttrArea    `json:"nearest_area"`
}

type wttrCurrent struct {
	TempC         string        `json:"temp_C"`
	TempF         string        `json:"temp_F"`
	FeelsLikeC    string        `json:"FeelsLikeC"`
	Humidity      string        `json:"humidity"`
	WindspeedKmph string        `json:"windspeedKmph"`
	WindDir       string        `json:"winddir16Point"`
	WeatherDesc   []wttrLangVal `json:"weatherDesc"`
	LangZh        []wttrLangVal `json:"lang_zh"`
	Visibility    string        `json:"visibility"`
	Pressure      string        `json:"pressure"`
	UVIndex       string        `json:"uvIndex"`
	ObsTime       string        `json:"observation_time"`
}

type wttrArea struct {
	AreaName []wttrLangVal `json:"areaName"`
	Country  []wttrLangVal `json:"country"`
}

type wttrLangVal struct {
	Value string `json:"value"`
}

type weatherResult struct {
	City       string `json:"city"`
	Country    string `json:"country,omitempty"`
	TempC      string `json:"temp_c"`
	TempF      string `json:"temp_f"`
	FeelsLikeC string `json:"feels_like_c"`
	Humidity   string `json:"humidity"`
	Wind       string `json:"wind"`
	Condition  string `json:"condition"`
	Visibility string `json:"visibility_km"`
	Pressure   string `json:"pressure_hpa"`
	UVIndex    string `json:"uv_index"`
	ObsTime    string `json:"observation_time"`
}

func (w *wttrResponse) toResult(city, lang string) weatherResult {
	r := weatherResult{City: city}
	if len(w.CurrentCondition) > 0 {
		c := w.CurrentCondition[0]
		r.TempC = c.TempC
		r.TempF = c.TempF
		r.FeelsLikeC = c.FeelsLikeC
		r.Humidity = c.Humidity + "%"
		r.Wind = c.WindspeedKmph + " km/h " + c.WindDir
		r.Visibility = c.Visibility
		r.Pressure = c.Pressure
		r.UVIndex = c.UVIndex
		r.ObsTime = c.ObsTime

		if lang == "zh" {
			r.Condition = firstLangValue(c.LangZh)
		}
		if r.Condition == "" {
			r.Condition = firstLangValue(c.WeatherDesc)
		}
	}
	if len(w.NearestArea) > 0 {
		a := w.NearestArea[0]
		r.Country = firstLangValue(a.Country)
		if r.City == "" {
			r.City = firstLangValue(a.AreaName)
		}
	}
	return r
}

func firstLangValue(values []wttrLangVal) string {
	if len(values) == 0 {
		return ""
	}
	return values[0].Value
}
