package sandbox

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newSnapshotCommand() *cli.Command {
	return &cli.Command{Name: "snapshot", Aliases: []string{"snapshots"}, Usage: "Manage reusable disk and full snapshots", Subcommands: []*cli.Command{
		{Name: "create", Usage: "Capture a running sandbox without external disks", ArgsUsage: "<sandbox-id|name>", Flags: []cli.Flag{
			&cli.StringFlag{Name: "type", Required: true, Usage: "Capture type: disk or full"},
			&cli.StringFlag{Name: "name", Usage: "Optional display label (snapshots are addressed by ID)"},
		}, Action: runSnapshotCreate},
		{Name: "list", Aliases: []string{"ls"}, Usage: "List snapshots", Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Value: 50, Usage: "Page size (1–500)"},
			&cli.IntFlag{Name: "offset", Usage: "Number of snapshots to skip"},
		}, Action: runSnapshotList},
		{Name: "get", Aliases: []string{"show"}, Usage: "Show snapshot details and capture status", ArgsUsage: "<snapshot-id>", Action: runSnapshotGet},
		{Name: "rm", Aliases: []string{"delete"}, Usage: "Delete a snapshot", ArgsUsage: "<snapshot-id>", Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Aliases: []string{"yes", "y"}, Usage: "Skip deletion confirmation"},
		}, Action: runSnapshotDelete},
	}}
}

func snapshotClient(c *cli.Context) (*api.SandboxClient, error) {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return nil, fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	return client, nil
}

func snapshotArg(c *cli.Context) (string, error) {
	if c.NArg() != 1 || strings.HasPrefix(c.Args().First(), "-") {
		return "", fmt.Errorf("provide exactly one ID; place flags before the ID")
	}
	return c.Args().First(), nil
}

func runSnapshotCreate(c *cli.Context) error {
	ref, err := snapshotArg(c)
	if err != nil {
		return err
	}
	kind := c.String("type")
	if kind != "disk" && kind != "full" {
		return fmt.Errorf("--type must be disk or full")
	}
	client, err := snapshotClient(c)
	if err != nil {
		return err
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return err
	}
	view, err := client.CreateSnapshot(c.Context, id, api.SnapshotCreateReq{Type: kind, Name: strings.TrimSpace(c.String("name"))})
	if err != nil {
		return err
	}
	output.Render(c, view, func() {
		printSnapshots([]api.SnapshotView{*view})
		fmt.Printf("Check capture progress: createos sandbox snapshot get %s\n", view.ID)
	})
	return nil
}

func runSnapshotList(c *cli.Context) error {
	if c.NArg() != 0 {
		return fmt.Errorf("snapshot list takes no positional arguments")
	}
	if c.Int("limit") < 1 || c.Int("limit") > 500 || c.Int("offset") < 0 {
		return fmt.Errorf("--limit must be 1–500 and --offset must be nonnegative")
	}
	client, err := snapshotClient(c)
	if err != nil {
		return err
	}
	page, err := client.ListSnapshots(c.Context, c.Int("limit"), c.Int("offset"))
	if err != nil {
		return err
	}
	output.Render(c, page, func() { printSnapshots(page.Data) })
	return nil
}

func runSnapshotGet(c *cli.Context) error {
	id, err := snapshotArg(c)
	if err != nil {
		return err
	}
	client, err := snapshotClient(c)
	if err != nil {
		return err
	}
	view, err := client.GetSnapshot(c.Context, id)
	if err != nil {
		return err
	}
	output.Render(c, view, func() {
		printSnapshots([]api.SnapshotView{*view})
		fmt.Printf("Source: %s\nShape: %s (%d vCPU, %d MiB memory, %d MiB disk)\nCreated: %s\n", view.SandboxID, view.Shape, view.VCPU, view.MemMib, view.DiskMib, view.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	})
	return nil
}

func runSnapshotDelete(c *cli.Context) error {
	id, err := snapshotArg(c)
	if err != nil {
		return err
	}
	client, err := snapshotClient(c)
	if err != nil {
		return err
	}
	if !c.Bool("force") {
		if !terminal.IsInteractive() {
			return fmt.Errorf("use --force before the snapshot ID to confirm deletion")
		}
		confirmed, confirmErr := pterm.DefaultInteractiveConfirm.WithDefaultValue(false).Show("Delete snapshot " + id + "?")
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return nil
		}
	}
	if err := client.DeleteSnapshot(c.Context, id); err != nil {
		return err
	}
	output.Render(c, map[string]string{"id": id, "status": "deleted"}, func() { fmt.Printf("Deleted snapshot %s\n", id) })
	return nil
}

func printSnapshots(snapshots []api.SnapshotView) {
	if len(snapshots) == 0 {
		fmt.Println("No snapshots found.")
		return
	}
	rows := pterm.TableData{{"ID", "Name", "Type", "Status", "Size"}}
	for _, snapshot := range snapshots {
		name := ""
		if snapshot.Name != nil {
			name = *snapshot.Name
		}
		rows = append(rows, []string{snapshot.ID, name, snapshot.Type, snapshot.Status, humanBytes(snapshot.SizeBytes)})
	}
	_ = output.RenderTable(rows) //nolint:errcheck
}
