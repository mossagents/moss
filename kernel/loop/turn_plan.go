package loop

import (
	"fmt"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"slices"
	"strings"
)

type ToolRouteStatus string

const (
	ToolRouteVisible          ToolRouteStatus = "visible"
	ToolRouteHidden           ToolRouteStatus = "hidden"
	ToolRouteApprovalRequired ToolRouteStatus = "approval-required"
)

type ToolRouteDecision struct {
	Name         string          `json:"name"`
	Source       string          `json:"source,omitempty"`
	Owner        string          `json:"owner,omitempty"`
	Risk         tool.RiskLevel  `json:"risk,omitempty"`
	Status       ToolRouteStatus `json:"status"`
	Capabilities []string        `json:"capabilities,omitempty"`
	ReasonCodes  []string        `json:"reason_codes,omitempty"`
}

type ModelRoutePlan struct {
	Lane         string               `json:"lane,omitempty"`
	Requirements *mdl.TaskRequirement `json:"requirements,omitempty"`
	ReasonCodes  []string             `json:"reason_codes,omitempty"`
}

type TurnPlan struct {
	RunID              string              `json:"run_id,omitempty"`
	TurnID             string              `json:"turn_id,omitempty"`
	Iteration          int                 `json:"iteration,omitempty"`
	InstructionProfile string              `json:"instruction_profile,omitempty"`
	LightweightChat    bool                `json:"lightweight_chat,omitempty"`
	ToolRoute          []ToolRouteDecision `json:"tool_route,omitempty"`
	ModelRoute         ModelRoutePlan      `json:"model_route,omitempty"`
}

func buildTurnPlan(sess *session.Session, runID string, iteration int, reg tool.Registry) TurnPlan {
	plan := TurnPlan{
		RunID:              strings.TrimSpace(runID),
		TurnID:             buildTurnID(runID, sess, iteration),
		Iteration:          iteration,
		InstructionProfile: instructionProfileForSession(sess),
		LightweightChat:    session.LatestUserTurnIsLightweightChat(session.PromptMessages(sess)),
	}
	plan.ToolRoute = buildToolRoute(sess, reg, plan)
	plan.ModelRoute = buildModelRoute(sess, plan)
	return plan
}

func buildTurnID(runID string, sess *session.Session, iteration int) string {
	prefix := strings.TrimSpace(runID)
	if prefix == "" && sess != nil {
		prefix = strings.TrimSpace(sess.ID)
	}
	if prefix == "" {
		prefix = "turn"
	}
	return fmt.Sprintf("%s-turn-%03d", prefix, iteration)
}

func instructionProfileForSession(sess *session.Session) string {
	if sess == nil {
		return "default"
	}
	if sess.Config.Metadata != nil {
		if raw, ok := sess.Config.Metadata[session.MetadataInstructionProfile]; ok {
			if profile, ok := raw.(string); ok && strings.TrimSpace(profile) != "" {
				return strings.TrimSpace(profile)
			}
		}
	}
	_, _, _, taskMode := session.ProfileMetadataValues(sess)
	if strings.TrimSpace(taskMode) != "" {
		return taskMode
	}
	if raw, ok := sess.Config.Metadata[session.MetadataTaskMode]; ok {
		if taskMode, ok := raw.(string); ok && strings.TrimSpace(taskMode) != "" {
			return strings.TrimSpace(taskMode)
		}
	}
	if profile := strings.TrimSpace(sess.Config.Profile); profile != "" {
		return profile
	}
	return "default"
}

func buildToolRoute(sess *session.Session, reg tool.Registry, plan TurnPlan) []ToolRouteDecision {
	if reg == nil {
		return nil
	}
	_, trust, approval, taskMode := session.ProfileMetadataValues(sess)
	trust = normalizeTurnTrust(trust, sess)
	approval = normalizeTurnApproval(approval)
	taskMode = strings.ToLower(strings.TrimSpace(taskMode))
	specs := reg.List()
	decisions := make([]ToolRouteDecision, 0, len(specs))
	for _, spec := range specs {
		decision := ToolRouteDecision{
			Name:         spec.Name,
			Source:       classifyToolSource(spec),
			Owner:        classifyToolOwner(spec),
			Risk:         spec.Risk,
			Status:       ToolRouteVisible,
			Capabilities: append([]string(nil), spec.Capabilities...),
		}
		switch {
		case plan.LightweightChat:
			decision.Status = ToolRouteHidden
			decision.ReasonCodes = append(decision.ReasonCodes, "lightweight_chat")
		case taskMode == "readonly" && spec.Risk != tool.RiskLow:
			decision.Status = ToolRouteHidden
			decision.ReasonCodes = append(decision.ReasonCodes, "readonly_mode")
		case (taskMode == "planning" || taskMode == "research") && shouldHideForPlanning(spec):
			decision.Status = ToolRouteHidden
			decision.ReasonCodes = append(decision.ReasonCodes, "planning_mode")
		case trust == "restricted" && shouldRequireApprovalForRestricted(spec):
			decision.Status = ToolRouteApprovalRequired
			decision.ReasonCodes = append(decision.ReasonCodes, "restricted_trust")
		case approval != "full-auto" && spec.Risk == tool.RiskHigh:
			decision.Status = ToolRouteApprovalRequired
			decision.ReasonCodes = append(decision.ReasonCodes, "high_risk_requires_approval")
		}
		if decision.Status == ToolRouteVisible {
			decision.ReasonCodes = append(decision.ReasonCodes, "visible")
		}
		decisions = append(decisions, decision)
	}
	slices.SortFunc(decisions, func(a, b ToolRouteDecision) int {
		return strings.Compare(a.Name, b.Name)
	})
	return decisions
}

func buildModelRoute(sess *session.Session, plan TurnPlan) ModelRoutePlan {
	req := cloneTaskRequirement(nil)
	if sess != nil && sess.Config.ModelConfig.Requirements != nil {
		req = cloneTaskRequirement(sess.Config.ModelConfig.Requirements)
	}
	if req == nil {
		req = &mdl.TaskRequirement{}
	}
	_, _, _, taskMode := session.ProfileMetadataValues(sess)
	taskMode = strings.ToLower(strings.TrimSpace(taskMode))
	lane := "default"
	reasons := []string{}
	visibleTools := visibleToolNames(plan.ToolRoute)
	approvalTools := approvalRequiredToolNames(plan.ToolRoute)
	switch {
	case plan.LightweightChat:
		lane = "cheap"
		req.PreferCheap = true
		if req.MaxCostTier == 0 || req.MaxCostTier > 1 {
			req.MaxCostTier = 1
		}
		reasons = append(reasons, "lightweight_chat")
	case len(visibleTools)+len(approvalTools) > 0:
		addModelCapability(req, mdl.CapFunctionCalling)
		reasons = append(reasons, "tool_route")
		if len(visibleTools)+len(approvalTools) >= 4 {
			lane = "tool-heavy"
		}
	}
	switch taskMode {
	case "planning", "research":
		lane = "reasoning"
		addModelCapability(req, mdl.CapReasoning)
		reasons = append(reasons, taskMode+"_mode")
	case "coding":
		addModelCapability(req, mdl.CapCodeGeneration)
		reasons = append(reasons, "coding_mode")
	}
	if strings.EqualFold(strings.TrimSpace(firstNonEmptySessionMode(sess)), "background") && lane == "default" {
		lane = "background-task"
		req.PreferCheap = true
		reasons = append(reasons, "background_mode")
	}
	req.Lane = lane
	return ModelRoutePlan{
		Lane:         lane,
		Requirements: req,
		ReasonCodes:  compactStrings(reasons),
	}
}

func cloneTaskRequirement(in *mdl.TaskRequirement) *mdl.TaskRequirement {
	if in == nil {
		return nil
	}
	cp := *in
	cp.Capabilities = append([]mdl.ModelCapability(nil), in.Capabilities...)
	return &cp
}

func addModelCapability(req *mdl.TaskRequirement, cap mdl.ModelCapability) {
	if req == nil {
		return
	}
	for _, existing := range req.Capabilities {
		if existing == cap {
			return
		}
	}
	req.Capabilities = append(req.Capabilities, cap)
}

func visibleToolNames(route []ToolRouteDecision) []string {
	names := make([]string, 0, len(route))
	for _, decision := range route {
		if decision.Status == ToolRouteVisible {
			names = append(names, decision.Name)
		}
	}
	return names
}

func approvalRequiredToolNames(route []ToolRouteDecision) []string {
	names := make([]string, 0, len(route))
	for _, decision := range route {
		if decision.Status == ToolRouteApprovalRequired {
			names = append(names, decision.Name)
		}
	}
	return names
}

func hiddenToolNames(route []ToolRouteDecision) []string {
	names := make([]string, 0, len(route))
	for _, decision := range route {
		if decision.Status == ToolRouteHidden {
			names = append(names, decision.Name)
		}
	}
	return names
}

func allowedToolNames(route []ToolRouteDecision) []string {
	names := make([]string, 0, len(route))
	for _, decision := range route {
		if decision.Status != ToolRouteHidden {
			names = append(names, decision.Name)
		}
	}
	return names
}

func toolRouteDigest(route []ToolRouteDecision) string {
	parts := make([]string, 0, len(route))
	for _, decision := range route {
		parts = append(parts, fmt.Sprintf("%s:%s", decision.Name, decision.Status))
	}
	return strings.Join(parts, ",")
}

func toolRoutePayload(route []ToolRouteDecision) []map[string]any {
	payload := make([]map[string]any, 0, len(route))
	for _, decision := range route {
		item := map[string]any{
			"name":         decision.Name,
			"status":       string(decision.Status),
			"source":       decision.Source,
			"owner":        decision.Owner,
			"risk":         string(decision.Risk),
			"reason_codes": append([]string(nil), decision.ReasonCodes...),
		}
		if len(decision.Capabilities) > 0 {
			item["capabilities"] = append([]string(nil), decision.Capabilities...)
		}
		payload = append(payload, item)
	}
	return payload
}

func classifyToolSource(spec tool.ToolSpec) string {
	if source := strings.TrimSpace(spec.Source); source != "" {
		return source
	}
	for _, cap := range spec.Capabilities {
		switch strings.TrimSpace(cap) {
		case "mcp":
			return "mcp"
		case "delegation":
			return "agent"
		}
	}
	return "runtime"
}

func classifyToolOwner(spec tool.ToolSpec) string {
	if owner := strings.TrimSpace(spec.Owner); owner != "" {
		return owner
	}
	source := classifyToolSource(spec)
	if source == "mcp" && len(spec.Capabilities) > 1 {
		return strings.TrimSpace(spec.Capabilities[1])
	}
	return source
}

func shouldHideForPlanning(spec tool.ToolSpec) bool {
	if spec.Risk != tool.RiskHigh {
		return false
	}
	if classifyToolSource(spec) == "agent" {
		return false
	}
	for _, cap := range spec.Capabilities {
		switch strings.TrimSpace(cap) {
		case "filesystem", "workspace":
			return true
		}
	}
	switch strings.TrimSpace(spec.Name) {
	case "write_file", "edit_file", "run_command":
		return true
	default:
		return false
	}
}

func shouldRequireApprovalForRestricted(spec tool.ToolSpec) bool {
	if spec.Risk == tool.RiskHigh {
		return true
	}
	return classifyToolSource(spec) == "agent" && spec.Risk != tool.RiskLow
}

func normalizeTurnApproval(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "confirm"
	}
	return mode
}

func normalizeTurnTrust(trust string, sess *session.Session) string {
	trust = strings.ToLower(strings.TrimSpace(trust))
	if trust != "" {
		return trust
	}
	if sess != nil {
		if value := strings.ToLower(strings.TrimSpace(sess.Config.TrustLevel)); value != "" {
			return value
		}
	}
	return "trusted"
}

func compactStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

func firstNonEmptySessionMode(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	return strings.TrimSpace(sess.Config.Mode)
}
