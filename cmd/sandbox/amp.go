package sandbox

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"

	"github.com/urfave/cli/v2"
)

// Amp runs self-hosted orbs with `amp orb provider <program>`. Amp sends
// spawn, pause and delete jobs, and the provider runs <program> once per job
// with the job in env vars: ORB_OPERATION, ORB_ID, ORB_TOKEN and AMP_URL.
// Contract: https://ampcode.com/docs/orbs/self-hosted#dispatch-contract
//
// `createos sandbox setup amp` is the human half. It checks the environment,
// writes a one-line wrapper (Amp takes a program path, not a command line) and
// runs `amp orb provider <wrapper>`. `... setup amp --dispatch` is the machine
// half the wrapper runs. One orb is one sandbox.

const (
	ampDefaultShape    = "s-4vcpu-8gb"
	ampDefaultTemplate = "amp-orb"
	// Ubuntu 24.04 with git, Node, tmux and rg, booted while the template is
	// not ready. ampProvision fills in the rest of the orb image contract.
	ampFallbackRootfs = "devbox:1"
	// Amp kills a job after five minutes. The template wait leaves the
	// fallback room to provision; the settle wait covers a pause in flight.
	ampTemplateWait = 2 * time.Minute
	ampSettleWait   = 4 * time.Minute
	ampPollEvery    = 2 * time.Second
	// One deadline for the whole job, under Amp's five minutes, so every wait
	// above is cut short rather than adding up past the limit.
	ampJobDeadline = 4*time.Minute + 40*time.Second
	ampWrapperName = "amp-dispatch"
)

// CreateOS templates may only build FROM nodeops/sandbox:* with no COPY or
// ADD, so Amp's ORB_IMAGE cannot boot as-is. This rebuilds its contract.
//
//go:embed amp-orb.Dockerfile
var ampDockerfile string

// Shape and template names are written into the wrapper script.
var ampFlagValueRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,63}$`)

// The API cannot look a sandbox up by name, and its list holds every sandbox
// ever made, newest first. A name search over a plain list therefore misses a
// thread paused long ago, and spawn would create a duplicate. Live sandboxes
// are few (the concurrency quota caps them), so search each live status.
var ampLiveStatuses = []string{
	api.SandboxStatusRunning, api.SandboxStatusPaused, api.SandboxStatusPausing,
	api.SandboxStatusResuming, api.SandboxStatusCreating, api.SandboxStatusForking,
	api.SandboxStatusError,
}

func ampSettling(status string) bool {
	switch status {
	case api.SandboxStatusPausing, api.SandboxStatusResuming,
		api.SandboxStatusCreating, api.SandboxStatusForking:
		return true
	}
	return false
}

// Makes any Debian or Ubuntu rootfs meet the orb image contract. Idempotent,
// and close to a no-op on the template. Keep the packages in step with
// amp-orb.Dockerfile.
const ampProvision = `set -e
grep -q "$(hostname)" /etc/hosts || echo "127.0.1.1 $(hostname)" >>/etc/hosts
id user >/dev/null 2>&1 || useradd -m -s /bin/bash user
# Judge each tool as the orb's user, not as root: on devbox:1 /usr/local/bin/node
# is a link into /root, which user cannot enter, so root sees Node and user does not.
missing=""
for pair in git:git node:nodejs tmux:tmux sudo:sudo rg:ripgrep gh:gh curl:curl \
	ssh:openssh-client pkill:procps; do
	runuser -u user -- bash -lc "command -v ${pair%%:*}" >/dev/null 2>&1 || missing="$missing ${pair#*:}"
done
if [ -n "$missing" ]; then
	# archive.ubuntu.com hands out servers that take 15-30 s to answer one small
	# file (measured from a sandbox: 2 of 9 addresses), which turned a 4 s install
	# into 3 minutes. The kernel.org mirror answered in 0.09 s every time.
	if grep -q '^ID=ubuntu' /etc/os-release; then
		sed -i -E 's#https?://(archive|security)\.ubuntu\.com/ubuntu#http://mirrors.edge.kernel.org/ubuntu#g' \
			/etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true
	fi
	# A stalled connection is dropped after 10 s and retried, not held for apt's default 120 s.
	APT="-o Acquire::http::Timeout=10 -o Acquire::https::Timeout=10 -o Acquire::Retries=3"
	DEBIAN_FRONTEND=noninteractive apt-get $APT update -qq
	DEBIAN_FRONTEND=noninteractive apt-get $APT install -y -qq --no-install-recommends ca-certificates $missing >/dev/null
fi
echo "user ALL=(ALL) NOPASSWD: ALL" >/etc/sudoers.d/user
if [ ! -x /usr/local/bin/amp ]; then
	tmp=$(mktemp -d)
	# HOME too, so the checksum-verifying installer edits no real shell profile.
	curl -fsSL https://ampcode.com/install.sh | HOME="$tmp" AMP_HOME="$tmp" bash >/dev/null
	install -m 755 "$(readlink -f "$tmp/bin/amp")" /usr/local/bin/amp
	rm -rf "$tmp"
fi
# A resumed snapshot still holds the previous bootstrap, connected with an old
# token. Anchored, so it cannot match this script, whose command line holds the phrase.
pkill -f "^amp orb bootstrap" || true
`

func newSetupAmpCommand() *cli.Command {
	return &cli.Command{
		Name:  "amp",
		Usage: "Run Amp self-hosted orbs on CreateOS Sandboxes",
		Description: "Runs Amp's orb provider so each Amp thread gets its own sandbox.\n\n" +
			"   First, in Amp, open Settings > Orbs, click Add Orb Provider and copy\n" +
			"   the token. Then:\n\n" +
			"     export AMP_ORB_PROVIDER_TOKEN=<the token>\n" +
			"     createos sandbox setup amp\n\n" +
			"   Leave it running, then start a thread with:\n" +
			"     amp -ox \"your message\" --orb-provider <provider name>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "doctor",
				Usage: "Check the prerequisites and report, without starting anything",
			},
			&cli.BoolFlag{
				Name:   "dispatch",
				Usage:  "Run one orb job (Amp's provider calls this, not you)",
				Hidden: true,
			},
			&cli.StringFlag{
				Name:    "shape",
				Usage:   "Sandbox size for each orb",
				Value:   ampDefaultShape,
				EnvVars: []string{"CREATEOS_ORB_SHAPE"},
			},
			&cli.StringFlag{
				Name:    "template",
				Usage:   "Template each orb boots from; built on first use if missing",
				Value:   ampDefaultTemplate,
				EnvVars: []string{"CREATEOS_ORB_TEMPLATE"},
			},
		},
		Action: func(c *cli.Context) error {
			for _, f := range []string{"shape", "template"} {
				if !ampFlagValueRE.MatchString(c.String(f)) {
					return fmt.Errorf("--%s %q is not a valid name", f, c.String(f))
				}
			}
			if c.Bool("dispatch") {
				return runAmpDispatch(c)
			}
			return runAmpSetup(c, c.Bool("doctor"))
		},
	}
}

// ---------------------------------------------------------------- human half

func runAmpSetup(c *cli.Context, doctorOnly bool) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("the Amp orb provider runs on macOS and Linux only")
	}
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' first")
	}
	if _, _, err := client.ListSandboxes(c.Context, api.ListSandboxesOpts{Limit: 1}); err != nil {
		return fmt.Errorf("your session is not usable — run 'createos login' again: %w", err)
	}
	fmt.Println("signed in to CreateOS")

	ampBin, err := exec.LookPath("amp")
	if err != nil {
		return fmt.Errorf("amp is not on PATH\n\n  Install it with:\n    curl -fsSL https://ampcode.com/install.sh | bash")
	}
	fmt.Println("amp found at", ampBin)

	if os.Getenv("AMP_ORB_PROVIDER_TOKEN") == "" {
		return fmt.Errorf("AMP_ORB_PROVIDER_TOKEN is not set\n\n" +
			"  In Amp, open Settings > Orbs, click Add Orb Provider and copy the token, then:\n" +
			"    export AMP_ORB_PROVIDER_TOKEN=<the token>")
	}
	fmt.Println("AMP_ORB_PROVIDER_TOKEN is set")
	if doctorOnly {
		return nil
	}

	wrapper, err := writeAmpWrapper(c.String("shape"), c.String("template"))
	if err != nil {
		return err
	}
	fmt.Printf("\nStarting the orb provider. Leave this running.\n"+
		"Each Amp thread on this provider gets its own %s sandbox.\n"+
		"Run it under systemd or launchd to keep it up after a reboot.\n\n", c.String("shape"))

	// Forward Ctrl+C and service-manager stops to amp instead of leaving it
	// orphaned, then report amp's own exit status.
	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()
	// #nosec G204 -- ampBin comes from LookPath and wrapper is our own file.
	cmd := exec.CommandContext(ctx, ampBin, "orb", "provider", wrapper)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	// amp drains running jobs on SIGTERM; give the longest one its whole deadline.
	cmd.WaitDelay = ampJobDeadline + 30*time.Second
	err = cmd.Run()
	if ctx.Err() != nil && cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil // stopped on request and amp exited by itself after draining
	}
	return err
}

// writeAmpWrapper writes the program Amp runs per job and returns its path.
// The flags are baked in rather than read from env, since nothing promises
// that the provider passes its own environment through to each job.
func writeAmpWrapper(shape, template string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not find the createos binary: %w", err)
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return "", fmt.Errorf("could not find the createos binary: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find your home folder: %w", err)
	}
	dir := filepath.Join(home, ".createos")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// A unique temp file, renamed into place, so two setups never see a half-written wrapper.
	tmp, err := os.CreateTemp(dir, ampWrapperName+".*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() //nolint:errcheck // gone after a successful rename
	if _, err = tmp.WriteString(ampWrapperScript(self, shape, template)); err == nil {
		err = tmp.Chmod(0o700)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	// One wrapper per setting, so two providers with different flags under one
	// account never run each other's. Both values match ampFlagValueRE.
	path := filepath.Join(dir, ampWrapperName+"-"+shape+"-"+template)
	return path, os.Rename(tmp.Name(), path)
}

func ampWrapperScript(self, shape, template string) string {
	return "#!/bin/sh\n" +
		"# Written by `createos sandbox setup amp`. Amp's orb provider runs this once per job.\n" +
		"exec " + shellQuote(self) + " sandbox setup amp --dispatch" +
		" --shape " + shellQuote(shape) + " --template " + shellQuote(template) + "\n"
}

// -------------------------------------------------------------- machine half

func runAmpDispatch(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("not signed in — run 'createos login' on the provider's machine")
	}
	orbID := strings.TrimSpace(os.Getenv("ORB_ID"))
	if orbID == "" {
		return fmt.Errorf("ORB_ID is not set; Amp's orb provider runs this, not you")
	}
	ctx, cancel := context.WithTimeout(c.Context, ampJobDeadline)
	defer cancel()
	name := ampSandboxName(orbID)

	unlock, err := ampLock(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()

	sb, err := ampSettle(ctx, client, name)
	if err != nil {
		return err
	}
	switch op := os.Getenv("ORB_OPERATION"); op {
	case "spawn":
		return ampSpawn(ctx, client, name, sb, c.String("shape"), c.String("template"))
	case "pause":
		// CreateOS pause keeps disk and memory. ORB_SNAPSHOT_ID is ignored,
		// which the contract allows: each new thread boots fresh.
		if sb == nil || sb.Status != api.SandboxStatusRunning {
			return nil
		}
		if _, err := client.PauseSandbox(ctx, sb.ID); err != nil {
			return fmt.Errorf("pause %s: %w", name, err)
		}
		return ampWaitStatus(ctx, client, sb.ID, api.SandboxStatusPaused, ampSettleWait)
	case "delete":
		if sb == nil {
			return nil
		}
		if err := client.DestroySandbox(ctx, sb.ID); err != nil && !api.IsNotFound(err) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
		ampLog("deleted %s", name)
		return nil
	default:
		return fmt.Errorf("unknown ORB_OPERATION %q", op)
	}
}

// ampSandboxName maps an orb to its sandbox. Names cap at 22 characters and
// ORB_ID (amp-<thread id>) is longer, so hash it.
func ampSandboxName(orbID string) string {
	sum := sha256.Sum256([]byte(orbID))
	return "amp-" + hex.EncodeToString(sum[:])[:18]
}

// ampFind returns the live sandbox for name, or nil when there is none.
// A lookup failure is an error, never "absent": read as absent, delete would
// report success and leave the sandbox running.
func ampFind(ctx context.Context, client *api.SandboxClient, name string) (*api.SandboxView, error) {
	var found []api.SandboxView
	for _, status := range ampLiveStatuses {
		for offset := 0; ; {
			rows, page, err := client.ListSandboxes(ctx, api.ListSandboxesOpts{Limit: 200, Offset: offset, Status: status})
			if err != nil {
				return nil, fmt.Errorf("look up sandbox %s: %w", name, err)
			}
			for _, r := range rows {
				if r.Name != nil && *r.Name == name {
					found = append(found, r)
				}
			}
			offset += len(rows)
			if len(rows) == 0 || offset >= page.Total {
				break
			}
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	if len(found) > 1 {
		ampLog("warning: %d live sandboxes are named %s; using the newest", len(found), name)
	}
	sb := mostRecent(found)
	return &sb, nil
}

// ampSettle finds the sandbox and waits out a transition in flight, since Amp
// can send a job while the previous one is still settling (spawn right after
// pause). It fails rather than act on a state that never settles.
func ampSettle(ctx context.Context, client *api.SandboxClient, name string) (*api.SandboxView, error) {
	sb, err := ampFind(ctx, client, name)
	if err != nil || sb == nil || !ampSettling(sb.Status) {
		return sb, err
	}
	deadline := time.Now().Add(ampSettleWait)
	for ampSettling(sb.Status) {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("sandbox %s is still %s after %s", name, sb.Status, ampSettleWait)
		}
		if err = ampSleep(ctx, ampPollEvery); err != nil {
			return nil, err
		}
		if sb, err = client.GetSandbox(ctx, sb.ID); err != nil {
			return nil, fmt.Errorf("look up sandbox %s: %w", name, err)
		}
	}
	return sb, nil
}

func ampSpawn(ctx context.Context, client *api.SandboxClient, name string, sb *api.SandboxView, shape, template string) error {
	if sb != nil && sb.Status == api.SandboxStatusError {
		ampLog("%s is in an error state; replacing it", name)
		if err := client.DestroySandbox(ctx, sb.ID); err != nil && !api.IsNotFound(err) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
		sb = nil
	}

	var id string
	switch {
	case sb == nil:
		created, err := client.CreateSandbox(ctx, api.SandboxCreateReq{
			Name:   name,
			Shape:  shape,
			Rootfs: ampPickRootfs(ctx, client, template),
			// A process may only override env keys declared at create time.
			Envs: map[string]string{"ORB_TOKEN": "", "AMP_URL": ""},
		})
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		id = created.ID
		ampLog("created %s (%s)", name, id)
	case sb.Status == api.SandboxStatusPaused:
		id = sb.ID
		if _, err := client.ResumeSandbox(ctx, id); err != nil {
			return fmt.Errorf("resume %s: %w", name, err)
		}
		ampLog("resuming %s", name)
	case sb.Status == api.SandboxStatusRunning:
		id = sb.ID
	default:
		return fmt.Errorf("sandbox %s is %s; refusing to guess", name, sb.Status)
	}
	if err := orcaWaitRunning(ctx, client, id, ampSettleWait); err != nil {
		return err
	}

	exit, err := client.ExecSandboxStream(ctx, id, api.SandboxExecReq{Cmd: "bash", Args: []string{"-c", ampProvision}},
		func(ev api.SandboxExecStreamEvent) {
			// Spawn output shows in the thread's setup log, so keep it.
			_, _ = io.WriteString(os.Stderr, ev.Stdout+ev.Stderr+ev.Error) //nolint:errcheck
		})
	if err != nil {
		return fmt.Errorf("set up %s: %w", name, err)
	}
	if exit != 0 {
		return fmt.Errorf("set up %s: exited with %d", name, exit)
	}

	// The token travels in the request body, not on any command line, and
	// debug logging (--debug, CREATEOS_DEBUG) would print that body. This
	// process ends with the job, so turning it off here touches nothing else.
	client.Client.SetDebug(false)
	proc, err := client.CreateProcess(ctx, id, api.ProcessCreateRequest{
		Cmd: "sudo",
		Args: []string{"-u", "user", "-H", "--preserve-env=ORB_TOKEN,AMP_URL", "bash", "-lc",
			`[ -n "$AMP_URL" ] || unset AMP_URL; cd ~ && exec amp orb bootstrap`},
		Env: map[string]string{"ORB_TOKEN": os.Getenv("ORB_TOKEN"), "AMP_URL": os.Getenv("AMP_URL")},
	})
	if err != nil {
		return fmt.Errorf("start amp orb bootstrap in %s: %w", name, err)
	}
	ampLog("started amp orb bootstrap in %s (process %s)", name, proc.ProcessID)
	return nil
}

// ampPickRootfs returns the template once it is ready, building it first if
// it is missing. A slow or broken build must not fail the job, so after
// ampTemplateWait, or on a failed build, it falls back to devbox:1.
//
// A failed template is left alone: rebuilding it on every spawn would spend
// the template quota on a Dockerfile that fails anyway.
func ampPickRootfs(ctx context.Context, client *api.SandboxClient, template string) string {
	submitted := false
	deadline := time.Now().Add(ampTemplateWait)
	for time.Now().Before(deadline) {
		tpl, err := client.GetTemplate(ctx, template, false)
		switch {
		case err == nil && tpl.Status == "ready":
			return template
		case err == nil && tpl.Status == "failed":
			ampLog("template %s failed to build; booting %s instead. See 'createos sandbox template logs %s', then delete it to rebuild",
				template, ampFallbackRootfs, template)
			return ampFallbackRootfs
		case api.IsNotFound(err) && submitted:
			ampLog("template %s was not accepted for building", template)
			return ampFallbackRootfs
		case api.IsNotFound(err):
			submitted = true
			// Refused when a concurrent spawn submitted it first; the next poll sees that build.
			if _, subErr := client.CreateTemplate(ctx, api.TemplateCreateReq{Name: template, Dockerfile: ampDockerfile}); subErr != nil {
				ampLog("submit template %s: %v", template, subErr)
			} else {
				ampLog("building template %s", template)
			}
		case err != nil:
			ampLog("look up template %s: %v", template, err)
			return ampFallbackRootfs
		}
		if ampSleep(ctx, 3*time.Second) != nil {
			break
		}
	}
	ampLog("template %s is not ready; booting %s instead", template, ampFallbackRootfs)
	return ampFallbackRootfs
}

func ampWaitStatus(ctx context.Context, client *api.SandboxClient, id, want string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		sb, err := client.GetSandbox(ctx, id)
		if err == nil && sb.Status == want {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("sandbox %s did not reach %s within %s: %w", id, want, wait, err)
			}
			return fmt.Errorf("sandbox %s did not reach %s within %s; it is %s", id, want, wait, sb.Status)
		}
		if err := ampSleep(ctx, ampPollEvery); err != nil {
			return err
		}
	}
}

func ampSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return errors.Join(ctx.Err(), context.Cause(ctx))
	case <-time.After(d):
		return nil
	}
}

// Amp's provider log and the thread setup log show stderr.
func ampLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "createos: "+format+"\n", args...)
}
