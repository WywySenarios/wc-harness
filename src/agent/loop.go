// Package agent runs the model/tool loop.
package agent

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/WywySenarios/wc-harness/src/llm"
	"github.com/WywySenarios/wc-harness/src/tools"
)

const SystemPrompt = `You are a coding agent operating in one project workspace.
Use the bash tool to inspect and modify files.
Run one command per call.
When the task is complete, reply with a short summary and do not call a tool.`

const (
	maxToolArgsLog = 500
	maxToolContent = 32 << 10
)

// Agent drives one conversation against one tool registry.
type Agent struct {
	Client    *llm.Client
	Tools     *tools.Registry
	Workspace string
	MaxTurns  int
	Out       io.Writer
	Log       io.Writer
}

// Run the agent loop until the model answers without a tool call.
func (a *Agent) Run(ctx context.Context, prompt string) error {
	messages := []llm.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: prompt},
	}

	for turn := 0; turn < a.MaxTurns; turn++ {
		message, err := a.Client.Stream(ctx, messages, a.Tools.Schemas(), func(text string) {
			io.WriteString(a.Out, text)
		})
		if err != nil {
			return err
		}
		if message.Content != "" {
			fmt.Fprintln(a.Out)
		}
		messages = append(messages, message)

		if len(message.ToolCalls) == 0 {
			return nil
		}

		for _, call := range message.ToolCalls {
			content, err := a.execute(ctx, call)
			if err != nil {
				return err
			}
			messages = append(messages, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    content,
			})
		}
	}

	return fmt.Errorf("reached %d turns without a final answer", a.MaxTurns)
}

func (a *Agent) execute(ctx context.Context, call llm.ToolCall) (string, error) {
	fmt.Fprintf(a.Log, "[tool] %s %s\n", call.Function.Name, truncate(call.Function.Arguments, maxToolArgsLog))

	tool, ok := a.Tools.Lookup(call.Function.Name)
	if !ok {
		fmt.Fprintf(a.Log, "[tool] %s unknown\n", call.Function.Name)
		return fmt.Sprintf("error: unknown tool %q", call.Function.Name), nil
	}

	result, err := tool.Run(ctx, a.Workspace, call.Function.Arguments)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		fmt.Fprintf(a.Log, "[tool] %s error: %v\n", tool.Name, err)
		return "error: " + err.Error(), nil
	}

	fmt.Fprintf(a.Log, "[tool] %s exit %d (%s)\n", tool.Name, result.ExitCode, result.Duration.Round(time.Millisecond))

	var content strings.Builder
	fmt.Fprintf(&content, "exit code: %d\n", result.ExitCode)
	if result.Stdout != "" {
		content.WriteString("stdout:\n")
		content.WriteString(result.Stdout)
		if !strings.HasSuffix(result.Stdout, "\n") {
			content.WriteByte('\n')
		}
	}
	if result.Stderr != "" {
		content.WriteString("stderr:\n")
		content.WriteString(result.Stderr)
		if !strings.HasSuffix(result.Stderr, "\n") {
			content.WriteByte('\n')
		}
	}
	if result.Truncated {
		content.WriteString("[output truncated]\n")
	}

	return truncate(content.String(), maxToolContent), nil
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
