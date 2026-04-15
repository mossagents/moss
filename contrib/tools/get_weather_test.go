package tools

import (
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestToResult_UsesZhConditionWhenAvailable(t *testing.T) {
	w := wttrResponse{
		CurrentCondition: []wttrCurrent{{
			TempC:       "20",
			LangZh:      []wttrLangVal{{Value: "晴"}},
			WeatherDesc: []wttrLangVal{{Value: "Sunny"}},
		}},
		NearestArea: []wttrArea{{Country: []wttrLangVal{{Value: "China"}}}},
	}

	got := w.toResult("Beijing", "zh")
	if got.Condition != "晴" {
		t.Fatalf("condition: want %q got %q", "晴", got.Condition)
	}
	if got.Country != "China" {
		t.Fatalf("country: want %q got %q", "China", got.Country)
	}
}

func TestToResult_FallsBackToWeatherDesc(t *testing.T) {
	w := wttrResponse{
		CurrentCondition: []wttrCurrent{{
			WeatherDesc: []wttrLangVal{{Value: "Cloudy"}},
		}},
	}

	got := w.toResult("Shanghai", "zh")
	if got.Condition != "Cloudy" {
		t.Fatalf("condition fallback: want %q got %q", "Cloudy", got.Condition)
	}
}

func TestToResult_FillsCityFromNearestAreaWhenInputEmpty(t *testing.T) {
	w := wttrResponse{
		NearestArea: []wttrArea{{AreaName: []wttrLangVal{{Value: "Tokyo"}}}},
	}

	got := w.toResult("", "en")
	if got.City != "Tokyo" {
		t.Fatalf("city fallback: want %q got %q", "Tokyo", got.City)
	}
}

func TestRegisterWeather_RegistersSpecAndHandler(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterWeather(reg)

	got, ok := reg.Get(WeatherSpec.Name)
	if !ok {
		t.Fatalf("tool %q not registered", WeatherSpec.Name)
	}
	if got.Spec().Name != WeatherSpec.Name {
		t.Fatalf("spec name: want %q got %q", WeatherSpec.Name, got.Spec().Name)
	}
}
