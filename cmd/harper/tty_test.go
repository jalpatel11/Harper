package main

import (
	"os"
	"testing"
)

func TestIsInteractiveTerminal_FalseForPipes(t *testing.T) {
	// A pipe is never a terminal — this is the one thing we can assert
	// deterministically without a real pty. It's also exactly the case
	// the fallback exists to protect: piped stdin, piped stdout.
	origStdin, origStdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = origStdin, origStdout }()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer outR.Close()
	defer outW.Close()

	os.Stdin = inR
	os.Stdout = outW

	if isInteractiveTerminal() {
		t.Fatalf("expected false when stdin/stdout are pipes, not terminals")
	}
}
