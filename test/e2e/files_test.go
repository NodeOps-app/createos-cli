//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesPushPull(t *testing.T) {
	sb := newSandbox(t)

	content := []byte("e2e-test-content-" + runID)
	localFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(localFile, content, 0600); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	// Push local file into sandbox.
	pushCtx, pushCancel := context.WithTimeout(context.Background(), pushPullTimeout)
	defer pushCancel()

	stdout, stderr, code := runCLICtx(pushCtx, "sandbox", "push", sb.ID, localFile, "/tmp/e2e-test.txt")
	if code != 0 {
		t.Fatalf("sandbox push failed (exit %d)\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// Pull the file back to a local path.
	outFile := filepath.Join(t.TempDir(), "out.txt")

	pullCtx, pullCancel := context.WithTimeout(context.Background(), pushPullTimeout)
	defer pullCancel()

	stdout, stderr, code = runCLICtx(pullCtx, "sandbox", "pull", sb.ID, "/tmp/e2e-test.txt", outFile)
	if code != 0 {
		t.Fatalf("sandbox pull failed (exit %d)\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("pulled content mismatch\n got: %q\nwant: %q", got, content)
	}
}

func TestFilesPullToStdout(t *testing.T) {
	sb := newSandbox(t)

	content := []byte("e2e-test-content-" + runID)
	localFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(localFile, content, 0600); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	// Push first so there is something to pull.
	pushCtx, pushCancel := context.WithTimeout(context.Background(), pushPullTimeout)
	defer pushCancel()

	stdout, stderr, code := runCLICtx(pushCtx, "sandbox", "push", sb.ID, localFile, "/tmp/e2e-test.txt")
	if code != 0 {
		t.Fatalf("sandbox push failed (exit %d)\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// Pull to stdout ("-").
	pullCtx, pullCancel := context.WithTimeout(context.Background(), pushPullTimeout)
	defer pullCancel()

	stdout, stderr, code = runCLICtx(pullCtx, "sandbox", "pull", sb.ID, "/tmp/e2e-test.txt", "-")
	if code != 0 {
		t.Fatalf("sandbox pull to stdout failed (exit %d)\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "e2e-test-content-"+runID) {
		t.Fatalf("stdout does not contain expected content\n got: %q\nwant it to contain: %q", stdout, "e2e-test-content-"+runID)
	}
}
