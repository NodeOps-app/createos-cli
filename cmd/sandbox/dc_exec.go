package sandbox

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// newDCExecCommand wires `createos sandbox dc exec`.
//
// Wraps the user's command in `docker compose exec -T <service> <cmd>`
// and runs it through the existing exec stream API. -T is always set —
// the exec stream isn't a real PTY, and 'docker compose exec' refuses
// to attach to one over a non-TTY pipe.
//
// For a real interactive shell (vim, htop, psql prompt) use
// `createos sandbox shell <sandbox>` to land in the VM, then
// `docker compose exec <svc> <cmd>` inside.
func newDCExecCommand() *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "Run a command inside one of the compose service containers",
		ArgsUsage: "<service> -- <cmd> [args…]",
		Description: `Wraps the inner command in 'docker compose exec -T <service> ...'
inside the sandbox so it runs in the right container. Streams stdout
and stderr live. Exit code is preserved.

For an interactive PTY (psql prompt, vim, htop) use:
  createos sb shell <sandbox-id>
  # then inside the VM:
  docker compose -p <project> exec <svc> bash

Examples:
  createos sb dc exec db -- psql -U dev -d app -c 'SELECT 42'
  createos sb dc exec web -- ls /etc/nginx
  echo 'SELECT now()' | createos sb dc exec db -- psql -U dev -d app`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "Path to docker-compose.yml (default: ./docker-compose.yml)",
				Value:   "docker-compose.yml",
			},
			&cli.StringFlag{
				Name:    "user",
				Aliases: []string{"u"},
				Usage:   "Run as this uid:gid inside the container",
			},
			&cli.StringFlag{
				Name:    "workdir",
				Aliases: []string{"w"},
				Usage:   "Working directory inside the container",
			},
			&cli.StringSliceFlag{
				Name:    "env",
				Aliases: []string{"e"},
				Usage:   "Set an environment variable (repeatable): KEY=VALUE",
			},
		},
		Action: runDCExec,
	}
}

func runDCExec(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	_, lock, err := loadDCProject(c.String("file"))
	if err != nil {
		return err
	}

	// Positional args: <service> [--] <cmd> [args...]
	// Mirror cmd/sandbox/exec.go's tolerant handling — accept either
	// the explicit '--' separator or just trailing tokens.
	service, innerCmd, innerArgs, err := splitDCExecArgs(c)
	if err != nil {
		return err
	}

	// Build: docker compose -p <project> -f <file> exec -T [-u U] [-w W] [-e K=V…] <service> <cmd> <args...>
	composeArgs := []string{
		"compose",
		"-p", lock.ProjectName,
		"-f", lock.ComposeFile,
		"exec",
		"-T",
	}
	if v := strings.TrimSpace(c.String("user")); v != "" {
		composeArgs = append(composeArgs, "-u", v)
	}
	if v := strings.TrimSpace(c.String("workdir")); v != "" {
		composeArgs = append(composeArgs, "-w", v)
	}
	for _, env := range c.StringSlice("env") {
		env = strings.TrimSpace(env)
		if env == "" {
			continue
		}
		composeArgs = append(composeArgs, "-e", env)
	}
	composeArgs = append(composeArgs, service, innerCmd)
	composeArgs = append(composeArgs, innerArgs...)

	// Forward piped stdin (one-shot — the API takes the whole buffer as a
	// string). When stdin is a real TTY we leave it empty so the inner
	// command's stdin stays bound to /dev/null inside the container.
	req := api.SandboxExecReq{
		Cmd:  "docker",
		Args: composeArgs,
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) { // #nosec G115 -- fd fits in int
		buf, rerr := io.ReadAll(os.Stdin)
		if rerr != nil {
			return fmt.Errorf("read stdin: %w", rerr)
		}
		req.Stdin = string(buf)
	}

	exit, err := client.ExecSandboxStream(c.Context, lock.SandboxID, req, func(ev api.SandboxExecStreamEvent) {
		switch {
		case ev.Stdout != "":
			_, _ = io.WriteString(os.Stdout, ev.Stdout) //nolint:errcheck
		case ev.Stderr != "":
			_, _ = io.WriteString(os.Stderr, ev.Stderr) //nolint:errcheck
		case ev.Error != "":
			pterm.Error.Println(ev.Error)
		}
	})
	if err != nil {
		return err
	}
	if exit != 0 {
		os.Exit(exit)
	}
	return nil
}

// splitDCExecArgs reads the positional arguments and pulls out
// (service, cmd, args). Accepts both:
//
//	createos sb dc exec web -- ls -la
//	createos sb dc exec web ls -la
//
// Returns a friendly error when service or cmd is missing.
func splitDCExecArgs(c *cli.Context) (service, cmd string, args []string, err error) {
	all := c.Args().Slice()
	if len(all) == 0 {
		return "", "", nil, fmt.Errorf("missing <service> and command\n\n  Example:\n    createos sb dc exec web -- ls /etc/nginx")
	}
	service = strings.TrimSpace(all[0])
	rest := all[1:]
	// Drop an optional '--' between service and cmd.
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return "", "", nil, fmt.Errorf("missing command to run inside %s\n\n  Example:\n    createos sb dc exec %s -- ls /etc/nginx", service, service)
	}
	cmd = rest[0]
	if len(rest) > 1 {
		args = rest[1:]
	}
	return service, cmd, args, nil
}
