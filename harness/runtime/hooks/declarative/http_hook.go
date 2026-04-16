package declarative

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mossagents/moss/kernel/hooks"
)

// httpHook sends tool event information to a webhook URL and blocks the tool
// call if the response is non-2xx (when block_on_failure is true).
func httpHook(cfg HookConfig) hooks.Hook[hooks.ToolEvent] {
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodPost
	}

	return func(ctx context.Context, ev *hooks.ToolEvent) error {
		timeout := hookTimeout(cfg)
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		payload := map[string]any{
			"hook":      cfg.Name,
			"tool_name": ev.Tool.Name,
			"tool_risk": string(ev.Tool.Risk),
		}
		if ev.Session != nil {
			payload["session_id"] = ev.Session.ID
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return hookError(cfg.Name, fmt.Sprintf("marshal payload: %v", err), cfg.BlockOnFailure)
		}

		req, err := http.NewRequestWithContext(ctx, method, cfg.URL, bytes.NewReader(body))
		if err != nil {
			return hookError(cfg.Name, fmt.Sprintf("create request: %v", err), cfg.BlockOnFailure)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return hookError(cfg.Name, fmt.Sprintf("http error: %v", err), cfg.BlockOnFailure)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return hookError(cfg.Name, fmt.Sprintf("http status %d", resp.StatusCode), cfg.BlockOnFailure)
		}
		return nil
	}
}
