// Package llm is a minimal streaming client for an OpenAI-compatible
// /chat/completions endpoint.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const maxErrorBody = 2 << 10

// Message is one chat message. ToolCallID is set on tool-result messages.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a function call requested by the model.
type ToolCall struct {
	Index    int          `json:"-"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the tool name and its raw JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a function advertised to the model.
type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef is the name, description, and JSON schema of a tool.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Client talks to one upstream endpoint.
type Client struct {
	BaseURL         string
	APIKey          string
	Model           string
	ReasoningEffort string
	HTTP            *http.Client
}

// APIError is an error response from the upstream endpoint.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.StatusCode == 0 {
		return "upstream error: " + e.Message
	}
	return fmt.Sprintf("upstream %d: %s", e.StatusCode, e.Message)
}

type request struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	Tools           []Tool    `json:"tools,omitempty"`
	Stream          bool      `json:"stream"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Stream sends the conversation and streams assistant text to onText as it arrives.
// It returns the complete assistant message, including any tool calls requested by the model.
func (c *Client) Stream(ctx context.Context, messages []Message, tools []Tool, onText func(string)) (Message, error) {
	body, err := json.Marshal(request{
		Model:           c.Model,
		Messages:        messages,
		Tools:           tools,
		Stream:          true,
		ReasoningEffort: c.ReasoningEffort,
	})
	if err != nil {
		return Message{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("call upstream: %w", err)
	}
	// Best effort: the body is closed after it has been read.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if readErr != nil {
			return Message{}, fmt.Errorf("read upstream error body: %w", readErr)
		}
		return Message{}, &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(snippet))}
	}

	assistant := Message{Role: "assistant"}
	calls := make(map[int]*ToolCall)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var text strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}

		var event chunk
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return Message{}, fmt.Errorf("decode stream event: %w", err)
		}
		if event.Error != nil {
			return Message{}, &APIError{Message: event.Error.Message}
		}

		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				text.WriteString(choice.Delta.Content)
				onText(choice.Delta.Content)
			}
			for _, delta := range choice.Delta.ToolCalls {
				call := calls[delta.Index]
				if call == nil {
					call = &ToolCall{Index: delta.Index, Type: "function"}
					calls[delta.Index] = call
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				if delta.Type != "" {
					call.Type = delta.Type
				}
				call.Function.Name += delta.Function.Name
				call.Function.Arguments += delta.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Message{}, fmt.Errorf("read stream: %w", err)
	}

	assistant.Content = text.String()
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		call := calls[index]
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", index)
		}
		assistant.ToolCalls = append(assistant.ToolCalls, *call)
	}

	return assistant, nil
}
