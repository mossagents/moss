package tools

import "context"

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type ToolInput map[string]any

type ToolOutput struct {
	Success bool
	Data    map[string]any
	Error   string
}

type Tool interface {
	Name() string
	Description() string
	Risk() RiskLevel
	Capabilities() []string
	Execute(ctx context.Context, input ToolInput) (ToolOutput, error)
}

type Catalog struct {
	tools map[string]Tool
}

func NewCatalog() *Catalog {
	return &Catalog{tools: make(map[string]Tool)}
}

func (c *Catalog) Register(t Tool) {
	c.tools[t.Name()] = t
}

func (c *Catalog) Get(name string) (Tool, bool) {
	t, ok := c.tools[name]
	return t, ok
}

func (c *Catalog) List() []Tool {
	result := make([]Tool, 0, len(c.tools))
	for _, t := range c.tools {
		result = append(result, t)
	}
	return result
}
