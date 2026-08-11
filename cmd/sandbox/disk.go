package sandbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// newDiskCommand returns the `sandbox disk` group. Disks are
// user-registered S3-compatible buckets that get mounted into
// sandboxes (at create time or live).
func newDiskCommand() *cli.Command {
	return &cli.Command{
		Name:    "disk",
		Aliases: []string{"disks"},
		Usage:   "Manage S3 disks you can mount into your sandboxes",
		Subcommands: []*cli.Command{
			newDiskCreateCommand(),
			newDiskListCommand(),
			newDiskShowCommand(),
			newDiskRmCommand(),
			newDiskAttachCommand(),
			newDiskDetachCommand(),
		},
	}
}

// ── create ───────────────────────────────────────────────────────

func newDiskCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "Register an S3 bucket as a disk you can mount into sandboxes",
		ArgsUsage: "[<name>]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "bucket", Usage: "S3 bucket name"},
			&cli.StringFlag{Name: "endpoint", Usage: "S3 endpoint URL (e.g. https://s3.amazonaws.com, https://your-minio:9000)"},
			&cli.StringFlag{Name: "access-key", Usage: "Access key ID"},
			&cli.StringFlag{Name: "secret-key", Usage: "Secret access key"},
			&cli.StringFlag{Name: "region", Usage: "AWS region (e.g. us-east-1) — optional"},
			&cli.BoolFlag{Name: "path-style", Usage: "Use path-style URLs (needed for MinIO and most self-hosted S3)"},
		},
		Action: runDiskCreate,
	}
}

func runDiskCreate(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	tty := terminal.IsInteractive()

	// urfave/cli v2 stops parsing flags at the first positional, so
	// `disk create my-disk --bucket=foo` loses everything after.
	// Re-scan args ourselves so flag order doesn't matter.
	name, flags := parseDiskCreateArgs(c)
	bucket := flags["bucket"]
	endpoint := flags["endpoint"]
	access := flags["access-key"]
	secret := flags["secret-key"]
	region := flags["region"]
	pathStyle := flags["path-style"] == "true"

	// Interactive: fill in anything missing.
	if tty {
		if name == "" {
			v, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText("Name this disk (your sandboxes will reference it by this name)").
				Show()
			if err != nil {
				return fmt.Errorf("could not read name: %w", err)
			}
			name = strings.TrimSpace(v)
		}
		if bucket == "" {
			v, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText("Bucket name").
				Show()
			if err != nil {
				return fmt.Errorf("could not read bucket: %w", err)
			}
			bucket = strings.TrimSpace(v)
		}
		if endpoint == "" {
			v, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText("Endpoint URL (e.g. https://s3.amazonaws.com)").
				Show()
			if err != nil {
				return fmt.Errorf("could not read endpoint: %w", err)
			}
			endpoint = strings.TrimSpace(v)
		}
		if access == "" {
			v, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText("Access key").
				Show()
			if err != nil {
				return fmt.Errorf("could not read access key: %w", err)
			}
			access = strings.TrimSpace(v)
		}
		if secret == "" {
			v, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText("Secret key").
				WithMask("*").
				Show()
			if err != nil {
				return fmt.Errorf("could not read secret key: %w", err)
			}
			secret = v
		}
	}

	if name == "" || bucket == "" || endpoint == "" || access == "" || secret == "" {
		return fmt.Errorf("missing required values\n\n  Need: <name>, --bucket, --endpoint, --access-key, --secret-key\n  Optional: --region, --path-style")
	}

	spinner, _ := pterm.DefaultSpinner.Start("Checking the bucket…") //nolint:errcheck
	d, err := client.CreateDisk(c.Context, api.DiskCreateReq{
		Name: name,
		Kind: "s3",
		Config: api.DiskConfig{
			Bucket:       bucket,
			Endpoint:     endpoint,
			Region:       region,
			UsePathStyle: pathStyle,
		},
		Credentials: api.DiskCredentials{
			AccessKey: access,
			SecretKey: secret,
		},
	})
	if err != nil {
		spinner.Fail("Could not register the disk")
		return err
	}
	spinner.Success(fmt.Sprintf("Registered disk %s (%s)", d.Name, d.ID))
	renderResult(c, "disk_created", map[string]any{
		"id":   d.ID,
		"name": d.Name,
	}, func() {
		pterm.Println(pterm.Gray("  Attach it at create time:    createos sandbox create --disk " + d.Name + ":/mnt/data"))
		pterm.Println(pterm.Gray("  Or live-attach it later:     createos sandbox disk attach <sandbox> " + d.Name + " /mnt/data"))
	})
	return nil
}

// ── list ─────────────────────────────────────────────────────────

func newDiskListCommand() *cli.Command {
	return &cli.Command{
		Name:    "ls",
		Aliases: []string{"list"},
		Usage:   "List your disks",
		Action:  runDiskList,
	}
}

func runDiskList(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	disks, err := client.ListDisks(c.Context)
	if err != nil {
		return err
	}
	output.Render(c, disks, func() {
		if len(disks) == 0 {
			fmt.Println("You don't have any disks yet.")
			pterm.Println(pterm.Gray("  Create one with: createos sandbox disk create"))
			return
		}
		table := pterm.TableData{{"Name", "ID", "Kind", "Bucket", "Created"}}
		for _, d := range disks {
			table = append(table, []string{
				d.Name, d.ID, d.Kind, d.Config.Bucket,
				d.CreatedAt.Format("2006-01-02 15:04"),
			})
		}
		_ = pterm.DefaultTable.WithHasHeader().WithData(table).Render() //nolint:errcheck
	})
	return nil
}

// ── show ─────────────────────────────────────────────────────────

func newDiskShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "Show details for one disk",
		ArgsUsage: "<name|id>",
		Action:    runDiskShow,
	}
}

func runDiskShow(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := strings.TrimSpace(c.Args().First())
	if ref == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a disk name or ID\n\n  To see your disks, run:\n    createos sandbox disk ls")
		}
		picked, err := pickDisk(c, client, "Show which disk?")
		if err != nil {
			return err
		}
		if picked == "" {
			fmt.Println("Cancelled. Nothing to show.")
			return nil
		}
		ref = picked
	}
	d, err := client.GetDisk(c.Context, ref)
	if err != nil {
		return err
	}
	output.Render(c, d, func() {
		label := pterm.NewStyle(pterm.FgCyan)
		row := func(k, v string) {
			if v == "" {
				return
			}
			label.Printf("  %-12s ", k+":")
			fmt.Println(v)
		}
		pterm.Println()
		pterm.NewStyle(pterm.FgCyan, pterm.Bold).Printfln("  %s (%s)", d.Name, d.ID)
		pterm.Println()
		row("Kind", d.Kind)
		row("Bucket", d.Config.Bucket)
		row("Endpoint", d.Config.Endpoint)
		row("Region", d.Config.Region)
		if d.Config.UsePathStyle {
			row("Path style", "yes")
		}
		row("Created", d.CreatedAt.Format("2006-01-02 15:04:05"))
	})
	return nil
}

// ── rm ───────────────────────────────────────────────────────────

func newDiskRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Aliases:   []string{"delete"},
		Usage:     "Delete one or more disks (each must not be currently mounted)",
		ArgsUsage: "[<name|id> …]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y", "force"}, Usage: "Skip the confirmation prompt"},
		},
		Action: runDiskRm,
	}
}

func runDiskRm(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	refs, forceFromArgs := splitForceFlag(c.Args().Slice())
	if len(refs) == 0 {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide at least one disk name or ID\n\n  To see your disks, run:\n    createos sandbox disk ls")
		}
		picked, err := pickDisksForDelete(c, client)
		if err != nil {
			return err
		}
		if len(picked) == 0 {
			fmt.Println("Cancelled. Nothing deleted.")
			return nil
		}
		refs = picked
	}
	force := c.Bool("yes") || forceFromArgs
	if !terminal.IsInteractive() && !force {
		return fmt.Errorf("non-interactive: pass --yes to confirm deletion")
	}
	if terminal.IsInteractive() && !force {
		prompt := fmt.Sprintf("Permanently delete disk %q? (the bucket itself is not touched)", refs[0])
		if len(refs) > 1 {
			prompt = fmt.Sprintf("Permanently delete %d disks? (buckets are not touched)", len(refs))
		}
		ok, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText(prompt).
			WithDefaultValue(false).
			Show()
		if err != nil {
			return fmt.Errorf("could not read confirmation: %w", err)
		}
		if !ok {
			fmt.Println("Cancelled. Nothing deleted.")
			return nil
		}
	}
	failed := 0
	results := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		if err := deleteDiskCascade(c, client, ref); err != nil {
			pterm.Error.Printfln("%s: %v", ref, err)
			results = append(results, map[string]any{"ref": ref, "deleted": false, "error": err.Error()})
			failed++
			continue
		}
		results = append(results, map[string]any{"ref": ref, "deleted": true})
		pterm.Success.Printfln("Deleted disk %s", ref)
	}
	renderResult(c, "disk_deleted", map[string]any{
		"results": results,
		"deleted": len(results) - failed,
		"failed":  failed,
	}, func() {})
	if failed > 0 {
		return fmt.Errorf("%d of %d deletes failed", failed, len(refs))
	}
	return nil
}

// deleteDiskCascade detaches the disk from every sandbox that has it
// mounted, then deletes the disk record. "rm" means "remove it,
// including the wiring".
//
// There's no reverse-lookup endpoint, so we walk the caller's running
// and paused sandboxes and check each one's attachments. N is small in
// practice; this is the same scan `pickSandboxesForDelete` does.
func deleteDiskCascade(c *cli.Context, client *api.SandboxClient, ref string) error {
	d, err := client.GetDisk(c.Context, ref)
	if err != nil {
		return err
	}
	var sandboxes []api.SandboxView
	for _, st := range []string{"running", "paused"} {
		page, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{
			Limit: 200, Status: st,
		})
		if err != nil {
			return fmt.Errorf("list sandboxes: %w", err)
		}
		sandboxes = append(sandboxes, page...)
	}
	for _, sb := range sandboxes {
		attachments, err := client.ListSandboxDisks(c.Context, sb.ID)
		if err != nil {
			return fmt.Errorf("list attachments on %s: %w", sb.ID, err)
		}
		for _, a := range attachments {
			if a.DiskID != d.ID {
				continue
			}
			if derr := client.DetachDisk(c.Context, sb.ID, d.ID, a.MountPath); derr != nil {
				return fmt.Errorf("detach %s from %s: %w", d.Name, sb.ID, derr)
			}
			pterm.Println(pterm.Gray(fmt.Sprintf("  detached %s from %s (%s)", d.Name, sb.ID, a.MountPath)))
		}
	}
	return client.DeleteDisk(c.Context, ref)
}

// pickDisksForDelete renders a multi-select over the caller's disks
// and returns the picked names. Returns nil/empty when the user cancels.
func pickDisksForDelete(c *cli.Context, client *api.SandboxClient) ([]string, error) {
	disks, err := client.ListDisks(c.Context)
	if err != nil {
		return nil, err
	}
	if len(disks) == 0 {
		fmt.Println("You don't have any disks to delete.")
		return nil, nil
	}
	options := make([]string, 0, len(disks))
	byOpt := make(map[string]string, len(disks))
	for _, d := range disks {
		opt := fmt.Sprintf("%s   (bucket: %s, id: %s)", d.Name, d.Config.Bucket, d.ID)
		options = append(options, opt)
		byOpt[opt] = d.Name
	}
	picked, err := multiselect("Pick disks to delete (space = select, enter = confirm)").
		WithOptions(options).
		Show()
	if err != nil {
		return nil, fmt.Errorf("could not read your selection: %w", err)
	}
	out := make([]string, 0, len(picked))
	for _, p := range picked {
		if name, ok := byOpt[p]; ok {
			out = append(out, name)
		}
	}
	return out, nil
}

// ── attach ───────────────────────────────────────────────────────

func newDiskAttachCommand() *cli.Command {
	return &cli.Command{
		Name:      "attach",
		Usage:     "Mount an existing disk into a running sandbox",
		ArgsUsage: "[<sandbox> <disk> <mount-path>]",
		Action:    runDiskAttach,
	}
}

func runDiskAttach(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	args := c.Args().Slice()
	sandboxRef := ""
	diskRef := ""
	mountPath := ""
	if len(args) > 0 {
		sandboxRef = args[0]
	}
	if len(args) > 1 {
		diskRef = args[1]
	}
	if len(args) > 2 {
		mountPath = args[2]
	}

	tty := terminal.IsInteractive()
	if sandboxRef == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox disk attach <sandbox> <disk> <mount-path>")
		}
		pickedID, label, err := pickByStatus(c, client, "Attach to which sandbox?", "running")
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled. Nothing attached.")
			return nil
		}
		sandboxRef = label
		_ = pickedID
	}
	sandboxID, err := resolveSandboxRef(c.Context, client, sandboxRef)
	if err != nil {
		return err
	}

	if diskRef == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox disk attach <sandbox> <disk> <mount-path>")
		}
		var picked string
		picked, err = pickDisk(c, client, "Attach which disk?")
		if err != nil {
			return err
		}
		if picked == "" {
			fmt.Println("Cancelled. Nothing attached.")
			return nil
		}
		diskRef = picked
	}

	if mountPath == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox disk attach <sandbox> <disk> <mount-path>")
		}
		var v string
		v, err = pterm.DefaultInteractiveTextInput.
			WithDefaultText("Where in the sandbox should it mount (absolute path, e.g. /mnt/data)").
			WithDefaultValue("/mnt/" + diskRef).
			Show()
		if err != nil {
			return fmt.Errorf("could not read mount path: %w", err)
		}
		mountPath = strings.TrimSpace(v)
	}
	if !strings.HasPrefix(mountPath, "/") {
		return fmt.Errorf("mount path must be absolute (start with '/'), got %q", mountPath)
	}

	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Attaching %s → %s:%s", diskRef, refLabel(sandboxRef, sandboxID), mountPath)) //nolint:errcheck
	err = client.AttachDisk(c.Context, sandboxID, api.DiskAttachReq{
		DiskID:    diskRef,
		MountPath: mountPath,
	})
	if err != nil {
		spinner.Fail("Attach failed")
		return err
	}
	spinner.Success(fmt.Sprintf("Attached %s → %s:%s", diskRef, refLabel(sandboxRef, sandboxID), mountPath))
	renderResult(c, "disk_attached", map[string]any{
		"disk":       diskRef,
		"sandbox_id": sandboxID,
		"mount_path": mountPath,
	}, func() {
		pterm.Println(pterm.Gray("  The mount appears inside the sandbox within a few seconds."))
	})
	return nil
}

// ── detach ───────────────────────────────────────────────────────

func newDiskDetachCommand() *cli.Command {
	return &cli.Command{
		Name:      "detach",
		Usage:     "Unmount a disk from a running sandbox (the bucket itself is untouched)",
		ArgsUsage: "[<sandbox> <disk> <mount-path>]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "Skip the confirmation prompt"},
		},
		Action: runDiskDetach,
	}
}

func runDiskDetach(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	args := c.Args().Slice()
	sandboxRef, diskRef, mountPath := "", "", ""
	if len(args) > 0 {
		sandboxRef = args[0]
	}
	if len(args) > 1 {
		diskRef = args[1]
	}
	if len(args) > 2 {
		mountPath = args[2]
	}

	tty := terminal.IsInteractive()

	if sandboxRef == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox disk detach <sandbox> <disk> <mount-path>")
		}
		pickedID, label, err := pickByStatus(c, client, "Detach from which sandbox?", "running")
		if err != nil {
			return err
		}
		if pickedID == "" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
		sandboxRef = label
	}
	sandboxID, err := resolveSandboxRef(c.Context, client, sandboxRef)
	if err != nil {
		return err
	}

	// On TTY with no disk arg: pick from what's actually attached.
	if diskRef == "" || mountPath == "" {
		if !tty {
			return fmt.Errorf("usage: createos sandbox disk detach <sandbox> <disk> <mount-path>")
		}
		attached, err := client.ListSandboxDisks(c.Context, sandboxID)
		if err != nil {
			return err
		}
		if len(attached) == 0 {
			fmt.Println("This sandbox has no disks attached.")
			return nil
		}
		options := make([]string, 0, len(attached))
		byOpt := make(map[string]struct{ disk, mount string }, len(attached))
		for _, a := range attached {
			label := fmt.Sprintf("%s @ %s   (status: %s)", a.Name, a.MountPath, a.MountStatus)
			options = append(options, label)
			byOpt[label] = struct{ disk, mount string }{a.DiskID, a.MountPath}
		}
		picked, err := pterm.DefaultInteractiveSelect.
			WithOptions(options).
			WithDefaultText("Detach which attachment?").
			Show()
		if err != nil {
			return fmt.Errorf("could not read your selection: %w", err)
		}
		v := byOpt[picked]
		diskRef, mountPath = v.disk, v.mount
	}

	if !strings.HasPrefix(mountPath, "/") {
		return fmt.Errorf("mount path must be absolute (start with '/'), got %q", mountPath)
	}

	force := c.Bool("yes")
	if !tty && !force {
		return fmt.Errorf("non-interactive: pass --yes to confirm detach")
	}
	if tty && !force {
		ok, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText(fmt.Sprintf("Unmount %s from %s at %s? (the bucket itself is not touched)", diskRef, refLabel(sandboxRef, sandboxID), mountPath)).
			WithDefaultValue(false).
			Show()
		if err != nil {
			return fmt.Errorf("could not read confirmation: %w", err)
		}
		if !ok {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
	}

	if err := client.DetachDisk(c.Context, sandboxID, diskRef, mountPath); err != nil {
		return err
	}
	renderResult(c, "disk_detached", map[string]any{
		"disk":       diskRef,
		"sandbox_id": sandboxID,
		"mount_path": mountPath,
	}, func() {
		pterm.Success.Printfln("Detached %s from %s at %s", diskRef, refLabel(sandboxRef, sandboxID), mountPath)
	})
	return nil
}

// pickDisk renders a single-select picker over the caller's disks and
// returns the picked NAME (which the server accepts wherever an ID
// does). Returns "" when the user cancels.
func pickDisk(c *cli.Context, client *api.SandboxClient, title string) (string, error) {
	disks, err := client.ListDisks(c.Context)
	if err != nil {
		return "", err
	}
	if len(disks) == 0 {
		fmt.Println("You don't have any disks yet.")
		pterm.Println(pterm.Gray("  Create one with: createos sandbox disk create"))
		return "", nil
	}
	options := make([]string, 0, len(disks))
	nameByOpt := make(map[string]string, len(disks))
	for _, d := range disks {
		opt := fmt.Sprintf("%s   (bucket: %s, id: %s)", d.Name, d.Config.Bucket, d.ID)
		options = append(options, opt)
		nameByOpt[opt] = d.Name
	}
	picked, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText(title).
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read your selection: %w", err)
	}
	return nameByOpt[picked], nil
}

// parseDiskCreateArgs splits c.Args() so flags can appear in any
// position. urfave/cli's defaults still seed the map for flags that
// appeared BEFORE the first positional; positional re-scan only adds.
//
// Recognises:  --bucket / --endpoint / --access-key / --secret-key /
// --region / --path-style (boolean — its mere presence sets "true").
// Both `--foo=bar` and `--foo bar` forms are accepted.
func parseDiskCreateArgs(c *cli.Context) (name string, flags map[string]string) {
	flags = map[string]string{
		"bucket":     strings.TrimSpace(c.String("bucket")),
		"endpoint":   strings.TrimSpace(c.String("endpoint")),
		"access-key": strings.TrimSpace(c.String("access-key")),
		"secret-key": c.String("secret-key"),
		"region":     strings.TrimSpace(c.String("region")),
	}
	if c.Bool("path-style") {
		flags["path-style"] = "true"
	}

	known := map[string]bool{
		"bucket": true, "endpoint": true, "access-key": true,
		"secret-key": true, "region": true,
	}
	args := c.Args().Slice()
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--path-style":
			flags["path-style"] = "true"
		case strings.HasPrefix(a, "--path-style="):
			flags["path-style"] = strings.TrimPrefix(a, "--path-style=")
		case strings.HasPrefix(a, "--"):
			// e.g. --foo=bar  OR  --foo bar
			raw := strings.TrimPrefix(a, "--")
			key, val := raw, ""
			if eq := strings.IndexByte(raw, '='); eq >= 0 {
				key, val = raw[:eq], raw[eq+1:]
			} else if i+1 < len(args) {
				val = args[i+1]
				i++
			}
			if known[key] {
				flags[key] = strings.TrimSpace(val)
			}
		default:
			if name == "" {
				name = strings.TrimSpace(a)
			}
		}
	}
	return name, flags
}

// context.Context guard so the import isn't dropped after refactors.
var _ = context.Background
