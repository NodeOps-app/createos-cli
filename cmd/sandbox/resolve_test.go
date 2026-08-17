package sandbox

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// sbView builds a SandboxView with just the fields matchSandboxRef reads.
// age shifts CreatedAt backwards so "most recent wins" is testable.
func sbView(id, name string, age time.Duration) api.SandboxView {
	v := api.SandboxView{
		ID:        id,
		CreatedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC).Add(-age),
	}
	if name != "" {
		v.Name = &name
	}
	return v
}

func TestMatchSandboxRefByID(t *testing.T) {
	rows := []api.SandboxView{
		sbView("sb-01243eaysdgfh", "alpha", 3*time.Hour),
		sbView("sb-01243fnq8k2m1", "beta", 2*time.Hour),
		sbView("sb-09zzz000000ab", "", time.Hour),
	}

	cases := map[string]string{
		// Full id resolves to itself.
		"sb-01243eaysdgfh": "sb-01243eaysdgfh",
		// The reported bug: a unique leading chunk must resolve.
		"sb-01243e": "sb-01243eaysdgfh",
		"sb-01243f": "sb-01243fnq8k2m1",
		"sb-09":     "sb-09zzz000000ab",
		// One character short of ambiguous.
		"sb-01243ea": "sb-01243eaysdgfh",
	}
	for ref, want := range cases {
		got, err := matchSandboxRef(rows, ref)
		if err != nil {
			t.Errorf("matchSandboxRef(%q) unexpected err: %v", ref, err)
			continue
		}
		if got != want {
			t.Errorf("matchSandboxRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestMatchSandboxRefExactIDBeatsPrefix(t *testing.T) {
	// "sb-01" is both a real id and a prefix of the other two. The exact
	// match must win rather than reporting ambiguity.
	rows := []api.SandboxView{
		sbView("sb-01aaa", "", time.Hour),
		sbView("sb-01bbb", "", time.Hour),
		sbView("sb-01", "", time.Hour),
	}
	got, err := matchSandboxRef(rows, "sb-01")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "sb-01" {
		t.Errorf("exact id match = %q, want %q", got, "sb-01")
	}
}

func TestMatchSandboxRefAmbiguousID(t *testing.T) {
	rows := []api.SandboxView{
		sbView("sb-01243eaysdgfh", "alpha", 3*time.Hour),
		sbView("sb-01243fnq8k2m1", "beta", time.Hour),
	}
	_, err := matchSandboxRef(rows, "sb-01243")
	if err == nil {
		t.Fatal(`matchSandboxRef("sb-01243") expected ambiguity error, got nil`)
	}

	// Must be an APIError or api.UserMessage rewrites it to a generic
	// "something went wrong" at the rm.go call site.
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *api.APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}

	msg := apiErr.Message
	for _, want := range []string{"sb-01243eaysdgfh", "sb-01243fnq8k2m1", "matches 2 sandboxes"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ambiguity message missing %q:\n%s", want, msg)
		}
	}
	// Newest first, so the likeliest intent is at the top.
	if strings.Index(msg, "sb-01243fnq8k2m1") > strings.Index(msg, "sb-01243eaysdgfh") {
		t.Errorf("expected newest candidate listed first:\n%s", msg)
	}
}

func TestMatchSandboxRefUnknownIDPassesThrough(t *testing.T) {
	// The visible list is capped at 200, so an id that matches nothing
	// must still reach the API for an authoritative answer instead of
	// the CLI inventing a "not found".
	rows := []api.SandboxView{sbView("sb-01aaa", "alpha", time.Hour)}
	got, err := matchSandboxRef(rows, "sb-99notinlist")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "sb-99notinlist" {
		t.Errorf("unknown id = %q, want it returned verbatim", got)
	}
}

func TestMatchSandboxRefByName(t *testing.T) {
	rows := []api.SandboxView{
		sbView("sb-01aaa", "web-server", time.Hour),
		sbView("sb-01bbb", "worker", time.Hour),
		sbView("sb-01ccc", "", time.Hour), // unnamed rows must not panic
	}

	cases := map[string]string{
		"web-server": "sb-01aaa", // exact
		"web":        "sb-01aaa", // unique prefix
		"wor":        "sb-01bbb",
	}
	for ref, want := range cases {
		got, err := matchSandboxRef(rows, ref)
		if err != nil {
			t.Errorf("matchSandboxRef(%q) unexpected err: %v", ref, err)
			continue
		}
		if got != want {
			t.Errorf("matchSandboxRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestMatchSandboxRefExactNameBeatsPrefix(t *testing.T) {
	// "web" exactly names one sandbox and prefixes another. Exact wins.
	rows := []api.SandboxView{
		sbView("sb-01aaa", "web-server", time.Hour),
		sbView("sb-01bbb", "web", time.Hour),
	}
	got, err := matchSandboxRef(rows, "web")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "sb-01bbb" {
		t.Errorf("exact name match = %q, want %q", got, "sb-01bbb")
	}
}

func TestMatchSandboxRefDuplicateNamesPickNewest(t *testing.T) {
	// The API does not enforce unique names; newest wins.
	rows := []api.SandboxView{
		sbView("sb-old", "api", 5*time.Hour),
		sbView("sb-new", "api", time.Hour),
		sbView("sb-mid", "api", 3*time.Hour),
	}
	got, err := matchSandboxRef(rows, "api")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "sb-new" {
		t.Errorf("duplicate names = %q, want newest %q", got, "sb-new")
	}
}

func TestMatchSandboxRefAmbiguousName(t *testing.T) {
	rows := []api.SandboxView{
		sbView("sb-01aaa", "web-server", time.Hour),
		sbView("sb-01bbb", "web-worker", time.Hour),
	}
	_, err := matchSandboxRef(rows, "web-")
	if err == nil {
		t.Fatal(`matchSandboxRef("web-") expected ambiguity error, got nil`)
	}
	msg := api.UserMessage(err)
	for _, want := range []string{"web-server", "web-worker", "sb-01aaa", "sb-01bbb"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ambiguity message missing %q:\n%s", want, msg)
		}
	}
}

func TestMatchSandboxRefUnknownNameErrors(t *testing.T) {
	rows := []api.SandboxView{sbView("sb-01aaa", "web-server", time.Hour)}
	_, err := matchSandboxRef(rows, "database")
	if err == nil {
		t.Fatal(`matchSandboxRef("database") expected not-found error, got nil`)
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *api.APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
	if !strings.Contains(apiErr.Message, "database") {
		t.Errorf("not-found message should quote the ref:\n%s", apiErr.Message)
	}
}

func TestAmbiguousRefErrorTruncates(t *testing.T) {
	rows := make([]api.SandboxView, 0, 15)
	for i := range 15 {
		rows = append(rows, sbView("sb-01"+string(rune('a'+i)), "", time.Duration(i)*time.Hour))
	}
	err := ambiguousRefError("sb-01", rows)
	msg := api.UserMessage(err)
	if !strings.Contains(msg, "matches 15 sandboxes") {
		t.Errorf("expected full count in header:\n%s", msg)
	}
	if !strings.Contains(msg, "and 5 more") {
		t.Errorf("expected truncation notice after %d entries:\n%s", ambiguousRefErrorLimit, msg)
	}
}
