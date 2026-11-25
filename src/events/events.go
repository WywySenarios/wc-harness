// Package events renders agent progress as either human text or JSONL.
package events

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// Emitter receives the progress of one run.
type Emitter interface {
	// SessionStart reports the session id and model at the start of a run.
	SessionStart(sessionID, model string)
	// AssistantDelta reports a streamed fragment of assistant text.
	AssistantDelta(text string)
	// AssistantMessage reports the final assistant text for one turn.
	AssistantMessage(text string)
	// ToolCall reports a tool invocation.
	ToolCall(id, name, args string)
	// ToolResult reports the outcome of a tool invocation.
	ToolResult(id, name string, exit int, durationMS int64, output string, truncated bool)
	// Error reports a run failure.
	Error(message string)
	// SessionEnd reports whether the run succeeded.
	SessionEnd(ok bool)
}

// New returns a text emitter for "jsonl" and a human emitter otherwise.
func New(mode string, out, log io.Writer) Emitter {
	if mode == "jsonl" {
		return &jsonEmitter{enc: json.NewEncoder(out)}
	}
	return &textEmitter{out: out, log: log}
}

type record struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id,omitempty"`
	Model      string `json:"model,omitempty"`
	Text       string `json:"text,omitempty"`
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	Args       string `json:"args,omitempty"`
	Exit       *int   `json:"exit,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Output     string `json:"output,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	Message    string `json:"message,omitempty"`
	OK         *bool  `json:"ok,omitempty"`
}

type jsonEmitter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (e *jsonEmitter) emit(value record) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Best effort: the event stream has no error channel, so a write failure
	// cannot be reported to the caller.
	_ = e.enc.Encode(value)
}

func (e *jsonEmitter) SessionStart(sessionID, model string) {
	e.emit(record{Type: "session_start", SessionID: sessionID, Model: model})
}

func (e *jsonEmitter) AssistantDelta(text string) {
	if text == "" {
		return
	}
	e.emit(record{Type: "assistant_delta", Text: text})
}

func (e *jsonEmitter) AssistantMessage(text string) {
	if text == "" {
		return
	}
	e.emit(record{Type: "assistant_message", Text: text})
}

func (e *jsonEmitter) ToolCall(id, name, args string) {
	e.emit(record{Type: "tool_call", ID: id, Name: name, Args: args})
}

func (e *jsonEmitter) ToolResult(id, name string, exit int, durationMS int64, output string, truncated bool) {
	e.emit(record{
		Type:       "tool_result",
		ID:         id,
		Name:       name,
		Exit:       &exit,
		DurationMS: durationMS,
		Output:     output,
		Truncated:  truncated,
	})
}

func (e *jsonEmitter) Error(message string) {
	e.emit(record{Type: "error", Message: message})
}

func (e *jsonEmitter) SessionEnd(ok bool) {
	e.emit(record{Type: "session_end", OK: &ok})
}

type textEmitter struct {
	out io.Writer
	log io.Writer
}

func (e *textEmitter) SessionStart(string, string) {}

func (e *textEmitter) AssistantDelta(text string) {
	// Best effort: stdout is the process output.
	_, _ = io.WriteString(e.out, text)
}

func (e *textEmitter) AssistantMessage(text string) {
	if text != "" {
		// Best effort: stdout is the process output.
		_, _ = fmt.Fprintln(e.out)
	}
}

func (e *textEmitter) ToolCall(_ string, name, args string) {
	// Best effort: stderr is the process log.
	_, _ = fmt.Fprintf(e.log, "[tool] %s %s\n", name, args)
}

func (e *textEmitter) ToolResult(_ string, name string, exit int, durationMS int64, _ string, _ bool) {
	// Best effort: stderr is the process log.
	_, _ = fmt.Fprintf(e.log, "[tool] %s exit %d (%s)\n", name, exit, (time.Duration(durationMS) * time.Millisecond).Round(time.Millisecond))
}

func (e *textEmitter) Error(message string) {
	// Best effort: stderr is the process log.
	_, _ = fmt.Fprintf(e.log, "wc-harness: %s\n", message)
}

func (e *textEmitter) SessionEnd(bool) {}
