package agent

import (
	"context"

	"harper/internal/llm"
)

// SubtaskReporter receives each step of a Delegate call's subtask loop,
// tagged with the ID of the Delegate tool call it belongs to — multiple
// Delegate calls can run concurrently (see Loop.Run's fan-out), so the ID
// is how a listener tells their steps apart.
type SubtaskReporter func(toolCallID string, message llm.Message)

type subtaskReporterKey struct{}

func WithSubtaskReporter(ctx context.Context, r SubtaskReporter) context.Context {
	return context.WithValue(ctx, subtaskReporterKey{}, r)
}

func subtaskReporterFromContext(ctx context.Context) (SubtaskReporter, bool) {
	r, ok := ctx.Value(subtaskReporterKey{}).(SubtaskReporter)
	return r, ok
}

type toolCallIDKey struct{}

func WithToolCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

func toolCallIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(toolCallIDKey{}).(string)
	return id, ok
}
