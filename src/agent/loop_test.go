package agent_test

import (
	"context"
	"io"
	"testing"

	"github.com/WywySenarios/wc-harness/src/agent"
	"github.com/WywySenarios/wc-harness/src/llm"
	"github.com/WywySenarios/wc-harness/src/tools"
)

// fakeClient records the conversation and answers with one configured message.
type fakeClient struct {
	message  llm.Message
	messages []llm.Message
}

func (f *fakeClient) Stream(_ context.Context, messages []llm.Message, _ []llm.Tool, onText func(string)) (llm.Message, error) {
	f.messages = append(f.messages, messages...)
	onText(f.message.Content)
	return f.message, nil
}

// emitted is one recorded emitter call.
type emitted struct {
	method string
	text   string
}

// fakeEmitter records the events the agent emits.
type fakeEmitter struct {
	calls []emitted
}

func (f *fakeEmitter) SessionStart(string, string) {}

func (f *fakeEmitter) AssistantDelta(text string) {
	f.calls = append(f.calls, emitted{method: "assistant_delta", text: text})
}

func (f *fakeEmitter) AssistantMessage(text string) {
	f.calls = append(f.calls, emitted{method: "assistant_message", text: text})
}

func (f *fakeEmitter) ToolCall(string, string, string) {}

func (f *fakeEmitter) ToolResult(string, string, int, int64, string, bool) {}

func (f *fakeEmitter) Error(string) {}

func (f *fakeEmitter) SessionEnd(bool) {}

// TestRunEmitsAnswer proves Run sends the prompt to the client and emits the
// streamed answer followed by the final message.
func TestRunEmitsAnswer(t *testing.T) {
	const (
		prompt = "ex-prompt"
		answer = "ex-answer"
	)
	client := &fakeClient{message: llm.Message{Role: "assistant", Content: answer}}
	emitter := &fakeEmitter{}
	runner := &agent.Agent{
		Client:   client,
		Tools:    &tools.Registry{},
		MaxTurns: 1,
		Out:      io.Discard,
		Log:      io.Discard,
		Events:   emitter,
	}

	if err := runner.Run(context.Background(), prompt); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(client.messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(client.messages))
	}
	if client.messages[1].Content != prompt {
		t.Errorf("user prompt = %q, want %q", client.messages[1].Content, prompt)
	}
	if len(emitter.calls) != 2 {
		t.Fatalf("emitted = %d, want 2", len(emitter.calls))
	}
	if emitter.calls[0].method != "assistant_delta" || emitter.calls[0].text != answer {
		t.Errorf("first event = %+v, want assistant_delta %q", emitter.calls[0], answer)
	}
	if emitter.calls[1].method != "assistant_message" || emitter.calls[1].text != answer {
		t.Errorf("second event = %+v, want assistant_message %q", emitter.calls[1], answer)
	}
}
