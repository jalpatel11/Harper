package agent

import (
	"context"
	"testing"

	"harper/internal/llm"
)

func TestWithSubtaskReporter_RoundTripsThroughContext(t *testing.T) {
	var got []llm.Message
	reporter := SubtaskReporter(func(toolCallID string, m llm.Message) {
		got = append(got, m)
	})

	ctx := WithSubtaskReporter(context.Background(), reporter)
	r, ok := subtaskReporterFromContext(ctx)
	if !ok {
		t.Fatalf("expected a reporter to be found in the context")
	}
	r("call_0", llm.Message{Content: "hi"})
	if len(got) != 1 || got[0].Content != "hi" {
		t.Fatalf("expected the reporter round-tripped through context to be callable, got: %v", got)
	}
}

func TestSubtaskReporterFromContext_FalseWhenNotSet(t *testing.T) {
	_, ok := subtaskReporterFromContext(context.Background())
	if ok {
		t.Fatalf("expected no reporter found on a plain context")
	}
}

func TestWithToolCallID_RoundTripsThroughContext(t *testing.T) {
	ctx := WithToolCallID(context.Background(), "call_42")
	id, ok := toolCallIDFromContext(ctx)
	if !ok || id != "call_42" {
		t.Fatalf("expected call_42 round-tripped through context, got %q, %v", id, ok)
	}
}

func TestToolCallIDFromContext_FalseWhenNotSet(t *testing.T) {
	_, ok := toolCallIDFromContext(context.Background())
	if ok {
		t.Fatalf("expected no tool call ID found on a plain context")
	}
}
