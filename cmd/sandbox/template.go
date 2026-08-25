package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// newTemplateCommand wires up `createos sandbox template`.
//
// Templates are user-built sandbox images: submit a Dockerfile, get a
// rootfs the sandbox API can spawn from. The build runs async in our
// build cluster; this command group submits, lists, follows logs, and
// removes them.
func newTemplateCommand() *cli.Command {
	return &cli.Command{
		Name:    "template",
		Aliases: []string{"templates", "tpl"},
		Usage:   "Build your own sandbox images from a Dockerfile",
		Subcommands: []*cli.Command{
			newTemplateSubmitCommand(),
			newTemplateListCommand(),
			newTemplateShowCommand(),
			newTemplateLogsCommand(),
			newTemplateRmCommand(),
		},
	}
}

// ── submit ───────────────────────────────────────────────────────

func newTemplateSubmitCommand() *cli.Command {
	return &cli.Command{
		Name:      "submit",
		Aliases:   []string{"build", "create"},
		Usage:     "Send a Dockerfile to be built into a sandbox image",
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Value: "Dockerfile", Usage: "Path to your Dockerfile"},
			&cli.BoolFlag{Name: "no-follow", Usage: "Submit and exit; don't wait for the build"},
		},
		Action: runTemplateSubmit,
	}
}

func runTemplateSubmit(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	name, path, follow := parseTemplateSubmitArgs(c)
	if name == "" {
		return fmt.Errorf("template name required\n\n  Example:\n    createos sandbox template submit my-rails -f Dockerfile")
	}
	body, err := os.ReadFile(path) // #nosec G304 -- path is a user-supplied Dockerfile path
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(body) == 0 {
		return fmt.Errorf("the Dockerfile at %s is empty", path)
	}

	view, err := client.CreateTemplate(c.Context, api.TemplateCreateReq{
		Name:       name,
		Dockerfile: string(body),
	})
	if err != nil {
		return err
	}
	pterm.Success.Printfln("Submitted template %s (status: %s)", view.Name, view.Status)
	if !follow {
		pterm.Println(pterm.Gray(fmt.Sprintf("  Watch progress with:  createos sandbox template logs %s --follow", view.Name)))
		return nil
	}
	return streamTemplateLogs(c, client, view.ID)
}

// parseTemplateSubmitArgs is forgiving about flag placement —
// urfave/cli v2 stops flag parsing at the first positional, so
// `submit my-rails -f Dockerfile` would silently keep -f at its
// default. Rescan c.Args() to recover late-positioned flags.
func parseTemplateSubmitArgs(c *cli.Context) (name, path string, follow bool) {
	path = c.String("file")
	follow = !c.Bool("no-follow")
	args := c.Args().Slice()
	var positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "--file":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-f=") || strings.HasPrefix(a, "--file="):
			path = a[strings.Index(a, "=")+1:]
		case a == "--no-follow":
			follow = false
		default:
			positionals = append(positionals, a)
		}
	}
	if len(positionals) > 0 {
		name = positionals[0]
	}
	return
}

// ── list ─────────────────────────────────────────────────────────

func newTemplateListCommand() *cli.Command {
	return &cli.Command{
		Name:    "ls",
		Aliases: []string{"list"},
		Usage:   "List your templates",
		Action:  runTemplateList,
	}
}

func runTemplateList(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	tpls, err := client.ListTemplates(c.Context)
	if err != nil {
		return err
	}
	sort.SliceStable(tpls, func(i, j int) bool {
		return tpls[i].CreatedAt.After(tpls[j].CreatedAt)
	})
	output.Render(c, tpls, func() {
		if len(tpls) == 0 {
			fmt.Println("You don't have any templates yet.")
			pterm.Println(pterm.Gray("  Build one with: createos sandbox template submit <name> -f Dockerfile"))
			return
		}
		table := pterm.TableData{{"Name", "ID", "Status", "Size", "Created"}}
		for _, t := range tpls {
			table = append(table, []string{
				t.Name, t.ID, t.Status,
				humanBytes(t.Ext4SizeBytes),
				t.CreatedAt.Local().Format("2006-01-02 15:04"),
			})
		}
		_ = output.RenderTable(table) //nolint:errcheck
		pterm.Println()
		pterm.Println(pterm.Gray("  Spawn from a ready template: createos sandbox create --rootfs <name>"))
	})
	return nil
}

// ── show ─────────────────────────────────────────────────────────

func newTemplateShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Aliases:   []string{"get"},
		Usage:     "Show one template's details",
		ArgsUsage: "[<name|tpl_id>]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dockerfile", Usage: "Also print the submitted Dockerfile"},
		},
		Action: runTemplateShow,
	}
}

func runTemplateShow(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref, err := resolveTemplateRefArg(c, client, "Show which template?")
	if err != nil || ref == "" {
		return err
	}
	withDockerfile := c.Bool("dockerfile")
	t, err := client.GetTemplate(c.Context, ref, withDockerfile)
	if err != nil {
		return err
	}
	output.Render(c, t, func() {
		label := pterm.NewStyle(pterm.FgCyan)
		row := func(k, v string) {
			if v == "" {
				return
			}
			label.Printf("  %-12s ", k+":")
			fmt.Println(v)
		}
		pterm.Println()
		pterm.NewStyle(pterm.FgCyan, pterm.Bold).Printfln("  %s (%s)", t.Name, t.ID)
		pterm.Println()
		row("Status", t.Status)
		row("Base", t.Base)
		if t.Ext4SizeBytes > 0 {
			row("Size", humanBytes(t.Ext4SizeBytes))
		}
		row("Created", t.CreatedAt.Local().Format("2006-01-02 15:04:05"))
		if t.BuiltAt != nil {
			row("Built", t.BuiltAt.Local().Format("2006-01-02 15:04:05"))
		}
		switch t.Status {
		case "failed":
			pterm.Println()
			pterm.Println(pterm.Gray(fmt.Sprintf("  Build failed. See logs:  createos sandbox template logs %s", t.Name)))
		case "ready":
			pterm.Println()
			pterm.Println(pterm.Gray(fmt.Sprintf("  Spawn from it:  createos sandbox create --rootfs %s", t.Name)))
		}
		if withDockerfile && t.Dockerfile != "" {
			pterm.Println()
			pterm.NewStyle(pterm.FgCyan, pterm.Bold).Println("  Dockerfile:")
			fmt.Println(indent(t.Dockerfile, "    "))
		}
	})
	return nil
}

// ── logs ─────────────────────────────────────────────────────────

func newTemplateLogsCommand() *cli.Command {
	return &cli.Command{
		Name:      "logs",
		Usage:     "Show the build output for a template",
		ArgsUsage: "[<name|tpl_id>]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "Keep showing output until the build finishes"},
		},
		Action: runTemplateLogs,
	}
}

func runTemplateLogs(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref, err := resolveTemplateRefArg(c, client, "Show logs for which template?")
	if err != nil || ref == "" {
		return err
	}
	if c.Bool("follow") {
		return streamTemplateLogs(c, client, ref)
	}
	resp, err := client.StreamTemplateLogs(c.Context, ref, false)
	if err != nil {
		return err
	}
	defer func() { _ = resp.RawBody().Close() }() //nolint:errcheck
	_, _ = io.Copy(os.Stdout, resp.RawBody())     //nolint:errcheck
	return nil
}

// streamTemplateLogs follows the NDJSON log stream until {"final":true}.
// A spinner covers the pending → claimed → pod-scheduled silence; it
// drops as soon as the first real line lands.
func streamTemplateLogs(c *cli.Context, client *api.SandboxClient, ref string) error {
	resp, err := client.StreamTemplateLogs(c.Context, ref, true)
	if err != nil {
		return err
	}
	defer func() { _ = resp.RawBody().Close() }() //nolint:errcheck

	var spinner *pterm.SpinnerPrinter
	if terminal.IsInteractive() {
		spinner, _ = pterm.DefaultSpinner.Start("template build queued…") //nolint:errcheck
	}
	stopSpinner := func() {
		if spinner != nil {
			_ = spinner.Stop() //nolint:errcheck
			spinner = nil
		}
	}
	defer stopSpinner()

	scanner := bufio.NewScanner(resp.RawBody())
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var ev api.TemplateLogEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		if ev.Final {
			stopSpinner()
			fmt.Println()
			switch ev.Status {
			case "ready":
				pterm.Success.Println("Build succeeded.")
			case "failed":
				pterm.Error.Println("Build failed.")
				return cli.Exit("", 1)
			default:
				pterm.Println(pterm.Gray("(stream ended)"))
			}
			return nil
		}
		if ev.Line != "" {
			stopSpinner()
			_, _ = os.Stdout.WriteString(ev.Line) //nolint:errcheck
			_, _ = os.Stdout.WriteString("\n")    //nolint:errcheck
		}
	}
	return scanner.Err()
}

// ── rm ───────────────────────────────────────────────────────────

func newTemplateRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Aliases:   []string{"delete"},
		Usage:     "Delete one or more templates",
		ArgsUsage: "[<name|tpl_id> …]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y", "force"}, Usage: "Skip the confirmation prompt"},
		},
		Action: runTemplateRm,
	}
}

func runTemplateRm(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	refs, forceFromArgs := splitForceFlag(c.Args().Slice())
	if len(refs) == 0 {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide at least one template name or ID")
		}
		picked, err := pickTemplatesForDelete(c, client)
		if err != nil {
			return err
		}
		if len(picked) == 0 {
			fmt.Println("Cancelled.")
			return nil
		}
		refs = picked
	}
	force := c.Bool("yes") || forceFromArgs
	if !terminal.IsInteractive() && !force {
		return fmt.Errorf("non-interactive: pass --yes to confirm deletion")
	}
	if terminal.IsInteractive() && !force {
		prompt := fmt.Sprintf("Permanently delete template %q?", refs[0])
		if len(refs) > 1 {
			prompt = fmt.Sprintf("Permanently delete %d templates?", len(refs))
		}
		pterm.Println(pterm.Gray("  (Paused sandboxes that were built from these can still be resumed.)"))
		ok, err := pterm.DefaultInteractiveConfirm.
			WithDefaultText(prompt).
			WithDefaultValue(false).
			Show()
		if err != nil {
			return fmt.Errorf("could not read confirmation: %w", err)
		}
		if !ok {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	failed := 0
	for _, ref := range refs {
		if err := client.DeleteTemplate(c.Context, ref); err != nil {
			pterm.Error.Printfln("%s: %s", ref, api.UserMessageVerbose(err))
			failed++
			continue
		}
		pterm.Success.Printfln("Deleted template %s", ref)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d deletes failed", failed, len(refs))
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────

// resolveTemplateRefArg returns the first positional arg or — on a
// TTY when none is given — pops a single-select picker. Returns
// ref="" and nil error when the user cancels the picker.
func resolveTemplateRefArg(c *cli.Context, client *api.SandboxClient, prompt string) (string, error) {
	ref := strings.TrimSpace(c.Args().First())
	if ref != "" {
		return ref, nil
	}
	if !terminal.IsInteractive() {
		return "", fmt.Errorf("please provide a template name or ID\n\n  To see your templates, run:\n    createos sandbox template ls")
	}
	tpls, err := client.ListTemplates(c.Context)
	if err != nil {
		return "", err
	}
	if len(tpls) == 0 {
		fmt.Println("You don't have any templates yet.")
		pterm.Println(pterm.Gray("  Build one with: createos sandbox template submit <name> -f Dockerfile"))
		return "", nil
	}
	sort.SliceStable(tpls, func(i, j int) bool {
		return tpls[i].CreatedAt.After(tpls[j].CreatedAt)
	})
	options := make([]string, 0, len(tpls))
	byOpt := make(map[string]string, len(tpls))
	for _, t := range tpls {
		opt := fmt.Sprintf("%s   (%s, %s)", t.Name, t.Status, t.CreatedAt.Local().Format("2006-01-02 15:04"))
		options = append(options, opt)
		byOpt[opt] = t.Name
	}
	picked, err := pterm.DefaultInteractiveSelect.
		WithOptions(options).
		WithDefaultText(prompt).
		Show()
	if err != nil {
		return "", fmt.Errorf("could not read your selection: %w", err)
	}
	return byOpt[picked], nil
}

func pickTemplatesForDelete(c *cli.Context, client *api.SandboxClient) ([]string, error) {
	tpls, err := client.ListTemplates(c.Context)
	if err != nil {
		return nil, err
	}
	if len(tpls) == 0 {
		fmt.Println("You don't have any templates to delete.")
		return nil, nil
	}
	sort.SliceStable(tpls, func(i, j int) bool {
		return tpls[i].CreatedAt.After(tpls[j].CreatedAt)
	})
	options := make([]string, 0, len(tpls))
	byOpt := make(map[string]string, len(tpls))
	for _, t := range tpls {
		opt := fmt.Sprintf("%s   (%s, %s)", t.Name, t.Status, t.CreatedAt.Local().Format("2006-01-02 15:04"))
		options = append(options, opt)
		byOpt[opt] = t.Name
	}
	picked, err := multiselect("Pick templates to delete (space = select, enter = confirm)").
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

// indent prefixes every line in s with the given prefix.
func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

// satisfy time import for go-imports — already used via TemplateView fields above.
var _ = time.Time{}
