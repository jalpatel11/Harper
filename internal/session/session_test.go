package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"harper/internal/config"
	"harper/internal/llm"
)

func TestSaveThenLoad_RoundTripsHistoryAndConfig(t *testing.T) {
	SetSessionsRoot(t.TempDir())

	want := Session{
		History: []llm.Message{
			{Role: llm.RoleUser, Content: "do the thing"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Delegate", Args: map[string]any{"task": "look something up"}},
			}},
			{Role: llm.RoleTool, Content: "looked it up", ToolCallID: "call_0"},
			{Role: llm.RoleAssistant, Content: "here's the answer"},
		},
		Brain:   config.ModelConfig{Provider: "ollama", Model: "qwen3-coder:30b", Effort: "high"},
		Mode:    "simple",
		SavedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	}

	if err := Save("/some/project/dir", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, found, err := Load("/some/project/dir")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true after a Save")
	}
	if len(got.History) != len(want.History) {
		t.Fatalf("expected %d history messages, got %d", len(want.History), len(got.History))
	}
	if got.History[0].Content != "do the thing" || got.History[0].Role != llm.RoleUser {
		t.Fatalf("unexpected first message: %+v", got.History[0])
	}
	if len(got.History[1].ToolCalls) != 1 || got.History[1].ToolCalls[0].ID != "call_0" || got.History[1].ToolCalls[0].Name != "Delegate" {
		t.Fatalf("expected the tool call round-tripped, got: %+v", got.History[1])
	}
	if got.History[2].ToolCallID != "call_0" || got.History[2].Content != "looked it up" {
		t.Fatalf("expected the tool result round-tripped, got: %+v", got.History[2])
	}
	if got.Brain != want.Brain {
		t.Fatalf("expected Brain round-tripped, got %+v want %+v", got.Brain, want.Brain)
	}
	if got.Mode != want.Mode {
		t.Fatalf("expected Mode round-tripped, got %q want %q", got.Mode, want.Mode)
	}
	if !got.SavedAt.Equal(want.SavedAt) {
		t.Fatalf("expected SavedAt round-tripped, got %v want %v", got.SavedAt, want.SavedAt)
	}
}

func TestLoad_NoSavedSession_ReturnsNotFoundNoError(t *testing.T) {
	SetSessionsRoot(t.TempDir())

	_, found, err := Load("/a/directory/nothing/was/ever/saved/for")
	if err != nil {
		t.Fatalf("expected no error for a directory with no saved session, got: %v", err)
	}
	if found {
		t.Fatalf("expected found=false when nothing was ever saved")
	}
}

func TestLoad_CorruptedFile_ReturnsErrorNotFoundNeverPanics(t *testing.T) {
	SetSessionsRoot(t.TempDir())

	if err := Save("/corrupt/me", Session{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path, err := sessionPath("/corrupt/me")
	if err != nil {
		t.Fatalf("sessionPath: %v", err)
	}
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatalf("corrupt the file: %v", err)
	}

	_, found, err := Load("/corrupt/me")
	if found {
		t.Fatalf("expected found=false for a corrupted file")
	}
	if err == nil {
		t.Fatalf("expected a non-nil error for a corrupted file, so callers can warn about it specifically")
	}
}

func TestSave_DifferentDirectoriesDoNotCollide(t *testing.T) {
	SetSessionsRoot(t.TempDir())

	if err := Save("/project/a", Session{Mode: "mode-a"}); err != nil {
		t.Fatalf("Save a: %v", err)
	}
	if err := Save("/project/b", Session{Mode: "mode-b"}); err != nil {
		t.Fatalf("Save b: %v", err)
	}

	gotA, foundA, err := Load("/project/a")
	if err != nil || !foundA {
		t.Fatalf("Load a: found=%v err=%v", foundA, err)
	}
	gotB, foundB, err := Load("/project/b")
	if err != nil || !foundB {
		t.Fatalf("Load b: found=%v err=%v", foundB, err)
	}
	if gotA.Mode != "mode-a" || gotB.Mode != "mode-b" {
		t.Fatalf("expected distinct sessions per directory, got a=%q b=%q", gotA.Mode, gotB.Mode)
	}
}

func TestSave_CreatesSessionsRootWhenMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	SetSessionsRoot(root)

	if err := Save("/whatever", Session{Mode: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("expected sessionsRoot created by Save, got: %v", err)
	}
}
