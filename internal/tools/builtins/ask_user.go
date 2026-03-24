package builtins

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mossagi/moss/internal/tools"
)

type AskUserTool struct {
	reader io.Reader
	writer io.Writer
}

func NewAskUserTool(reader io.Reader, writer io.Writer) *AskUserTool {
	if reader == nil {
		reader = os.Stdin
	}
	if writer == nil {
		writer = os.Stdout
	}
	return &AskUserTool{reader: reader, writer: writer}
}

func (t *AskUserTool) Name() string           { return "ask_user" }
func (t *AskUserTool) Description() string    { return "Asks the user a question and returns their answer" }
func (t *AskUserTool) Risk() tools.RiskLevel  { return tools.RiskLow }
func (t *AskUserTool) Capabilities() []string { return []string{"interact"} }

func (t *AskUserTool) Execute(ctx context.Context, input tools.ToolInput) (tools.ToolOutput, error) {
	question, _ := input["question"].(string)
	if question == "" {
		return tools.ToolOutput{Success: false, Error: "question is required"}, nil
	}
	fmt.Fprintf(t.writer, "\n[?] %s\nAnswer: ", question)
	scanner := bufio.NewScanner(t.reader)
	if scanner.Scan() {
		answer := strings.TrimSpace(scanner.Text())
		return tools.ToolOutput{
			Success: true,
			Data:    map[string]any{"answer": answer},
		}, nil
	}
	if err := scanner.Err(); err != nil {
		return tools.ToolOutput{Success: false, Error: err.Error()}, nil
	}
	return tools.ToolOutput{Success: true, Data: map[string]any{"answer": ""}}, nil
}
