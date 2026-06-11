package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/dclock"
)

// newDCLogsCommand wires `createos sandbox dc logs`.
//
// Streams `docker compose logs -f` from inside the sandbox over the
// existing /v1/sandboxes/:id/exec?stream=true API. NDJSON frames land
// in the callback and we copy stdout/stderr through as they arrive.
// Ctrl+C cancels the underlying HTTP context which terminates the
// remote process.
func newDCLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Tail logs from one or more compose services",
		ArgsUsage: "[service...]",
		Description: `Streams logs from compose services running inside the sandbox.
Omitting service names tails every service. Ctrl+C to stop.

Examples:
  createos sb dc logs
  createos sb dc logs web
  createos sb dc logs --tail 200 web db
  createos sb dc logs -f docker-compose.dev.yml worker`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "Path to docker-compose.yml (default: ./docker-compose.yml)",
				Value:   "docker-compose.yml",
			},
			&cli.BoolFlag{
				Name:  "follow",
				Usage: "Follow log output (default true; pass --follow=false for a one-shot dump)",
				Value: true,
			},
			&cli.IntFlag{
				Name:  "tail",
				Usage: "Number of lines to show from the end of the logs per service",
				Value: 100,
			},
			&cli.BoolFlag{
				Name:    "timestamps",
				Aliases: []string{"t"},
				Usage:   "Show timestamps",
			},
			&cli.BoolFlag{
				Name:  "no-color",
				Usage: "Disable color in compose output",
			},
		},
		Action: runDCLogs,
	}
}

func runDCLogs(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	projectDir, lock, err := loadDCProject(c.String("file"))
	if err != nil {
		return err
	}
	_ = projectDir // not yet used here; will be by 'up'

	args := []string{
		"compose",
		"-f", lock.ComposeFile,
		"-p", lock.ProjectName,
		"logs",
	}
	if c.Bool("follow") {
		args = append(args, "--follow")
	}
	if c.Int("tail") > 0 {
		args = append(args, "--tail", strconv.Itoa(c.Int("tail")))
	}
	if c.Bool("timestamps") {
		args = append(args, "--timestamps")
	}
	if c.Bool("no-color") {
		args = append(args, "--no-color")
	}
	// Trailing positionals = service names to scope the tail to.
	args = append(args, c.Args().Slice()...)

	// Note: compose's `-f <absolute path>` resolves relative paths in
	// the file (./src, ./pgdata, ...) against the compose file's dir,
	// not the process cwd — so we don't need to chdir into RemoteWorkdir
	// here. RemoteWorkdir is kept in the lockfile for `dc exec`'s use
	// (which often does want a real working directory).
	req := api.SandboxExecReq{
		Cmd:  "docker",
		Args: args,
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
	if exit > 0 {
		os.Exit(exit)
	}
	return nil
}

// loadDCProject resolves the compose file path relative to cwd, then
// loads the per-project lockfile from <projectDir>/.createos/dc.lock.
// projectDir is the directory CONTAINING the compose file.
//
// Errors translate ErrNotFound into a friendly hint so users see
// "run 'createos sb dc up' first" instead of a raw filesystem error.
func loadDCProject(composeFlag string) (projectDir string, lock *dclock.Lock, err error) {
	if composeFlag == "" {
		composeFlag = "docker-compose.yml"
	}
	abs, err := filepath.Abs(composeFlag)
	if err != nil {
		return "", nil, fmt.Errorf("resolve %s: %w", composeFlag, err)
	}
	projectDir = filepath.Dir(abs)
	lock, err = dclock.Load(projectDir)
	if errors.Is(err, dclock.ErrNotFound) {
		return "", nil, fmt.Errorf("no compose project here — run 'createos sb dc up' first (looked in %s)", filepath.Join(projectDir, dclock.DirName, dclock.FileName))
	}
	if err != nil {
		return "", nil, err
	}
	return projectDir, lock, nil
}
