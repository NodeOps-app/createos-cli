package sandbox

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Aliases:   []string{"c"},
		Usage:     "Create a new sandbox",
		ArgsUsage: " ",
		Description: `Create a new sandbox on fc-spawn.

Examples:
  # Smallest possible sandbox
  createos sandbox create --shape s-1vcpu-256mb

  # With a name and an SSH key so you can shell in later
  createos sandbox create --shape s-1vcpu-1gb \
    --name my-box --ssh-key ~/.ssh/id_ed25519.pub

  # Public HTTPS URL (great for demos)
  createos sandbox create --shape s-1vcpu-1gb --ingress

  # Attach an S3 disk and join a private network
  createos sandbox create --shape s-1vcpu-1gb \
    --disk my-bucket:/mnt/data --network my-net`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "shape",
				Usage: "Size of the sandbox (run 'createos sandbox shapes' to see options)",
			},
			&cli.StringFlag{
				Name:  "name",
				Usage: "Friendly name for the sandbox (one is generated if you skip it)",
			},
			&cli.StringFlag{
				Name:  "rootfs",
				Usage: "Base image or template to start from (defaults to the standard one)",
			},
			&cli.Int64Flag{
				Name:  "disk-mib",
				Usage: "Disk size in MiB (defaults to the shape's standard disk)",
			},
			&cli.StringSliceFlag{
				Name:  "ssh-key",
				Usage: "Path to an SSH public key file so you can sign in (repeatable)",
			},
			&cli.StringSliceFlag{
				Name:  "egress",
				Usage: "Allowed website or address the sandbox can reach (repeatable). When empty the sandbox can reach anything.",
			},
			&cli.StringSliceFlag{
				Name:  "env",
				Usage: "Environment variable available to every exec (repeatable): KEY=VALUE",
			},
			&cli.BoolFlag{
				Name:  "ingress",
				Usage: "Give the sandbox a public HTTPS URL so anyone can reach its HTTP services",
			},
			&cli.StringSliceFlag{
				Name:    "network",
				Aliases: []string{"net"},
				Usage:   "Private network to join at creation (repeatable): <name|id>",
			},
			&cli.StringSliceFlag{
				Name:  "disk",
				Usage: "S3 disk to mount at creation (repeatable): <name|id>:/mount/path",
			},
			&cli.StringFlag{
				Name:  "auto-pause",
				Usage: "Pause the sandbox automatically after this long with no activity — e.g. 10m, 1h, 30m. Leave empty to keep it running until you stop it.",
			},
		},
		Action: runCreate,
	}
}

func runCreate(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	shape := strings.TrimSpace(c.String("shape"))
	name := strings.TrimSpace(c.String("name"))
	rootfs := strings.TrimSpace(c.String("rootfs"))
	ingress := c.Bool("ingress")
	netIDs := stringSliceCleanup(c.StringSlice("network"))

	// Read --ssh-key files up front so we can both seed the wizard
	// (skipping the SSH step when keys are already supplied) and reuse
	// the result downstream.
	sshKeys, err := readSSHPubkeys(c.StringSlice("ssh-key"))
	if err != nil {
		return err
	}

	// Interactive wizard: only when --shape is missing AND stdout is a TTY.
	// Lets users walk through name / rootfs / network / ssh keys without
	// remembering every flag. Headless callers continue to get the
	// "use --shape" error.
	// Pre-parse --auto-pause so the wizard can skip the step when supplied.
	var autoPauseSecs *int
	if raw := strings.TrimSpace(c.String("auto-pause")); raw != "" {
		secs, parseErr := parseDurationToSeconds(raw)
		if parseErr != nil {
			return fmt.Errorf("--auto-pause %q: %w", raw, parseErr)
		}
		autoPauseSecs = &secs
	}

	if shape == "" {
		w, werr := runCreateWizard(c, client, wizardSeed{
			name:          name,
			rootfs:        rootfs,
			ingress:       ingress,
			netIDs:        netIDs,
			sshKeys:       sshKeys,
			autoPauseSecs: autoPauseSecs,
		})
		if werr != nil {
			return werr
		}
		if w == nil {
			// User cancelled mid-wizard — exit quietly, no error.
			return nil
		}
		shape = w.shape
		if name == "" {
			name = w.name
		}
		if rootfs == "" {
			rootfs = w.rootfs
		}
		if len(netIDs) == 0 {
			netIDs = w.netIDs
		}
		if len(sshKeys) == 0 {
			sshKeys = w.sshKeys
		}
		ingress = ingress || w.ingress
		if autoPauseSecs == nil {
			autoPauseSecs = w.autoPauseSecs
		}
	}

	req := api.SandboxCreateReq{
		Shape:          shape,
		Name:           name,
		Rootfs:         rootfs,
		DiskMib:        c.Int64("disk-mib"),
		IngressEnabled: ingress,
	}

	if envs, envErr := parseEnvFlags(c.StringSlice("env")); envErr != nil {
		return envErr
	} else if len(envs) > 0 {
		req.Envs = envs
	}

	if egress := c.StringSlice("egress"); len(egress) > 0 {
		req.Egress = egress
	}

	if len(sshKeys) > 0 {
		req.SSHPubkeys = sshKeys
	}

	if len(netIDs) > 0 {
		req.Networks = make([]api.SandboxNetworkAttach, 0, len(netIDs))
		for _, n := range netIDs {
			req.Networks = append(req.Networks, api.SandboxNetworkAttach{ID: n})
		}
	}

	if rawDisks := c.StringSlice("disk"); len(rawDisks) > 0 {
		disks, derr := parseDiskFlags(rawDisks)
		if derr != nil {
			return derr
		}
		req.Disks = disks
	}

	if autoPauseSecs != nil {
		req.AutoPauseAfterSeconds = autoPauseSecs
	}

	spinner, _ := pterm.DefaultSpinner.Start("Creating sandbox…") //nolint:errcheck
	resp, err := client.CreateSandbox(c.Context, req)
	if err != nil {
		spinner.Fail("Could not create sandbox")
		return err
	}
	spinner.Success("Sandbox is ready")

	renderResult(c, "created", map[string]any{
		"id":            resp.ID,
		"name":          str(resp.Name),
		"shape":         resp.Shape,
		"rootfs":        str(resp.Rootfs),
		"ip":            resp.IP,
		"ingress_url":   resp.IngressURLTemplate,
		"shell_command": fmt.Sprintf("createos sandbox shell %s", resp.ID),
	}, func() { printCreateResult(resp) })
	return nil
}

// parseEnvFlags turns --env KEY=VALUE entries into a map. Surfaces
// a friendly error rather than a stack trace on malformed input.
func parseEnvFlags(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for _, kv := range raw {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			return nil, fmt.Errorf("--env %q is missing '=' (expected KEY=VALUE)", kv)
		}
		key := strings.TrimSpace(kv[:i])
		if key == "" {
			return nil, fmt.Errorf("--env %q has an empty key", kv)
		}
		out[key] = kv[i+1:]
	}
	return out, nil
}

// parseDiskFlags turns --disk <name|id>:/mount/path entries into the
// API attachment shape. Refuses anything without a mount path.
func parseDiskFlags(raw []string) ([]api.SandboxDiskAttach, error) {
	out := make([]api.SandboxDiskAttach, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		i := strings.IndexByte(entry, ':')
		if i <= 0 || i == len(entry)-1 {
			return nil, fmt.Errorf("--disk %q must be <name|id>:/mount/path", entry)
		}
		out = append(out, api.SandboxDiskAttach{
			DiskID:    entry[:i],
			MountPath: entry[i+1:],
		})
	}
	return out, nil
}

// readSSHPubkeys reads each path supplied by --ssh-key and returns the
// canonicalised public-key strings. Missing or unreadable files yield
// a friendly error.
func readSSHPubkeys(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p) // #nosec G304 -- p is a user-supplied SSH public-key path
		if err != nil {
			return nil, fmt.Errorf("could not read SSH public key %s: %w", p, err)
		}
		out = append(out, strings.TrimSpace(string(b)))
	}
	return out, nil
}

// printCreateResult shows the user the new sandbox + how to reach it.
func printCreateResult(resp *api.SandboxCreateResp) {
	name := ""
	if resp.Name != nil {
		name = *resp.Name
	}

	pterm.Println()
	if name != "" {
		pterm.NewStyle(pterm.FgCyan).Printfln("  Sandbox %s (%s)", name, resp.ID)
	} else {
		pterm.NewStyle(pterm.FgCyan).Printfln("  Sandbox %s", resp.ID)
	}

	row := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Printf("    %-9s %s\n", label+":", value)
	}
	row("Size", resp.Shape)
	if resp.Rootfs != nil && *resp.Rootfs != "" {
		row("Image", *resp.Rootfs)
	}
	row("IP", resp.IP)

	if resp.IngressURLTemplate != "" {
		pterm.Println()
		pterm.Success.Println("Reachable from anywhere over HTTPS:")
		fmt.Printf("    %s\n", resp.IngressURLTemplate)
		pterm.Println(pterm.Gray("  Replace <port> with the port your service is listening on."))
	}

	if resp.AutoPauseAfterSeconds != nil {
		d := time.Duration(*resp.AutoPauseAfterSeconds) * time.Second
		pterm.Println(pterm.Gray(fmt.Sprintf("  Will pause automatically after %s with no activity.", formatDuration(d))))
	}
}

// parseDurationToSeconds parses human durations like "10m", "1h", "30m" into
// seconds. Returns an error for values outside 60–86400.
func parseDurationToSeconds(s string) (int, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("use a duration like 10m, 1h, or 30m")
	}
	secs := int(d.Seconds())
	if secs < 60 || secs > 86400 {
		return 0, fmt.Errorf("must be between 1 minute (1m) and 24 hours (24h)")
	}
	return secs, nil
}

// formatDuration renders a duration in the most readable unit (e.g. "10m", "1h").
func formatDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return d.String()
}
