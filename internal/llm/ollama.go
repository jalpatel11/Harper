package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type OllamaProvider struct {
	baseURL string
	model   string
	numCtx  int
	client  *http.Client
}

func NewOllamaProvider(baseURL, model string, numCtx int) *OllamaProvider {
	return &OllamaProvider{baseURL: baseURL, model: model, numCtx: numCtx, client: http.DefaultClient}
}

type ollamaFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ollamaTool struct {
	Type     string         `json:"type"`
	Function ollamaFunction `json:"function"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

func (p *OllamaProvider) Complete(ctx context.Context, messages []Message, tools []ToolDef) (Response, error) {
	req := ollamaChatRequest{
		Model:   p.model,
		Stream:  false,
		Options: map[string]any{"num_ctx": p.numCtx},
	}
	for _, m := range messages {
		om := ollamaMessage{Role: string(m.Role), Content: m.Content}
		for _, tc := range m.ToolCalls {
			var call ollamaToolCall
			call.Function.Name = tc.Name
			call.Function.Arguments = tc.Args
			om.ToolCalls = append(om.ToolCalls, call)
		}
		req.Messages = append(req.Messages, om)
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, ollamaTool{
			Type:     "function",
			Function: ollamaFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("call ollama: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("ollama returned status %d", httpResp.StatusCode)
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&chatResp); err != nil {
		return Response{}, fmt.Errorf("decode ollama response: %w", err)
	}

	msg := Message{Role: RoleAssistant, Content: chatResp.Message.Content}
	if chatResp.PromptEvalCount > 0 || chatResp.EvalCount > 0 {
		msg.Usage = &Usage{PromptTokens: chatResp.PromptEvalCount, CompletionTokens: chatResp.EvalCount}
	}
	for i, tc := range chatResp.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:   fmt.Sprintf("call_%d", i),
			Name: tc.Function.Name,
			Args: tc.Function.Arguments,
		})
	}
	return Response{Message: msg}, nil
}
