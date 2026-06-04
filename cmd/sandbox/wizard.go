package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
	"github.com/NodeOps-app/createos-cli/internal/ui"
)

// wizardSeed lets the caller pre-fill some answers so already-supplied
// flags aren't asked for again.
type wizardSeed struct {
	name    string
	rootfs  string
	ingress bool
	netIDs  []string
	sshKeys []string // canonicalised key strings (NOT paths)
}

// wizardResult is what runCreateWizard returns when the user finished.
// nil result = user cancelled (caller exits quietly).
type wizardResult struct {
	shape   string
	name    string
	rootfs  string
	ingress bool
	netIDs  []string
	sshKeys []string // canonicalised key strings (NOT paths)
}

// runCreateWizard walks the user through shape → name → rootfs → ingress
// → networks. Each step lets the user cancel (q / esc / ctrl+c) and
// exit cleanly. Returns nil on cancel.
//
// Behavior:
//   - On non-TTY this should never be reached — caller guards with the
//     terminal.IsInteractive() check.
//   - Each step that was already supplied via a flag is skipped.
//   - Rootfs / network steps may fail to fetch (API error) — they fall
//     through with a warning rather than aborting the whole wizard.
func runCreateWizard(c *cli.Context, client *api.SandboxClient, seed wizardSeed) (*wizardResult, error) {
	if !terminal.IsInteractive() {
		return nil, fmt.Errorf("please choose a size with --shape\n\n  To see the options, run:\n    createos sandbox shapes")
	}
	out := &wizardResult{
		name:    seed.name,
		rootfs:  seed.rootfs,
		ingress: seed.ingress,
		netIDs:  append([]string{}, seed.netIDs...),
		sshKeys: append([]string{}, seed.sshKeys...),
	}

	// ── 1. Shape (required) ─────────────────────────────────────────
	shape, err := wizardPickShape(c, client)
	if err != nil {
		return nil, err
	}
	if shape == "" {
		return nil, nil
	}
	out.shape = shape

	// ── 2. Name (optional; default = server-generated) ──────────────
	if out.name == "" {
		nameInput, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Name for your sandbox (leave empty to auto-generate)").
			Show()
		if err != nil {
			return nil, fmt.Errorf("could not read sandbox name: %w", err)
		}
		out.name = strings.TrimSpace(nameInput)
	}

	// ── 3. Rootfs (optional; default = host default) ────────────────
	if out.rootfs == "" {
		picked, err := wizardPickRootfs(c, client)
		switch {
		case err != nil:
			// Non-fatal — log and continue with the server default.
			pterm.Println(pterm.Gray("  Could not load image list — using the default."))
		case picked == "":
			// User cancelled the rootfs step specifically — keep going
			// with the default rather than aborting the whole wizard.
		default:
			out.rootfs = picked
		}
	}

	// ── 4. Public URL? ─────────────────────────────────────────────
	if !out.ingress {
		yes, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText("Give this sandbox a public HTTPS URL?").
			WithDefaultValue(false).
			Show()
		if err != nil {
			return nil, fmt.Errorf("could not read confirmation: %w", err)
		}
		out.ingress = yes
	}

	// ── 5. Attach to private networks (optional; skip if user has none) ──
	if len(out.netIDs) == 0 {
		picked, err := wizardPickNetworks(c, client)
		if err != nil {
			pterm.Println(pterm.Gray("  Could not load networks — skipping."))
		} else {
			out.netIDs = picked
		}
	}

	// ── 6. SSH keys (optional; pick from ~/.ssh/, fall back to a path prompt) ──
	if len(out.sshKeys) == 0 {
		picked, err := wizardPickSSHKeys()
		if err != nil {
			pterm.Println(pterm.Gray(fmt.Sprintf("  SSH key step skipped (%v).", err)))
		} else {
			out.sshKeys = picked
		}
	}

	return out, nil
}

// wizardPickShape — bubbletea picker over GET /v1/shapes.
func wizardPickShape(c *cli.Context, client *api.SandboxClient) (string, error) {
	spinner, _ := pterm.DefaultSpinner.Start("Loading sizes…") //nolint:errcheck
	shapes, err := client.ListShapes(c.Context)
	_ = spinner.Stop() //nolint:errcheck
	if err != nil {
		return "", err
	}
	if len(shapes) == 0 {
		return "", fmt.Errorf("no sandbox sizes are available right now")
	}
	return ui.PickShape(shapes)
}

// wizardPickRootfs — bubbletea picker over the union of built-in
// images (GET /v1/rootfs) AND the user's own ready templates
// (GET /v1/templates, status=ready). Empty return = user cancelled
// the step; caller falls back to the default.
func wizardPickRootfs(c *cli.Context, client *api.SandboxClient) (string, error) {
	spinner, _ := pterm.DefaultSpinner.Start("Loading images…") //nolint:errcheck
	cat, err := client.ListRootfs(c.Context)
	if err != nil {
		_ = spinner.Stop() //nolint:errcheck
		return "", err
	}
	// Templates are best-effort: a fetch failure shouldn't kill the
	// create flow. Same forgiveness the UI shows.
	tpls, _ := client.ListTemplates(c.Context) //nolint:errcheck
	_ = spinner.Stop()                         //nolint:errcheck
	if cat == nil || len(cat.Rootfs) == 0 {
		return "", nil
	}
	items := make([]ui.PickerItem, 0, len(cat.Rootfs)+len(tpls)+1)
	// First option is "default" so users can punt without thinking.
	items = append(items, ui.PickerItem{
		Title:    "(use the default)",
		Subtitle: "→ " + cat.Default,
		Value:    "",
	})
	descByName := make(map[string]string)
	for _, e := range cat.Entries {
		descByName[e.Name] = e.Description
	}
	for _, name := range cat.Rootfs {
		sub := "built-in image"
		if d := descByName[name]; d != "" {
			sub = d
		}
		items = append(items, ui.PickerItem{
			Title:    name,
			Subtitle: sub,
			Value:    name,
		})
	}
	// Append the user's own templates. Only ready ones can boot a
	// sandbox; others are filtered out so we don't show un-bootable
	// entries. Names go through verbatim — server resolves them.
	for _, t := range tpls {
		if t.Status != "ready" {
			continue
		}
		items = append(items, ui.PickerItem{
			Title:    t.Name,
			Subtitle: "your template",
			Value:    t.Name,
		})
	}
	return ui.Pick("Pick a base image", items)
}

// wizardPickNetworks — multi-select over GET /v1/networks. Returns []
// empty when the user has no networks or skips the prompt.
func wizardPickNetworks(c *cli.Context, client *api.SandboxClient) ([]string, error) {
	spinner, _ := pterm.DefaultSpinner.Start("Loading networks…") //nolint:errcheck
	nets, err := client.ListNetworks(c.Context)
	_ = spinner.Stop() //nolint:errcheck
	if err != nil {
		return nil, err
	}
	if len(nets) == 0 {
		return nil, nil
	}
	options := make([]string, 0, len(nets))
	idByOption := make(map[string]string, len(nets))
	for _, n := range nets {
		label := n.Name + "   " + n.ID
		options = append(options, label)
		idByOption[label] = n.ID
	}
	picked, err := multiselect("Attach to which networks? (space = pick, enter = confirm, leave none to skip)").
		WithOptions(options).
		Show()
	if err != nil {
		return nil, fmt.Errorf("could not read your selection: %w", err)
	}
	out := make([]string, 0, len(picked))
	for _, p := range picked {
		if id, ok := idByOption[p]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// wizardPickSSHKeys scans the user's `~/.ssh/` for likely public-key
// files, lets them multi-select which to install, and optionally lets
// them paste a custom path. Returns the canonicalised key strings (not
// paths) so the caller can drop them straight into SandboxCreateReq.
//
// Behavior:
//   - No `~/.ssh/` or no candidates → offer a path prompt (still
//     skippable with empty input).
//   - Returns nil (no error) if the user picks nothing — SSH access is
//     optional for a sandbox.
func wizardPickSSHKeys() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not resolve $HOME: %w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	candidates := discoverSSHPubkeys(sshDir)

	// Brief explainer — users coming from a hosted-PaaS background may
	// not know why SSH keys would be involved with a sandbox.
	pterm.Println()
	pterm.NewStyle(pterm.FgCyan).Println("  SSH keys")
	pterm.Println(pterm.Gray("  Installing a public key lets you sign into the sandbox with"))
	pterm.Println(pterm.Gray("  `createos sandbox shell` and forward ports (e.g. open a web"))
	pterm.Println(pterm.Gray("  server inside the sandbox in your local browser). Skip this"))
	pterm.Println(pterm.Gray("  step if you only need `exec`, files, or the public URL."))

	if len(candidates) == 0 {
		// Nothing auto-detected. Offer a one-shot manual path entry.
		var path string
		path, err = pterm.DefaultInteractiveTextInput.
			WithDefaultText("Path to a public-key file to install (leave empty to skip)").
			Show()
		if err != nil {
			return nil, fmt.Errorf("could not read the key path: %w", err)
		}
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, nil
		}
		return readSSHPubkeys([]string{path})
	}

	// Multi-select over the discovered files. Show absolute paths so
	// users on multi-key setups can tell them apart.
	options := make([]string, 0, len(candidates))
	pathByOpt := make(map[string]string, len(candidates))
	for _, path := range candidates {
		label := relToHome(path, home)
		options = append(options, label)
		pathByOpt[label] = path
	}
	picked, err := multiselect("Install which SSH keys? (space = pick, enter = confirm, leave none to skip)").
		WithOptions(options).
		Show()
	if err != nil {
		return nil, fmt.Errorf("could not read your selection: %w", err)
	}
	if len(picked) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(picked))
	for _, p := range picked {
		if realPath, ok := pathByOpt[p]; ok {
			paths = append(paths, realPath)
		}
	}
	return readSSHPubkeys(paths)
}

// discoverSSHPubkeys lists candidate `<algo>.pub` files under sshDir.
// Filters out anything that doesn't look like a public key (size 0 or
// missing the `ssh-` / `ecdsa-` prefix on the first line).
func discoverSSHPubkeys(sshDir string) []string {
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		path := filepath.Join(sshDir, e.Name())
		head, err := os.ReadFile(path) // #nosec G304 -- path is from os.ReadDir over the user's ~/.ssh
		if err != nil || len(head) == 0 {
			continue
		}
		// First-line sniff for openssh public-key shape.
		first := strings.SplitN(strings.TrimSpace(string(head)), " ", 2)[0]
		if !strings.HasPrefix(first, "ssh-") && !strings.HasPrefix(first, "ecdsa-") && !strings.HasPrefix(first, "sk-") {
			continue
		}
		out = append(out, path)
	}
	return out
}

// relToHome shortens absolute paths under $HOME to `~/...` so the
// picker shows compact labels.
func relToHome(path, home string) string {
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// stringSliceCleanup trims and drops empty entries — pulled out of
// runCreate so the wizard plumbing can reuse it.
func stringSliceCleanup(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
