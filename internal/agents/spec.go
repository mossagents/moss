package agents

type Role string

const (
	RoleManager    Role = "manager"
	RoleResearcher Role = "researcher"
	RoleCoder      Role = "coder"
	RoleReviewer   Role = "reviewer"
)

type AgentSpec struct {
	Name                string
	Role                Role
	Instructions        string
	AllowedCapabilities []string
	AllowedTools        []string
	Limits              *AgentLimits
}

type AgentLimits struct {
	MaxSteps  int
	MaxTokens int
}
