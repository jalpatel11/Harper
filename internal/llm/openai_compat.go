package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OpenAICompatProvider talks to any server implementing the OpenAI
// chat-completions API shape — LM Studio, llama.cpp's server, and vLLM all
// serve this same endpoint, differing only in base URL. effort is accepted
// for interface parity but unused: OpenAI's reasoning_effort parameter
// only applies to specific reasoning models, and these servers can be
// pointed at any locally-loaded model, so sending it unconditionally would
// error against most of them.
type OpenAICompatProvider struct {
	baseURL string
	model   string
	effort  string
	client  *http.Client
}

func NewOpenAICompatProvider(baseURL, model, effort string) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		baseURL: baseURL,
		model:   model,
		effort:  effort,
		client:  &http.Client{Timeout: defaultRequestTimeout},
	}
}

type openaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openaiToolCallFunction `json:"function"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openaiChatRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Tools    []openaiTool    `json:"tools,omitempty"`
}

type openaiChoice struct {
	Message openaiMessage `json:"message"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openaiChatResponse struct {
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
}

func (p *OpenAICompatProvider) Complete(ctx context.Context, messages []Message, tools []ToolDef) (Response, error) {
	req := openaiChatRequest{Model: p.model}

	// Tool-role messages need the tool's name (some OpenAI-compatible
	// servers require it, not just tool_call_id), which Harper's own
	// Message doesn't carry on the result — recover it from the assistant
	// message that made the original call.
	toolCallNames := map[string]string{}
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			toolCallNames[tc.ID] = tc.Name
		}
	}

	for _, m := range messages {
		om := openaiMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == RoleTool {
			om.ToolCallID = m.ToolCallID
			om.Name = toolCallNames[m.ToolCallID]
		}
		for _, tc := range m.ToolCalls {
			argsJSON, err := json.Marshal(tc.Args)
			if err != nil {
				return Response{}, fmt.Errorf("marshal tool call arguments: %w", err)
			}
			om.ToolCalls = append(om.ToolCalls, openaiToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: openaiToolCallFunction{Name: tc.Name, Arguments: string(argsJSON)},
			})
		}
		req.Messages = append(req.Messages, om)
	}

	for _, t := range tools {
		req.Tools = append(req.Tools, openaiTool{
			Type:     "function",
			Function: openaiFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal openai-compat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build openai-compat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call openai-compat endpoint: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read openai-compat response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("openai-compat endpoint returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var apiResp openaiChatResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return Response{}, fmt.Errorf("decode openai-compat response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return Response{}, fmt.Errorf("openai-compat response had no choices")
	}

	choice := apiResp.Choices[0].Message
	msg := Message{Role: RoleAssistant, Content: choice.Content}
	for _, tc := range choice.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return Response{}, fmt.Errorf("decode tool call arguments: %w", err)
			}
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: args})
	}
	if apiResp.Usage.PromptTokens > 0 || apiResp.Usage.CompletionTokens > 0 {
		msg.Usage = &Usage{PromptTokens: apiResp.Usage.PromptTokens, CompletionTokens: apiResp.Usage.CompletionTokens}
	}
	return Response{Message: msg}, nil
}
