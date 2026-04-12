package loop

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/mossagents/moss/kernel/model"
)

func repairToolArguments(args json.RawMessage) json.RawMessage {
	return model.RepairToolCallArguments(args)
}

func previewToolArguments(args json.RawMessage) string {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "{}" {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(trimmed)); err == nil {
		trimmed = compact.String()
	}
	if len(trimmed) > 160 {
		return trimmed[:160] + "..."
	}
	return trimmed
}
