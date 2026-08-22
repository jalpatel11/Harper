package main

import (
	"os"

	"golang.org/x/term"
)

// isInteractiveTerminal reports whether both stdin and stdout are real
// terminals. Checking stdout alone is not enough: `echo "x" | harper` or
// `harper < input.txt` has a real stdout but a piped stdin, and launching
// the TUI in that situation hangs or garbles input — Bubble Tea takes over
// the display while trying to read keystrokes from a fixed byte stream.
func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
