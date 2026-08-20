package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	anthropicBaseURL    = "https://api.anthropic.com"
	anthropicVersion    = "2023-06-01"
	anthropicDefaultMax = 8192
)

// AnthropicProvider talks to Anthropic's Messages API. effort is accepted
// for interface parity with the other providers but currently unused —
// mapping it to extended thinking would also require carrying thinking
// blocks back through tool-result turns, which this minimal implementation
// doesn't do yet.
type AnthropicProvider struct {
	baseURL string
	apiKey  string
	model   string
	effort  string
	client  *http.Client
}

func NewAnthropicProvider(apiKey, model, effort string) *AnthropicProvider {
	return &AnthropicProvider{
		baseURL: anthropicBaseURL,
		apiKey:  apiKey,
		model:   model,
		effort:  effort,
		client:  &http.Client{Timeout: defaultRequestTimeout},
	}
}

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicResponse struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
	Usage   anthropicUsage          `json:"usage"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, messages []Message, tools []ToolDef) (Response, error) {
	req := anthropicRequest{Model: p.model, MaxTokens: anthropicDefaultMax}

	for _, m := range messages {
		if m.Role == RoleSystem {
			req.System = m.Content
			continue
		}
		req.Messages = append(req.Messages, toAnthropicMessage(m))
	}

	for _, t := range tools {
		req.Tools = append(req.Tools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build anthropic request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call anthropic: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read anthropic response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("anthropic returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return Response{}, fmt.Errorf("decode anthropic response: %w", err)
	}

	msg := Message{Role: RoleAssistant}
	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			msg.Content += block.Text
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: block.ID, Name: block.Name, Args: block.Input})
		}
	}
	if apiResp.Usage.InputTokens > 0 || apiResp.Usage.OutputTokens > 0 {
		msg.Usage = &Usage{PromptTokens: apiResp.Usage.InputTokens, CompletionTokens: apiResp.Usage.OutputTokens}
	}
	return Response{Message: msg}, nil
}

// toAnthropicMessage maps a single Harper message to Anthropic's format.
// Harper's RoleTool (a tool's result) has no equivalent role in the
// Messages API — Anthropic represents tool results as a "user" message
// carrying a tool_result content block instead.
func toAnthropicMessage(m Message) anthropicMessage {
	if m.Role == RoleTool {
		return anthropicMessage{
			Role: "user",
			Content: []anthropicContentBlock{
				{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content},
			},
		}
	}

	am := anthropicMessage{Role: string(m.Role)}
	if m.Content != "" {
		am.Content = append(am.Content, anthropicContentBlock{Type: "text", Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		am.Content = append(am.Content, anthropicContentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: tc.Args})
	}
	return am
}
