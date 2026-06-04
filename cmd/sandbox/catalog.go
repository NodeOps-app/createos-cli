package sandbox

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

// newShapesCommand lists the static VM size catalog.
func newShapesCommand() *cli.Command {
	return &cli.Command{
		Name:   "shapes",
		Usage:  "List the available sandbox sizes (vCPU / RAM / disk)",
		Action: runShapes,
	}
}

func runShapes(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	shapes, err := client.ListShapes(c.Context)
	if err != nil {
		return err
	}
	output.Render(c, shapes, func() {
		if len(shapes) == 0 {
			fmt.Println("No sizes available.")
			return
		}
		table := pterm.TableData{{"ID", "vCPU", "RAM", "Default disk"}}
		for _, s := range shapes {
			table = append(table, []string{
				s.ID,
				fmt.Sprintf("%d", s.VCPU),
				fmt.Sprintf("%d MB", s.MemMib),
				fmt.Sprintf("%d MB", s.DefaultDiskMib),
			})
		}
		_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render() //nolint:errcheck
		pterm.Println()
		pterm.Println(pterm.Gray("  Pick one when creating: createos sandbox create --shape <id>"))
	})
	return nil
}

// newRootfsCommand lists the built-in rootfs images that any sandbox
// can boot from. User-built templates are not included here — see
// `sandbox template ls`.
func newRootfsCommand() *cli.Command {
	return &cli.Command{
		Name:    "rootfs",
		Aliases: []string{"images"},
		Usage:   "List the built-in OS images you can boot a sandbox from",
		Action:  runRootfs,
	}
}

func runRootfs(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	cat, err := client.ListRootfs(c.Context)
	if err != nil {
		return err
	}
	output.Render(c, cat, func() {
		if cat == nil || len(cat.Rootfs) == 0 {
			fmt.Println("No built-in images available.")
			return
		}
		// Use the per-entry view when the server provides it; otherwise
		// just the names.
		hasEntries := len(cat.Entries) > 0
		if hasEntries {
			table := pterm.TableData{{"Name", "Description", "Status"}}
			for _, e := range cat.Entries {
				status := ""
				switch {
				case e.Name == cat.Default:
					status = "default"
				case e.Deprecated:
					status = "deprecated"
					if e.Successor != "" {
						status += " → " + e.Successor
					}
				}
				table = append(table, []string{e.Name, e.Description, status})
			}
			_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render() //nolint:errcheck
		} else {
			table := pterm.TableData{{"Name", "Default"}}
			for _, name := range cat.Rootfs {
				def := ""
				if name == cat.Default {
					def = "yes"
				}
				table = append(table, []string{name, def})
			}
			_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render() //nolint:errcheck
		}
		pterm.Println()
		pterm.Println(pterm.Gray("  Pick one when creating: createos sandbox create --rootfs <name>"))
		pterm.Println(pterm.Gray("  To list your own custom templates: createos sandbox template ls"))
	})
	return nil
}
