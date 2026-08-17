package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"harper/internal/config"
	"harper/internal/llm"
	"harper/internal/tools"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type session interface {
	ListTools(ctx context.Context) ([]llm.ToolDef, error)
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
}

type mcpTool struct {
	namespacedName string
	underlyingName string
	description    string
	schema         map[string]any
	sess           session
}

func (t *mcpTool) Name() string        { return t.namespacedName }
func (t *mcpTool) Description() string { return t.description }
func (t *mcpTool) InputSchema() map[string]any {
	if t.schema != nil {
		return t.schema
	}
	return map[string]any{"type": "object"}
}

func (t *mcpTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	return t.sess.CallTool(ctx, t.underlyingName, input)
}

func MergeTools(
	ctx context.Context,
	servers []config.MCPServerConfig,
	connect func(context.Context, config.MCPServerConfig) (session, error),
) ([]tools.Tool, error) {
	var merged []tools.Tool

	for _, srv := range servers {
		sess, err := connect(ctx, srv)
		if err != nil {
			return nil, fmt.Errorf("mcp: connect to %s: %w", srv.Name, err)
		}

		remoteTools, err := sess.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("mcp: list tools on %s: %w", srv.Name, err)
		}

		for _, rt := range remoteTools {
			merged = append(merged, &mcpTool{
				namespacedName: fmt.Sprintf("mcp_%s_%s", srv.Name, rt.Name),
				underlyingName: rt.Name,
				description:    rt.Description,
				schema:         rt.Parameters,
				sess:           sess,
			})
		}
	}

	return merged, nil
}

// schemaToParameters converts the go-sdk's InputSchema (declared as `any`;
// on the client side it already holds a map[string]any, but this handles
// any JSON-marshalable value defensively) into the raw map[string]any shape
// llm.ToolDef.Parameters expects, via a JSON marshal/unmarshal round trip.
func schemaToParameters(schema any) map[string]any {
	if schema == nil {
		return nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var params map[string]any
	if err := json.Unmarshal(data, &params); err != nil {
		return nil
	}
	return params
}

type sdkSession struct {
	cs *sdkmcp.ClientSession
}

func (s *sdkSession) ListTools(ctx context.Context) ([]llm.ToolDef, error) {
	result, err := s.cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	var defs []llm.ToolDef
	for _, t := range result.Tools {
		defs = append(defs, llm.ToolDef{Name: t.Name, Description: t.Description, Parameters: schemaToParameters(t.InputSchema)})
	}
	return defs, nil
}

func (s *sdkSession) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	result, err := s.cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	var out string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			out += tc.Text
		}
	}
	return out, nil
}

func Connect(ctx context.Context, cfg config.MCPServerConfig) (session, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "harper", Version: "0.1.0"}, nil)
	transport := &sdkmcp.CommandTransport{Command: exec.CommandContext(ctx, cfg.Command, cfg.Args...)}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect to %s: %w", cfg.Name, err)
	}
	return &sdkSession{cs: cs}, nil
}
