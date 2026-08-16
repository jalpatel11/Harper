package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	Usage      *Usage // only set on assistant messages, and only when the provider reports it
}

type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Response struct {
	Message Message
}

type Provider interface {
	Complete(ctx context.Context, messages []Message, tools []ToolDef) (Response, error)
}
