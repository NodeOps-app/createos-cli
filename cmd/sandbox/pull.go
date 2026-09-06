package sandbox

import (
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newPullCommand() *cli.Command {
	return &cli.Command{
		Name:      "pull",
		Aliases:   []string{"download", "cp-down"},
		Usage:     "Copy a file out of a sandbox",
		ArgsUsage: "<sandbox> <remote-path> <local-path|->",
		Description: `Download a file from a sandbox to your machine.
A local path of "-" streams the bytes to stdout — useful in pipes.

Examples:
  # Write to a real file
  createos sandbox pull my-box /workspace/result.csv ./result.csv

  # Stream to stdout
  createos sandbox pull my-box /workspace/result.csv - | head -5`,
		Action: runPull,
	}
}

func runPull(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	args := c.Args().Slice()
	if len(args) < 3 {
		return fmt.Errorf("please provide <sandbox> <remote-path> <local-path>\n\n  Example:\n    createos sandbox pull my-box /workspace/result.csv ./result.csv")
	}
	ref, remote, local := strings.TrimSpace(args[0]), args[1], args[2]
	if !strings.HasPrefix(remote, "/") {
		return fmt.Errorf("remote path must be absolute (got %q)\n\n  Example: /workspace/result.csv", remote)
	}

	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return err
	}
	if runErr := ensureSandboxRunningFor(c, client, ref, id, "pull"); runErr != nil {
		return runErr
	}

	// "-" writes to stdout. Anything else is a real file we create.
	if local == "-" {
		_, err = client.DownloadFile(c.Context, id, remote, os.Stdout)
		return err
	}

	f, err := os.Create(local) // #nosec G304 -- local is a user-supplied destination path
	if err != nil {
		return fmt.Errorf("could not create %s: %w", local, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck

	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Downloading %s:%s → %s", refLabel(ref, id), remote, local)) //nolint:errcheck
	n, err := client.DownloadFile(c.Context, id, remote, f)
	if err != nil {
		spinner.Fail("Download failed")
		_ = os.Remove(local) //nolint:errcheck // don't leave a half-written file behind
		return err
	}
	spinner.Success(fmt.Sprintf("Downloaded %s:%s → %s (%s)", refLabel(ref, id), remote, local, humanBytes(n)))
	return nil
}
