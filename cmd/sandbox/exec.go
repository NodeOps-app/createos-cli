package sandbox

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newExecCommand() *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "Run a command inside a sandbox",
		ArgsUsage: "<sandbox> -- <cmd> [args…]",
		Description: `Run a one-shot command inside a sandbox. Anything after the literal
'--' becomes the command. The default is a buffered exec — output
arrives all at once when the command finishes. Pass --stream to see
stdout/stderr live as it happens.

Examples:
  createos sandbox exec my-box -- uname -a
  createos sandbox exec my-box -- python3 -c 'print("hi")'
  createos sandbox exec my-box --stream -- pip install requests
  createos sandbox exec my-box -- bash -c "echo $USER && date"

The command's exit code is preserved — if the program inside the
sandbox exits with 1, this CLI also exits with 1.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "stream",
				Aliases: []string{"s"},
				Usage:   "Show output live as the command runs",
			},
			&cli.StringSliceFlag{
				Name: "env",
				Usage: "Override an environment variable for this exec (repeatable): KEY=VALUE. " +
					"The KEY must have been declared at sandbox create time " +
					"(with `createos sandbox create --env KEY=value`). To add a fresh var " +
					"inline, prefix your command: `bash -c 'FOO=bar mycmd'`.",
			},
		},
		Action: runExec,
	}
}

func runExec(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	ref, cmd, args := parseExecArgs(c)

	// Resolve / pick the sandbox first.
	var id string
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  Example:\n    createos sandbox exec my-box -- ls -la")
		}
		pickedID, label, perr := pickByStatus(c, client, "Run a command in which sandbox?", "running")
		if perr != nil {
			return perr
		}
		if pickedID == "" {
			fmt.Println("Cancelled. Nothing ran.")
			return nil
		}
		id = pickedID
		ref = label
	} else {
		resolved, err := resolveSandboxRef(c.Context, client, ref)
		if err != nil {
			return err
		}
		id = resolved
	}

	// Then ask for the command if we don't have one yet.
	if cmd == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please pass the command to run after '--'\n\n  Example:\n    createos sandbox exec %s -- ls -la", ref)
		}
		line, perr := pterm.DefaultInteractiveTextInput.
			WithDefaultText(fmt.Sprintf("Command to run in %s (e.g. `ls -la`)", refLabel(ref, id))).
			Show()
		if perr != nil {
			return fmt.Errorf("could not read your command: %w", perr)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			fmt.Println("Cancelled. Nothing ran.")
			return nil
		}
		fields := strings.Fields(line)
		cmd = fields[0]
		if len(fields) > 1 {
			args = fields[1:]
		}
	}

	envs, err := parseEnvFlags(c.StringSlice("env"))
	if err != nil {
		return err
	}
	req := api.SandboxExecReq{
		Cmd:  cmd,
		Args: args,
		Env:  envs,
	}

	if c.Bool("stream") {
		return runExecStream(c, client, id, req)
	}
	return runExecBuffered(c, client, id, req)
}

func runExecBuffered(c *cli.Context, client *api.SandboxClient, id string, req api.SandboxExecReq) error {
	resp, err := client.ExecSandbox(c.Context, id, req)
	if err != nil {
		return err
	}
	if resp.Result.Stdout != "" {
		fmt.Print(resp.Result.Stdout)
		if !strings.HasSuffix(resp.Result.Stdout, "\n") {
			fmt.Println()
		}
	}
	if resp.Result.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Result.Stderr)
		if !strings.HasSuffix(resp.Result.Stderr, "\n") {
			fmt.Fprintln(os.Stderr)
		}
	}
	if resp.Result.Error != "" {
		pterm.Error.Println(resp.Result.Error)
	}
	if resp.Result.ExitCode != 0 {
		// Preserve the inner command's exit code so scripts can check $?.
		os.Exit(resp.Result.ExitCode)
	}
	return nil
}

func runExecStream(c *cli.Context, client *api.SandboxClient, id string, req api.SandboxExecReq) error {
	exit, err := client.ExecSandboxStream(c.Context, id, req, func(ev api.SandboxExecStreamEvent) {
		switch {
		case ev.Stdout != "":
			_, _ = io.WriteString(os.Stdout, ev.Stdout)
		case ev.Stderr != "":
			_, _ = io.WriteString(os.Stderr, ev.Stderr)
		case ev.Error != "":
			pterm.Error.Println(ev.Error)
		}
		// HB / exit_code frames are not user-visible.
	})
	if err != nil {
		return err
	}
	if exit > 0 {
		os.Exit(exit)
	}
	return nil
}

// parseExecArgs splits the positional args at the literal '--'.
// Everything before `--` is the sandbox ref (zero or one token);
// everything after is cmd + args.
//
// Both forms work:
//
//   createos sandbox exec my-box -- ls -la   # explicit separator
//   createos sandbox exec my-box ls -la      # implicit (first token = ref)
//   createos sandbox exec -- ls -la          # no ref, picker on TTY
//   createos sandbox exec                    # nothing — picker + prompt
//
// urfave/cli v2 strips a LEADING `--` (it interprets that as
// "end-of-flags" and consumes the token). To distinguish
// `exec -- ls` (no ref, cmd=ls) from `exec my-box ls` (ref=my-box,
// cmd=ls), we re-scan os.Args ourselves and look for a `--` between
// the literal `exec` token and the first positional.
func parseExecArgs(c *cli.Context) (ref, cmd string, args []string) {
	all := c.Args().Slice()
	if len(all) == 0 {
		return "", "", nil
	}

	// First: did the user write `... exec -- …`? Scan os.Args.
	leadingDoubleDash := false
	for i, a := range os.Args {
		if a == "exec" && i+1 < len(os.Args) && os.Args[i+1] == "--" {
			leadingDoubleDash = true
			break
		}
	}

	// Find any `--` that urfave passed through (will be present when it
	// sits between positionals, e.g. `exec my-box -- ls`).
	sep := -1
	for i, a := range all {
		if a == "--" {
			sep = i
			break
		}
	}

	switch {
	case leadingDoubleDash:
		// `exec -- cmd args…` → no ref; everything is cmd+args.
		cmd = all[0]
		if len(all) > 1 {
			args = all[1:]
		}
	case sep >= 0:
		// `exec ref -- cmd args…`
		if sep > 0 {
			ref = strings.TrimSpace(all[0])
		}
		rest := all[sep+1:]
		if len(rest) > 0 {
			cmd = rest[0]
			if len(rest) > 1 {
				args = rest[1:]
			}
		}
	default:
		// `exec ref [cmd args…]`
		ref = strings.TrimSpace(all[0])
		rest := all[1:]
		if len(rest) > 0 {
			cmd = rest[0]
			if len(rest) > 1 {
				args = rest[1:]
			}
		}
	}
	return ref, cmd, args
}
