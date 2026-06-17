//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Package-level state set once in TestMain and shared by all test files.
var (
	binaryPath  string
	testHomeDir string
	runID       string
	testShape   string
	testRootfs  string
)

func TestMain(m *testing.M) {
	key := os.Getenv("CREATEOS_E2E_API_KEY")
	if key == "" {
		fmt.Println("CREATEOS_E2E_API_KEY not set — skipping e2e suite")
		os.Exit(0)
	}

	// Isolate the test suite from the real ~/.createos by redirecting HOME.
	tmpDir, err := os.MkdirTemp("", "createos-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	testHomeDir = tmpDir
	os.Setenv("HOME", tmpDir)            // #nosec G104 -- HOME must be set; failure is non-recoverable but unreachable in practice
	os.Setenv("XDG_CONFIG_HOME", tmpDir) // #nosec G104
	os.Setenv("XDG_DATA_HOME", tmpDir)   // #nosec G104

	// Build the binary into the temp dir so tests invoke a known artifact.
	binPath := tmpDir + "/createos"
	binaryPath = binPath
	if out, buildErr := buildBinary(binPath); buildErr != nil {
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n%s\n", buildErr, out)
		os.Exit(1)
	}

	// Authenticate into the temp HOME.
	stdout, stderr, code := runCLI("login", "--token", key)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "login failed (exit %d)\nstdout: %s\nstderr: %s\n", code, stdout, stderr)
		os.Exit(1)
	}

	// Derive a run-scoped ID from crypto/rand so parallel CI runs don't collide.
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		fmt.Fprintf(os.Stderr, "crypto/rand failed: %v\n", err)
		os.Exit(1)
	}
	runID = fmt.Sprintf("%x", b)

	// Discover the smallest available shape (lowest vcpu+mem_mib sum).
	// Honour CREATEOS_E2E_SHAPE override.
	if override := os.Getenv("CREATEOS_E2E_SHAPE"); override != "" {
		testShape = override
	} else {
		testShape = discoverSmallestShape()
	}

	// Honour CREATEOS_E2E_ROOTFS override; otherwise leave empty (server picks default).
	testRootfs = os.Getenv("CREATEOS_E2E_ROOTFS")

	// Pre-sweep: delete any leftover e2e- sandboxes from prior failed runs.
	presweep()

	// Post-sweep: best-effort cleanup after the suite finishes.
	// Called explicitly before os.Exit so defers are not skipped.
	code = m.Run()
	postsweep()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

// discoverSmallestShape calls `sandbox shapes -o json` and returns the shape
// with the lowest vcpu+mem_mib sum. Falls back to empty string on any error
// so create calls get the server's default.
func discoverSmallestShape() string {
	stdout, _, code := runCLI("sandbox", "shapes")
	if code != 0 || strings.TrimSpace(stdout) == "" {
		return ""
	}

	var shapes []struct {
		ID     string `json:"id"`
		VCPU   int    `json:"vcpu"`
		MemMib int    `json:"mem_mib"`
	}
	if err := json.Unmarshal([]byte(stdout), &shapes); err != nil {
		return ""
	}
	if len(shapes) == 0 {
		return ""
	}

	best := shapes[0]
	for _, s := range shapes[1:] {
		if s.VCPU+s.MemMib < best.VCPU+best.MemMib {
			best = s
		}
	}
	return best.ID
}

// presweep deletes stale e2e-* sandboxes left by prior failed runs.
// It intentionally matches all e2e-* prefixes (not just this runID) to
// collect orphans from previous sessions. The nightly CI enforces
// cancel-in-progress to prevent concurrent runs on the same account.
func presweep() {
	stdout, _, code := runCLI("sandbox", "list", "--all")
	if code != 0 || strings.TrimSpace(stdout) == "" {
		return
	}

	var sandboxes []struct {
		ID   string  `json:"id"`
		Name *string `json:"name,omitempty"`
	}
	if err := json.Unmarshal([]byte(stdout), &sandboxes); err != nil {
		return
	}

	for _, sb := range sandboxes {
		if sb.Name == nil || !strings.HasPrefix(*sb.Name, "e2e-") {
			continue
		}
		_, _, _ = runCLI("sandbox", "rm", "--force", sb.ID)
		fmt.Printf("[presweep] removed stale sandbox %s (%s)\n", *sb.Name, sb.ID)
	}
}

// postsweep is called explicitly before os.Exit in TestMain; best-effort
// delete of sandboxes created by THIS run only (prefix "e2e-<runID>-").
func postsweep() {
	stdout, _, code := runCLI("sandbox", "list", "--all")
	if code != 0 || strings.TrimSpace(stdout) == "" {
		return
	}

	var sandboxes []struct {
		ID   string  `json:"id"`
		Name *string `json:"name,omitempty"`
	}
	if err := json.Unmarshal([]byte(stdout), &sandboxes); err != nil {
		return
	}

	runPrefix := "e2e-" + runID + "-"
	for _, sb := range sandboxes {
		if sb.Name == nil || !strings.HasPrefix(*sb.Name, runPrefix) {
			continue
		}
		_, _, _ = runCLI("sandbox", "rm", "--force", sb.ID)
		fmt.Printf("[postsweep] removed sandbox %s (%s)\n", *sb.Name, sb.ID)
	}
}
