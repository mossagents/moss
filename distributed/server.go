package distributed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	taskrt "github.com/mossagents/moss/kernel/task"
)

// TaskRuntimeServer wraps a TaskRuntime (and optionally JobRuntime +
// AtomicJobRuntime) as an HTTP API server, enabling multi-instance
// Agent Worker deployments to share a single task queue.
type TaskRuntimeServer struct {
	rt   taskrt.TaskRuntime
	mrt  taskrt.TaskMessageRuntime
	jrt  taskrt.JobRuntime
	ajrt taskrt.AtomicJobRuntime
	mux  *http.ServeMux
}

// NewTaskRuntimeServer creates a server wrapping the provided runtimes.
// rt must not be nil. jrt and ajrt are optional (endpoints return 501 if nil).
func NewTaskRuntimeServer(rt taskrt.TaskRuntime, jrt taskrt.JobRuntime, ajrt taskrt.AtomicJobRuntime) *TaskRuntimeServer {
	mrt, _ := rt.(taskrt.TaskMessageRuntime)
	s := &TaskRuntimeServer{rt: rt, mrt: mrt, jrt: jrt, ajrt: ajrt}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	return s
}

// Handler returns the underlying http.Handler (for embedding in existing servers).
func (s *TaskRuntimeServer) Handler() http.Handler { return s.mux }

// Serve starts the HTTP server on addr (blocking).
func (s *TaskRuntimeServer) Serve(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

func (s *TaskRuntimeServer) registerRoutes() {
	// Tasks
	s.mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			s.handleUpsertTask(w, r)
		case http.MethodGet:
			s.handleListTasks(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	s.mux.HandleFunc("/tasks/claim", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleClaimNextReady(w, r)
	})
	s.mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		s.handleTasksSubpath(w, r)
	})
	// Jobs
	s.mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			s.handleUpsertJob(w, r)
		case http.MethodGet:
			s.handleListJobs(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	s.mux.HandleFunc("/jobs/", func(w http.ResponseWriter, r *http.Request) {
		s.handleJobsSubpath(w, r)
	})
}

func (s *TaskRuntimeServer) handleTasksSubpath(w http.ResponseWriter, r *http.Request) {
	// /tasks/{taskID}
	// /tasks/{taskID}/messages
	// /tasks/{taskID}/messages/consume
	path := strings.TrimPrefix(r.URL.Path, "/tasks/")
	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleGetTask(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "messages" {
		s.handleTaskMessages(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "messages" && parts[2] == "consume" {
		s.handleConsumeTaskMessages(w, r, parts[0])
		return
	}
	http.NotFound(w, r)
}

// ---- Task handlers -------------------------------------------------------

func (s *TaskRuntimeServer) handleUpsertTask(w http.ResponseWriter, r *http.Request) {
	var task taskrt.TaskRecord
	if !decode(w, r, &task) {
		return
	}
	if err := s.rt.UpsertTask(r.Context(), task); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *TaskRuntimeServer) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/tasks/")
	task, err := s.rt.GetTask(r.Context(), id)
	if err == taskrt.ErrTaskNotFound {
		writeError(w, err, http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, task)
}

func (s *TaskRuntimeServer) handleListTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := taskrt.TaskQuery{
		AgentName: q.Get("agent"),
		Status:    taskrt.TaskStatus(q.Get("status")),
		ClaimedBy: q.Get("claimed_by"),
		SessionID: q.Get("session_id"),
		Limit:     parseIntParam(q.Get("limit")),
	}
	tasks, err := s.rt.ListTasks(r.Context(), query)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, tasks)
}

func (s *TaskRuntimeServer) handleClaimNextReady(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Claimer        string `json:"claimer"`
		PreferredAgent string `json:"preferred_agent"`
	}
	if !decode(w, r, &body) {
		return
	}
	task, err := s.rt.ClaimNextReady(r.Context(), body.Claimer, body.PreferredAgent)
	if err == taskrt.ErrNoReadyTask {
		writeError(w, err, http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, task)
}

func (s *TaskRuntimeServer) handleTaskMessages(w http.ResponseWriter, r *http.Request, taskID string) {
	if s.mrt == nil {
		http.Error(w, "task message runtime not available", http.StatusNotImplemented)
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		writeError(w, fmt.Errorf("task id is required"), http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Content string `json:"content"`
		}
		if !decode(w, r, &body) {
			return
		}
		msg, err := s.mrt.EnqueueTaskMessage(r.Context(), taskrt.TaskMessage{TaskID: taskID, Content: body.Content})
		if err == taskrt.ErrTaskNotFound {
			writeError(w, err, http.StatusNotFound)
			return
		}
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, msg)
	case http.MethodGet:
		limit := parseIntParam(r.URL.Query().Get("limit"))
		messages, err := s.mrt.ListTaskMessages(r.Context(), taskID, limit)
		if err == taskrt.ErrTaskNotFound {
			writeError(w, err, http.StatusNotFound)
			return
		}
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, messages)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *TaskRuntimeServer) handleConsumeTaskMessages(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.mrt == nil {
		http.Error(w, "task message runtime not available", http.StatusNotImplemented)
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		writeError(w, fmt.Errorf("task id is required"), http.StatusBadRequest)
		return
	}
	var body struct {
		Limit int `json:"limit"`
	}
	if !decode(w, r, &body) {
		return
	}
	messages, err := s.mrt.ConsumeTaskMessages(r.Context(), taskID, body.Limit)
	if err == taskrt.ErrTaskNotFound {
		writeError(w, err, http.StatusNotFound)
		return
	}
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, messages)
}

// ---- Job handlers --------------------------------------------------------

func (s *TaskRuntimeServer) handleUpsertJob(w http.ResponseWriter, r *http.Request) {
	if s.jrt == nil {
		http.Error(w, "job runtime not available", http.StatusNotImplemented)
		return
	}
	var job taskrt.AgentJob
	if !decode(w, r, &job) {
		return
	}
	if err := s.jrt.UpsertJob(r.Context(), job); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *TaskRuntimeServer) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if s.jrt == nil {
		http.Error(w, "job runtime not available", http.StatusNotImplemented)
		return
	}
	q := r.URL.Query()
	query := taskrt.JobQuery{
		AgentName: q.Get("agent"),
		Status:    taskrt.AgentJobStatus(q.Get("status")),
		Limit:     parseIntParam(q.Get("limit")),
	}
	jobs, err := s.jrt.ListJobs(r.Context(), query)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, jobs)
}

func (s *TaskRuntimeServer) handleJobsSubpath(w http.ResponseWriter, r *http.Request) {
	// /jobs/{jobID}
	// /jobs/{jobID}/items
	// /jobs/{jobID}/items/{itemID}/running
	// /jobs/{jobID}/items/{itemID}/result
	path := strings.TrimPrefix(r.URL.Path, "/jobs/")
	parts := strings.SplitN(path, "/", 4)

	if len(parts) == 1 {
		// /jobs/{jobID}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.jrt == nil {
			http.Error(w, "job runtime not available", http.StatusNotImplemented)
			return
		}
		job, err := s.jrt.GetJob(r.Context(), parts[0])
		if err == taskrt.ErrJobNotFound {
			writeError(w, err, http.StatusNotFound)
			return
		}
		if err != nil {
			writeError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, job)
		return
	}

	if len(parts) >= 2 && parts[1] == "items" {
		jobID := parts[0]
		switch len(parts) {
		case 2:
			// /jobs/{jobID}/items
			switch r.Method {
			case http.MethodPost:
				s.handleUpsertJobItem(w, r, jobID)
			case http.MethodGet:
				s.handleListJobItems(w, r, jobID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		case 4:
			itemID := parts[2]
			switch parts[3] {
			case "running":
				if r.Method == http.MethodPost {
					s.handleMarkJobItemRunning(w, r, jobID, itemID)
				} else {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				}
			case "result":
				if r.Method == http.MethodPost {
					s.handleReportJobItemResult(w, r, jobID, itemID)
				} else {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				}
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
		return
	}
	http.NotFound(w, r)
}

func (s *TaskRuntimeServer) handleUpsertJobItem(w http.ResponseWriter, r *http.Request, jobID string) {
	if s.jrt == nil {
		http.Error(w, "job runtime not available", http.StatusNotImplemented)
		return
	}
	var item taskrt.AgentJobItem
	if !decode(w, r, &item) {
		return
	}
	item.JobID = jobID
	if err := s.jrt.UpsertJobItem(r.Context(), item); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *TaskRuntimeServer) handleListJobItems(w http.ResponseWriter, r *http.Request, jobID string) {
	if s.jrt == nil {
		http.Error(w, "job runtime not available", http.StatusNotImplemented)
		return
	}
	q := r.URL.Query()
	query := taskrt.JobItemQuery{
		JobID:  jobID,
		Status: taskrt.AgentJobStatus(q.Get("status")),
		Limit:  parseIntParam(q.Get("limit")),
	}
	items, err := s.jrt.ListJobItems(r.Context(), query)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func (s *TaskRuntimeServer) handleMarkJobItemRunning(w http.ResponseWriter, r *http.Request, jobID, itemID string) {
	if s.ajrt == nil {
		http.Error(w, "atomic job runtime not available", http.StatusNotImplemented)
		return
	}
	var body struct {
		Executor string `json:"executor"`
	}
	if !decode(w, r, &body) {
		return
	}
	item, err := s.ajrt.MarkJobItemRunning(r.Context(), jobID, itemID, body.Executor)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, item)
}

func (s *TaskRuntimeServer) handleReportJobItemResult(w http.ResponseWriter, r *http.Request, jobID, itemID string) {
	if s.ajrt == nil {
		http.Error(w, "atomic job runtime not available", http.StatusNotImplemented)
		return
	}
	var body struct {
		Executor string                `json:"executor"`
		Status   taskrt.AgentJobStatus `json:"status"`
		Result   string                `json:"result"`
		Error    string                `json:"error"`
	}
	if !decode(w, r, &body) {
		return
	}
	item, err := s.ajrt.ReportJobItemResult(r.Context(), jobID, itemID, body.Executor, body.Status, body.Result, body.Error)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, item)
}

// ---- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encoding error", http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, fmt.Errorf("invalid request body: %w", err), http.StatusBadRequest)
		return false
	}
	return true
}

func parseIntParam(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
