package session

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"harper/internal/config"
	"harper/internal/llm"
)

// Session is everything a resumed conversation needs to pick up where it
// left off: the raw message history both RunREPL and the TUI's Model
// thread through Loop.Run unchanged, plus which brain model/mode was
// active when it was saved.
type Session struct {
	History []llm.Message      `json:"history"`
	Brain   config.ModelConfig `json:"brain"`
	Mode    string             `json:"mode"`
	SavedAt time.Time          `json:"saved_at"`
}

// sessionsRoot returns the directory session files are stored under.
// Overridden in tests via SetSessionsRoot, so tests never touch a real
// user's home directory.
var sessionsRoot = defaultSessionsRoot

func defaultSessionsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("session: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".harper", "sessions"), nil
}

// SetSessionsRoot overrides the directory session files are stored under.
// Exported for tests in other packages (cmd/harper, cmd/harper/tui) that
// exercise Save/Load indirectly through RunREPL/RunTUI and must not touch
// a real ~/.harper/sessions directory. Not safe for concurrent use across
// parallel tests in the same process.
func SetSessionsRoot(dir string) {
	sessionsRoot = func() (string, error) { return dir, nil }
}

// sessionPath maps a working directory to its session file: the absolute
// path's sha256 hex digest, under sessionsRoot. Hashing sidesteps any
// escaping/length concerns a literal path-derived filename would raise,
// and guarantees one file per distinct project directory.
func sessionPath(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("session: resolve %q: %w", dir, err)
	}
	root, err := sessionsRoot()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(root, fmt.Sprintf("%x.json", sum)), nil
}

// Save writes s as the session for the working directory dir, creating
// sessionsRoot on first use.
func Save(dir string, s Session) error {
	path, err := sessionPath(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("session: create sessions directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("session: write: %w", err)
	}
	return nil
}

// Load reads the session for dir. found is false with a nil error only
// when no session has ever been saved for dir — that's the expected,
// common case, not an error. A session file that exists but can't be read
// or parsed returns a non-nil error so the caller can warn about it
// specifically, while still treating it as "nothing to resume" (found is
// always false alongside a non-nil error) rather than blocking startup.
func Load(dir string) (Session, bool, error) {
	path, err := sessionPath(dir)
	if err != nil {
		return Session{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Session{}, false, nil
		}
		return Session{}, false, fmt.Errorf("session: read %s: %w", path, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, false, fmt.Errorf("session: parse %s: %w", path, err)
	}
	return s, true, nil
}
