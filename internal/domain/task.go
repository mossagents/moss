package domain

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

type Task struct {
	TaskID        string
	RunID         string
	AssignedAgent string
	Goal          string
	Constraints   []string
	Status        TaskStatus
	Result        *TaskResult
}

type TaskResult struct {
	Summary   string
	Artifacts []string
	Success   bool
	Error     string
}
