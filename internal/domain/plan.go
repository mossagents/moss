package domain

type Plan struct {
	Summary string
	Steps   []PlanStep
}

type PlanStep struct {
	StepID        string
	Title         string
	AssignedAgent string
	Goal          string
	DependsOn     []string
	Status        TaskStatus
}

func (p *Plan) FirstStepID() string {
	if p == nil || len(p.Steps) == 0 {
		return ""
	}
	return p.Steps[0].StepID
}
