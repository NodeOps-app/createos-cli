package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/NodeOps-app/createos-cli/internal/api"

	"github.com/urfave/cli/v2"
)

// Herdr is a terminal workspace manager, not a coding agent, so this setup
// inverts the Orca one. Orca runs a recipe per workspace and the CLI is the
// machine half; Herdr has no such hook, and its own CLI is the whole plugin
// API. So `createos sandbox setup herdr` performs every install step itself:
// it registers the plugin, writes its config, and binds the keys.
//
// The plugin it installs puts a coding agent *inside* a sandbox and attaches
// that agent's PTY to a Herdr pane.

const (
	herdrPluginID      = "createos.sandbox"
	herdrPluginRepo    = "NodeOps-app/createos-plugins"
	herdrPluginPkgPath = "packages/herdr-plugin"
	// Plugin panes and `pane split --env` both landed in 0.7.5.
	herdrMinVersion    = "0.7.5"
	herdrDefaultAgent  = "claude-code"
	herdrDefaultShape  = "s-2vcpu-4gb"
	herdrDefaultPause  = "30m"
	herdrDefaultRemote = "/workspace"
)

// Agents the plugin can install inside a sandbox. Kept in step with
// packages/herdr-plugin/src/agents.ts; the plugin owns the install commands,
// this list only validates --agent before writing it to the plugin config.
var herdrAgents = map[string]string{
	"claude-code": "Claude Code",
	"codex":       "Codex",
	"opencode":    "OpenCode",
	"pi":          "Pi",
	"cursor":      "Cursor",
	"shell":       "a plain shell, no agent",
}

// One binding per plugin action. Herdr ignores keys declared in a plugin
// manifest, so they have to be spliced into the user's config.toml.
var herdrKeys = []struct{ key, action, description string }{
	{"prefix+shift+s", "start", "start an agent in a new CreateOS sandbox"},
	{"prefix+shift+c", "attach", "reattach the agent to this pane"},
	{"prefix+shift+y", "sync", "two-way sync this pane's sandbox"},
	{"prefix+shift+a", "apply", "apply sandbox changes locally"},
	{"prefix+shift+i", "info", "show this pane's sandbox mapping"},
	{"prefix+shift+x", "delete", "delete this pane's sandbox"},
}

func newSetupHerdrCommand() *cli.Command {
	return &cli.Command{
		Name:  "herdr",
		Usage: "Set up Herdr to run coding agents inside CreateOS Sandboxes",
		Description: "Installs the CreateOS Sandbox plugin into Herdr, writes its\n" +
			"configuration, and binds its keys. After this, one Herdr pane maps to\n" +
			"one sandbox with a coding agent running inside it.\n\n" +
			"Run with --doctor first if you only want to check the prerequisites.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "doctor",
				Usage: "Check the prerequisites and report, without changing anything",
			},
			&cli.StringFlag{
				Name:  "local",
				Usage: "Link this local plugin directory instead of installing from GitHub",
			},
			&cli.StringFlag{
				Name:    "agent",
				Usage:   "Coding agent to run in each sandbox: " + herdrAgentNames(),
				Value:   herdrDefaultAgent,
				EnvVars: []string{"CREATEOS_AGENT"},
			},
			&cli.StringFlag{
				Name:    "shape",
				Usage:   "Sandbox size for each agent",
				Value:   herdrDefaultShape,
				EnvVars: []string{"CREATEOS_SHAPE"},
			},
			&cli.StringFlag{
				Name:    "rootfs",
				Usage:   "Sandbox image for each agent",
				EnvVars: []string{"CREATEOS_ROOTFS"},
			},
			&cli.StringFlag{
				Name:  "auto-pause",
				Usage: "Pause a sandbox after this long with no activity",
				Value: herdrDefaultPause,
			},
			&cli.StringFlag{
				Name:  "remote-root",
				Usage: "Absolute path the worktree lands on inside the sandbox",
				Value: herdrDefaultRemote,
			},
			&cli.BoolFlag{
				Name:  "no-keys",
				Usage: "Do not add keybindings to your Herdr config.toml",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Overwrite an existing plugin config.json",
			},
		},
		Action: func(c *cli.Context) error {
			return runHerdrSetup(c, c.Bool("doctor"))
		},
	}
}

func herdrAgentNames() string {
	names := make([]string, 0, len(herdrAgents))
	for name := range herdrAgents {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func runHerdrSetup(c *cli.Context, doctorOnly bool) error {
	agent := strings.TrimSpace(c.String("agent"))
	if _, ok := herdrAgents[agent]; !ok {
		return fmt.Errorf("unknown agent %q; pick one of: %s", agent, herdrAgentNames())
	}

	// ---- prerequisites -----------------------------------------------------

	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' first")
	}
	if _, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{}); err != nil {
		return fmt.Errorf("your session is not usable — run 'createos login' again: %w", err)
	}
	fmt.Println("signed in to CreateOS")

	herdrBin, err := exec.LookPath("herdr")
	if err != nil {
		return fmt.Errorf("herdr is not on PATH — install it from https://herdr.dev")
	}
	version, err := herdrVersion(herdrBin)
	if err != nil {
		return err
	}
	if herdrVersionLess(version, herdrMinVersion) {
		return fmt.Errorf("herdr %s is too old; the plugin needs %s or newer", version, herdrMinVersion)
	}
	fmt.Printf("herdr %s found at %s\n", version, herdrBin)

	if _, err := exec.LookPath("bun"); err != nil {
		return fmt.Errorf("bun is not on PATH — the plugin runs on it; install it from https://bun.sh")
	}
	fmt.Println("bun found on PATH")

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is not on PATH; the plugin uploads what git tracks")
	}
	fmt.Println("git found on PATH")

	if doctorOnly {
		fmt.Println("\nEverything the plugin needs is present. Run this again without --doctor to install it.")
		return nil
	}

	// ---- install -----------------------------------------------------------

	if local := strings.TrimSpace(c.String("local")); local != "" {
		if err := herdrLink(herdrBin, local); err != nil {
			return err
		}
	} else {
		source := herdrPluginRepo + "/" + herdrPluginPkgPath
		fmt.Printf("installing %s from %s\n", herdrPluginID, source)
		if out, err := herdrRun(herdrBin, "plugin", "install", source, "--yes"); err != nil {
			return fmt.Errorf("herdr plugin install failed: %w\n%s", err, out)
		}
		fmt.Println("plugin installed")
	}

	// ---- plugin config -----------------------------------------------------

	configDir, err := herdrRun(herdrBin, "plugin", "config-dir", herdrPluginID)
	if err != nil {
		return fmt.Errorf("could not find the plugin config directory: %w", err)
	}
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return fmt.Errorf("herdr returned no config directory for %s", herdrPluginID)
	}
	written, err := herdrWritePluginConfig(c, configDir, agent)
	if err != nil {
		return err
	}
	if written {
		fmt.Printf("wrote %s\n", filepath.Join(configDir, "config.json"))
	} else {
		fmt.Printf("kept your existing %s (pass --force to replace it)\n", filepath.Join(configDir, "config.json"))
	}

	// ---- keybindings -------------------------------------------------------

	if c.Bool("no-keys") {
		fmt.Println("skipped keybindings (--no-keys)")
	} else {
		added, path, err := herdrWriteKeys(configDir)
		if err != nil {
			return err
		}
		switch {
		case added == 0:
			fmt.Printf("keybindings already present in %s\n", path)
		default:
			fmt.Printf("added %d keybindings to %s (undo with 'herdr config reset-keys')\n", added, path)
		}
		if out, err := herdrRun(herdrBin, "config", "check"); err != nil {
			return fmt.Errorf("herdr rejected the updated config: %w\n%s", err, out)
		}
		// Only a running server can reload; a failure here is not a setup failure.
		if _, err := herdrRun(herdrBin, "server", "reload-config"); err != nil {
			fmt.Println("no running Herdr server to reload — the keys apply next time you start one")
		} else {
			fmt.Println("reloaded the running Herdr server")
		}
	}

	// ---- what to do next ---------------------------------------------------

	fmt.Printf("\nDone. Open Herdr in a Git worktree and press %s to start %s in a new sandbox.\n",
		herdrKeys[0].key, herdrAgents[agent])
	fmt.Println("Authenticate the agent in that pane the first time it opens.")
	fmt.Printf("Every action is also listed by: herdr plugin action list --plugin %s\n", herdrPluginID)
	return nil
}

func herdrLink(herdrBin, local string) error {
	dir, err := filepath.Abs(local)
	if err != nil {
		return fmt.Errorf("could not resolve %q: %w", local, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "herdr-plugin.toml")); err != nil {
		return fmt.Errorf("%s does not look like the plugin: no herdr-plugin.toml", dir)
	}
	fmt.Printf("linking %s\n", dir)
	if out, err := herdrRun(herdrBin, "plugin", "link", dir); err != nil {
		return fmt.Errorf("herdr plugin link failed: %w\n%s", err, out)
	}
	// `plugin link` deliberately does not run build commands, so the generated
	// run.sh that carries the absolute bun and createos paths is missing.
	build := filepath.Join(dir, "build.sh")
	if _, err := os.Stat(build); err != nil {
		return fmt.Errorf("%s is missing; the plugin cannot generate its launcher", build)
	}
	fmt.Println("running the plugin build step")
	cmd := exec.Command("sh", build)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", build, err)
	}
	fmt.Println("plugin linked")
	return nil
}

// herdrWritePluginConfig writes config.json unless one is already there and
// --force was not passed. It reports whether it wrote.
func herdrWritePluginConfig(c *cli.Context, configDir, agent string) (bool, error) {
	path := filepath.Join(configDir, "config.json")
	if _, err := os.Stat(path); err == nil && !c.Bool("force") {
		return false, nil
	}
	settings := map[string]string{
		"agent":      agent,
		"shape":      c.String("shape"),
		"autoPause":  c.String("auto-pause"),
		"remoteRoot": c.String("remote-root"),
	}
	if rootfs := strings.TrimSpace(c.String("rootfs")); rootfs != "" {
		settings["rootfs"] = rootfs
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, fmt.Errorf("could not build the plugin config: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return false, fmt.Errorf("could not create %s: %w", configDir, err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return false, fmt.Errorf("could not write %s: %w", path, err)
	}
	return true, nil
}

// herdrWriteKeys appends the missing bindings to config.toml and reports how
// many it added. It never rewrites a binding the user already has.
func herdrWriteKeys(pluginConfigDir string) (int, string, error) {
	// pluginConfigDir is <herdr config>/plugins/config/<id>, and asking Herdr
	// for it beats guessing where Herdr keeps its configuration.
	root := filepath.Dir(filepath.Dir(filepath.Dir(pluginConfigDir)))
	path := filepath.Join(root, "config.toml")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return 0, path, fmt.Errorf("could not read %s: %w", path, err)
	}
	current := string(existing)

	var block strings.Builder
	added := 0
	for _, k := range herdrKeys {
		command := herdrPluginID + "." + k.action
		if strings.Contains(current, command) {
			continue
		}
		fmt.Fprintf(&block, "\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\ndescription = %q\n",
			k.key, command, k.description)
		added++
	}
	if added == 0 {
		return 0, path, nil
	}

	if len(existing) > 0 {
		backup := path + ".before-createos"
		if err := os.WriteFile(backup, existing, 0o600); err != nil {
			return 0, path, fmt.Errorf("could not back up %s: %w", path, err)
		}
		fmt.Printf("backed up your config to %s\n", backup)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, path, fmt.Errorf("could not create %s: %w", root, err)
	}
	updated := current
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	updated += "\n# Added by 'createos sandbox setup herdr'.\n" + strings.TrimPrefix(block.String(), "\n")
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		return 0, path, fmt.Errorf("could not write %s: %w", path, err)
	}
	return added, path, nil
}

func herdrRun(bin string, args ...string) (string, error) {
	out, err := exec.Command(bin, args...).CombinedOutput()
	return string(out), err
}

func herdrVersion(bin string) (string, error) {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("could not run 'herdr --version': %w", err)
	}
	// "herdr 0.8.2"
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("'herdr --version' printed nothing")
	}
	return strings.TrimSpace(fields[len(fields)-1]), nil
}

// herdrVersionLess compares dotted numeric versions. A part that is not a
// number sorts as 0, so a pre-release suffix never reads as newer.
func herdrVersionLess(have, want string) bool {
	haveParts := strings.Split(strings.SplitN(have, "-", 2)[0], ".")
	wantParts := strings.Split(want, ".")
	for i := 0; i < len(wantParts); i++ {
		var h int
		if i < len(haveParts) {
			h, _ = strconv.Atoi(haveParts[i])
		}
		w, _ := strconv.Atoi(wantParts[i])
		if h != w {
			return h < w
		}
	}
	return false
}
