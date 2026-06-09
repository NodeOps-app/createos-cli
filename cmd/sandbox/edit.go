package sandbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// newEditCommand returns the `sandbox edit` command. Two ways to use:
//
//  1. Flag form (script-friendly):
//     createos sandbox edit <ref> --ingress on|off
//     createos sandbox edit <ref> --add-ssh-key ~/.ssh/id_ed25519.pub
//
//  2. Interactive (TTY, no flags):
//     createos sandbox edit <ref>
//     → menu: toggle public URL / add SSH key / cancel
//
// SSH-key removal is not supported by the server today — once a key is
// on a sandbox you cannot retract it without destroying the sandbox.
func newEditCommand() *cli.Command {
	return &cli.Command{
		Name:      "edit",
		Usage:     "Change a sandbox's settings (public URL, SSH keys)",
		ArgsUsage: "[<sandbox>]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "ingress",
				Usage: "Turn the public HTTPS URL `on` or `off`",
			},
			&cli.StringSliceFlag{
				Name:  "add-ssh-key",
				Usage: "Path to a public-key file to add (repeatable)",
			},
			&cli.StringFlag{
				Name:  "auto-pause",
				Usage: "Pause automatically after this long with no activity (e.g. 10m, 1h). Use `off` to disable.",
			},
		},
		Action: runEdit,
	}
}

func runEdit(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	// urfave/cli v2 stops flag parsing at the first positional, so
	// `edit my-sb --ingress on` loses `--ingress`. Re-scan args by hand
	// so users can put flags anywhere.
	ref, ingressFlag, autoPauseFlag, sshFiles := parseEditArgs(c)
	hasFlagChanges := ingressFlag != "" || autoPauseFlag != "" || len(sshFiles) > 0

	// Resolve the sandbox first — either from positional or via picker.
	id, label, err := resolveTarget(c, client, ref)
	if err != nil {
		return err
	}
	if id == "" {
		// Cancelled mid-pick.
		fmt.Println("Cancelled. Nothing changed.")
		return nil
	}

	// Flag mode wins whenever any flag is set — no prompts. We apply
	// each requested change and bail with the first error.
	if hasFlagChanges {
		if ingressFlag != "" {
			if err := applyIngressFlag(c, client, label, id, ingressFlag); err != nil {
				return err
			}
		}
		if autoPauseFlag != "" {
			if err := applyAutoPauseFlag(c, client, label, id, autoPauseFlag); err != nil {
				return err
			}
		}
		if len(sshFiles) > 0 {
			if err := applyAddSSHKeys(c, client, label, id, sshFiles); err != nil {
				return err
			}
		}
		return nil
	}

	// No flags — interactive only.
	if !terminal.IsInteractive() {
		return fmt.Errorf("nothing to do — pass --ingress, --auto-pause, or --add-ssh-key, or run again on a terminal for an interactive menu")
	}
	return runEditMenu(c, client, label, id)
}

// parseEditArgs walks c.Args() AND merges anything urfave/cli already
// parsed before the first positional. Pulls out the first positional
// as the sandbox ref, and recognises `--ingress <value>`,
// `--ingress=<value>`, `--add-ssh-key <path>`, `--add-ssh-key=<path>`
// in any position.
func parseEditArgs(c *cli.Context) (ref, ingressVal, autoPauseVal string, sshPaths []string) {
	ingressVal = strings.ToLower(strings.TrimSpace(c.String("ingress")))
	autoPauseVal = strings.TrimSpace(c.String("auto-pause"))
	sshPaths = append([]string{}, c.StringSlice("add-ssh-key")...)

	args := c.Args().Slice()
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--ingress":
			if i+1 < len(args) {
				ingressVal = strings.ToLower(strings.TrimSpace(args[i+1]))
				i++
			}
		case strings.HasPrefix(a, "--ingress="):
			ingressVal = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(a, "--ingress=")))
		case a == "--auto-pause":
			if i+1 < len(args) {
				autoPauseVal = strings.TrimSpace(args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--auto-pause="):
			autoPauseVal = strings.TrimSpace(strings.TrimPrefix(a, "--auto-pause="))
		case a == "--add-ssh-key":
			if i+1 < len(args) {
				sshPaths = append(sshPaths, strings.TrimSpace(args[i+1]))
				i++
			}
		case strings.HasPrefix(a, "--add-ssh-key="):
			sshPaths = append(sshPaths, strings.TrimSpace(strings.TrimPrefix(a, "--add-ssh-key=")))
		default:
			if ref == "" {
				ref = strings.TrimSpace(a)
			}
		}
	}
	return ref, ingressVal, autoPauseVal, sshPaths
}

// resolveTarget figures out which sandbox the user wants to edit. With
// a positional ref → resolve. Without one, picker on TTY, error otherwise.
func resolveTarget(c *cli.Context, client *api.SandboxClient, ref string) (id, label string, err error) {
	if ref != "" {
		resolved, err := resolveSandboxRef(c.Context, client, ref)
		if err != nil {
			return "", "", err
		}
		return resolved, ref, nil
	}
	if !terminal.IsInteractive() {
		return "", "", fmt.Errorf("please provide a sandbox ID or name\n\n  To see your sandboxes, run:\n    createos sandbox list")
	}
	return pickByStatus(c, client, "Pick a sandbox to edit", "running")
}

// runEditMenu is the interactive flow once a sandbox is selected. Pulls
// the current view so the menu shows real state, then loops until the
// user is done.
func runEditMenu(c *cli.Context, client *api.SandboxClient, label, id string) error {
	sb, err := client.GetSandbox(c.Context, id)
	if err != nil {
		return err
	}
	// Bandwidth is on a sibling endpoint. Best-effort — a stale/missing
	// counter shouldn't block the rest of the edit menu.
	bw, _ := client.GetBandwidth(c.Context, id) //nolint:errcheck

	fmt.Println()
	pterm.NewStyle(pterm.FgCyan, pterm.Bold).Printfln("  Editing %s", refLabel(label, id))
	autoPauseLabel := "off"
	if sb.AutoPauseAfterSeconds != nil {
		autoPauseLabel = "pauses after " + formatDuration(time.Duration(*sb.AutoPauseAfterSeconds)*time.Second) + " idle"
	}
	header := fmt.Sprintf("  Public URL: %s   SSH keys: %d   Auto-pause: %s", onOff(sb.IngressEnabled), len(sb.SSHPubkeys), autoPauseLabel)
	if bw != nil {
		bwLine := fmt.Sprintf("%s used of %s", humanBytes(bw.UsedBytes), humanBytes(bw.QuotaBytes))
		if bw.Capped {
			bwLine += "  (CAPPED)"
		}
		header += "   Bandwidth: " + bwLine
	}
	pterm.Println(pterm.Gray(header))
	pterm.Println()

	const (
		optIngress   = "Toggle public URL"
		optSSH       = "Add an SSH key"
		optBandwidth = "Top up bandwidth"
		optAutoPause = "Auto-pause when idle"
		optDone      = "Done"
	)
	for {
		choice, err := pterm.DefaultInteractiveSelect.
			WithOptions([]string{optIngress, optSSH, optBandwidth, optAutoPause, optDone}).
			WithDefaultText("What would you like to change?").
			Show()
		if err != nil {
			return fmt.Errorf("could not read your choice: %w", err)
		}
		switch choice {
		case optIngress:
			target := !sb.IngressEnabled
			confirm := fmt.Sprintf("Turn public URL %s for %s?", onOff(target), refLabel(label, id))
			yes, err := pterm.DefaultInteractiveConfirm.
				WithDefaultText(confirm).
				WithDefaultValue(true).
				Show()
			if err != nil {
				return fmt.Errorf("could not read confirmation: %w", err)
			}
			if !yes {
				continue
			}
			updated, err := client.SetSandboxIngress(c.Context, id, target)
			if err != nil {
				return err
			}
			sb = updated
			if target {
				pterm.Success.Printfln("Public URL is on for %s", refLabel(label, id))
				if updated.IngressURLTemplate != "" {
					fmt.Printf("    %s\n", updated.IngressURLTemplate)
				}
			} else {
				pterm.Success.Printfln("Public URL is off for %s", refLabel(label, id))
			}
		case optSSH:
			path, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText("Path to your public-key file (e.g. ~/.ssh/id_ed25519.pub)").
				Show()
			if err != nil {
				return fmt.Errorf("could not read the key path: %w", err)
			}
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if err := applyAddSSHKeys(c, client, label, id, []string{path}); err != nil {
				pterm.Error.Printfln("%v", err)
				continue
			}
			// Refresh so the next pass shows the new count.
			if refreshed, err := client.GetSandbox(c.Context, id); err == nil {
				sb = refreshed
			}
		case optBandwidth:
			// Show current balance then pop the slider for the amount.
			if bw != nil {
				pterm.Println(pterm.Gray(fmt.Sprintf("  Current: %s used of %s  (%s left%s)",
					humanBytes(bw.UsedBytes), humanBytes(bw.QuotaBytes),
					humanBytes(bw.RemainingBytes),
					func() string {
						if bw.Capped {
							return ", CAPPED"
						}
						return ""
					}())))
			}
			picked, err := pickRechargeAmountGB(5)
			if err != nil {
				return fmt.Errorf("could not read amount: %w", err)
			}
			if picked <= 0 {
				continue
			}
			bytes := int64(picked) << 30 // GiB, matches humanBytes() display
			updated, rerr := client.RechargeBandwidth(c.Context, id, bytes)
			if rerr != nil {
				pterm.Error.Printfln("%v", rerr)
				continue
			}
			bw = updated
			pterm.Success.Printfln("Added %s. New balance: %s of %s  (%s left)",
				humanBytes(bytes),
				humanBytes(updated.UsedBytes), humanBytes(updated.QuotaBytes),
				humanBytes(updated.RemainingBytes))
		case optAutoPause:
			current := "off"
			if sb.AutoPauseAfterSeconds != nil {
				current = formatDuration(time.Duration(*sb.AutoPauseAfterSeconds) * time.Second)
			}
			input, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText(fmt.Sprintf("Pause after how long with no activity? (current: %s — e.g. 10m, 1h, or 'off')", current)).
				Show()
			if err != nil {
				return fmt.Errorf("could not read your input: %w", err)
			}
			input = strings.TrimSpace(input)
			if input == "" {
				continue
			}
			if err := applyAutoPauseFlag(c, client, label, id, input); err != nil {
				pterm.Error.Printfln("%v", err)
				continue
			}
			if refreshed, err := client.GetSandbox(c.Context, id); err == nil {
				sb = refreshed
			}
		case optDone:
			return nil
		}
	}
}

// applyIngressFlag honours the --ingress on|off flag.
func applyIngressFlag(c *cli.Context, client *api.SandboxClient, label, id, value string) error {
	var target bool
	switch value {
	case "on", "true", "yes", "enable":
		target = true
	case "off", "false", "no", "disable":
		target = false
	default:
		return fmt.Errorf("--ingress %q is not a value I understand — use `on` or `off`", value)
	}
	updated, err := client.SetSandboxIngress(c.Context, id, target)
	if err != nil {
		return err
	}
	if target {
		pterm.Success.Printfln("Public URL is on for %s", refLabel(label, id))
		if updated.IngressURLTemplate != "" {
			fmt.Printf("    %s\n", updated.IngressURLTemplate)
			pterm.Println(pterm.Gray("  Replace <port> with the port your service is listening on."))
		}
	} else {
		pterm.Success.Printfln("Public URL is off for %s", refLabel(label, id))
	}
	return nil
}

// applyAddSSHKeys reads each public-key file path and POSTs the bundle.
func applyAddSSHKeys(c *cli.Context, client *api.SandboxClient, label, id string, paths []string) error {
	keys, err := readSSHPubkeys(paths)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("no SSH keys to add — every path was empty")
	}
	count, err := client.AddSSHPubkeys(c.Context, id, keys)
	if err != nil {
		return err
	}
	pterm.Success.Printfln("Added %d SSH key(s) to %s — total now %d", len(keys), refLabel(label, id), count)
	return nil
}

// applyAutoPauseFlag handles --auto-pause <value>: "off" disables, a duration enables.
func applyAutoPauseFlag(c *cli.Context, client *api.SandboxClient, label, id, value string) error {
	var seconds *int
	switch strings.ToLower(value) {
	case "off", "disable", "false", "no":
		// leave seconds nil → disable
	default:
		secs, err := parseDurationToSeconds(value)
		if err != nil {
			return fmt.Errorf("--auto-pause %q: %w", value, err)
		}
		seconds = &secs
	}
	updated, err := client.SetAutoPause(c.Context, id, seconds)
	if err != nil {
		return err
	}
	if updated.AutoPauseAfterSeconds != nil {
		d := time.Duration(*updated.AutoPauseAfterSeconds) * time.Second
		pterm.Success.Printfln("Auto-pause set to %s for %s", formatDuration(d), refLabel(label, id))
	} else {
		pterm.Success.Printfln("Auto-pause turned off for %s", refLabel(label, id))
	}
	return nil
}

// onOff renders true/false as the verb the user typed mentally.
func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// refLabel renders both the friendly name (if the user typed one) and
// the resolved id so the user sees what was actually touched.
func refLabel(ref, id string) string {
	if ref == id || ref == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", ref, id)
}

// pickByStatus shows a single-select picker filtered to one status
// (running, paused, etc). Reused by edit / pause / resume / fork.
func pickByStatus(c *cli.Context, client *api.SandboxClient, title, status string) (id, label string, err error) {
	rows, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{
		Limit: 200, Status: status,
	})
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		if status == "" {
			fmt.Println("You don't have any sandboxes yet.")
		} else {
			fmt.Printf("You have no %s sandboxes.\n", status)
		}
		return "", "", nil
	}
	options := make([]string, 0, len(rows))
	idByOpt := make(map[string]string, len(rows))
	labelByOpt := make(map[string]string, len(rows))
	for _, r := range rows {
		lbl := r.ID
		if r.Name != nil && *r.Name != "" {
			lbl = *r.Name
		}
		opt := fmt.Sprintf("%s   (id: %s)", lbl, r.ID)
		options = append(options, opt)
		idByOpt[opt] = r.ID
		labelByOpt[opt] = lbl
	}
	picked, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText(title).
		Show()
	if err != nil {
		return "", "", fmt.Errorf("could not read your selection: %w", err)
	}
	return idByOpt[picked], labelByOpt[picked], nil
}
