package sandbox

import (
	"encoding/base64"
	"fmt"
	"path"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newCheckpointCommand() *cli.Command {
	confirmation := func() []cli.Flag {
		return []cli.Flag{&cli.BoolFlag{Name: "force", Aliases: []string{"yes", "y"}, Usage: "Skip confirmation (required in scripts)"}}
	}
	return &cli.Command{Name: "checkpoint", Aliases: []string{"checkpoints"}, Usage: "Save and restore directory history inside a running sandbox", Description: "Stop directory writers before create or restore. Checkpoints stay on the sandbox disk and are lost when it is deleted. Place flags before positional arguments.", Subcommands: []*cli.Command{
		{Name: "create", Usage: "Save a directory's current file state", ArgsUsage: "<sandbox> <absolute-path>", Flags: []cli.Flag{&cli.StringFlag{Name: "name", Usage: "Optional display label"}}, Action: runCheckpointCreate},
		{Name: "list", Aliases: []string{"ls"}, Usage: "List directory checkpoints", ArgsUsage: "<sandbox>", Action: runCheckpointList},
		{Name: "restore", Usage: "Replace a directory's current contents with a checkpoint", ArgsUsage: "<sandbox> <checkpoint-id>", Flags: confirmation(), Action: runCheckpointRestore},
		{Name: "rm", Aliases: []string{"delete"}, Usage: "Delete a checkpoint record", ArgsUsage: "<sandbox> <checkpoint-id>", Flags: confirmation(), Action: runCheckpointDelete},
	}}
}

func checkpointArgs(c *cli.Context, count int) error {
	if c.NArg() != count {
		return fmt.Errorf("expected %d arguments; place flags before positional arguments", count)
	}
	for _, arg := range c.Args().Slice() {
		if strings.TrimSpace(arg) == "" || strings.HasPrefix(arg, "-") {
			return fmt.Errorf("invalid argument; place flags before positional arguments")
		}
	}
	return nil
}

func checkpointTarget(c *cli.Context) (*api.SandboxClient, string, error) {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return nil, "", fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := c.Args().First()
	// Full IDs also work with sandbox-scoped credentials, which cannot list all sandboxes.
	if strings.HasPrefix(ref, sandboxIDPrefix) {
		return client, ref, nil
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	return client, id, err
}

func runCheckpointCreate(c *cli.Context) error {
	if err := checkpointArgs(c, 2); err != nil {
		return err
	}
	directory := c.Args().Get(1)
	if !path.IsAbs(directory) {
		return fmt.Errorf("checkpoint path must be absolute inside the sandbox")
	}
	client, id, err := checkpointTarget(c)
	if err != nil {
		return err
	}
	result, err := client.CreateCheckpoint(c.Context, id, api.CheckpointCreateReq{Path: directory, Name: c.String("name")})
	if err != nil {
		return err
	}
	output.Render(c, result, func() { printCheckpoints([]api.DirectoryCheckpoint{*result}) })
	return nil
}

func runCheckpointList(c *cli.Context) error {
	if err := checkpointArgs(c, 1); err != nil {
		return err
	}
	client, id, err := checkpointTarget(c)
	if err != nil {
		return err
	}
	result, err := client.ListCheckpoints(c.Context, id)
	if err != nil {
		return err
	}
	if result == nil {
		result = []api.DirectoryCheckpoint{}
	}
	output.Render(c, result, func() { printCheckpoints(result) })
	return nil
}

func checkpointConfirm(c *cli.Context, action string) (bool, error) {
	if err := checkpointArgs(c, 2); err != nil {
		return false, err
	}
	checkpointID := c.Args().Get(1)
	raw, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(checkpointID, "cp_"))
	if !strings.HasPrefix(checkpointID, "cp_") || err != nil || len(raw) != 16 {
		return false, fmt.Errorf("provide a valid cp_ checkpoint ID, not its display name")
	}
	if c.Bool("force") {
		return true, nil
	}
	if !terminal.IsInteractive() {
		return false, fmt.Errorf("use --force before the sandbox ID to confirm %s", action)
	}
	prompt := action + " checkpoint " + checkpointID + "?"
	if action == "restore" {
		prompt += " This replaces current directory contents; stop writers first."
	}
	return pterm.DefaultInteractiveConfirm.WithDefaultValue(false).Show(prompt)
}

func runCheckpointRestore(c *cli.Context) error {
	confirmed, err := checkpointConfirm(c, "restore")
	if err != nil || !confirmed {
		return err
	}
	client, id, err := checkpointTarget(c)
	if err != nil {
		return err
	}
	result, err := client.RestoreCheckpoint(c.Context, id, c.Args().Get(1))
	if err != nil {
		return err
	}
	output.Render(c, result, func() { fmt.Printf("Restored %s at %s\n", result.CheckpointID, result.Path) })
	return nil
}

func runCheckpointDelete(c *cli.Context) error {
	confirmed, err := checkpointConfirm(c, "delete")
	if err != nil || !confirmed {
		return err
	}
	client, id, err := checkpointTarget(c)
	if err != nil {
		return err
	}
	checkpointID := c.Args().Get(1)
	if err := client.DeleteCheckpoint(c.Context, id, checkpointID); err != nil {
		return err
	}
	output.Render(c, map[string]string{"id": checkpointID, "status": "deleted"}, func() { fmt.Printf("Deleted checkpoint %s\n", checkpointID) })
	return nil
}

func printCheckpoints(checkpoints []api.DirectoryCheckpoint) {
	if len(checkpoints) == 0 {
		fmt.Println("No checkpoints found.")
		return
	}
	rows := pterm.TableData{{"ID", "Path", "Name", "Parent", "Created"}}
	for _, cp := range checkpoints {
		name, parent := "", ""
		if cp.Name != nil {
			name = *cp.Name
		}
		if cp.ParentID != nil {
			parent = *cp.ParentID
		}
		rows = append(rows, []string{cp.ID, cp.Path, name, parent, cp.CreatedAt.Local().Format("2006-01-02 15:04:05")})
	}
	_ = output.RenderTable(rows) //nolint:errcheck
}
