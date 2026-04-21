package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/harness/appkit/product"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
)

func runInit(cfg *config) error {
	out, err := product.InitWorkspaceBootstrap(cfg.flags.Workspace, appName)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func runDoctor(ctx context.Context, cfg *config) error {
	invocation, err := resolveRuntimeInvocation(cfg, "interactive")
	if err != nil {
		return err
	}
	flags := cloneAppFlags(invocation.CompatFlags)
	report := product.BuildDoctorReport(ctx, appName, flags.Workspace, flags, cfg.explicitFlags, invocation.ApprovalMode, sessionSelectorReportFromInvocation(invocation), cfg.governance)
	if cfg.doctorJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal doctor report: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Print(product.RenderDoctorReport(report))
	return nil
}

func runDebugConfig(cfg *config) error {
	invocation, err := resolveRuntimeInvocation(cfg, "interactive")
	if err != nil {
		return err
	}
	flags := cloneAppFlags(invocation.CompatFlags)
	report := product.BuildDebugConfigReport(
		appName,
		flags.Workspace,
		flags.DisplayProviderName(),
		flags.Model,
		flags.Trust,
		invocation.ApprovalMode,
		invocation.DisplayProfile,
		sessionSelectorReportFromInvocation(invocation),
		currentThemeName(),
		"",
		"",
		"",
	)
	if cfg.debugConfigJSON {
		return printJSON(report)
	}
	fmt.Println(product.RenderDebugConfigReport(report))
	return nil
}

func sessionSelectorReportFromInvocation(invocation runtimeInvocation) product.SessionSelectorReport {
	if !invocation.Typed {
		return product.SessionSelectorReport{}
	}
	return product.SessionSelectorReport{
		RunMode:           strings.TrimSpace(invocation.ResolvedSpec.Runtime.RunMode),
		Preset:            strings.TrimSpace(invocation.ResolvedSpec.Origin.Preset),
		CollaborationMode: strings.TrimSpace(invocation.ResolvedSpec.Intent.CollaborationMode),
		PromptPack:        strings.TrimSpace(invocation.ResolvedSpec.Intent.PromptPack.ID),
		PermissionProfile: strings.TrimSpace(invocation.ResolvedSpec.Runtime.PermissionProfile),
		SessionPolicy:     strings.TrimSpace(invocation.ResolvedSpec.Runtime.SessionPolicyName),
		ModelProfile:      strings.TrimSpace(invocation.ResolvedSpec.Runtime.ModelProfile),
	}
}

func runCompletion(cfg *config) error {
	if len(cfg.completionArgs) != 1 {
		return fmt.Errorf("usage: mosscode completion <powershell|bash|zsh>")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.completionArgs[0])) {
	case "powershell":
		fmt.Print(renderPowerShellCompletion())
		return nil
	case "bash":
		fmt.Print(renderBashCompletion())
		return nil
	case "zsh":
		fmt.Print(renderZshCompletion())
		return nil
	default:
		return fmt.Errorf("unsupported shell %q (supported: powershell, bash, zsh)", cfg.completionArgs[0])
	}
}

func currentThemeName() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MOSSCODE_THEME"))) {
	case "plain":
		return "plain"
	default:
		return "default"
	}
}

func renderPowerShellCompletion() string {
	return `Register-ArgumentCompleter -CommandName mosscode -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $commands = @('exec','resume','fork','init','doctor','debug-config','completion','config','review','checkpoint','apply','rollback','changes')
    $commands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`
}

func renderBashCompletion() string {
	return `_mosscode_completions() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local commands="exec resume fork init doctor debug-config completion config review checkpoint apply rollback changes"
    COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
}
complete -F _mosscode_completions mosscode
`
}

func renderZshCompletion() string {
	return `#compdef mosscode
local -a commands
commands=(
  'exec:run one-shot prompt'
  'resume:resume saved thread'
  'fork:fork from thread or checkpoint'
  'init:scaffold AGENTS.md and commands'
  'doctor:run diagnostics'
  'debug-config:show config and path diagnostics'
  'completion:emit shell completion script'
  'config:manage persisted config'
  'review:inspect working tree review state'
  'checkpoint:manage checkpoints'
  'apply:apply explicit patch'
  'rollback:rollback a change'
  'changes:list or inspect persisted changes'
)
_describe 'command' commands
`
}

func runConfig(cfg *config) error {
	args := cfg.configArgs
	if len(args) == 0 || args[0] == "show" {
		return showConfig(cfg.flags)
	}
	switch args[0] {
	case "path":
		cfgPath, err := product.ConfigPath()
		if err != nil {
			return err
		}
		fmt.Println(cfgPath)
		return nil
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: mosscode config set <provider|name|model|base_url> <value>")
		}
		cfgPath, err := product.ConfigPath()
		if err != nil {
			return err
		}
		if _, err := product.SetConfig(args[1], strings.Join(args[2:], " "), false); err != nil {
			return err
		}
		fmt.Printf("Updated %s in %s\n", strings.ToLower(strings.TrimSpace(args[1])), cfgPath)
		return showConfig(effectiveFlags())
	case "unset":
		if len(args) != 2 {
			return fmt.Errorf("usage: mosscode config unset <name|model|base_url>")
		}
		cfgPath, err := product.ConfigPath()
		if err != nil {
			return err
		}
		if err := product.UnsetConfig(args[1], false); err != nil {
			return err
		}
		fmt.Printf("Cleared %s in %s\n", strings.ToLower(strings.TrimSpace(args[1])), cfgPath)
		return showConfig(effectiveFlags())
	case "mcp":
		return runConfigMCP(cfg.flags, args[1:])
	default:
		return fmt.Errorf("unknown config command %q (supported: show, path, set, unset, mcp)", args[0])
	}
}

func runConfigMCP(flags *appkit.AppFlags, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		servers, err := product.ListMCPServers(flags.Workspace, flags.Trust)
		if err != nil {
			return err
		}
		fmt.Print(product.RenderMCPServerList(servers))
		return nil
	}
	switch args[0] {
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: mosscode config mcp show <name>")
		}
		servers, err := product.GetMCPServer(flags.Workspace, flags.Trust, args[1])
		if err != nil {
			return err
		}
		fmt.Print(product.RenderMCPServerDetail(servers))
		return nil
	case "enable", "disable":
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("usage: mosscode config mcp %s <name> [global|project]", args[0])
		}
		enabled := args[0] == "enable"
		scope := ""
		if len(args) == 3 {
			scope = args[2]
		}
		server, err := product.SetMCPEnabled(flags.Workspace, args[1], scope, enabled)
		if err != nil {
			return err
		}
		fmt.Printf("Updated MCP %s [%s]: enabled=%t effective=%t status=%s\n", server.Name, server.Source, server.Enabled, server.Effective, server.Status)
		return nil
	default:
		return fmt.Errorf("unknown mcp config command %q (supported: list, show, enable, disable)", args[0])
	}
}

func runReview(ctx context.Context, cfg *config) error {
	report, err := runtimeenv.BuildReviewReport(ctx, cfg.flags.Workspace, cfg.reviewArgs)
	if err != nil {
		return err
	}
	if cfg.reviewJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal review report: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Print(runtimeenv.RenderReviewReport(report))
	return nil
}

func showConfig(flags *appkit.AppFlags) error {
	out, err := product.ShowConfig(flags, false)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
