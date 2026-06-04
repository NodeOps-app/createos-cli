package sandbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show details for one sandbox",
		ArgsUsage: "<sandbox-id>",
		Description: `Show every detail for a sandbox you own.

  createos sandbox get sb-01k...`,
		Action: runGet,
	}
}

func runGet(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	ref := strings.TrimSpace(c.Args().First())
	var id string
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a sandbox ID or name\n\n  To see your sandboxes, run:\n    createos sandbox list")
		}
		pickedID, _, err := pickByStatus(c, client, "Show details for which sandbox?", "")
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled.")
			return nil
		}
		id = pickedID
	} else {
		resolved, err := resolveSandboxRef(c.Context, client, ref)
		if err != nil {
			return err
		}
		id = resolved
	}

	sb, err := client.GetSandbox(c.Context, id)
	if err != nil {
		return err
	}

	// Bandwidth lives on a sibling endpoint. Best-effort: don't fail the
	// whole get just because the counter is unavailable (e.g. destroyed
	// sandbox where the meter is gone).
	var bw *api.BandwidthView
	if sb.Status == "running" || sb.Status == "paused" {
		if v, berr := client.GetBandwidth(c.Context, id); berr == nil {
			bw = v
		}
	}

	output.Render(c, struct {
		*api.SandboxView
		Bandwidth *api.BandwidthView `json:"bandwidth,omitempty"`
	}{sb, bw}, func() { printSandbox(sb, bw) })
	return nil
}

func printSandbox(s *api.SandboxView, bw *api.BandwidthView) {
	label := pterm.NewStyle(pterm.FgCyan)
	row := func(k, v string) {
		if v == "" {
			return
		}
		label.Printf("  %-15s ", k+":")
		fmt.Println(v)
	}
	rowTime := func(k string, t *time.Time) {
		if t == nil {
			return
		}
		row(k, t.Format("2006-01-02 15:04:05"))
	}

	// Header
	pterm.Println()
	if s.Name != nil && *s.Name != "" {
		pterm.NewStyle(pterm.FgCyan, pterm.Bold).Printfln("  %s (%s)", *s.Name, s.ID)
	} else {
		pterm.NewStyle(pterm.FgCyan, pterm.Bold).Printfln("  %s", s.ID)
	}
	pterm.Println()

	row("Status", s.Status)
	row("Size", s.Shape)
	if s.Rootfs != nil && *s.Rootfs != "" {
		row("Image", *s.Rootfs)
	}
	row("VCPU", fmt.Sprintf("%d", s.VCPU))
	row("RAM", fmt.Sprintf("%d MB", s.MemMib))
	row("Disk", fmt.Sprintf("%d MB", s.DiskMib))
	if s.IP != nil {
		row("IP", *s.IP)
	}
	row("Region", s.Region)

	if s.IngressEnabled {
		if s.IngressURLTemplate != "" {
			row("Public URL", s.IngressURLTemplate)
		} else {
			row("Public URL", "on")
		}
	} else {
		row("Public URL", "off")
	}

	if len(s.Egress) > 0 {
		row("Firewall", strings.Join(s.Egress, ", "))
	} else {
		row("Firewall", "open (all outbound traffic allowed)")
	}

	if bw != nil {
		bwLine := fmt.Sprintf("%s used of %s",
			humanBytes(bw.UsedBytes), humanBytes(bw.QuotaBytes))
		if bw.Capped {
			bwLine += "  (CAPPED — top up with 'sandbox edit')"
		} else if bw.RemainingBytes < bw.QuotaBytes/10 {
			bwLine += fmt.Sprintf("  (%s left)", humanBytes(bw.RemainingBytes))
		}
		row("Bandwidth", bwLine)
	}
	if len(s.Envs) > 0 {
		row("Env keys", strings.Join(s.Envs, ", "))
	}

	rowTime("Created", &s.CreatedAt)
	rowTime("Running since", s.RunningAt)
	rowTime("Paused at", s.PausedAt)
	rowTime("Last resumed", s.LastResumedAt)
	rowTime("Destroyed at", s.DestroyedAt)

	if s.ForkedFrom != nil && *s.ForkedFrom != "" {
		row("Forked from", *s.ForkedFrom)
	}
}
