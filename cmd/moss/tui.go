package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/mossagents/moss/appkit/product"
	intr "github.com/mossagents/moss/kernel/interaction"
	"io"
	"os"
	"strconv"
	"strings"
)

// cliUserIO 是基于终端的 UserIO 实现（用于 run 命令的 CLI 模式）。
type cliUserIO struct {
	writer    io.Writer
	reader    *os.File
	workspace string
	profile   string
}

func (c *cliUserIO) Send(_ context.Context, msg intr.OutputMessage) error {
	var err error
	switch msg.Type {
	case intr.OutputText:
		_, err = fmt.Fprintln(c.writer, msg.Content)
	case intr.OutputStream:
		_, err = fmt.Fprint(c.writer, msg.Content)
	case intr.OutputStreamEnd:
		_, err = fmt.Fprintln(c.writer)
	case intr.OutputReasoning:
		_, err = fmt.Fprintf(c.writer, "💭 %s\n", msg.Content)
	case intr.OutputProgress:
		_, err = fmt.Fprintf(c.writer, "⏳ %s\n", msg.Content)
	case intr.OutputToolStart:
		_, err = fmt.Fprintf(c.writer, "🔧 Running %s...\n", msg.Content)
	case intr.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			_, err = fmt.Fprintf(c.writer, "❌ %s\n", msg.Content)
		} else {
			_, err = fmt.Fprintf(c.writer, "✅ %s\n", truncate(msg.Content, 200))
		}
	}
	return err
}

func (c *cliUserIO) Ask(_ context.Context, req intr.InputRequest) (intr.InputResponse, error) {
	reader := bufio.NewReader(c.reader)
	switch req.Type {
	case intr.InputConfirm:
		if req.Approval != nil {
			options := cliApprovalOptions(req.Approval, c.workspace)
			if _, err := fmt.Fprintf(c.writer, "%s\n", req.Prompt); err != nil {
				return intr.InputResponse{}, err
			}
			for i, opt := range options {
				if _, err := fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt); err != nil {
					return intr.InputResponse{}, err
				}
			}
			if _, err := fmt.Fprint(c.writer, "Choose decision: "); err != nil {
				return intr.InputResponse{}, err
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				return intr.InputResponse{}, err
			}
			choice, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || choice < 1 || choice > len(options) {
				choice = len(options)
			}
			selected := options[choice-1]
			return c.cliApprovalResponse(req.Approval, selected)
		}
		prompt := req.Prompt
		if _, err := fmt.Fprintf(c.writer, "%s [y/N]: ", prompt); err != nil {
			return intr.InputResponse{}, err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return intr.InputResponse{}, err
		}
		line = strings.TrimSpace(strings.ToLower(line))
		approved := line == "y" || line == "yes"
		var decision *intr.ApprovalDecision
		if req.Approval != nil {
			decision = &intr.ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  approved,
				Source:    "cli",
			}
		}
		return intr.InputResponse{Approved: approved, Decision: decision}, nil

	case intr.InputSelect:
		for i, opt := range req.Options {
			if _, err := fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt); err != nil {
				return intr.InputResponse{}, err
			}
		}
		if _, err := fmt.Fprintf(c.writer, "%s: ", req.Prompt); err != nil {
			return intr.InputResponse{}, err
		}
		var sel int
		if _, err := fmt.Fscan(c.reader, &sel); err != nil {
			return intr.InputResponse{}, err
		}
		return intr.InputResponse{Selected: sel - 1}, nil

	default: // FreeText
		if _, err := fmt.Fprintf(c.writer, "%s: ", req.Prompt); err != nil {
			return intr.InputResponse{}, err
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return intr.InputResponse{}, err
		}
		return intr.InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func cliApprovalOptions(req *intr.ApprovalRequest, workspace string) []string {
	options := []string{"Allow once"}
	if req != nil && strings.TrimSpace(req.CacheKey) != "" {
		options = append(options, "Allow for this session")
	}
	if req != nil && strings.TrimSpace(workspace) != "" && req.ProposedAmendment != nil {
		options = append(options, "Always allow for this project")
	}
	options = append(options, "Deny")
	return options
}

func (c *cliUserIO) cliApprovalResponse(req *intr.ApprovalRequest, selected string) (intr.InputResponse, error) {
	decision := &intr.ApprovalDecision{
		RequestID: req.ID,
		Type:      intr.ApprovalDecisionDeny,
		Approved:  selected != "Deny",
		Source:    "cli",
	}
	switch selected {
	case "Allow for this session":
		if req.ProposedPermissions != nil {
			decision.Type = intr.ApprovalDecisionGrantPermission
			decision.GrantedPermissions = req.ProposedPermissions
			decision.Source = "cli-session-permission"
		} else {
			decision.Type = intr.ApprovalDecisionApproveSession
			decision.Source = "cli-session-rule"
		}
	case "Always allow for this project":
		if err := product.PersistProjectApprovalAmendment(c.workspace, c.profile, req.ProposedAmendment); err != nil {
			return intr.InputResponse{}, err
		}
		decision.Type = intr.ApprovalDecisionPolicyAmendment
		decision.PolicyAmendment = req.ProposedAmendment
		decision.Source = "cli-project-amendment"
	case "Allow once":
		decision.Type = intr.ApprovalDecisionApprove
		decision.Source = "cli-allow-once"
	default:
		decision.Type = intr.ApprovalDecisionDeny
		decision.Source = "cli-deny"
	}
	return intr.InputResponse{Approved: decision.Type != intr.ApprovalDecisionDeny, Decision: intr.NormalizeApprovalDecision(decision)}, nil
}
