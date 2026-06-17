//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Timeouts used across the suite.
const (
	createTimeout   = 240 * time.Second
	execTimeout     = 90 * time.Second
	pushPullTimeout = 90 * time.Second
	defaultTimeout  = 60 * time.Second
	waitRunningCap  = 5 * time.Minute
)

// SandboxView is a local minimal projection of the sandbox JSON returned by
// `sandbox get -o json`. Kept here so we don't import internal/api.
type SandboxView struct {
	ID                    string     `json:"id"`
	Name                  *string    `json:"name,omitempty"`
	Status                string     `json:"status"`
	Shape                 string     `json:"shape,omitempty"`
	IP                    *string    `json:"ip,omitempty"`
	IngressEnabled        bool       `json:"ingress_enabled,omitempty"`
	AutoPauseAfterSeconds *int       `json:"auto_pause_after_seconds,omitempty"`
	VCPU                  int        `json:"vcpu"`
	MemMib                int        `json:"mem_mib"`
	DiskMib               int64      `json:"disk_mib"`
	Rootfs                *string    `json:"rootfs,omitempty"`
	Region                string     `json:"region"`
	Egress                []string   `json:"egress,omitempty"`
	CreatedAt             *time.Time `json:"created_at,omitempty"`
}

// buildBinary compiles the CLI into the given path. Returns any combined
// output and a non-nil error on failure.
func buildBinary(dest string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// #nosec G204 -- dest is a controlled temp path, args are hardcoded
	cmd := exec.CommandContext(ctx, "go", "build", "-o", dest, ".")
	cmd.Dir = projectRoot()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

// projectRoot returns the module root by walking upward from this file's
// directory. At test time the working directory is the package directory
// (test/e2e), so we go up two levels.
func projectRoot() string {
	// __file__ is not available at runtime; use os.Getwd as a proxy.
	// `go test` sets the working directory to the package directory.
	wd, _ := os.Getwd()
	// test/e2e → go up two levels to reach module root.
	// Walk parent until we find go.mod.
	dir := wd
	for {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}
		parent := dir[:strings.LastIndex(dir, "/")]
		if parent == dir {
			return wd
		}
		dir = parent
	}
}

// runCLI executes the compiled binary with args using a default 60-second
// timeout. Returns stdout, stderr, and the process exit code.
func runCLI(args ...string) (stdout, stderr string, exitCode int) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	return runCLICtx(ctx, args...)
}

// runCLICtx executes the compiled binary with an explicit context.
func runCLICtx(ctx context.Context, args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.CommandContext(ctx, binaryPath, args...) // #nosec G204,G702 -- binaryPath is a controlled build artifact; args are test-supplied CLI flags

	// Inherit the current process environment but replace HOME so the test
	// suite never touches the real ~/.createos directory.
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+testHomeDir)
	cmd.Env = env

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return outBuf.String(), errBuf.String(), ee.ExitCode()
		}
		// Context deadline / other OS error — treat as exit 1.
		return outBuf.String(), errBuf.String(), 1
	}
	return outBuf.String(), errBuf.String(), 0
}

// mustJSON decodes JSON string s into T. Calls t.Fatalf on any error.
func mustJSON[T any](t *testing.T, s string) T { //nolint:unused // called by future test files in this package
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("mustJSON: %v\ninput: %s", err, s)
	}
	return v
}

// newSandbox creates a sandbox named e2e-<runID>-<testName>, waits until it
// is running, registers cleanup, and returns the parsed SandboxView.
// Callers may pass extra CLI flags (e.g. "--ingress") via extraArgs.
func newSandbox(t *testing.T, extraArgs ...string) SandboxView { //nolint:unused // called by future test files in this package
	t.Helper()

	// Sanitise t.Name(): replace any characters that the API rejects in names.
	safeName := sanitiseName(t.Name())
	name := fmt.Sprintf("e2e-%s-%s", runID, safeName)

	args := []string{"sandbox", "create", "--name", name}
	if testShape != "" {
		args = append(args, "--shape", testShape)
	}
	if testRootfs != "" {
		args = append(args, "--rootfs", testRootfs)
	}
	args = append(args, extraArgs...)

	ctx, cancel := context.WithTimeout(context.Background(), createTimeout)
	defer cancel()

	stdout, stderr, code := runCLICtx(ctx, args...)
	if code != 0 {
		t.Fatalf("sandbox create failed (exit %d)\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// Parse the create response to extract the ID.
	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &createResp); err != nil {
		t.Fatalf("sandbox create: could not parse JSON response: %v\noutput: %s", err, stdout)
	}
	if createResp.ID == "" {
		t.Fatalf("sandbox create: response has empty id\noutput: %s", stdout)
	}
	id := createResp.ID

	// Register cleanup before waiting so the sandbox is always removed even
	// if waitRunning or the test itself fails.
	t.Cleanup(func() {
		_, _, _ = runCLI("sandbox", "rm", "--force", id)
	})

	waitRunning(t, id)

	getOut, getErr, getCode := runCLI("sandbox", "get", id)
	if getCode != 0 {
		t.Fatalf("sandbox get failed after create (exit %d)\nstdout: %s\nstderr: %s", getCode, getOut, getErr)
	}
	return mustJSON[SandboxView](t, getOut)
}

// waitRunning polls `sandbox get -o json <id>` every 2 seconds until
// status == "running", up to waitRunningCap. Calls t.Fatalf on timeout.
func waitRunning(t *testing.T, id string) { //nolint:unused // called by newSandbox and future test files in this package
	t.Helper()

	deadline := time.Now().Add(waitRunningCap)
	var lastStatus string
	for time.Now().Before(deadline) {
		stdout, _, code := runCLI("sandbox", "get", id)
		if code == 0 {
			var view struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(stdout), &view); err == nil {
				lastStatus = view.Status
				if view.Status == "running" {
					return
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("waitRunning: sandbox %s did not reach 'running' within %s (last status: %q)", id, waitRunningCap, lastStatus)
}

// sanitiseName converts a test name (e.g. "TestFoo/subtest") into a string
// safe for use in a sandbox name: lowercase alphanumeric + hyphens, max 40 chars.
func sanitiseName(name string) string { //nolint:unused // called by newSandbox
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	// Collapse runs of hyphens.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
