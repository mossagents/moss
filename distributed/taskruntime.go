// Package distributed provides a distributed TaskRuntime implementation over HTTP.
// It allows multiple Agent Worker instances to share a single task queue
// by communicating with a TaskRuntimeServer via REST API.
package distributed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	taskrt "github.com/mossagents/moss/kernel/task"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RemoteTaskRuntime implements taskrt.TaskRuntime, taskrt.JobRuntime,
// and taskrt.AtomicJobRuntime over HTTP.
type RemoteTaskRuntime struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// RemoteOption configures a RemoteTaskRuntime.
type RemoteOption func(*RemoteTaskRuntime)

// WithToken sets a Bearer token for HTTP authentication.
func WithToken(token string) RemoteOption {
	return func(r *RemoteTaskRuntime) { r.token = token }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) RemoteOption {
	return func(r *RemoteTaskRuntime) { r.httpClient = c }
}

// NewRemoteTaskRuntime creates a RemoteTaskRuntime pointing at baseURL.
func NewRemoteTaskRuntime(baseURL string, opts ...RemoteOption) *RemoteTaskRuntime {
	r := &RemoteTaskRuntime{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ---- TaskRuntime ---------------------------------------------------------

func (r *RemoteTaskRuntime) UpsertTask(ctx context.Context, task taskrt.TaskRecord) error {
	return r.post(ctx, "/tasks", task, nil)
}

func (r *RemoteTaskRuntime) GetTask(ctx context.Context, id string) (*taskrt.TaskRecord, error) {
	var task taskrt.TaskRecord
	if err := r.get(ctx, "/tasks/"+url.PathEscape(id), nil, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *RemoteTaskRuntime) ListTasks(ctx context.Context, query taskrt.TaskQuery) ([]taskrt.TaskRecord, error) {
	params := url.Values{}
	if query.AgentName != "" {
		params.Set("agent", query.AgentName)
	}
	if query.Status != "" {
		params.Set("status", string(query.Status))
	}
	if query.ClaimedBy != "" {
		params.Set("claimed_by", query.ClaimedBy)
	}
	if query.SessionID != "" {
		params.Set("session_id", query.SessionID)
	}
	if query.Limit > 0 {
		params.Set("limit", strconv.Itoa(query.Limit))
	}
	var tasks []taskrt.TaskRecord
	if err := r.get(ctx, "/tasks", params, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *RemoteTaskRuntime) ClaimNextReady(ctx context.Context, claimer string, preferredAgent string) (*taskrt.TaskRecord, error) {
	body := map[string]string{"claimer": claimer, "preferred_agent": preferredAgent}
	var task taskrt.TaskRecord
	if err := r.post(ctx, "/tasks/claim", body, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ---- JobRuntime ----------------------------------------------------------

func (r *RemoteTaskRuntime) UpsertJob(ctx context.Context, job taskrt.AgentJob) error {
	return r.post(ctx, "/jobs", job, nil)
}

func (r *RemoteTaskRuntime) GetJob(ctx context.Context, id string) (*taskrt.AgentJob, error) {
	var job taskrt.AgentJob
	if err := r.get(ctx, "/jobs/"+url.PathEscape(id), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *RemoteTaskRuntime) ListJobs(ctx context.Context, query taskrt.JobQuery) ([]taskrt.AgentJob, error) {
	params := url.Values{}
	if query.AgentName != "" {
		params.Set("agent", query.AgentName)
	}
	if query.Status != "" {
		params.Set("status", string(query.Status))
	}
	if query.Limit > 0 {
		params.Set("limit", strconv.Itoa(query.Limit))
	}
	var jobs []taskrt.AgentJob
	if err := r.get(ctx, "/jobs", params, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *RemoteTaskRuntime) UpsertJobItem(ctx context.Context, item taskrt.AgentJobItem) error {
	path := fmt.Sprintf("/jobs/%s/items", url.PathEscape(item.JobID))
	return r.post(ctx, path, item, nil)
}

func (r *RemoteTaskRuntime) ListJobItems(ctx context.Context, query taskrt.JobItemQuery) ([]taskrt.AgentJobItem, error) {
	params := url.Values{}
	if query.Status != "" {
		params.Set("status", string(query.Status))
	}
	if query.Limit > 0 {
		params.Set("limit", strconv.Itoa(query.Limit))
	}
	var items []taskrt.AgentJobItem
	path := fmt.Sprintf("/jobs/%s/items", url.PathEscape(query.JobID))
	if err := r.get(ctx, path, params, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// ---- AtomicJobRuntime ----------------------------------------------------

func (r *RemoteTaskRuntime) MarkJobItemRunning(ctx context.Context, jobID, itemID, executor string) (*taskrt.AgentJobItem, error) {
	path := fmt.Sprintf("/jobs/%s/items/%s/running", url.PathEscape(jobID), url.PathEscape(itemID))
	body := map[string]string{"executor": executor}
	var item taskrt.AgentJobItem
	if err := r.post(ctx, path, body, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *RemoteTaskRuntime) ReportJobItemResult(ctx context.Context, jobID, itemID, executor string, status taskrt.AgentJobStatus, result string, errMsg string) (*taskrt.AgentJobItem, error) {
	path := fmt.Sprintf("/jobs/%s/items/%s/result", url.PathEscape(jobID), url.PathEscape(itemID))
	body := map[string]string{
		"executor": executor,
		"status":   string(status),
		"result":   result,
		"error":    errMsg,
	}
	var item taskrt.AgentJobItem
	if err := r.post(ctx, path, body, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// ---- HTTP helpers --------------------------------------------------------

type apiError struct {
	Error string `json:"error"`
}

func (r *RemoteTaskRuntime) get(ctx context.Context, path string, params url.Values, out any) error {
	u := r.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return r.do(req, out)
}

func (r *RemoteTaskRuntime) post(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return r.do(req, out)
}

func (r *RemoteTaskRuntime) do(req *http.Request, out any) error {
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("distributed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return taskrt.ErrTaskNotFound
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode >= 400 {
		var apiErr apiError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error != "" {
			return fmt.Errorf("distributed: server error: %s", apiErr.Error)
		}
		return fmt.Errorf("distributed: status %d", resp.StatusCode)
	}
	if out != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// compile-time interface checks
var _ taskrt.TaskRuntime = (*RemoteTaskRuntime)(nil)
var _ taskrt.JobRuntime = (*RemoteTaskRuntime)(nil)
var _ taskrt.AtomicJobRuntime = (*RemoteTaskRuntime)(nil)
