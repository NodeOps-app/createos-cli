package sandbox

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"

	"github.com/urfave/cli/v2"
)

// Orca drives a per-workspace environment by running one command for all four
// lifecycle phases and selecting the phase with ORCA_VM_MODE. The command
// prints one JSON object on stdout; everything else must go to stderr.
//
// `createos setup orca` is the human half: it checks the environment and hands
// the plugin install over to Orca. `createos setup orca --recipe` is the
// machine half, and is the string the plugin manifest carries.

const (
	orcaSchemaVersion = 2
	// Orca's git-URL installer clones a whole repo and requires
	// orca-plugin.json at its root, so this monorepo subdirectory cannot be
	// installed that way. Until it ships as its own single-package repo, the
	// only working route is a local checkout via Settings > Plugins > Dev Paths.
	orcaPluginRepoURL  = "https://github.com/NodeOps-app/createos-plugins"
	orcaPluginPkgPath  = "packages/orca-plugin"
	orcaDefaultShape   = "s-4vcpu-8gb" // remote editors need more than 2 GiB
	orcaDefaultRootfs  = "devbox:1"    // ships sshd, git, Node 22, opencode, pi
	orcaDefaultRoot    = "/workspace/repo"
	orcaSSHUser        = "root"
	orcaDefaultSSHWait = 180 * time.Second
)

// Orca adopts a provisioned root only when the checked-out branch equals the
// workspace name verbatim, so the name reaches git unaltered and must be a
// valid ref. Reject anything else rather than silently sanitizing it.
var orcaBranchRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,239}$`)

// Absolute POSIX path, no shell metacharacters. This lands inside a remote
// `sh -c` script and in a git refspec target.
var orcaRootRE = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,239}$`)

// Coding agents the recipe can install into a fresh sandbox, keyed by the name
// users pass to --agents. Orca never tells a recipe which agent the workspace
// uses (it passes ORCA_VM_MODE, ORCA_REPO_*, ORCA_WORKSPACE_NAME, and no agent
// identity), so this cannot be inferred and has to be asked for.
//
// bin is what the installer actually puts on PATH — `cursor` is deliberately
// absent, since cursor.com/install lays down `cursor-agent`.
var orcaAgents = map[string]struct{ bin, install string }{
	"opencode": {"opencode", "curl -fsSL https://opencode.ai/install | bash"},
	"claude":   {"claude", "curl -fsSL https://claude.ai/install.sh | bash"},
	"pi":       {"pi", "curl -fsSL https://pi.dev/install.sh | sh"},
	"codex":    {"codex", "curl -fsSL https://chatgpt.com/codex/install.sh | CODEX_NON_INTERACTIVE=1 sh"},
	"cursor":   {"cursor-agent", "curl https://cursor.com/install -fsS | bash"},
}

// Installers drop binaries in these; a non-login `sh -c` does not pick them up.
const orcaAgentPath = "$HOME/.local/bin:$HOME/.opencode/bin:$HOME/.asdf/installs/nodejs/lts/bin:$PATH"

// The PATH a non-interactive process gets. An agent that does not resolve here
// is unusable to Orca's exec path, whatever the login shell can see.
const orcaSystemPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// Last line of a successful install script. Distinctive enough not to collide
// with installer chatter, and checked only as the final line.
const (
	orcaAgentInstalled = "ORCA_AGENT_INSTALLED_OK"
	orcaAgentSkipped   = "ORCA_AGENT_ALREADY_PRESENT"
)

// Every value below reaches `git` as an argv element. argv defeats shell
// injection but not argument injection: git reads a leading `-` as an option,
// so `--upload-pack=...` in ORCA_REPO_REF_HEAD would run a command of the
// caller's choosing. Pin the head to a hex object id and the ref to a ref shape.
var orcaSHARE = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

type orcaUserData struct {
	Provider   string `json:"provider"`
	ResourceID string `json:"resourceId"`
}

type orcaTarget struct {
	Label      string `json:"label"`
	ConfigHost string `json:"configHost"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
}

type orcaConnection struct {
	Type        string     `json:"type"`
	ProjectRoot string     `json:"projectRoot"`
	Target      orcaTarget `json:"target"`
}

type orcaRecipeResult struct {
	SchemaVersion int            `json:"schemaVersion"`
	CheckoutMode  string         `json:"checkoutMode"`
	Connection    orcaConnection `json:"connection"`
	UserData      orcaUserData   `json:"userData"`
}

// Orca hands the create result back on stdin for suspend, resume, and destroy.
type orcaLifecyclePayload struct {
	RecipeResult struct {
		UserData orcaUserData `json:"userData"`
	} `json:"recipeResult"`
}

// NewSetupCommand returns `createos setup`, the harness integration group.
func NewSetupCommand() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "Connect CreateOS Sandbox to a coding harness",
		Description: "Each subcommand wires one harness to CreateOS Sandbox, so a\n" +
			"workspace runs on a disposable microVM instead of your laptop.",
		Subcommands: []*cli.Command{newSetupOrcaCommand()},
	}
}

func newSetupOrcaCommand() *cli.Command {
	return &cli.Command{
		Name:  "orca",
		Usage: "Set up Orca to run workspaces on CreateOS Sandboxes",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "doctor",
				Usage: "Check the prerequisites and report, without changing anything",
			},
			&cli.BoolFlag{
				Name:   "recipe",
				Usage:  "Run one Orca lifecycle phase (Orca calls this, not you)",
				Hidden: true,
			},
			&cli.StringFlag{
				Name:    "shape",
				Usage:   "Sandbox size for each workspace",
				Value:   orcaDefaultShape,
				EnvVars: []string{"CREATEOS_SHAPE"},
			},
			&cli.StringFlag{
				Name:    "rootfs",
				Usage:   "Sandbox image for each workspace",
				Value:   orcaDefaultRootfs,
				EnvVars: []string{"CREATEOS_ROOTFS"},
			},
			&cli.StringFlag{
				Name:    "project-root",
				Usage:   "Absolute path the checkout lands on inside the sandbox",
				Value:   orcaDefaultRoot,
				EnvVars: []string{"CREATEOS_PROJECT_ROOT"},
			},
			&cli.DurationFlag{
				Name:    "ssh-timeout",
				Usage:   "How long to wait for sshd to accept connections",
				Value:   orcaDefaultSSHWait,
				EnvVars: []string{"CREATEOS_SSH_READY_TIMEOUT"},
			},
			&cli.StringFlag{
				Name:    "agents",
				Usage:   "Coding agents to install, comma-separated: " + orcaAgentNames(),
				EnvVars: []string{"CREATEOS_AGENTS"},
			},
		},
		Action: func(c *cli.Context) error {
			if c.Bool("recipe") {
				return runOrcaRecipe(c)
			}
			return runOrcaSetup(c, c.Bool("doctor"))
		},
	}
}

// ---------------------------------------------------------------- human half

func runOrcaSetup(c *cli.Context, doctorOnly bool) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' first")
	}
	if _, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{}); err != nil {
		return fmt.Errorf("your session is not usable — run 'createos login' again: %w", err)
	}
	fmt.Println("signed in to CreateOS")

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is not on PATH; the recipe pushes your checkout into the sandbox")
	}
	fmt.Println("git found on PATH")

	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("ssh is not on PATH")
	}
	fmt.Println("ssh found on PATH")

	if doctorOnly {
		return nil
	}

	fmt.Print("\nNext, install the Orca plugin. Orca performs the install itself,\n" +
		"so it can show you what the plugin contributes before you approve it.\n\n" +
		"  1. Clone " + orcaPluginRepoURL + "\n" +
		"  2. Open Orca > Settings > Plugins > Dev Paths\n" +
		"  3. Add the path to " + orcaPluginPkgPath + "\n" +
		"  4. Approve the \"CreateOS Sandbox\" VM recipe when Orca asks\n" +
		"  5. Turn on Settings > Cloud VM\n\n" +
		"Then pick \"CreateOS Sandbox\" when you create a workspace, under\n" +
		"Run on > Per-Workspace Environment.\n\n" +
		"Installing by git URL does not work yet: Orca's installer needs\n" +
		"orca-plugin.json at a repository root, and the plugin lives in a\n" +
		"subdirectory of that monorepo.\n")
	return nil
}

// -------------------------------------------------------------- machine half

func runOrcaRecipe(c *cli.Context) error {
	switch mode := strings.TrimSpace(os.Getenv("ORCA_VM_MODE")); mode {
	case "", "create":
		return orcaCreate(c)
	case "destroy":
		return orcaDestroy(c)
	case "suspend", "resume":
		// Orca requires suspend and resume together or not at all, and SSH does
		// not come back reliably after a resume. The plugin declares neither.
		return fmt.Errorf("%s is not supported by this recipe", mode)
	default:
		return fmt.Errorf("unknown ORCA_VM_MODE: %q", mode)
	}
}

func orcaCreate(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("not signed in — run 'createos login'")
	}

	// Orca cancels a create it no longer wants (a race with a second attempt,
	// the user closing the dialog) by killing this process. With no handler
	// that is an unrecoverable SIGTERM: the sandbox this func just created
	// survives with nothing left running to clean it up. Trade the sandbox's
	// lifetime for a graceful window instead — catch the signal, cancel the
	// context so downstream calls unwind, and let the failure path below
	// destroy what was already created.
	ctx, stopSignals := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	c.Context = ctx

	// Fail loudly rather than degrading to the ordinary recipe shape, which
	// would hand Orca a box with no checkout at projectRoot.
	if v := os.Getenv("ORCA_RECIPE_RESULT_SCHEMA_VERSION"); v != "2" {
		return fmt.Errorf("expected schema version 2 (provisioned-root), got %q", v)
	}
	head := strings.TrimSpace(os.Getenv("ORCA_REPO_REF_HEAD"))
	if !orcaSHARE.MatchString(head) {
		return fmt.Errorf("ORCA_REPO_REF_HEAD is not a commit id: %q", head)
	}
	repoPath := strings.TrimSpace(os.Getenv("ORCA_REPO_PATH"))
	if !filepath.IsAbs(repoPath) || strings.HasPrefix(repoPath, "-") {
		return fmt.Errorf("ORCA_REPO_PATH is not an absolute path: %q", repoPath)
	}

	// Orca sets ORCA_REPO_BRANCH only when the user names a branch in the
	// create dialog. Naming the workspace alone leaves it empty.
	branch := strings.TrimSpace(os.Getenv("ORCA_REPO_BRANCH"))
	if branch == "" {
		branch = strings.TrimSpace(os.Getenv("ORCA_WORKSPACE_NAME"))
	}
	if !orcaBranchRE.MatchString(branch) {
		return fmt.Errorf("cannot use %q as a branch name; rename the workspace", branch)
	}
	root := c.String("project-root")
	if !orcaRootRE.MatchString(root) {
		return fmt.Errorf("refusing unsafe --project-root %q", root)
	}

	// Validated before the sandbox exists: a typo'd agent name should cost
	// nothing, not a create-then-destroy round trip.
	agents, err := orcaParseAgents(c.String("agents"))
	if err != nil {
		return err
	}

	// Do NOT set AutoPauseAfterSeconds. Resume is unsupported above, so a
	// sandbox that pauses itself becomes permanently unreachable.
	created, err := client.CreateSandbox(c.Context, api.SandboxCreateReq{
		Name:   orcaSandboxName(),
		Shape:  c.String("shape"),
		Rootfs: c.String("rootfs"),
	})
	if err != nil {
		return fmt.Errorf("sandbox create: %w", err)
	}
	id := created.ID
	orcaLog("created %s", id)

	// Everything past this point must tear the sandbox down on failure. A
	// surviving sandbox bills until someone notices.
	if err := orcaProvision(c, client, id, repoPath, root, branch, agents); err != nil {
		// Orca's UI only shows "Recipe exited with code 1", not our stderr, so
		// the reason has to be in this log line — a swallowed error here is a
		// swallowed error everywhere the user or a debugging session can see.
		orcaLog("create failed: %v", err)
		orcaLog("removing %s", id)
		// A signal-triggered failure means ctx is already cancelled, and an
		// already-cancelled context fails every request immediately — including
		// this cleanup call, which is the one call that must not skip. Give it
		// its own uncancelled window instead of inheriting ctx.
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelCleanup()
		if rmErr := client.DestroySandbox(cleanupCtx, id); rmErr != nil {
			orcaLog("WARNING destroy of %s failed: %v", id, rmErr)
			orcaLog("WARNING %s may still exist. Remove it with: createos sandbox rm --force %s", id, id)
		}
		_, _ = removeSSHBlock(sshAlias(id))
		removeDedicatedKey(sshAlias(id))
		return err
	}

	return json.NewEncoder(os.Stdout).Encode(orcaRecipeResult{
		SchemaVersion: orcaSchemaVersion,
		CheckoutMode:  "provisioned-root",
		Connection: orcaConnection{
			Type:        "ssh",
			ProjectRoot: root,
			// `configHost` is the ~/.ssh/config alias, so Orca resolves
			// HostName, HostKeyAlias, ProxyCommand and IdentityFile via `ssh -G`.
			Target: orcaTarget{
				Label:      id,
				ConfigHost: sshAlias(id),
				Host:       "127.0.0.1",
				Port:       22,
				Username:   orcaSSHUser,
			},
		},
		UserData: orcaUserData{Provider: "createos", ResourceID: id},
	})
}

func orcaProvision(c *cli.Context, client *api.SandboxClient, id, repoPath, root, branch string, agents []string) error {
	if err := orcaWireSSH(c, client, id, c.Duration("ssh-timeout")); err != nil {
		return err
	}
	if err := orcaSeedRepo(c, client, id, repoPath, root, branch); err != nil {
		return err
	}
	return orcaInstallAgents(c, client, id, agents)
}

// orcaAgentNames lists the installable agents for help text and errors.
func orcaAgentNames() string {
	names := make([]string, 0, len(orcaAgents))
	for name := range orcaAgents {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// orcaParseAgents turns the --agents value into install order, rejecting
// unknown names. Deduplicated so "claude,claude" installs once.
func orcaParseAgents(raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, field := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(field))
		if name == "" || seen[name] {
			continue
		}
		if _, ok := orcaAgents[name]; !ok {
			return nil, fmt.Errorf("unknown agent %q; pick from: %s", name, orcaAgentNames())
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// orcaInstallScript builds one agent's install script.
//
// Orca reaches the agent two ways: an interactive login shell, and a
// non-interactive exec that inherits the relay's env. The claude, codex and
// cursor installers only write to ~/.local/bin, which the second path need not
// have, so link into /usr/local/bin and judge presence on the system PATH
// rather than on the PATH this script sets.
//
// Outcome is reported by a trailing marker, not an exit code: `set -e` would
// propagate a vendor installer's own exit status, and a status reused as
// "already present" would silently turn a failed install into a skip.
//
// The binary name and install command are package constants, never caller
// input, so nothing here needs quoting against injection.
func orcaInstallScript(agent struct{ bin, install string }) string {
	return fmt.Sprintf(`set -eu
export PATH=%[1]s
sys_has() { (PATH=%[4]s; command -v "$1" >/dev/null 2>&1); }

if sys_has %[2]s; then echo %[5]s; exit 0; fi
command -v %[2]s >/dev/null 2>&1 || { %[3]s ; }
p=$(command -v %[2]s) || { echo "%[2]s not on PATH after install" >&2; exit 1; }
ln -sf "$p" /usr/local/bin/%[2]s
sys_has %[2]s || { echo "%[2]s not on the system PATH after linking" >&2; exit 1; }
echo %[6]s
`, orcaAgentPath, agent.bin, agent.install, orcaSystemPath, orcaAgentSkipped, orcaAgentInstalled)
}

// orcaLastLine returns the final non-blank line, which carries the install
// script's completion marker. Installers are chatty, so only the last line
// counts — a marker anywhere earlier is the vendor's output, not ours.
func orcaLastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// orcaInstallAgents installs each requested agent that is not already present.
//
// devbox:1 ships some of these already, and re-running a vendor installer over
// a good install is a needless minute and a needless failure mode, so each is
// skipped when its binary already resolves.
func orcaInstallAgents(c *cli.Context, client *api.SandboxClient, id string, agents []string) error {
	alias := sshAlias(id)
	if !editorAliasRE.MatchString(alias) {
		return fmt.Errorf("refusing to run over shell-unsafe SSH alias %q", alias)
	}
	for _, name := range agents {
		agent := orcaAgents[name]
		// Names are map keys, and the commands are constants — neither is
		// caller-controlled, so nothing here is interpolated from user input.
		//
		// Orca reaches the agent two ways: an interactive login shell, and a
		// non-interactive exec that inherits the relay's env. The claude, codex
		// and cursor installers only write to ~/.local/bin, which the second
		// path need not have, so link into /usr/local/bin and judge presence on
		// the system PATH rather than on the PATH this script sets.
		//
		// Outcome is reported by a trailing marker, not an exit code: `set -e`
		// would propagate a vendor installer's own exit status, and a status
		// reused as "already present" would silently turn a failed install into
		// a skip.
		script := orcaInstallScript(agent)

		start := time.Now()
		// Deliberately SSH, not ExecSandbox: the exec API runs in a different
		// mount namespace from sshd, so anything it installs outside the shared
		// /workspace is invisible to the SSH session Orca actually launches the
		// agent in. Verified: a file written via exec to /usr/local/bin cannot
		// be read back over SSH.
		var stdout, stderr strings.Builder
		// #nosec G204 -- alias is editorAliasRE-validated above; script is built
		// from constants only, never from caller input.
		cmd := exec.CommandContext(c.Context, "ssh",
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			alias, script)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("install %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
		}
		switch orcaLastLine(stdout.String()) {
		case orcaAgentInstalled:
			orcaLog("installed %s (%s) in %s", name, agent.bin, time.Since(start).Round(time.Second))
		case orcaAgentSkipped:
			orcaLog("%s already present, skipped", name)
		default:
			// Exit 0 without the marker means the script did not reach its end.
			return fmt.Errorf("install %s: no completion marker; last output: %q",
				name, orcaLastLine(stdout.String()))
		}
	}
	return nil
}

// orcaWireSSH registers a throwaway key, starts sshd, writes the ~/.ssh/config
// alias, and does not return until sshd accepts a connection.
//
// Returning early hands Orca a target it cannot dial, and Orca then tears the
// whole workspace down. `sandbox editor` only warns after 15 s, which is often
// shorter than a cold box needs.
func orcaWireSSH(c *cli.Context, client *api.SandboxClient, id string, wait time.Duration) error {
	alias := sshAlias(id)
	if !editorAliasRE.MatchString(alias) {
		return fmt.Errorf("refusing to write shell-unsafe SSH alias %q", alias)
	}
	privPath, pubBytes, _, err := ensureDedicatedKey(alias)
	if err != nil {
		return err
	}

	if err := orcaWaitRunning(c.Context, client, id, wait); err != nil {
		return err
	}
	sb, err := client.GetSandbox(c.Context, id)
	if err != nil {
		return fmt.Errorf("could not fetch sandbox %s: %w", id, err)
	}

	// The gateway authenticates against the sandbox row; the guest sshd reads
	// authorized_keys. The tunnel path needs both hops.
	if _, err := client.AddSSHPubkeys(c.Context, id, []string{strings.TrimSpace(string(pubBytes))}); err != nil {
		return fmt.Errorf("could not register key with gateway: %w", err)
	}
	if err := ensureAuthorizedKey(c, client, id, orcaSSHUser, id, pubBytes, true); err != nil {
		return fmt.Errorf("could not install key in guest: %w", err)
	}
	if err := startGuestSshd(c, client, id, orcaSSHUser); err != nil {
		return err
	}

	gwHost, gwPort := gatewayAddr()
	if !editorHostRE.MatchString(gwHost) {
		return fmt.Errorf("refusing shell-unsafe gateway host %q", gwHost)
	}
	sbIP := ""
	if sb.IP != nil {
		sbIP = strings.TrimSpace(*sb.IP)
	}
	sbName := ""
	if sb.Name != nil {
		sbName = *sb.Name
	}
	block, err := renderSSHBlock(alias, "tunnel", id, sbIP, gwHost, gwPort, orcaSSHUser, privPath, sbName)
	if err != nil {
		return err
	}
	if err := writeSSHBlock(alias, block); err != nil {
		return err
	}
	orcaLog("wrote ~/.ssh/config entry %s", alias)

	if err := probeSSH(c.Context, alias, wait); err != nil {
		return fmt.Errorf("sshd did not accept connections on %s within %s: %w", id, wait, err)
	}
	orcaLog("sshd accepting on %s", id)
	return nil
}

func orcaWaitRunning(ctx context.Context, client *api.SandboxClient, id string, wait time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		sb, err := client.GetSandbox(deadline, id)
		if err == nil && sb.Status == "running" {
			return nil
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("sandbox %s did not reach running within %s", id, wait)
		case <-time.After(time.Second):
		}
	}
}

// orcaSeedRepo mirrors the user's local working directory into the box.
//
// The point is local state, not history fidelity: uncommitted edits, staged
// changes, and untracked-but-not-ignored files all have to land, because the
// user expects the workspace to look like their laptop. `git push` cannot do
// that — it only moves committed objects — and it rides the SSH gateway
// tunnel, which measured far slower than the control-plane upload path.
//
// So: tar the working tree plus .git, upload once, unpack in the box. The
// tar is built in Go rather than shelling out to tar(1), because this command
// also runs on Windows.
//
// Ignored paths are skipped via `git ls-files`, which is what keeps
// node_modules and build output out of the payload.
func orcaSeedRepo(c *cli.Context, client *api.SandboxClient, id, repoPath, root, branch string) error {
	orcaLog("packing local state from %s", repoPath)
	packStart := time.Now()
	tarPath, size, fileCount, err := orcaBuildSeedTar(c.Context, repoPath)
	if err != nil {
		return fmt.Errorf("pack local state: %w", err)
	}
	defer func() { _ = os.Remove(tarPath) }() // #nosec G703 -- path is from os.CreateTemp, not user input.
	orcaLog("packed %d files, %s, in %s", fileCount, humanBytes(size), time.Since(packStart).Round(time.Second))

	f, err := os.Open(tarPath) // #nosec G304,G703 -- path is from os.CreateTemp, not user input.
	if err != nil {
		return fmt.Errorf("open packed tar: %w", err)
	}
	defer func() { _ = f.Close() }()

	orcaLog("uploading %s to %s", humanBytes(size), id)
	uploadStart := time.Now()
	const remoteTar = "/tmp/orca-seed.tar"
	if err := client.UploadFile(c.Context, id, remoteTar, f, size); err != nil {
		return fmt.Errorf("upload %s to %s after %s: %w", humanBytes(size), id, time.Since(uploadStart).Round(time.Second), err)
	}
	orcaLog("uploaded in %s", time.Since(uploadStart).Round(time.Second))

	// Orca adopts the checkout only when the branch equals the workspace name,
	// and the local branch is usually named something else, so re-point it here.
	// `checkout -B` keeps the working tree exactly as uploaded — including the
	// uncommitted changes that are the whole reason for this path.
	unpack := fmt.Sprintf(`set -eu
mkdir -p %[1]s
tar -xf %[2]s -C %[1]s
rm -f %[2]s
cd %[1]s
git checkout -q -B %[3]s
`, orcaShellQuote(root), remoteTar, orcaShellQuote(branch))
	if err := orcaExec(c.Context, client, id, unpack); err != nil {
		return fmt.Errorf("unpack seed: %w", err)
	}
	orcaLog("seeded %s onto branch %s", root, branch)
	return nil
}

// orcaBuildSeedTar writes a tar of the repository's live state to a temp file
// and returns its path and size. UploadFile needs the length up front, so the
// archive is staged on disk rather than streamed.
func orcaBuildSeedTar(ctx context.Context, repoPath string) (string, int64, int, error) {
	// Tracked + untracked, honouring .gitignore. This is exactly "what the user
	// would see", minus the build artifacts nobody wants to ship.
	out, err := exec.CommandContext(ctx, "git", "-C", repoPath, //#nosec G204,G702 -- repoPath is filepath.IsAbs-checked.
		"ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return "", 0, 0, fmt.Errorf("list local files: %w", err)
	}
	paths := make([]string, 0, 4096)
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	// .git carries the branch, the index, and the history Orca's remote git
	// reads, so it ships too even though ls-files never lists it.
	err = filepath.WalkDir(filepath.Join(repoPath, ".git"), // #nosec G703 -- repoPath is filepath.IsAbs-checked, ".git" is a literal.
		func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil //nolint:nilerr // an unreadable .git entry is not worth failing provisioning
			}
			rel, relErr := filepath.Rel(repoPath, p)
			if relErr == nil {
				paths = append(paths, filepath.ToSlash(rel))
			}
			return nil
		})
	if err != nil {
		return "", 0, 0, fmt.Errorf("walk .git: %w", err)
	}

	tmp, err := os.CreateTemp("", "orca-seed-*.tar")
	if err != nil {
		return "", 0, 0, err
	}
	defer func() { _ = tmp.Close() }()

	tw := tar.NewWriter(tmp)
	for _, rel := range paths {
		if err := orcaTarAppend(tw, repoPath, rel); err != nil {
			_ = tw.Close()
			return "", 0, 0, err
		}
	}
	if err := tw.Close(); err != nil {
		return "", 0, 0, err
	}
	info, err := tmp.Stat()
	if err != nil {
		return "", 0, 0, err
	}
	return tmp.Name(), info.Size(), len(paths), nil
}

func orcaTarAppend(tw *tar.Writer, repoPath, rel string) error {
	abs := filepath.Join(repoPath, rel)
	info, err := os.Lstat(abs) // #nosec G703 -- rel comes from git ls-files / a walk of repoPath, never "..".
	if err != nil {
		return nil //nolint:nilerr // a file deleted mid-walk is not fatal
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		if link, err = os.Readlink(abs); err != nil {
			return nil //nolint:nilerr
		}
	} else if !info.Mode().IsRegular() {
		return nil // sockets, fifos, devices have no place in a checkout
	}
	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	hdr.Name = rel
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if link != "" || info.Size() == 0 {
		return nil
	}
	f, err := os.Open(abs) // #nosec G304,G703 -- rel comes from git ls-files / a walk of repoPath.
	if err != nil {
		return nil //nolint:nilerr
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(tw, f)
	return err
}

func orcaDestroy(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("not signed in — run 'createos login'")
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read lifecycle payload: %w", err)
	}
	var payload orcaLifecyclePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("parse lifecycle payload: %w", err)
	}
	id := strings.TrimSpace(payload.RecipeResult.UserData.ResourceID)
	if id == "" {
		return fmt.Errorf("no resourceId in the lifecycle payload")
	}
	if err := client.DestroySandbox(c.Context, id); err != nil {
		return fmt.Errorf("destroy %s: %w", id, err)
	}
	_, _ = removeSSHBlock(sshAlias(id))
	removeDedicatedKey(sshAlias(id))
	orcaLog("destroyed %s", id)
	return nil
}

// -------------------------------------------------------------------- shared

func orcaSandboxName() string {
	raw := strings.TrimSpace(os.Getenv("ORCA_WORKSPACE_NAME"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("ORCA_VM_INSTANCE_ID"))
	}
	if raw == "" {
		raw = "workspace"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteRune('-')
		}
	}
	name := "orca-" + strings.Trim(b.String(), "-")
	if len(name) > 40 {
		name = strings.TrimRight(name[:40], "-")
	}
	return name
}

func orcaExec(ctx context.Context, client *api.SandboxClient, id, script string) error {
	resp, err := client.ExecSandbox(ctx, id, api.SandboxExecReq{
		Cmd:  "sh",
		Args: []string{"-c", script},
	})
	if err != nil {
		return err
	}
	if resp.Result.ExitCode != 0 {
		return fmt.Errorf("exit %d: %s", resp.Result.ExitCode, strings.TrimSpace(resp.Result.Stderr))
	}
	return nil
}

func orcaShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Only the recipe result JSON goes to stdout; Orca parses stdout.
func orcaLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "createos: "+format+"\n", args...)
}
