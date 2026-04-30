package loop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

type ToolRouteStatus string

const (
	ToolRouteVisible          ToolRouteStatus = "visible"
	ToolRouteHidden           ToolRouteStatus = "hidden"
	ToolRouteApprovalRequired ToolRouteStatus = "approval-required"
)

type ToolRouteDecision struct {
	Name               string                  `json:"name"`
	Source             string                  `json:"source,omitempty"`
	Owner              string                  `json:"owner,omitempty"`
	Risk               tool.RiskLevel          `json:"risk,omitempty"`
	Status             ToolRouteStatus         `json:"status"`
	Capabilities       []string                `json:"capabilities,omitempty"`
	Effects            []tool.Effect           `json:"effects,omitempty"`
	SideEffectClass    tool.SideEffectClass    `json:"side_effect_class,omitempty"`
	ApprovalClass      tool.ApprovalClass      `json:"approval_class,omitempty"`
	PlannerVisibility  tool.PlannerVisibility  `json:"planner_visibility,omitempty"`
	CommutativityClass tool.CommutativityClass `json:"commutativity_class,omitempty"`
	Idempotent         bool                    `json:"idempotent,omitempty"`
	ResourceScope      []string                `json:"resource_scope,omitempty"`
	LockScope          []string                `json:"lock_scope,omitempty"`
	ReasonCodes        []string                `json:"reason_codes,omitempty"`
}

type ModelRoutePlan struct {
	Lane         string                 `json:"lane,omitempty"`
	Requirements *model.TaskRequirement `json:"requirements,omitempty"`
	ReasonCodes  []string               `json:"reason_codes,omitempty"`
}

type TurnPlan struct {
	RunID              string              `json:"run_id,omitempty"`
	TurnID             string              `json:"turn_id,omitempty"`
	Iteration          int                 `json:"iteration,omitempty"`
	InstructionProfile string              `json:"instruction_profile,omitempty"`
	PromptVersion      string              `json:"prompt_version,omitempty"`
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
		PromptVersion:      promptVersionForSession(sess),
		LightweightChat:    session.LatestUserTurnIsLightweightChat(session.PromptMessages(sess)),
	}
	plan.ToolRoute = buildToolRoute(sess, reg, plan)
	plan.ModelRoute = buildModelRoute(sess, plan)
	return plan
}

func promptVersionForSession(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	if v, ok := sess.GetMetadata(session.MetadataPromptVersion); ok {
		if version, ok := v.(string); ok {
			return strings.TrimSpace(version)
		}
	}
	return ""
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
	if v, ok := sess.GetMetadata(session.MetadataInstructionProfile); ok {
		if profile, ok := v.(string); ok && strings.TrimSpace(profile) != "" {
			return strings.TrimSpace(profile)
		}
	}
	_, preset, _, collaborationMode, promptPack, permissionProfile, _, _ := session.SessionFacetValues(sess)
	if strings.TrimSpace(collaborationMode) != "" {
		return strings.TrimSpace(collaborationMode)
	}
	if strings.TrimSpace(preset) != "" {
		return strings.TrimSpace(preset)
	}
	if strings.TrimSpace(promptPack) != "" {
		return strings.TrimSpace(promptPack)
	}
	if strings.TrimSpace(permissionProfile) != "" {
		return strings.TrimSpace(permissionProfile)
	}
	return "default"
}

func buildToolRoute(sess *session.Session, reg tool.Registry, plan TurnPlan) []ToolRouteDecision {
	if reg == nil {
		return nil
	}
	summary, hasSummary := session.ToolPolicySummaryFromSession(sess)
	intentMode := sessionIntentMode(sess)
	specs := reg.List()
	decisions := make([]ToolRouteDecision, 0, len(specs))
	for _, t := range specs {
		spec := t.Spec()
		effects := spec.EffectiveEffects()
		decision := ToolRouteDecision{
			Name:               spec.Name,
			Source:             classifyToolSource(spec),
			Owner:              classifyToolOwner(spec),
			Risk:               spec.Risk,
			Status:             ToolRouteVisible,
			Capabilities:       append([]string(nil), spec.Capabilities...),
			Effects:            append([]tool.Effect(nil), effects...),
			SideEffectClass:    spec.EffectiveSideEffectClass(),
			ApprovalClass:      spec.EffectiveApprovalClass(),
			PlannerVisibility:  spec.EffectivePlannerVisibility(),
			CommutativityClass: spec.EffectiveCommutativityClass(),
			Idempotent:         spec.Idempotent,
			ResourceScope:      append([]string(nil), spec.ResourceScope...),
			LockScope:          append([]string(nil), spec.LockScope...),
		}
		switch {
		case decision.PlannerVisibility == tool.PlannerVisibilityHidden:
			decision.Status = ToolRouteHidden
			decision.ReasonCodes = append(decision.ReasonCodes, "planner_hidden")
		case plan.LightweightChat:
			decision.Status = ToolRouteHidden
			decision.ReasonCodes = append(decision.ReasonCodes, "lightweight_chat")
		case shouldHideForIntentMode(intentMode, spec):
			decision.Status = ToolRouteHidden
			decision.ReasonCodes = append(decision.ReasonCodes, intentMode+"_mode")
		default:
			decision.Status, decision.ReasonCodes = routeStatusFromPolicySummary(spec, summary, hasSummary)
		}
		if decision.Status == ToolRouteVisible {
			decision.ReasonCodes = append(decision.ReasonCodes, "visible")
		}
		decision.ReasonCodes = compactStrings(decision.ReasonCodes)
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
		req = &model.TaskRequirement{}
	}
	intentMode := sessionIntentMode(sess)
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
		addModelCapability(req, model.CapFunctionCalling)
		reasons = append(reasons, "tool_route")
		if len(visibleTools)+len(approvalTools) >= 4 {
			lane = "tool-heavy"
		}
	}
	switch intentMode {
	case "plan", "planning", "investigate", "research":
		lane = "reasoning"
		addModelCapability(req, model.CapReasoning)
		reasons = append(reasons, intentMode+"_mode")
	case "coding":
		addModelCapability(req, model.CapCodeGeneration)
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

func cloneTaskRequirement(in *model.TaskRequirement) *model.TaskRequirement {
	if in == nil {
		return nil
	}
	cp := *in
	cp.Capabilities = append([]model.ModelCapability(nil), in.Capabilities...)
	return &cp
}

func addModelCapability(req *model.TaskRequirement, cap model.ModelCapability) {
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
			"name":                decision.Name,
			"status":              string(decision.Status),
			"source":              decision.Source,
			"owner":               decision.Owner,
			"risk":                string(decision.Risk),
			"reason_codes":        append([]string(nil), decision.ReasonCodes...),
			"effects":             effectsToStrings(decision.Effects),
			"side_effect_class":   string(decision.SideEffectClass),
			"approval_class":      string(decision.ApprovalClass),
			"planner_visibility":  string(decision.PlannerVisibility),
			"commutativity_class": string(decision.CommutativityClass),
			"idempotent":          decision.Idempotent,
			"resource_scope":      append([]string(nil), decision.ResourceScope...),
			"lock_scope":          append([]string(nil), decision.LockScope...),
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
	if classifyToolSource(spec) == "agent" {
		return false
	}
	for _, effect := range spec.EffectiveEffects() {
		switch effect {
		case tool.EffectWritesWorkspace, tool.EffectWritesMemory, tool.EffectExternalSideEffect:
			return true
		}
	}
	return false
}

func shouldHideForIntentMode(mode string, spec tool.ToolSpec) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "plan", "planning", "investigate", "research":
		return shouldHideForPlanning(spec)
	default:
		return false
	}
}

func routeStatusFromPolicySummary(spec tool.ToolSpec, summary session.ToolPolicySummary, hasSummary bool) (ToolRouteStatus, []string) {
	if !hasSummary {
		return safeRouteStatus(spec)
	}
	hiddenReasons := []string{}
	approvalReasons := []string{}
	if approvalClassInSummary(spec.EffectiveApprovalClass(), summary.DeniedClasses) {
		hiddenReasons = append(hiddenReasons, "tool.approval_class_denied")
	}
	if approvalClassInSummary(spec.EffectiveApprovalClass(), summary.ApprovalRequiredClasses) {
		approvalReasons = append(approvalReasons, "tool.approval_class_requires_approval")
	}
	if appliesCommandAccess(spec) {
		applySummaryAccess(summary.CommandAccess, "command.default_requires_approval", "command.default_denied", &approvalReasons, &hiddenReasons)
	}
	if appliesHTTPAccess(spec) {
		applySummaryAccess(summary.HTTPAccess, "http.default_requires_approval", "http.default_denied", &approvalReasons, &hiddenReasons)
	}
	for _, effect := range spec.EffectiveEffects() {
		access := summaryAccessForEffect(summary, effect)
		if access == "" {
			continue
		}
		applySummaryAccess(access, "tool.effect_requires_approval", "tool.effect_denied", &approvalReasons, &hiddenReasons)
	}
	switch {
	case len(hiddenReasons) > 0:
		return ToolRouteHidden, compactStrings(hiddenReasons)
	case len(approvalReasons) > 0:
		return ToolRouteApprovalRequired, compactStrings(approvalReasons)
	default:
		return ToolRouteVisible, nil
	}
}

func safeRouteStatus(spec tool.ToolSpec) (ToolRouteStatus, []string) {
	reasons := []string{"policy_summary_missing"}
	if spec.EffectiveApprovalClass() == tool.ApprovalClassSupervisorOnly {
		return ToolRouteHidden, append(reasons, "tool.approval_class_denied")
	}
	if spec.IsReadOnly() && spec.EffectiveApprovalClass() == tool.ApprovalClassNone && spec.EffectiveSideEffectClass() == tool.SideEffectNone && spec.Risk == tool.RiskLow {
		return ToolRouteVisible, reasons
	}
	return ToolRouteApprovalRequired, append(reasons, "safe_default_requires_approval")
}

func applySummaryAccess(access, approvalReason, denyReason string, approvalReasons, hiddenReasons *[]string) {
	switch normalizeSummaryAccess(access) {
	case "deny":
		*hiddenReasons = append(*hiddenReasons, denyReason)
	case "require-approval":
		*approvalReasons = append(*approvalReasons, approvalReason)
	}
}

func summaryAccessForEffect(summary session.ToolPolicySummary, effect tool.Effect) string {
	switch effect {
	case tool.EffectWritesWorkspace:
		return summary.WorkspaceWriteAccess
	case tool.EffectWritesMemory:
		return summary.MemoryWriteAccess
	case tool.EffectGraphMutation:
		return summary.GraphMutationAccess
	default:
		return ""
	}
}

func approvalClassInSummary(class tool.ApprovalClass, classes []string) bool {
	value := strings.TrimSpace(string(class))
	if value == "" {
		return false
	}
	for _, candidate := range classes {
		if strings.EqualFold(strings.TrimSpace(candidate), value) {
			return true
		}
	}
	return false
}

func appliesCommandAccess(spec tool.ToolSpec) bool {
	if spec.Name == "run_command" {
		return true
	}
	return toolHasCapability(spec, "execution")
}

func appliesHTTPAccess(spec tool.ToolSpec) bool {
	if spec.Name == "http_request" {
		return true
	}
	return toolHasCapability(spec, "network")
}

// toolSpecNeedsStrictSchemaValidation reports tools that must have parseable
// InputSchema before execution (command/network/side-effect surfaces).
func toolSpecNeedsStrictSchemaValidation(spec tool.ToolSpec) bool {
	if appliesCommandAccess(spec) || appliesHTTPAccess(spec) {
		return true
	}
	if toolHasCapability(spec, "execution") || toolHasCapability(spec, "network") {
		return true
	}
	for _, effect := range spec.EffectiveEffects() {
		switch effect {
		case tool.EffectWritesWorkspace, tool.EffectWritesMemory, tool.EffectGraphMutation, tool.EffectExternalSideEffect:
			return true
		}
	}
	return spec.Risk == tool.RiskHigh
}

func toolHasCapability(spec tool.ToolSpec, want string) bool {
	for _, capability := range spec.Capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), want) {
			return true
		}
	}
	return false
}

func normalizeSummaryAccess(access string) string {
	switch strings.ToLower(strings.TrimSpace(access)) {
	case "allow":
		return "allow"
	case "deny":
		return "deny"
	case "require-approval":
		return "require-approval"
	default:
		return ""
	}
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

func effectsToStrings(effects []tool.Effect) []string {
	out := make([]string, 0, len(effects))
	for _, effect := range effects {
		if effect == "" {
			continue
		}
		out = append(out, string(effect))
	}
	return out
}

func firstNonEmptySessionMode(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	runMode, _, _, _, _, _, _, _ := session.SessionFacetValues(sess)
	return strings.TrimSpace(runMode)
}

func sessionIntentMode(sess *session.Session) string {
	_, _, _, collaborationMode, _, _, _, _ := session.SessionFacetValues(sess)
	if trimmed := strings.ToLower(strings.TrimSpace(collaborationMode)); trimmed != "" {
		return trimmed
	}
	_, _, taskMode := session.ProfileMetadataValues(sess)
	return strings.ToLower(strings.TrimSpace(taskMode))
}
