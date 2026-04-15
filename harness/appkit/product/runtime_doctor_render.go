package product

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/harness/internal/stringutil"
)

func RenderDoctorReport(report DoctorReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mosscode doctor\n")
	fmt.Fprintf(&b, "Workspace: %s\n", report.Workspace)
	fmt.Fprintf(&b, "Provider: %s | model=%s | trust=%s | approval=%s\n",
		report.Config.Name, stringutil.FirstNonEmpty(report.Config.Model, "(default)"), report.Config.Trust, report.Config.ApprovalMode)
	fmt.Fprintf(&b, "Config sources: flags=%s env=%s global=%t project=%t project_allowed=%t project_active=%t\n",
		renderList(report.Config.ExplicitFlags), renderList(report.Config.DetectedEnv), report.Config.GlobalExists, report.Config.ProjectExists, report.Config.ProjectAssetsAllowed, report.Config.ProjectConfigActive)
	fmt.Fprintf(&b, "Execution policy: command=%s http=%s cmd_network=%s enforcement=%s degraded=%t timeout=%ds allowed_roots=%d http_methods=%s redirects=%t hosts=%s\n",
		report.Execution.CommandAccess,
		report.Execution.HTTPAccess,
		report.Execution.CommandNetworkMode,
		report.Execution.CommandNetworkEnforcement,
		report.Execution.CommandNetworkDegraded,
		report.Execution.CommandTimeoutSeconds,
		report.Execution.CommandAllowedPaths,
		renderList(report.Execution.HTTPMethods),
		report.Execution.HTTPFollowRedirects,
		report.Execution.HTTPHostPolicy)
	if report.Execution.CommandRuleCount > 0 || report.Execution.HTTPRuleCount > 0 {
		fmt.Fprintf(&b, "Execution rules: command=%d http=%d\n", report.Execution.CommandRuleCount, report.Execution.HTTPRuleCount)
	}
	fmt.Fprintf(&b, "Model governance: retry=%t retries=%d initial=%s max=%s breaker=%t failures=%d reset=%s failover=%t available=%t candidates=%d per_candidate_retries=%d breaker_open_failover=%t router=%s",
		report.Governance.Model.RetryEnabled,
		report.Governance.Model.RetryMaxRetries,
		stringutil.FirstNonEmpty(report.Governance.Model.RetryInitialDelay, "-"),
		stringutil.FirstNonEmpty(report.Governance.Model.RetryMaxDelay, "-"),
		report.Governance.Model.BreakerEnabled,
		report.Governance.Model.BreakerMaxFailures,
		stringutil.FirstNonEmpty(report.Governance.Model.BreakerResetAfter, "-"),
		report.Governance.Model.FailoverEnabled,
		report.Governance.Model.FailoverAvailable,
		report.Governance.Model.FailoverMaxCandidates,
		report.Governance.Model.FailoverPerCandidateRetries,
		report.Governance.Model.FailoverOnBreakerOpen,
		stringutil.FirstNonEmpty(report.Governance.Model.RouterConfig, "(disabled)"))
	if report.Governance.Model.RouterEnabled {
		fmt.Fprintf(&b, " default=%s models=%d",
			stringutil.FirstNonEmpty(report.Governance.Model.RouterDefaultModel, "(unspecified)"),
			report.Governance.Model.RouterModels)
	}
	if report.Governance.Model.PricingCatalog != "" {
		fmt.Fprintf(&b, " pricing=%s", report.Governance.Model.PricingCatalog)
		if report.Governance.Model.PricingModels > 0 {
			fmt.Fprintf(&b, " pricing_models=%d", report.Governance.Model.PricingModels)
		}
	}
	if report.Governance.Model.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Governance.Model.Error)
	}
	if report.Governance.Model.PricingError != "" {
		fmt.Fprintf(&b, " pricing_err=%s", report.Governance.Model.PricingError)
	}
	b.WriteString("\n")
	if report.Governance.Adaptive != nil {
		fmt.Fprintf(&b, "Adaptive governance: events=%d sessions=%d runs=%d failover_attempts=%d recovery=%.0f%% approvals=%d/%d changes=%d rollback=%.0f%% inconsistent=%.0f%%\n",
			report.Governance.Adaptive.EventWindow,
			report.Governance.Adaptive.Sessions,
			report.Governance.Adaptive.Runs,
			report.Governance.Adaptive.Failover.Attempts,
			report.Governance.Adaptive.Failover.RecoveryRate*100,
			report.Governance.Adaptive.Approvals.Approved,
			report.Governance.Adaptive.Approvals.Resolved,
			report.Governance.Adaptive.ChangeWindow,
			report.Governance.Adaptive.Changes.RollbackRate*100,
			report.Governance.Adaptive.Changes.InconsistencyRate*100,
		)
	}
	fmt.Fprintf(&b, "State catalog: enabled=%t ready=%t entries=%d degraded=%t",
		report.Health.State.Enabled, report.Health.State.Ready, report.Health.State.Entries, report.Health.State.Degraded)
	if report.Health.State.LastError != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.State.LastError)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "State dir: %s\n", renderPathStatus(report.Paths.StateStoreDir))
	fmt.Fprintf(&b, "State events: %s\n", renderPathStatus(report.Paths.StateEventDir))
	fmt.Fprintf(&b, "Session store: %s\n", renderPathStatus(report.Paths.SessionStoreDir))
	fmt.Fprintf(&b, "Memory dir: %s\n", renderPathStatus(report.Paths.MemoryDir))
	fmt.Fprintf(&b, "Pricing catalog: %s\n", renderPathStatus(report.Paths.PricingCatalog))
	fmt.Fprintf(&b, "Audit log: %s\n", renderPathStatus(report.Paths.AuditLog))
	fmt.Fprintf(&b, "Debug log: %s\n", renderPathStatus(report.Paths.DebugLog))
	fmt.Fprintf(&b, "Task runtime: type=%s ready=%t", report.Health.Tasks.Type, report.Health.Tasks.Ready)
	if report.Health.Tasks.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Tasks.Error)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Workspace isolation: type=%s ready=%t", report.Health.Workspace.Type, report.Health.Workspace.Ready)
	if report.Health.Workspace.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Workspace.Error)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Sessions: recoverable=%d total=%d", report.Health.Sessions.Recoverable, report.Health.Sessions.Total)
	if report.Health.Sessions.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Sessions.Error)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Extensions: configured=%d enabled=%d disabled=%d mcp=%d prompt=%d discovered=%d",
		report.Health.Extensions.Configured, report.Health.Extensions.Enabled, report.Health.Extensions.Disabled,
		report.Health.Extensions.MCPServers, report.Health.Extensions.PromptSkills, report.Health.Extensions.DiscoveredSkills)
	if report.Health.Extensions.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Extensions.Error)
	}
	b.WriteString("\n")
	for _, server := range report.Health.Extensions.MCPServerStatus {
		fmt.Fprintf(&b, "  MCP %s [%s]: transport=%s enabled=%t effective=%t status=%s",
			server.Name, server.Source, stringutil.FirstNonEmpty(server.Transport, "-"), server.Enabled, server.Effective, server.Status)
		if server.Target != "" {
			fmt.Fprintf(&b, " target=%s", server.Target)
		}
		if server.HasEnv {
			fmt.Fprintf(&b, " env=%d", len(server.EnvKeys))
		}
		b.WriteString("\n")
	}
	for _, item := range report.Health.Extensions.CapabilityStatus {
		fmt.Fprintf(&b, "  Capability %s [%s]: state=%s critical=%t",
			stringutil.FirstNonEmpty(item.Name, item.Capability), stringutil.FirstNonEmpty(item.Kind, "runtime"), item.State, item.Critical)
		if item.Error != "" {
			fmt.Fprintf(&b, " err=%s", item.Error)
		}
		b.WriteString("\n")
	}
	if report.Health.Repo.Available {
		fmt.Fprintf(&b, "Repo: available=true root=%s branch=%s dirty=%t\n", report.Health.Repo.Root, stringutil.FirstNonEmpty(report.Health.Repo.Branch, "(detached)"), report.Health.Repo.Dirty)
	} else {
		fmt.Fprintf(&b, "Repo: available=false err=%s\n", report.Health.Repo.Error)
	}
	fmt.Fprintf(&b, "Snapshots: available=%t total=%d indexed_sessions=%d recoverable_matches=%d",
		report.Health.Snapshots.Available, report.Health.Snapshots.Total, report.Health.Snapshots.SessionIndexed, report.Health.Snapshots.RecoverableMatches)
	if report.Health.Snapshots.Error != "" {
		fmt.Fprintf(&b, " err=%s", report.Health.Snapshots.Error)
	}
	b.WriteString("\n")
	return b.String()
}

func renderPathStatus(status PathStatus) string {
	if status.Error != "" {
		return fmt.Sprintf("%s (exists=%t writable=%t err=%s)", status.Path, status.Exists, status.Writable, status.Error)
	}
	return fmt.Sprintf("%s (exists=%t writable=%t)", status.Path, status.Exists, status.Writable)
}

func renderList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ",")
}
