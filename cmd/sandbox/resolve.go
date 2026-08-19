package sandbox

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"atomicgo.dev/keyboard/keys"
	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// multiselect returns a pterm multiselect printer configured with
// space-to-select / enter-to-confirm, which matches what every other
// modern CLI (gum, huh, fzf) uses. pterm's default is enter-to-select
// / tab-to-confirm, which most users find unintuitive. Filter is off
// because Space conflicts with the filter input.
func multiselect(title string) *pterm.InteractiveMultiselectPrinter {
	return pterm.DefaultInteractiveMultiselect.
		WithDefaultText(title).
		WithFilter(false).
		WithKeySelect(keys.Space).
		WithKeyConfirm(keys.Enter).
		WithCheckmark(&pterm.Checkmark{Checked: "x", Unchecked: " "})
}

// sandboxIDPrefix is the literal prefix every fc-spawn sandbox id
// starts with. Anything that already carries the prefix is treated as
// an id and returned without a list-and-match round trip.
const sandboxIDPrefix = "sb-"

// splitForceFlag separates positional refs from --force/--yes/-y tokens.
// urfave/cli v2 stops flag parsing at the first positional, so `rm A B
// --yes` would otherwise treat `--yes` as another ref. Returns the
// stripped positional list and whether a force flag appeared anywhere.
func splitForceFlag(args []string) (refs []string, force bool) {
	refs = make([]string, 0, len(args))
	for _, a := range args {
		a = strings.TrimSpace(a)
		switch a {
		case "":
			continue
		case "--yes", "-y", "--force", "-yes":
			force = true
			continue
		}
		refs = append(refs, a)
	}
	return refs, force
}

// resolveSandboxRef resolves a sandbox identifier supplied on the CLI.
// The user can pass a full id (`sb-<ulid>`), an unambiguous leading
// chunk of one (`sb-01243e`), a friendly name, or a leading chunk of a
// name. It lists the caller's sandboxes (any status, up to 200) and
// hands the rows to matchSandboxRef, which holds all the matching
// rules — see there for precedence and ambiguity behavior.
func resolveSandboxRef(ctx context.Context, client *api.SandboxClient, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("please provide a sandbox ID or name")
	}

	rows, _, err := client.ListSandboxes(ctx, api.ListSandboxesOpts{Limit: 200})
	if err != nil {
		// Prefix matching needs the list, but an id-shaped ref used to
		// resolve without one. Keep that working when listing fails so a
		// flaky list endpoint can't break `rm sb-<full-id>`; the real
		// operation still reports an authoritative error if the id is
		// wrong. A name-shaped ref has no such fallback.
		if strings.HasPrefix(ref, sandboxIDPrefix) {
			return ref, nil
		}
		return "", err
	}
	return matchSandboxRef(rows, ref)
}

// matchSandboxRef picks the sandbox a user meant from the list they can
// see. It is pure so the matching rules can be unit-tested without an
// API — SandboxClient wraps a live resty client with no mock seam.
//
// Precedence is most-specific-first, the same shape Docker uses for
// container refs:
//
//	ref starts with `sb-`  → treated as an id
//	  exact id match       → that sandbox (ids are unique)
//	  one id prefix match  → that sandbox
//	  many prefix matches  → ambiguous; ask for more characters
//	  no match             → the ref verbatim, so the server decides
//	otherwise              → treated as a name
//	  exact name match     → most-recently-created, since the API does
//	                         not enforce unique names
//	  one name prefix hit  → that sandbox
//	  many prefix hits     → ambiguous; ask for more characters
//	  no match             → friendly error pointing at `sandbox list`
//
// Falling back to the verbatim ref on a zero-match id is deliberate:
// the visible list is capped, so a valid id outside that window (or a
// destroyed sandbox) must still reach the API for an authoritative
// answer rather than getting a wrong "not found" from the CLI.
//
// Comparison is case-sensitive throughout, matching how the server
// stores names.
func matchSandboxRef(rows []api.SandboxView, ref string) (string, error) {
	if strings.HasPrefix(ref, sandboxIDPrefix) {
		var prefixed []api.SandboxView
		for _, r := range rows {
			if r.ID == ref {
				return r.ID, nil
			}
			if strings.HasPrefix(r.ID, ref) {
				prefixed = append(prefixed, r)
			}
		}
		switch len(prefixed) {
		case 0:
			return ref, nil
		case 1:
			return prefixed[0].ID, nil
		default:
			return "", ambiguousRefError(ref, prefixed)
		}
	}

	var exact, prefixed []api.SandboxView
	for _, r := range rows {
		if r.Name == nil {
			continue
		}
		switch {
		case *r.Name == ref:
			exact = append(exact, r)
		case strings.HasPrefix(*r.Name, ref):
			prefixed = append(prefixed, r)
		}
	}
	if len(exact) > 0 {
		return mostRecent(exact).ID, nil
	}
	switch len(prefixed) {
	case 0:
		// APIError rather than a bare fmt.Errorf so the text survives
		// api.UserMessage, which rewrites any other error type into a
		// generic "something went wrong" (see internal/api/types.go).
		return "", &api.APIError{
			StatusCode: http.StatusNotFound,
			Message:    fmt.Sprintf("no sandbox matching %q\n\n  To see your sandboxes, run:\n    createos sandbox list", ref),
		}
	case 1:
		return prefixed[0].ID, nil
	default:
		return "", ambiguousRefError(ref, prefixed)
	}
}

// mostRecent returns the newest sandbox of the bunch. Stable sort keeps
// the pick deterministic when timestamps tie.
func mostRecent(rows []api.SandboxView) api.SandboxView {
	sorted := make([]api.SandboxView, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})
	return sorted[0]
}

// ambiguousRefErrorLimit caps how many candidates an ambiguity error
// lists, so a very short prefix doesn't flood the terminal.
const ambiguousRefErrorLimit = 10

// ambiguousRefError explains which sandboxes a prefix hit and asks for
// more characters. Candidates are listed newest first so the one the
// user most likely meant is at the top.
func ambiguousRefError(ref string, matches []api.SandboxView) error {
	sorted := make([]api.SandboxView, len(matches))
	copy(sorted, matches)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})

	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d sandboxes:\n", ref, len(sorted))
	for i, r := range sorted {
		if i == ambiguousRefErrorLimit {
			fmt.Fprintf(&b, "    … and %d more\n", len(sorted)-i)
			break
		}
		if r.Name != nil && *r.Name != "" {
			fmt.Fprintf(&b, "    %s  (%s)\n", r.ID, *r.Name)
		} else {
			fmt.Fprintf(&b, "    %s\n", r.ID)
		}
	}
	b.WriteString("\n  Type more characters to pick just one, or run:\n    createos sandbox list")
	// APIError so the text survives api.UserMessage — see the not-found
	// branch in matchSandboxRef for why. 400: the ref is under-specified,
	// not missing.
	return &api.APIError{StatusCode: http.StatusBadRequest, Message: b.String()}
}
