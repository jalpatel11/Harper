package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedWalk_StopsAtMaxEntries(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		os.WriteFile(filepath.Join(dir, string(rune('a'+i))+".txt"), []byte(""), 0o644)
	}

	visited := 0
	truncated, err := boundedWalk(context.Background(), dir, 5, time.Minute, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			visited++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("boundedWalk: %v", err)
	}
	if !truncated {
		t.Fatalf("expected truncated=true when maxEntries is exceeded")
	}
	if visited > 5 {
		t.Fatalf("expected at most 5 files visited, got %d", visited)
	}
}

func TestBoundedWalk_StopsAtTimeout(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, string(rune('a'+i))+".txt"), []byte(""), 0o644)
	}

	truncated, err := boundedWalk(context.Background(), dir, 100000, time.Nanosecond, func(path string, info os.FileInfo, err error) error {
		return nil
	})
	if err != nil {
		t.Fatalf("boundedWalk: %v", err)
	}
	if !truncated {
		t.Fatalf("expected truncated=true when the timeout has already elapsed")
	}
}

func TestBoundedWalk_CompletesNormallyWithinBounds(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte(""), 0o644)

	visited := 0
	truncated, err := boundedWalk(context.Background(), dir, 100000, time.Minute, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			visited++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("boundedWalk: %v", err)
	}
	if truncated {
		t.Fatalf("did not expect truncation for a small walk well within bounds")
	}
	if visited != 2 {
		t.Fatalf("expected 2 files visited, got %d", visited)
	}
}
