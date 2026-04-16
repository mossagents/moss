package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mosstui "github.com/mossagents/moss/contrib/tui"
	"github.com/mossagents/moss/harness/appkit/product/changes"
	"github.com/mossagents/moss/harness/sandbox"
	"github.com/mossagents/moss/kernel/workspace"

	tea "github.com/charmbracelet/bubbletea"
)

// newCodingExtension returns the mosscode-specific TUI extension that registers
// coding-oriented slash commands: /changes, /apply, /rollback, /diff, /git.
func newCodingExtension(ws string) *mosstui.Extension {
	return &mosstui.Extension{
		Name: "coding",
		SlashCommands: map[string]mosstui.SlashHandlerFunc{
			"/changes":  makeCodingChangesHandler(ws),
			"/apply":    makeCodingApplyHandler(ws),
			"/rollback": makeCodingRollbackHandler(ws),
			"/diff":     makeCodingDiffHandler(ws),
			"/git":      makeCodingGitHandler(ws),
		},
	}
}

func makeCodingChangesHandler(ws string) mosstui.SlashHandlerFunc {
	return func(_ mosstui.TUIContext, args []string) tea.Cmd {
		if len(args) == 0 {
			return mosstui.AppendSystemMessageCmd("Usage:\n  /changes list [limit]\n  /changes show <change_id>")
		}
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list":
			limit := 20
			if len(args) >= 2 {
				v, err := strconv.Atoi(args[1])
				if err != nil || v <= 0 {
					return mosstui.AppendErrorMessageCmd("Usage: /changes list [limit:int]")
				}
				limit = v
			}
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				items, err := changes.ListChangeOperations(ctx, ws, limit)
				if err != nil {
					return mosstui.ErrorMsg(fmt.Sprintf("failed to list changes: %v", err))
				}
				return mosstui.SystemMsg(changes.RenderChangeSummaries(items))
			}
		case "show":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return mosstui.AppendErrorMessageCmd("Usage: /changes show <change_id>")
			}
			changeID := strings.TrimSpace(args[1])
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				item, err := changes.LoadChangeOperation(ctx, ws, changeID)
				if err != nil {
					return mosstui.ErrorMsg(fmt.Sprintf("failed to show change: %v", err))
				}
				return mosstui.SystemMsg(changes.RenderChangeDetail(item))
			}
		default:
			return mosstui.AppendErrorMessageCmd("Usage: /changes list|show ...")
		}
	}
}

func makeCodingApplyHandler(ws string) mosstui.SlashHandlerFunc {
	return func(_ mosstui.TUIContext, args []string) tea.Cmd {
		if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
			return mosstui.AppendErrorMessageCmd("Usage: /apply <patch_file> [summary...]")
		}
		patchFile := strings.TrimSpace(args[0])
		if !filepath.IsAbs(patchFile) {
			patchFile = filepath.Join(ws, patchFile)
		}
		summary := strings.TrimSpace(strings.Join(args[1:], " "))
		return func() tea.Msg {
			data, err := os.ReadFile(patchFile)
			if err != nil {
				return mosstui.ErrorMsg(fmt.Sprintf("read patch file: %v", err))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			rt := changes.ChangeRuntime{
				Workspace:        ws,
				RepoStateCapture: sandbox.NewGitRepoStateCapture(ws),
				PatchApply:       sandbox.NewGitPatchApply(ws),
				PatchRevert:      sandbox.NewGitPatchRevert(ws),
			}
			item, err := changes.ApplyChange(ctx, rt, changes.ApplyChangeRequest{
				Patch:   string(data),
				Summary: summary,
				Source:  workspace.PatchSourceUser,
			})
			if err != nil {
				return mosstui.ErrorMsg(fmt.Sprintf("failed to apply change: %v", err))
			}
			return mosstui.SystemMsg(changes.RenderChangeDetail(item))
		}
	}
}

func makeCodingRollbackHandler(ws string) mosstui.SlashHandlerFunc {
	return func(_ mosstui.TUIContext, args []string) tea.Cmd {
		if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
			return mosstui.AppendErrorMessageCmd("Usage: /rollback <change_id>")
		}
		changeID := strings.TrimSpace(args[0])
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			rt := changes.ChangeRuntime{
				Workspace:        ws,
				RepoStateCapture: sandbox.NewGitRepoStateCapture(ws),
				PatchApply:       sandbox.NewGitPatchApply(ws),
				PatchRevert:      sandbox.NewGitPatchRevert(ws),
			}
			item, err := changes.RollbackChange(ctx, rt, changes.RollbackChangeRequest{ChangeID: changeID})
			if err != nil {
				return mosstui.ErrorMsg(fmt.Sprintf("failed to roll back change: %v", err))
			}
			return mosstui.SystemMsg(changes.RenderChangeDetail(item))
		}
	}
}

func makeCodingDiffHandler(ws string) mosstui.SlashHandlerFunc {
	return func(_ mosstui.TUIContext, args []string) tea.Cmd {
		cmdArgs := []string{"--no-pager", "diff"}
		if len(args) > 0 {
			cmdArgs = append(cmdArgs, "--")
			cmdArgs = append(cmdArgs, args...)
		}
		return func() tea.Msg {
			out, err := runGitCommand(ws, "git", cmdArgs)
			if err != nil {
				return mosstui.ErrorMsg(fmt.Sprintf("git diff failed: %v", err))
			}
			if strings.TrimSpace(out) == "" {
				return mosstui.SystemMsg("No diff.")
			}
			return mosstui.SystemMsg(out)
		}
	}
}

func makeCodingGitHandler(ws string) mosstui.SlashHandlerFunc {
	return func(_ mosstui.TUIContext, args []string) tea.Cmd {
		if len(args) == 0 {
			return mosstui.AppendSystemMessageCmd("Usage:\n  /git status\n  /git diff [path]\n  /git commit <message>\n  /git pr [args...]")
		}
		sub := strings.ToLower(args[0])
		switch sub {
		case "status":
			return func() tea.Msg {
				out, err := runGitCommand(ws, "git", []string{"--no-pager", "status", "--short"})
				if err != nil {
					return mosstui.ErrorMsg(fmt.Sprintf("git status failed: %v", err))
				}
				return mosstui.SystemMsg(out)
			}
		case "diff":
			cmdArgs := []string{"--no-pager", "diff"}
			if len(args) > 1 {
				cmdArgs = append(cmdArgs, args[1:]...)
			}
			return func() tea.Msg {
				out, err := runGitCommand(ws, "git", cmdArgs)
				if err != nil {
					return mosstui.ErrorMsg(fmt.Sprintf("git diff failed: %v", err))
				}
				return mosstui.SystemMsg(out)
			}
		case "commit":
			if len(args) < 2 {
				return mosstui.AppendErrorMessageCmd("Usage: /git commit <message>")
			}
			commitMsg := strings.Join(args[1:], " ")
			return func() tea.Msg {
				out, err := runGitCommand(ws, "git", []string{"commit", "-m", commitMsg})
				if err != nil {
					return mosstui.ErrorMsg(fmt.Sprintf("git commit failed: %v", err))
				}
				return mosstui.SystemMsg(out)
			}
		case "pr":
			prArgs := []string{"pr"}
			if len(args) > 1 {
				prArgs = append(prArgs, args[1:]...)
			} else {
				prArgs = append(prArgs, "status")
			}
			return func() tea.Msg {
				out, err := runGitCommand(ws, "gh", prArgs)
				if err != nil {
					return mosstui.ErrorMsg(fmt.Sprintf("gh pr failed: %v", err))
				}
				return mosstui.SystemMsg(out)
			}
		default:
			return mosstui.AppendErrorMessageCmd("Usage: /git status | /git diff [path] | /git commit <message> | /git pr [args...]")
		}
	}
}

// runGitCommand runs a shell command (git or gh) in the given working directory
// and returns the combined stdout+stderr output.
func runGitCommand(dir, name string, args []string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return "", fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(out)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
