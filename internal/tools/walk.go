package tools

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

const (
	maxWalkEntries = 10000
	walkTimeout    = 10 * time.Second
)

// boundedWalk wraps filepath.Walk with a maximum entry count and a
// wall-clock timeout, so a request to search a very large or slow directory
// tree (e.g. the filesystem root) can't hang or consume unbounded resources.
// Returns whether the walk was stopped early due to hitting a bound.
func boundedWalk(ctx context.Context, root string, maxEntries int, timeout time.Duration, fn filepath.WalkFunc) (truncated bool, err error) {
	deadline := time.Now().Add(timeout)
	visited := 0

	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if ctx.Err() != nil || time.Now().After(deadline) || visited >= maxEntries {
			truncated = true
			return filepath.SkipAll
		}
		visited++
		return fn(path, info, walkErr)
	})
	return truncated, err
}
