//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
)

// SandboxExecResult mirrors the exec result shape for test assertions.
// The CLI emits raw stdout/stderr (not JSON), so this struct is used
// for internal test logic only — we parse the raw output directly.
type SandboxExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func TestExecSuccess(t *testing.T) {
	sb := newSandbox(t)

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	// sandbox exec emits the command's raw stdout directly (non-JSON).
	stdout, stderr, code := runCLICtx(ctx, "sandbox", "exec", sb.ID, "--", "echo", "hello")
	if code != 0 {
		t.Fatalf("exec exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "hello") {
		t.Fatalf("expected stdout to contain %q, got: %s", "hello", stdout)
	}
}

func TestExecNonZeroExit(t *testing.T) {
	sb := newSandbox(t)

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	// The CLI calls os.Exit with the inner command's exit code, so the
	// process exit code reflects the sandbox command's exit code.
	// We don't assert code == 0 here — just that the CLI preserved exit 42.
	_, _, code := runCLICtx(ctx, "sandbox", "exec", sb.ID, "--", "sh", "-c", "exit 42")
	if code != 42 {
		t.Fatalf("expected CLI exit code 42 (preserved from inner command), got %d", code)
	}
}
