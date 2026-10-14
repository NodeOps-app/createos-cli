package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// The integrations monorepo. Every host below installs some part of it, and
// the two that have no installer of their own (OpenCode, DeepSeek Harness)
// need a checkout on disk, which setupPluginCheckout manages.
const (
	setupPluginRepo    = "NodeOps-app/createos-plugin"
	setupPluginRepoURL = "https://github.com/" + setupPluginRepo
	// The Claude Code marketplace declares itself under this name, so
	// `plugin@marketplace` ids resolve to it regardless of how the user
	// named the source when adding it.
	setupMarketplaceName = "createos"
)

// newSetupCommand returns `createos sandbox setup`, the harness integration
// group. One subcommand per host on
// https://createos.sh/docs/Sandbox/Integrations.
func newSetupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Connect a coding harness to CreateOS Sandbox",
		Description: "Each subcommand wires one harness to CreateOS Sandbox, so a\n" +
			"workspace runs on a disposable microVM instead of your laptop.\n\n" +
			"Every subcommand takes --doctor, which checks the prerequisites and\n" +
			"reports without changing anything.",
		Subcommands: []*cli.Command{
			newSetupClaudeCodeCommand(),
			newSetupCodexCommand(),
			newSetupDeepSeekCommand(),
			newSetupHerdrCommand(),
			newSetupOpenCodeCommand(),
			newSetupOrcaCommand(),
			newSetupPiCommand(),
		},
	}
}

// setupSignedIn confirms the session actually works before a host integration
// is wired to it. A plugin installed against a dead session fails later, in
// the host's UI, where the reason is much harder to see.
func setupSignedIn(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' first")
	}
	if _, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{}); err != nil {
		return fmt.Errorf("your session is not usable — run 'createos login' again: %w", err)
	}
	fmt.Println("signed in to CreateOS")
	return nil
}

// setupRequireBin resolves a host binary, turning a bare exec.LookPath miss
// into the install hint the user actually needs.
func setupRequireBin(name, hint string) (string, error) {
	bin, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s is not on PATH — %s", name, hint)
	}
	fmt.Printf("%s found at %s\n", name, bin)
	return bin, nil
}

// setupRun runs a host CLI and returns its combined output, which callers
// attach to any error: these tools explain their own failures far better than
// an exit status does.
func setupRun(ctx context.Context, bin string, args ...string) (string, error) {
	// #nosec G204 -- bin is an exec.LookPath result and every arg is either a
	// literal from this package or a path the user named on the command line;
	// it is one argv element, never a shell string.
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return string(out), err
}

// setupAlreadyDone reports whether a host CLI refused because the thing was
// already installed. Those tools exit non-zero for it, so a plain error check
// would make a second `setup` run fail on a box that is correctly set up.
func setupAlreadyDone(out string) bool {
	s := strings.ToLower(out)
	for _, phrase := range []string{
		"already exists",
		"already added",
		"already installed",
		"already registered",
		"already configured",
	} {
		if strings.Contains(s, phrase) {
			return true
		}
	}
	return false
}

// setupCheckoutDir is where setup keeps its own clone of the integrations
// monorepo. Beside the per-sandbox keys and ssh mux sockets the CLI already
// owns, so nothing of the user's is involved.
func setupCheckoutDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	return filepath.Join(home, ".config", "createos", "plugins", "createos-plugin"), nil
}

// setupPluginCheckout returns a path to the integrations monorepo.
//
// A --local path wins and is used as-is, so plugin developers can point the
// setup at their own working tree. Otherwise setup owns a clone under
// ~/.config/createos and refreshes it on every run, because the host reads
// these files directly — a stale checkout silently pins the user to whatever
// the plugin looked like the day they first ran setup.
func setupPluginCheckout(ctx context.Context, local string) (string, error) {
	if local = strings.TrimSpace(local); local != "" {
		dir, err := filepath.Abs(local)
		if err != nil {
			return "", fmt.Errorf("could not resolve %q: %w", local, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "packages")); err != nil {
			return "", fmt.Errorf("%s does not look like the integrations repo: no packages/ directory", dir)
		}
		fmt.Printf("using your checkout at %s\n", dir)
		return dir, nil
	}

	if _, err := setupRequireBin("git", "install it from https://git-scm.com"); err != nil {
		return "", err
	}
	dir, err := setupCheckoutDir()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		fmt.Printf("updating %s\n", dir)
		if out, pullErr := setupRun(ctx, "git", "-C", dir, "pull", "--ff-only", "--quiet"); pullErr != nil {
			// A diverged or dirty checkout is the user's, not ours to reset.
			// The stale copy still works, so warn and carry on.
			fmt.Printf("could not update the checkout, using it as-is: %s\n", strings.TrimSpace(out))
		}
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return "", fmt.Errorf("could not create %s: %w", filepath.Dir(dir), err)
	}
	fmt.Printf("cloning %s into %s\n", setupPluginRepoURL, dir)
	if out, err := setupRun(ctx, "git", "clone", "--depth", "1", setupPluginRepoURL, dir); err != nil {
		return "", fmt.Errorf("could not clone the integrations repo: %w\n%s", err, out)
	}
	return dir, nil
}

// setupPackageDir resolves one package inside the checkout and fails loudly
// when it is missing, which means the checkout is not what we think it is.
func setupPackageDir(checkout, pkg string) (string, error) {
	dir := filepath.Join(checkout, "packages", pkg)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("%s is missing from the checkout at %s", pkg, checkout)
	}
	return dir, nil
}

// setupDoctorFlag is the flag every subcommand shares.
func setupDoctorFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:  "doctor",
		Usage: "Check the prerequisites and report, without changing anything",
	}
}

// setupLocalFlag is shared by the hosts that need a checkout on disk.
func setupLocalFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "local",
		Usage: "Use this local checkout of " + setupPluginRepo + " instead of cloning it",
	}
}

// setupDoctorDone prints the line that ends a --doctor run.
func setupDoctorDone() {
	fmt.Println("\nEverything the plugin needs is present. Run this again without --doctor to install it.")
}
