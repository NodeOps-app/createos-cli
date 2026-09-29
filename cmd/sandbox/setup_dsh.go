package sandbox

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"
)

// DeepSeek Harness installs a bundle from a path on disk, so setup's job is to
// put the checkout there and register it against a DSH profile.
const (
	dshPkg            = "dsh-createos"
	dshDefaultProfile = "web"
	// The plugin's own requirement: Node ^22.19 or >=24.
	dshNodeMinMajor = 22
	dshNodeMinMinor = 19
)

// dshEnv lists the variables the plugin reads. The API key is deliberately
// never read or printed here — setup reports only whether one is present.
var dshEnv = []struct{ name, why string }{
	{"CREATEOS_SANDBOX_API_KEY", "your CreateOS API key"},
	{"CREATEOS_SANDBOX_SHAPE", "the sandbox size, e.g. s-2vcpu-2gb"},
}

func newSetupDeepSeekCommand() *cli.Command {
	return &cli.Command{
		Name:    "deepseek",
		Aliases: []string{"deepseek-harness", "dsh"},
		Usage:   "Set up DeepSeek Harness to run its tools in a CreateOS Sandbox",
		Description: "Clones the plugin and registers it with a DeepSeek Harness profile,\n" +
			"so the harness runs its files, subprocesses and terminals inside one\n" +
			"sandbox instead of on your laptop.\n\n" +
			"The harness reads its CreateOS credentials from environment\n" +
			"variables, which only you can set — setup prints the ones you still\n" +
			"need at the end.\n\n" +
			"Run with --doctor first if you only want to check the prerequisites.",
		Flags: []cli.Flag{
			setupDoctorFlag(),
			setupLocalFlag(),
			&cli.StringFlag{
				Name:  "profile",
				Value: dshDefaultProfile,
				Usage: "DeepSeek Harness profile to install into",
			},
		},
		Action: runDeepSeekSetup,
	}
}

func runDeepSeekSetup(c *cli.Context) error {
	profile := strings.TrimSpace(c.String("profile"))
	if profile == "" {
		return fmt.Errorf("--profile cannot be empty")
	}

	if err := setupSignedIn(c); err != nil {
		return err
	}
	dshBin, err := setupRequireBin("dsh", "install it from https://github.com/deepseek-ai/harness")
	if err != nil {
		return err
	}
	if nodeErr := dshCheckNode(c); nodeErr != nil {
		return nodeErr
	}
	if c.Bool("doctor") {
		dshReportEnv()
		setupDoctorDone()
		return nil
	}

	checkout, err := setupPluginCheckout(c.Context, c.String("local"))
	if err != nil {
		return err
	}
	pkgDir, err := setupPackageDir(checkout, dshPkg)
	if err != nil {
		return err
	}

	fmt.Printf("registering the plugin with the %q profile\n", profile)
	out, err := setupRun(c.Context, dshBin, "plugin", "--profile", profile, "add", pkgDir)
	switch {
	case err == nil:
	case setupAlreadyDone(out):
		fmt.Println("  already done, skipping")
	default:
		return fmt.Errorf("dsh plugin add failed: %w\n%s", err, strings.TrimSpace(out))
	}

	fmt.Println("\nDone.")
	if missing := dshReportEnv(); len(missing) > 0 {
		fmt.Println("\nSet those, then start the harness from the directory its tools")
		fmt.Printf("should work in:\n  dsh %s\n", profile)
		fmt.Println("\nGet an API key from https://createos.nodeops.network/profile.")
		return nil
	}
	fmt.Printf("\nStart the harness from the directory its tools should work in:\n  dsh %s\n", profile)
	return nil
}

// dshReportEnv prints which of the plugin's variables are set, and returns the
// names of the ones that are not. It reports presence only: a key's value is
// never read into the CLI's output.
func dshReportEnv() []string {
	var missing []string
	for _, e := range dshEnv {
		if strings.TrimSpace(os.Getenv(e.name)) != "" {
			fmt.Printf("%s is set\n", e.name)
			continue
		}
		missing = append(missing, e.name)
	}
	if len(missing) == 0 {
		return nil
	}
	fmt.Println("\nThe harness still needs these in its environment:")
	for _, e := range dshEnv {
		if strings.TrimSpace(os.Getenv(e.name)) == "" {
			fmt.Printf("  export %s='...'   # %s\n", e.name, e.why)
		}
	}
	return missing
}

// dshCheckNode enforces the plugin's Node requirement up front, because DSH
// fails much later and much less clearly when it is not met.
func dshCheckNode(c *cli.Context) error {
	nodeBin, err := setupRequireBin("node", "the plugin runs on it; install it from https://nodejs.org")
	if err != nil {
		return err
	}
	out, err := setupRun(c.Context, nodeBin, "--version")
	if err != nil {
		return fmt.Errorf("could not run 'node --version': %w\n%s", err, strings.TrimSpace(out))
	}
	version := strings.TrimSpace(out)
	if !dshNodeOK(version) {
		return fmt.Errorf("node %s is too old for the plugin — it needs 22.19 or newer on 22.x, or 24 and above", version)
	}
	return nil
}

// dshNodeOK reports whether a `node --version` string satisfies ^22.19 || >=24.
// 23.x is deliberately excluded: it is not a maintained line, which is why the
// plugin's own manifest skips it.
func dshNodeOK(version string) bool {
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	if major >= 24 {
		return true
	}
	return major == dshNodeMinMajor && minor >= dshNodeMinMinor
}
