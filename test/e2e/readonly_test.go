//go:build e2e

package e2e

import (
	"encoding/json"
	"testing"
)

// ShapeView is a minimal projection of the shape JSON returned by
// `sandbox shapes -o json`. Defined here since it is only used in readonly tests.
type ShapeView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	VCPU   int    `json:"vcpu"`
	MemMib int    `json:"mem_mib"`
}

// TestShapes asserts that `sandbox shapes -o json` returns a non-empty list
// of shapes with expected fields populated.
func TestShapes(t *testing.T) {
	stdout, stderr, code := runCLI("sandbox", "shapes", "-o", "json")
	if code != 0 {
		t.Fatalf("sandbox shapes exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	shapes := mustJSON[[]ShapeView](t, stdout)
	if len(shapes) == 0 {
		t.Fatal("sandbox shapes: expected at least one shape, got empty list")
	}

	first := shapes[0]
	if first.VCPU <= 0 {
		t.Errorf("sandbox shapes: first shape VCPU must be > 0, got %d", first.VCPU)
	}
	if first.MemMib <= 0 {
		t.Errorf("sandbox shapes: first shape MemMib must be > 0, got %d", first.MemMib)
	}
	if shapes[0].Name == "" {
		t.Errorf("shapes[0].Name is empty")
	}
}

// TestCatalog asserts that `sandbox catalog -o json` exits 0 and returns a
// non-empty JSON array where the first item has a recognisable key.
func TestCatalog(t *testing.T) {
	stdout, stderr, code := runCLI("sandbox", "catalog", "-o", "json")
	if code != 0 {
		t.Fatalf("sandbox catalog exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("sandbox catalog: could not parse JSON: %v\noutput: %s", err, stdout)
	}
	if len(items) == 0 {
		t.Fatal("sandbox catalog: expected at least one item, got empty list")
	}

	first := items[0]
	_, hasName := first["name"]
	_, hasID := first["id"]
	if !hasName && !hasID {
		t.Errorf("sandbox catalog: first item has neither 'name' nor 'id' key; keys: %v", keys(first))
	}
}

// TestRootfs asserts that `sandbox rootfs -o json` exits 0 and returns a
// non-nil, non-empty JSON value.
func TestRootfs(t *testing.T) {
	stdout, stderr, code := runCLI("sandbox", "rootfs", "-o", "json")
	if code != 0 {
		t.Fatalf("sandbox rootfs exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	var v any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("sandbox rootfs: could not parse JSON: %v\noutput: %s", err, stdout)
	}
	if v == nil {
		t.Fatal("sandbox rootfs: parsed JSON value is nil")
	}

	switch val := v.(type) {
	case []any:
		if len(val) == 0 {
			t.Fatal("sandbox rootfs: returned empty JSON array")
		}
	case map[string]any:
		if len(val) == 0 {
			t.Fatal("sandbox rootfs: returned empty JSON object")
		}
	}
}

// TestTemplateList asserts that `sandbox template list -o json` exits 0 and
// returns valid JSON (empty or non-empty slice).
func TestTemplateList(t *testing.T) {
	stdout, stderr, code := runCLI("sandbox", "template", "list", "-o", "json")
	if code != 0 {
		t.Fatalf("sandbox template list exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("sandbox template list: could not parse JSON: %v\noutput: %s", err, stdout)
	}
	// Empty slice is acceptable — the test account may have no templates.
}

// TestList asserts that `sandbox list -o json` exits 0 and returns valid JSON.
// An empty slice is acceptable since the account may have no sandboxes.
func TestList(t *testing.T) {
	stdout, stderr, code := runCLI("sandbox", "list", "-o", "json")
	if code != 0 {
		t.Fatalf("sandbox list exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	_ = mustJSON[[]SandboxView](t, stdout)
}

// TestGet creates a sandbox, then asserts that `sandbox get -o json <id>`
// exits 0 and returns a well-formed SandboxView with matching ID and status.
func TestGet(t *testing.T) {
	sb := newSandbox(t)

	stdout, stderr, code := runCLI("sandbox", "get", "-o", "json", sb.ID)
	if code != 0 {
		t.Fatalf("sandbox get exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	got := mustJSON[SandboxView](t, stdout)
	if got.ID != sb.ID {
		t.Errorf("sandbox get: ID mismatch: want %s, got %s", sb.ID, got.ID)
	}
	if got.Status == "" {
		t.Errorf("sandbox get: status field is empty")
	}
	if got.Shape != testShape {
		t.Errorf("got shape %q, want %q", got.Shape, testShape)
	}
}

// keys returns the key names of a map — used in error messages only.
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
