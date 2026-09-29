package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v2"
)

// OpenCode has no `plugin add` for an unpublished package, so setup does the
// two things its installer would: put the plugin on disk with its dependencies
// installed, and name it in the user's config.
const (
	openCodePkg         = "opencode-plugin"
	openCodeDefaultMode = "local"
)

// openCodePluginsKey matches the `"plugins": [` key, and only as a key — a
// bare mention of the word in a comment has no colon and bracket after it.
var openCodePluginsKey = regexp.MustCompile(`"plugins"\s*:\s*\[`)

func newSetupOpenCodeCommand() *cli.Command {
	return &cli.Command{
		Name:  "opencode",
		Usage: "Set up OpenCode to run its tools in CreateOS Sandboxes",
		Description: "Clones the plugin, installs its dependencies with bun, and adds it\n" +
			"to your OpenCode config. Your config is backed up before it changes.\n\n" +
			"In local mode OpenCode keeps running its own tools and gains the\n" +
			"sandbox tools. In remote mode its shell and file tools run inside a\n" +
			"sandbox instead.\n\n" +
			"Run with --doctor first if you only want to check the prerequisites.",
		Flags: []cli.Flag{
			setupDoctorFlag(),
			setupLocalFlag(),
			&cli.StringFlag{
				Name:  "config",
				Usage: "OpenCode config file to edit (default: ~/.config/opencode/opencode.json)",
			},
			&cli.StringFlag{
				Name:  "mode",
				Value: openCodeDefaultMode,
				Usage: "Where OpenCode's own shell and file tools run: `local` or `remote`",
			},
			&cli.StringFlag{
				Name:  "shape",
				Usage: "Sandbox size for remote mode (default: the plugin's own)",
			},
			&cli.StringFlag{
				Name:  "rootfs",
				Usage: "Sandbox image for remote mode (default: the plugin's own)",
			},
		},
		Action: runOpenCodeSetup,
	}
}

func runOpenCodeSetup(c *cli.Context) error {
	mode := strings.ToLower(strings.TrimSpace(c.String("mode")))
	if mode != "local" && mode != "remote" {
		return fmt.Errorf("--mode must be 'local' or 'remote', got %q", c.String("mode"))
	}

	if err := setupSignedIn(c); err != nil {
		return err
	}
	// OpenCode ships as `opencode`, and as `opencode2` for the V2 preview.
	// Either one reads the same config, so finding one is enough.
	if _, err := setupRequireBin("opencode", "install it from https://opencode.ai"); err != nil {
		if _, err2 := setupRequireBin("opencode2", "install it from https://opencode.ai"); err2 != nil {
			return err
		}
	}
	if _, err := setupRequireBin("bun", "the plugin runs on it; install it from https://bun.sh"); err != nil {
		return err
	}
	configPath, err := openCodeConfigPath(c.String("config"))
	if err != nil {
		return err
	}
	fmt.Printf("config file: %s\n", configPath)

	if c.Bool("doctor") {
		setupDoctorDone()
		return nil
	}

	checkout, err := setupPluginCheckout(c.Context, c.String("local"))
	if err != nil {
		return err
	}
	pkgDir, err := setupPackageDir(checkout, openCodePkg)
	if err != nil {
		return err
	}

	fmt.Println("installing plugin dependencies with bun")
	if out, bunErr := setupRun(c.Context, "bun", "install", "--cwd", pkgDir); bunErr != nil {
		return fmt.Errorf("bun install failed: %w\n%s", bunErr, strings.TrimSpace(out))
	}

	entry, err := openCodeEntry(pkgDir, mode, c.String("shape"), c.String("rootfs"))
	if err != nil {
		return err
	}
	changed, err := openCodeWriteConfig(configPath, entry, pkgDir)
	if err != nil {
		return err
	}
	if changed {
		fmt.Printf("added the plugin to %s\n", configPath)
	} else {
		fmt.Printf("%s already names this plugin — left it alone\n", configPath)
		fmt.Println("  edit that entry by hand to change mode, shape, or image")
	}

	fmt.Println("\nDone. Start OpenCode — the sandbox tools load with it.")
	if mode == "remote" {
		fmt.Println("Its shell and file tools now run inside a sandbox, created on the")
		fmt.Println("first tool call of each session.")
	} else {
		fmt.Println("Re-run with --mode remote to move OpenCode's own shell and file")
		fmt.Println("tools into a sandbox as well.")
	}
	return nil
}

// openCodeConfigPath resolves which config file to edit. OpenCode reads both
// opencode.json and opencode.jsonc, so an existing file of either name wins
// over creating the other.
func openCodeConfigPath(explicit string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return filepath.Abs(explicit)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	dir := filepath.Join(home, ".config", "opencode")
	for _, name := range []string{"opencode.json", "opencode.jsonc"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return filepath.Join(dir, "opencode.json"), nil
}

// openCodeEntry renders the array element for the plugin. Local mode is a bare
// path; remote mode carries the options that move OpenCode's tools into the
// sandbox. Both are produced by the JSON encoder so a path with a quote or a
// backslash in it cannot break the file.
func openCodeEntry(pkgDir, mode, shape, rootfs string) (string, error) {
	if mode == "local" {
		b, err := json.Marshal(pkgDir)
		if err != nil {
			return "", fmt.Errorf("could not encode the plugin path: %w", err)
		}
		return string(b), nil
	}
	options := map[string]string{"mode": "remote"}
	if s := strings.TrimSpace(shape); s != "" {
		options["shape"] = s
	}
	if r := strings.TrimSpace(rootfs); r != "" {
		options["rootfs"] = r
	}
	b, err := json.Marshal(struct {
		Package string            `json:"package"`
		Options map[string]string `json:"options"`
	}{Package: pkgDir, Options: options})
	if err != nil {
		return "", fmt.Errorf("could not encode the plugin entry: %w", err)
	}
	return string(b), nil
}

// openCodeWriteConfig inserts the entry, backing the file up first. Reports
// whether it changed anything.
func openCodeWriteConfig(path, entry, pkgDir string) (bool, error) {
	existing, err := os.ReadFile(path) // #nosec G304 -- the user's own config, chosen by --config or the documented default
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("could not read %s: %w", path, err)
	}
	updated, changed, err := insertOpenCodePlugin(string(existing), entry, pkgDir)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if len(existing) > 0 {
		backup := path + ".before-createos"
		// #nosec G703 -- path is the user's own config, from --config or the
		// documented default under $HOME; the suffix is a literal.
		if err := os.WriteFile(backup, existing, 0o600); err != nil {
			return false, fmt.Errorf("could not back up %s: %w", path, err)
		}
		fmt.Printf("backed up your config to %s\n", backup)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, fmt.Errorf("could not create %s: %w", filepath.Dir(path), err)
	}
	// #nosec G703 -- path is the user's own config, from --config or the
	// documented default under $HOME.
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		return false, fmt.Errorf("could not write %s: %w", path, err)
	}
	return true, nil
}

// insertOpenCodePlugin adds entry to the config's plugins array.
//
// The edit is textual rather than a parse-and-re-encode because the file is
// the user's: it may carry comments and their own formatting, and OpenCode
// documents JSONC support, which no standard-library decoder round-trips.
// Reports whether it changed anything — a config that already names pkgDir is
// left exactly as it is, so re-running setup is safe.
func insertOpenCodePlugin(existing, entry, pkgDir string) (string, bool, error) {
	if strings.Contains(existing, pkgDir) {
		return existing, false, nil
	}
	if strings.TrimSpace(existing) == "" {
		return fmt.Sprintf("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"plugins\": [\n    %s\n  ]\n}\n", entry), true, nil
	}

	if loc := openCodePluginsKey.FindStringIndex(existing); loc != nil {
		open := loc[1] // just past the '['
		rest := strings.TrimLeft(existing[open:], " \t\r\n")
		sep := ""
		if !strings.HasPrefix(rest, "]") {
			// A non-empty array needs a comma between our entry and the first
			// one already there.
			sep = ","
		}
		return existing[:open] + "\n    " + entry + sep + existing[open:], true, nil
	}

	brace := strings.Index(existing, "{")
	if brace < 0 {
		return "", false, fmt.Errorf("could not find a JSON object in the config — add the plugin by hand:\n  \"plugins\": [%s]", entry)
	}
	rest := strings.TrimLeft(existing[brace+1:], " \t\r\n")
	sep := ""
	if !strings.HasPrefix(rest, "}") {
		sep = ","
	}
	return existing[:brace+1] + "\n  \"plugins\": [\n    " + entry + "\n  ]" + sep + existing[brace+1:], true, nil
}
