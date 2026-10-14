package sandbox

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
)

// Claude Code, Codex and Pi each ship an installer of their own, so setup's
// whole job for them is: check the prerequisites, then drive that installer
// with the right arguments. setupHost describes one of them; setupHostAction
// is the shared runner.
//
// The hosts that have no installer — OpenCode and DeepSeek Harness — need a
// checkout and a config edit instead, and live in their own files.
type setupHost struct {
	name    string
	aliases []string
	usage   string
	// bin is the host's own CLI, and hint tells the user where to get it.
	bin  string
	hint string
	// steps run in order; each failure reports the host's own output, and a
	// step whose output says the work was already done is treated as done.
	steps []setupStep
	// next is printed on success: the first thing to actually try.
	next string
}

type setupStep struct {
	label string
	args  []string
}

func newSetupHostCommand(h setupHost) *cli.Command {
	return &cli.Command{
		Name:    h.name,
		Aliases: h.aliases,
		Usage:   h.usage,
		Description: fmt.Sprintf(
			"Installs the CreateOS Sandbox plugin into %s using its own\n"+
				"installer, so work can run in a disposable sandbox instead of on\n"+
				"your laptop.\n\n"+
				"Run with --doctor first if you only want to check the prerequisites.",
			h.bin),
		Flags:  []cli.Flag{setupDoctorFlag()},
		Action: func(c *cli.Context) error { return setupHostAction(c, h) },
	}
}

func setupHostAction(c *cli.Context, h setupHost) error {
	if err := setupSignedIn(c); err != nil {
		return err
	}
	bin, err := setupRequireBin(h.bin, h.hint)
	if err != nil {
		return err
	}
	if c.Bool("doctor") {
		setupDoctorDone()
		return nil
	}

	for _, step := range h.steps {
		fmt.Println(step.label)
		out, runErr := setupRun(c.Context, bin, step.args...)
		if runErr == nil {
			continue
		}
		// These installers exit non-zero when the marketplace or plugin is
		// already there. Re-running setup on a configured machine is a normal
		// thing to do, so that is a success, not a failure.
		if setupAlreadyDone(out) {
			fmt.Println("  already done, skipping")
			continue
		}
		return fmt.Errorf("%s %s failed: %w\n%s",
			h.bin, strings.Join(step.args, " "), runErr, strings.TrimSpace(out))
	}

	fmt.Printf("\nDone. %s\n", h.next)
	return nil
}

func newSetupClaudeCodeCommand() *cli.Command {
	return newSetupHostCommand(setupHost{
		name:    "claude-code",
		aliases: []string{"claude"},
		usage:   "Set up Claude Code to offload work to CreateOS Sandboxes",
		bin:     "claude",
		hint:    "install it from https://docs.claude.com/en/docs/claude-code",
		steps: []setupStep{
			{
				label: "adding the CreateOS marketplace",
				args:  []string{"plugin", "marketplace", "add", setupPluginRepo},
			},
			{
				label: "installing createos-sandbox",
				// --yes so the install completes when stdout is not a terminal;
				// without it Claude Code refuses to run unattended.
				args: []string{"plugin", "install", "createos-sandbox@" + setupMarketplaceName, "--yes"},
			},
		},
		next: "Start a new Claude Code session, then offload a heavy command:\n" +
			"  /createos-sandbox:offload . \"npm ci && npm test\"\n" +
			"Claude also reaches for a sandbox on its own once the skill loads.",
	})
}

func newSetupCodexCommand() *cli.Command {
	return newSetupHostCommand(setupHost{
		name:  "codex",
		usage: "Set up Codex to offload work to CreateOS Sandboxes",
		bin:   "codex",
		hint:  "install it from https://github.com/openai/codex",
		steps: []setupStep{
			{
				label: "adding the CreateOS marketplace",
				args:  []string{"plugin", "marketplace", "add", setupPluginRepo},
			},
			{
				label: "installing createos-sandbox-codex",
				args:  []string{"plugin", "add", "createos-sandbox-codex", "--marketplace", setupMarketplaceName},
			},
		},
		next: "Run codex. Its session-start hook checks your sign-in and the skill\n" +
			"teaches it when to move work off your machine.",
	})
}

func newSetupPiCommand() *cli.Command {
	return newSetupHostCommand(setupHost{
		name:  "pi",
		usage: "Set up Pi to run its tools in CreateOS Sandboxes",
		bin:   "pi",
		hint:  "install it from https://github.com/anthropics/pi",
		steps: []setupStep{
			{
				label: "installing the CreateOS extension",
				args:  []string{"install", "git:github.com/" + setupPluginRepo},
			},
		},
		next: "Run pi — the sandbox tools are available right away.\n" +
			"Add --inside-createos-sandbox to run Pi's own tools in a sandbox,\n" +
			"and --createos-sync-once to copy this project into it first.",
	})
}
