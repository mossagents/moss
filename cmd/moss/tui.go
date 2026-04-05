package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/kernel/port"
)

// cliUserIO 是基于终端的 UserIO 实现（用于 run 命令的 CLI 模式）。
type cliUserIO struct {
	writer    io.Writer
	reader    *os.File
	workspace string
	profile   string
}

func (c *cliUserIO) Send(_ context.Context, msg port.OutputMessage) error {
	switch msg.Type {
	case port.OutputText:
		fmt.Fprintln(c.writer, msg.Content)
	case port.OutputStream:
		fmt.Fprint(c.writer, msg.Content)
	case port.OutputStreamEnd:
		fmt.Fprintln(c.writer)
	case port.OutputReasoning:
		fmt.Fprintf(c.writer, "💭 %s\n", msg.Content)
	case port.OutputProgress:
		fmt.Fprintf(c.writer, "⏳ %s\n", msg.Content)
	case port.OutputToolStart:
		fmt.Fprintf(c.writer, "🔧 Running %s...\n", msg.Content)
	case port.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(c.writer, "❌ %s\n", msg.Content)
		} else {
			fmt.Fprintf(c.writer, "✅ %s\n", truncate(msg.Content, 200))
		}
	}
	return nil
}

func (c *cliUserIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	reader := bufio.NewReader(c.reader)
	switch req.Type {
	case port.InputConfirm:
		if req.Approval != nil {
			options := cliApprovalOptions(req.Approval, c.workspace)
			fmt.Fprintf(c.writer, "%s\n", req.Prompt)
			for i, opt := range options {
				fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt)
			}
			fmt.Fprint(c.writer, "Choose decision: ")
			line, err := reader.ReadString('\n')
			if err != nil {
				return port.InputResponse{}, err
			}
			choice, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || choice < 1 || choice > len(options) {
				choice = len(options)
			}
			selected := options[choice-1]
			return c.cliApprovalResponse(req.Approval, selected)
		}
		prompt := req.Prompt
		fmt.Fprintf(c.writer, "%s [y/N]: ", prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		line = strings.TrimSpace(strings.ToLower(line))
		approved := line == "y" || line == "yes"
		var decision *port.ApprovalDecision
		if req.Approval != nil {
			decision = &port.ApprovalDecision{
				RequestID: req.Approval.ID,
				Approved:  approved,
				Source:    "cli",
			}
		}
		return port.InputResponse{Approved: approved, Decision: decision}, nil

	case port.InputSelect:
		for i, opt := range req.Options {
			fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt)
		}
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		var sel int
		fmt.Fscan(c.reader, &sel)
		return port.InputResponse{Selected: sel - 1}, nil

	default: // FreeText
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		return port.InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func cliApprovalOptions(req *port.ApprovalRequest, workspace string) []string {
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

func (c *cliUserIO) cliApprovalResponse(req *port.ApprovalRequest, selected string) (port.InputResponse, error) {
	decision := &port.ApprovalDecision{
		RequestID: req.ID,
		Type:      port.ApprovalDecisionDeny,
		Approved:  selected != "Deny",
		Source:    "cli",
	}
	switch selected {
	case "Allow for this session":
		if req.ProposedPermissions != nil {
			decision.Type = port.ApprovalDecisionGrantPermission
			decision.GrantedPermissions = req.ProposedPermissions
			decision.Source = "cli-session-permission"
		} else {
			decision.Type = port.ApprovalDecisionApproveSession
			decision.Source = "cli-session-rule"
		}
	case "Always allow for this project":
		if err := product.PersistProjectApprovalAmendment(c.workspace, c.profile, req.ProposedAmendment); err != nil {
			return port.InputResponse{}, err
		}
		decision.Type = port.ApprovalDecisionPolicyAmendment
		decision.PolicyAmendment = req.ProposedAmendment
		decision.Source = "cli-project-amendment"
	case "Allow once":
		decision.Type = port.ApprovalDecisionApprove
		decision.Source = "cli-allow-once"
	default:
		decision.Type = port.ApprovalDecisionDeny
		decision.Source = "cli-deny"
	}
	return port.InputResponse{Approved: decision.Type != port.ApprovalDecisionDeny, Decision: port.NormalizeApprovalDecision(decision)}, nil
}
