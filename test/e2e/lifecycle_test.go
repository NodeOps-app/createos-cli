//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
)

func TestLifecycle(t *testing.T) {
	// Step 1: Create sandbox and wait until running.
	sb := newSandbox(t)
	if sb.Status != "running" {
		t.Fatalf("expected status 'running' after create, got %q", sb.Status)
	}

	// Step 2: Get — verify the sandbox is reachable and fields match.
	getCtx, getCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer getCancel()
	getOut, getErr, getCode := runCLICtx(getCtx, "sandbox", "get", sb.ID)
	if getCode != 0 {
		t.Fatalf("sandbox get failed (exit %d)\nstdout: %s\nstderr: %s", getCode, getOut, getErr)
	}
	got := mustJSON[SandboxView](t, getOut)
	if got.ID != sb.ID {
		t.Errorf("get: ID mismatch: want %q, got %q", sb.ID, got.ID)
	}
	if testShape != "" && got.Shape != testShape {
		t.Errorf("get: Shape mismatch: want %q, got %q", testShape, got.Shape)
	}
	if got.Status != "running" {
		t.Errorf("get: expected status 'running', got %q", got.Status)
	}

	// Step 3: Edit — set auto-pause to 30m (1800 seconds).
	editCtx, editCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer editCancel()
	editOut, editErr, editCode := runCLICtx(editCtx, "sandbox", "edit", sb.ID, "--auto-pause", "30m")
	if editCode != 0 {
		t.Fatalf("sandbox edit failed (exit %d)\nstdout: %s\nstderr: %s", editCode, editOut, editErr)
	}
	edited := mustJSON[SandboxView](t, editOut)
	if edited.AutoPauseAfterSeconds == nil {
		t.Fatalf("edit: AutoPauseAfterSeconds is nil; expected 1800")
	}
	if *edited.AutoPauseAfterSeconds != 1800 {
		t.Errorf("edit: AutoPauseAfterSeconds: want 1800, got %d", *edited.AutoPauseAfterSeconds)
	}

	// Step 4: Pause — command polls internally and renders JSON when not on TTY.
	pauseCtx, pauseCancel := context.WithTimeout(context.Background(), createTimeout)
	defer pauseCancel()
	pauseOut, pauseErr, pauseCode := runCLICtx(pauseCtx, "sandbox", "pause", sb.ID)
	if pauseCode != 0 {
		t.Fatalf("sandbox pause failed (exit %d)\nstdout: %s\nstderr: %s", pauseCode, pauseOut, pauseErr)
	}
	paused := mustJSON[SandboxView](t, pauseOut)
	if paused.Status != "paused" {
		t.Errorf("pause: expected status 'paused', got %q", paused.Status)
	}

	// Step 5: Resume — command polls internally and renders JSON when not on TTY.
	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), createTimeout)
	defer resumeCancel()
	resumeOut, resumeErr, resumeCode := runCLICtx(resumeCtx, "sandbox", "resume", sb.ID)
	if resumeCode != 0 {
		t.Fatalf("sandbox resume failed (exit %d)\nstdout: %s\nstderr: %s", resumeCode, resumeOut, resumeErr)
	}
	resumed := mustJSON[SandboxView](t, resumeOut)
	if resumed.Status != "running" {
		t.Errorf("resume: expected status 'running', got %q", resumed.Status)
	}

	// Step 6: Fork — command polls until running (default) and renders JSON.
	// Note: sandbox/fork does not expose a --name flag; the server assigns a name.
	forkName := fmt.Sprintf("e2e-%s-fork", runID)
	_ = forkName // no --name flag; kept for tracing only
	forkCtx, forkCancel := context.WithTimeout(context.Background(), createTimeout)
	defer forkCancel()
	forkOut, forkErr, forkCode := runCLICtx(forkCtx, "sandbox", "fork", sb.ID)
	if forkCode != 0 {
		t.Fatalf("sandbox fork failed (exit %d)\nstdout: %s\nstderr: %s", forkCode, forkOut, forkErr)
	}
	forked := mustJSON[SandboxView](t, forkOut)
	if forked.ID == sb.ID {
		t.Errorf("fork: expected new sandbox ID, got same ID %q", forked.ID)
	}
	// Register cleanup for the forked sandbox.
	t.Cleanup(func() {
		runCLI("sandbox", "rm", "--force", forked.ID) //nolint:errcheck
	})
	// Fork auto-resumes by default; wait to confirm it is running.
	waitRunning(t, forked.ID)

	// Step 7: Rm — explicitly delete the original sandbox.
	rmCtx, rmCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer rmCancel()
	rmOut, rmErr, rmCode := runCLICtx(rmCtx, "sandbox", "rm", "--force", sb.ID)
	if rmCode != 0 {
		t.Fatalf("sandbox rm failed (exit %d)\nstdout: %s\nstderr: %s", rmCode, rmOut, rmErr)
	}
	// Verify removal: get should fail with a non-zero exit (404).
	verCtx, verCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer verCancel()
	_, _, verCode := runCLICtx(verCtx, "sandbox", "get", sb.ID)
	if verCode == 0 {
		t.Errorf("expected non-zero exit after rm, but sandbox get succeeded for %q", sb.ID)
	}
}
