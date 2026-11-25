package events_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/WywySenarios/wc-harness/src/events"
)

// wire shape of one JSONL event
type jsonRecord struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id"`
	Model      string `json:"model"`
	Text       string `json:"text"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Args       string `json:"args"`
	Exit       *int   `json:"exit"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	Message    string `json:"message"`
	OK         *bool  `json:"ok"`
}

func decodeRecord(t *testing.T, line string) jsonRecord {
	t.Helper()
	var record jsonRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	return record
}

func TestJSONLSessionStart(t *testing.T) {
	const (
		sessionID = "ex_session"
		model     = "ex_model"
	)
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).SessionStart(sessionID, model)

	record := decodeRecord(t, out.String())
	if record.Type != "session_start" {
		t.Errorf("type = %q, want %q", record.Type, "session_start")
	}
	if record.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", record.SessionID, sessionID)
	}
	if record.Model != model {
		t.Errorf("model = %q, want %q", record.Model, model)
	}
}

func TestJSONLAssistantDelta(t *testing.T) {
	const text = "ex-text"
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).AssistantDelta(text)

	record := decodeRecord(t, out.String())
	if record.Type != "assistant_delta" {
		t.Errorf("type = %q, want %q", record.Type, "assistant_delta")
	}
	if record.Text != text {
		t.Errorf("text = %q, want %q", record.Text, text)
	}
}

func TestJSONLAssistantMessage(t *testing.T) {
	const text = "ex-text"
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).AssistantMessage(text)

	record := decodeRecord(t, out.String())
	if record.Type != "assistant_message" {
		t.Errorf("type = %q, want %q", record.Type, "assistant_message")
	}
	if record.Text != text {
		t.Errorf("text = %q, want %q", record.Text, text)
	}
}

func TestJSONLToolCall(t *testing.T) {
	const (
		callID = "ex_tool_call"
		name   = "ex-tool"
		args   = "ex-args"
	)
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).ToolCall(callID, name, args)

	record := decodeRecord(t, out.String())
	if record.Type != "tool_call" {
		t.Errorf("type = %q, want %q", record.Type, "tool_call")
	}
	if record.ID != callID {
		t.Errorf("id = %q, want %q", record.ID, callID)
	}
	if record.Name != name {
		t.Errorf("name = %q, want %q", record.Name, name)
	}
	if record.Args != args {
		t.Errorf("args = %q, want %q", record.Args, args)
	}
}

func TestJSONLToolResult(t *testing.T) {
	const (
		callID   = "ex_tool_call"
		name     = "ex-tool"
		output   = "ex-output"
		exitCode = 0
	)
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).ToolResult(callID, name, exitCode, 12, output, false)

	record := decodeRecord(t, out.String())
	if record.Type != "tool_result" {
		t.Errorf("type = %q, want %q", record.Type, "tool_result")
	}
	if record.ID != callID {
		t.Errorf("id = %q, want %q", record.ID, callID)
	}
	if record.Name != name {
		t.Errorf("name = %q, want %q", record.Name, name)
	}
	if record.Exit == nil {
		t.Fatal("exit is omitted, must be present even when zero")
	}
	if *record.Exit != exitCode {
		t.Errorf("exit = %d, want %d", *record.Exit, exitCode)
	}
	if record.DurationMS != 12 {
		t.Errorf("duration_ms = %d, want %d", record.DurationMS, 12)
	}
	if record.Output != output {
		t.Errorf("output = %q, want %q", record.Output, output)
	}
}

func TestJSONLError(t *testing.T) {
	const message = "ex-error"
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).Error(message)

	record := decodeRecord(t, out.String())
	if record.Type != "error" {
		t.Errorf("type = %q, want %q", record.Type, "error")
	}
	if record.Message != message {
		t.Errorf("message = %q, want %q", record.Message, message)
	}
}

func TestJSONLSessionEnd(t *testing.T) {
	const ok = false
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).SessionEnd(ok)

	record := decodeRecord(t, out.String())
	if record.Type != "session_end" {
		t.Errorf("type = %q, want %q", record.Type, "session_end")
	}
	if record.OK == nil {
		t.Fatal("ok is omitted, must be present even when false")
	}
	if *record.OK != ok {
		t.Errorf("ok = %v, want %v", *record.OK, ok)
	}
}

func TestJSONLAssistantDeltaSkipsEmpty(t *testing.T) {
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).AssistantDelta("")
	if out.Len() != 0 {
		t.Errorf("empty delta produced output: %q", out.String())
	}
}

func TestJSONLAssistantMessageSkipsEmpty(t *testing.T) {
	var out bytes.Buffer
	events.New("jsonl", &out, io.Discard).AssistantMessage("")
	if out.Len() != 0 {
		t.Errorf("empty message produced output: %q", out.String())
	}
}

func TestTextSessionStart(t *testing.T) {
	var out, log bytes.Buffer
	events.New("text", &out, &log).SessionStart("ex_session", "ex_model")
	if out.Len() != 0 || log.Len() != 0 {
		t.Errorf("session start wrote stdout=%q stderr=%q, want no output", out.String(), log.String())
	}
}

func TestTextSessionEnd(t *testing.T) {
	var out, log bytes.Buffer
	events.New("text", &out, &log).SessionEnd(true)
	if out.Len() != 0 || log.Len() != 0 {
		t.Errorf("session end wrote stdout=%q stderr=%q, want no output", out.String(), log.String())
	}
}

func TestTextAssistantDelta(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "text", text: "ex-text", want: "ex-text"},
		{name: "empty", text: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, log bytes.Buffer
			events.New("text", &out, &log).AssistantDelta(tt.text)
			if out.String() != tt.want {
				t.Errorf("stdout = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestTextAssistantMessage(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "text", text: "ex-text", want: "\n"},
		{name: "empty", text: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, log bytes.Buffer
			events.New("text", &out, &log).AssistantMessage(tt.text)
			if out.String() != tt.want {
				t.Errorf("stdout = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestTextToolCall(t *testing.T) {
	const (
		name = "ex-tool"
		args = "ex-args"
		want = "[tool] ex-tool ex-args"
	)
	var out, log bytes.Buffer
	events.New("text", &out, &log).ToolCall("ex_tool_call", name, args)

	lines := strings.Split(log.String(), "\n")
	if len(lines) < 1 || lines[0] != want {
		t.Errorf("stderr = %q, want line %q", log.String(), want)
	}
}

func TestTextToolResult(t *testing.T) {
	const (
		name = "ex-tool"
		want = "[tool] ex-tool exit 0 (12ms)"
	)
	var out, log bytes.Buffer
	events.New("text", &out, &log).ToolResult("ex_tool_call", name, 0, 12, "ex-output", false)

	lines := strings.Split(log.String(), "\n")
	if len(lines) < 1 || lines[0] != want {
		t.Errorf("stderr = %q, want line %q", log.String(), want)
	}
}

func TestTextError(t *testing.T) {
	const want = "wc-harness: ex-error"
	var out, log bytes.Buffer
	events.New("text", &out, &log).Error("ex-error")

	lines := strings.Split(log.String(), "\n")
	if len(lines) < 1 || lines[0] != want {
		t.Errorf("stderr = %q, want line %q", log.String(), want)
	}
}
