package worker

import (
	"context"
	"testing"
)

// TestGitCmd guards against a regression where gitCmd recursively called itself
// (instead of exec.CommandContext), which compiled fine but blew the stack at
// runtime on the first real git invocation. Calling gitCmd here would overflow
// the stack if that recursion ever returns.
func TestGitCmd(t *testing.T) {
	cmd := gitCmd(context.Background(), "version")
	if cmd == nil {
		t.Fatal("gitCmd returned nil")
	}
	if len(cmd.Args) < 2 || cmd.Args[len(cmd.Args)-1] != "version" {
		t.Fatalf("expected a git command ending in 'version', got args: %v", cmd.Args)
	}
	var hasPromptOff bool
	for _, e := range cmd.Env {
		if e == "GIT_TERMINAL_PROMPT=0" {
			hasPromptOff = true
		}
	}
	if !hasPromptOff {
		t.Error("gitCmd must set GIT_TERMINAL_PROMPT=0 to avoid credential-prompt hangs")
	}
}
