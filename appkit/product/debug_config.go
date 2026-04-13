package product

import (
	"fmt"
	"github.com/mossagents/moss/internal/strutil"
	"os"
	"path/filepath"
	"strings"

	appconfig "github.com/mossagents/moss/config"
)

type DebugConfigReport struct {
	App                   string   `json:"app"`
	Workspace             string   `json:"workspace"`
	Provider              string   `json:"provider"`
	Model                 string   `json:"model"`
	Trust                 string   `json:"trust"`
	ApprovalMode          string   `json:"approval_mode"`
	Profile               string   `json:"profile"`
	Theme                 string   `json:"theme"`
	DebugEnabled          bool     `json:"debug_enabled"`
	GlobalConfig          string   `json:"global_config"`
	ProjectConfig         string   `json:"project_config"`
	AppDir                string   `json:"app_dir"`
	CommandDirs           []string `json:"command_dirs"`
	StateStoreDir         string   `json:"state_store_dir"`
	StateEventDir         string   `json:"state_event_dir"`
	SessionStoreDir       string   `json:"session_store_dir"`
	TaskRuntimeDir        string   `json:"task_runtime_dir"`
	MemoryDir             string   `json:"memory_dir"`
	WorkspaceRootDir      string   `json:"workspace_root_dir"`
	AuditLog              string   `json:"audit_log"`
	DebugLog              string   `json:"debug_log"`
	RouterConfig          string   `json:"router_config"`
	PricingCatalog        string   `json:"pricing_catalog"`
	DetectedEnv           []string `json:"detected_env"`
	PromptBaseSource      string   `json:"prompt_base_source,omitempty"`
	PromptDynamicSections string   `json:"prompt_dynamic_sections,omitempty"`
	PromptSourceChain     string   `json:"prompt_source_chain,omitempty"`
}

func BuildDebugConfigReport(appName, workspace, provider, model, trust, approvalMode, profile, theme, promptBaseSource, promptDynamicSections, promptSourceChain string) DebugConfigReport {
	return DebugConfigReport{
		App:                   appName,
		Workspace:             workspace,
		Provider:              provider,
		Model:                 strutil.FirstNonEmpty(model, "(default)"),
		Trust:                 strutil.FirstNonEmpty(trust, appconfig.TrustTrusted),
		ApprovalMode:          strutil.FirstNonEmpty(approvalMode, "confirm"),
		Profile:               strutil.FirstNonEmpty(profile, "default"),
		Theme:                 strutil.FirstNonEmpty(theme, "default"),
		DebugEnabled:          os.Getenv("MOSS_DEBUG") == "1",
		GlobalConfig:          appconfig.DefaultGlobalConfigPath(),
		ProjectConfig:         appconfig.DefaultProjectConfigPath(workspace),
		AppDir:                appconfig.AppDir(),
		CommandDirs:           debugCommandDirs(appName, workspace),
		StateStoreDir:         StateStoreDir(),
		StateEventDir:         StateEventDir(),
		SessionStoreDir:       SessionStoreDir(),
		TaskRuntimeDir:        TaskRuntimeDir(),
		MemoryDir:             MemoryDir(),
		WorkspaceRootDir:      WorkspaceIsolationDir(),
		AuditLog:              AuditLogPath(),
		DebugLog:              DebugLogPath(),
		RouterConfig:          filepath.Join(appconfig.AppDir(), "models.yaml"),
		PricingCatalog:        filepath.Join(appconfig.AppDir(), "pricing.yaml"),
		DetectedEnv:           detectedEnvVars(),
		PromptBaseSource:      strings.TrimSpace(promptBaseSource),
		PromptDynamicSections: strings.TrimSpace(promptDynamicSections),
		PromptSourceChain:     strings.TrimSpace(promptSourceChain),
	}
}

func RenderDebugConfigReport(report DebugConfigReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mosscode debug-config\n")
	fmt.Fprintf(&b, "Workspace: %s\n", strutil.FirstNonEmpty(report.Workspace, "."))
	fmt.Fprintf(&b, "Provider: %s | model=%s | trust=%s | approval=%s | profile=%s | theme=%s\n",
		strutil.FirstNonEmpty(report.Provider, "(default)"),
		strutil.FirstNonEmpty(report.Model, "(default)"),
		strutil.FirstNonEmpty(report.Trust, appconfig.TrustTrusted),
		strutil.FirstNonEmpty(report.ApprovalMode, "confirm"),
		strutil.FirstNonEmpty(report.Profile, "default"),
		strutil.FirstNonEmpty(report.Theme, "default"))
	fmt.Fprintf(&b, "Debug logging: enabled=%t path=%s\n", report.DebugEnabled, report.DebugLog)
	fmt.Fprintf(&b, "Global config: %s\n", renderDebugPath(report.GlobalConfig))
	fmt.Fprintf(&b, "Project config: %s\n", renderDebugPath(report.ProjectConfig))
	fmt.Fprintf(&b, "App dir: %s\n", renderDebugPath(report.AppDir))
	for _, dir := range report.CommandDirs {
		fmt.Fprintf(&b, "Command dir: %s\n", renderDebugPath(dir))
	}
	fmt.Fprintf(&b, "State store: %s\n", renderDebugPath(report.StateStoreDir))
	fmt.Fprintf(&b, "State events: %s\n", renderDebugPath(report.StateEventDir))
	fmt.Fprintf(&b, "Session store: %s\n", renderDebugPath(report.SessionStoreDir))
	fmt.Fprintf(&b, "Task runtime: %s\n", renderDebugPath(report.TaskRuntimeDir))
	fmt.Fprintf(&b, "Memory dir: %s\n", renderDebugPath(report.MemoryDir))
	fmt.Fprintf(&b, "Workspace isolation: %s\n", renderDebugPath(report.WorkspaceRootDir))
	fmt.Fprintf(&b, "Audit log: %s\n", renderDebugPath(report.AuditLog))
	fmt.Fprintf(&b, "Router config: %s\n", renderDebugPath(report.RouterConfig))
	fmt.Fprintf(&b, "Pricing catalog: %s\n", renderDebugPath(report.PricingCatalog))
	fmt.Fprintf(&b, "Detected env: %s\n", renderList(report.DetectedEnv))
	if strings.TrimSpace(report.PromptBaseSource) != "" {
		fmt.Fprintf(&b, "Prompt base source: %s\n", report.PromptBaseSource)
	}
	if strings.TrimSpace(report.PromptDynamicSections) != "" {
		fmt.Fprintf(&b, "Prompt dynamic sections: %s\n", report.PromptDynamicSections)
	}
	if strings.TrimSpace(report.PromptSourceChain) != "" {
		fmt.Fprintf(&b, "Prompt source chain: %s\n", report.PromptSourceChain)
	}
	return strings.TrimRight(b.String(), "\n")
}

func debugCommandDirs(appName, workspace string) []string {
	dirs := []string{filepath.Join(appconfig.AppDir(), "commands")}
	if strings.TrimSpace(workspace) != "" {
		dirs = append(dirs,
			filepath.Join(workspace, "."+appName, "commands"),
			filepath.Join(workspace, ".agents", "commands"),
		)
	}
	return dirs
}

func renderDebugPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "(unset)"
	}
	if _, err := os.Stat(path); err == nil {
		return path + " (exists)"
	}
	return path + " (missing)"
}
