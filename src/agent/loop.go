// Package agent runs the model/tool loop.
package agent

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/WywySenarios/wc-harness/src/events"
	"github.com/WywySenarios/wc-harness/src/llm"
	"github.com/WywySenarios/wc-harness/src/tools"
)

// SystemPrompt is the instruction prepended to every conversation.
const SystemPrompt = `You are a coding agent operating in one project workspace.
Use the bash tool to inspect and modify files.
Run one command per call.
When the task is complete, reply with a short summary and do not call a tool.`

const (
	maxToolArgsLog  = 500
	maxToolContent  = 32 << 10
	maxEventContent = 4 << 10
)

// StreamingClient is the model transport the agent needs.
type StreamingClient interface {
	Stream(ctx context.Context, messages []llm.Message, tools []llm.Tool, onText func(string)) (llm.Message, error)
}

// MaxTurnsError reports that the loop hit its turn limit without a final answer.
type MaxTurnsError struct {
	MaxTurns int
}

func (e *MaxTurnsError) Error() string {
	return fmt.Sprintf("reached %d turns without a final answer", e.MaxTurns)
}

// Agent drives one conversation against one tool registry.
type Agent struct {
	Client    StreamingClient
	Tools     *tools.Registry
	Workspace string
	MaxTurns  int
	Out       io.Writer
	Log       io.Writer
	Events    events.Emitter
}

// emitter returns the configured emitter, defaulting to text output.
func (a *Agent) emitter() events.Emitter {
	if a.Events == nil {
		a.Events = events.New("text", a.Out, a.Log)
	}
	return a.Events
}

// Run the agent loop until the model answers without a tool call.
func (a *Agent) Run(ctx context.Context, prompt string) error {
	emit := a.emitter()
	messages := []llm.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: prompt},
	}

	for turn := 0; turn < a.MaxTurns; turn++ {
		message, err := a.Client.Stream(ctx, messages, a.Tools.Schemas(), func(text string) {
			emit.AssistantDelta(text)
		})
		if err != nil {
			return err
		}
		emit.AssistantMessage(message.Content)
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

	return &MaxTurnsError{MaxTurns: a.MaxTurns}
}

func (a *Agent) execute(ctx context.Context, call llm.ToolCall) (string, error) {
	emit := a.emitter()
	emit.ToolCall(call.ID, call.Function.Name, truncate(call.Function.Arguments, maxToolArgsLog))

	tool, ok := a.Tools.Lookup(call.Function.Name)
	if !ok {
		message := fmt.Sprintf("error: unknown tool %q", call.Function.Name)
		emit.ToolResult(call.ID, call.Function.Name, -1, 0, message, false)
		return message, nil
	}

	result, err := tool.Run(ctx, a.Workspace, call.Function.Arguments)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		message := "error: " + err.Error()
		emit.ToolResult(call.ID, tool.Name, -1, 0, message, false)
		return message, nil
	}

	rendered := renderResult(result)
	emit.ToolResult(call.ID, tool.Name, result.ExitCode, result.Duration.Milliseconds(), truncate(rendered, maxEventContent), len(rendered) > maxEventContent || result.Truncated)

	return truncate(rendered, maxToolContent), nil
}

// renderResult formats the captured output sent back to the model.
func renderResult(result tools.Result) string {
	var content strings.Builder
	_, _ = fmt.Fprintf(&content, "exit code: %d\n", result.ExitCode)
	if result.Stdout != "" {
		_, _ = content.WriteString("stdout:\n")
		_, _ = content.WriteString(result.Stdout)
		if !strings.HasSuffix(result.Stdout, "\n") {
			_ = content.WriteByte('\n')
		}
	}
	if result.Stderr != "" {
		_, _ = content.WriteString("stderr:\n")
		_, _ = content.WriteString(result.Stderr)
		if !strings.HasSuffix(result.Stderr, "\n") {
			_ = content.WriteByte('\n')
		}
	}
	if result.Truncated {
		_, _ = content.WriteString("[output truncated]\n")
	}
	return content.String()
}

// truncate cuts s to at most limit bytes without splitting a UTF-8 rune.
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
