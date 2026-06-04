package sandbox

import (
	"context"
	"fmt"
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
// The user can pass either a raw id (`sb-<ulid>`) or a friendly name
// they set at create time. Names are unique within a session but the
// API doesn't enforce uniqueness — when multiple sandboxes share the
// same name, the most-recently-created one wins (matches the
// "what they probably meant" intuition).
//
// Behavior:
//   - Input that already starts with `sb-` is returned verbatim — no
//     extra round-trip. We let the actual operation (GET / DELETE)
//     surface the "not found" if the id is bogus.
//   - Otherwise we list the caller's sandboxes (any status, up to 200)
//     and pick the most recent with a matching name.
//   - Whitespace is trimmed; comparison is case-sensitive (matches
//     how the server stores the name).
//
// Returns a friendly error pointing at `sandbox list` when no match
// is found.
func resolveSandboxRef(ctx context.Context, client *api.SandboxClient, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("please provide a sandbox ID or name")
	}
	if strings.HasPrefix(ref, sandboxIDPrefix) {
		return ref, nil
	}

	rows, _, err := client.ListSandboxes(ctx, api.ListSandboxesOpts{Limit: 200})
	if err != nil {
		return "", err
	}
	matches := make([]api.SandboxView, 0)
	for _, r := range rows {
		if r.Name != nil && *r.Name == ref {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no sandbox named %q\n\n  To see your sandboxes, run:\n    createos sandbox list", ref)
	}
	// Most-recent wins. Stable sort so deterministic when timestamps tie.
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	return matches[0].ID, nil
}
