package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

const (
	runSandboxRootfs = "devbox:1"
	runDefaultShape  = "s-1vcpu-1gb"
	runDiskWaitLimit = 45 * time.Second
)

func newRunCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Run a Docker image in a fresh sandbox",
		ArgsUsage: "<image> [args...]",
		Description: `Create a devbox sandbox, run a Docker image inside it, and optionally
forward a local port to the container.

Examples:
  createos sb run nginx --local 8080 --remote 80

  createos sb run postgres \
    --disk pg-data,/data:/var/lib/postgresql/data \
    --local 5432 --remote 5432

Disk format:
  --disk <name|id>,<sandbox-path>:<container-path>

The disk is attached to the sandbox at <sandbox-path>, then passed to Docker as
-v <sandbox-path>:<container-path>.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "shape", Value: runDefaultShape, Usage: "Sandbox size"},
			&cli.StringFlag{Name: "name", Usage: "Friendly name for the sandbox"},
			&cli.IntFlag{Name: "local", Usage: "Local port to listen on"},
			&cli.IntFlag{Name: "remote", Usage: "Container port to expose through the sandbox"},
			&cli.StringFlag{Name: "bind", Value: "127.0.0.1", Usage: "Local address to bind to"},
			&cli.StringSliceFlag{Name: "network", Aliases: []string{"net"}, Usage: "Private network to join at creation (repeatable): <name|id>"},
			&cli.GenericFlag{Name: "disk", Value: newRunDiskFlagValues(), Usage: "Disk to attach and mount into Docker (repeatable): <name|id>,<sandbox-path>:<container-path>"},
			&cli.GenericFlag{Name: "sync", Value: newRunSyncFlagValues(), Usage: "Sync a local directory and mount it into Docker (repeatable): <local-dir>,<sandbox-path>:<container-path>"},
			&cli.StringSliceFlag{Name: "env", Usage: "Docker environment variable (repeatable): KEY=VALUE"},
			&cli.StringFlag{Name: "identity", Aliases: []string{"i"}, Usage: "SSH private key override for --sync"},
			&cli.StringFlag{Name: "user", Aliases: []string{"u"}, Value: "root", Usage: "Username inside the sandbox for --sync"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "Skip prompts for explicit SSH key setup"},
			&cli.BoolFlag{Name: "force-sync", Usage: "Bypass local sensitive-path checks for --sync"},
			&cli.StringSliceFlag{Name: "exclude", Usage: "Sync ignore pattern for --sync (repeatable)"},
			&cli.StringFlag{Name: "sync-mode", Value: "two-way", Usage: "Sync direction for --sync: two-way | one-way | mirror"},
			&cli.BoolFlag{Name: "pull", Usage: "Pull the image before running it"},
			&cli.BoolFlag{Name: "push-local", Usage: "Upload a local Docker image into the sandbox before running it"},
			&cli.BoolFlag{Name: "rm", Usage: "Delete the sandbox when the container exits or this command is interrupted"},
			&cli.BoolFlag{Name: "keep-container", Usage: "Do not pass --rm to Docker"},
			&cli.BoolFlag{Name: "no-follow", Usage: "Start the container and print IDs without following output"},
		},
		Action: runDockerImage,
	}
}

type runOptions struct {
	image         string
	imageArgs     []string
	shape         string
	name          string
	local         int
	remote        int
	bind          string
	networks      []string
	disks         []runDiskMount
	syncs         []runSyncMount
	envs          []string
	identity      string
	user          string
	forceSync     bool
	assumeYes     bool
	exclude       []string
	syncMode      string
	pull          bool
	pushLocal     bool
	removeSandbox bool
	keepContainer bool
	noFollow      bool
}

type runDiskMount struct {
	diskID        string
	sandboxPath   string
	containerPath string
}

type runSyncMount struct {
	localPath     string
	sandboxPath   string
	containerPath string
}

func runDockerImage(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	opts, err := parseRunArgs(c)
	if err != nil {
		return err
	}

	req := api.SandboxCreateReq{
		Shape:  opts.shape,
		Name:   opts.name,
		Rootfs: runSandboxRootfs,
	}
	for _, n := range opts.networks {
		req.Networks = append(req.Networks, api.SandboxNetworkAttach{ID: n})
	}
	for _, d := range opts.disks {
		req.Disks = append(req.Disks, api.SandboxDiskAttach{
			DiskID:    d.diskID,
			MountPath: d.sandboxPath,
		})
	}

	spinner, _ := pterm.DefaultSpinner.Start("Creating sandbox...") //nolint:errcheck
	sb, err := client.CreateSandbox(c.Context, req)
	if err != nil {
		spinner.Fail("Could not create sandbox")
		return err
	}
	spinner.Success(fmt.Sprintf("Sandbox is ready: %s", refLabel(runSandboxName(sb), sb.ID)))
	if opts.removeSandbox {
		defer cleanupRunSandbox(sb.ID, client)
	}
	if len(opts.syncs) > 0 {
		cleanup, err := startRunSyncs(c, client, sb.ID, runSandboxName(sb), opts)
		if err != nil {
			return err
		}
		defer cleanup()
	}

	if len(opts.disks) > 0 {
		spinner, _ = pterm.DefaultSpinner.Start("Waiting for disk mounts...") //nolint:errcheck
		if err := waitForRunDisks(c.Context, client, sb.ID, opts.disks); err != nil {
			spinner.Fail("Disk mount failed")
			return err
		}
		spinner.Success("Disks are mounted")
	}

	if opts.pull {
		if err := runDockerPull(c, client, sb.ID, opts.image); err != nil {
			return err
		}
	}
	if opts.pushLocal {
		if err := pushLocalDockerImage(c, client, sb.ID, opts.image); err != nil {
			return err
		}
	}

	dockerArgs := buildDockerRunArgs(opts)
	spinner, _ = pterm.DefaultSpinner.Start("Starting container...") //nolint:errcheck
	proc, err := client.CreateProcess(c.Context, sb.ID, api.ProcessCreateRequest{
		Cmd:  "docker",
		Args: dockerArgs,
	})
	if err != nil {
		spinner.Fail("Could not start container")
		return err
	}
	spinner.Success(fmt.Sprintf("Container process started: %s", proc.ProcessID))

	if opts.noFollow {
		output.Render(c, map[string]any{
			"sandbox_id": sb.ID,
			"name":       runSandboxName(sb),
			"process_id": proc.ProcessID,
			"image":      opts.image,
		}, func() {
			pterm.Success.Printf("Started %s in %s.\n", proc.ProcessID, refLabel(runSandboxName(sb), sb.ID))
		})
		return nil
	}

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()
	if opts.remote > 0 {
		if opts.local <= 0 {
			opts.local = opts.remote
		}
		if opts.bind == "" {
			opts.bind = "127.0.0.1"
		}
		ctrlURL := strings.TrimSpace(c.String("sandbox-api-url"))
		if ctrlURL == "" {
			ctrlURL = api.DefaultSandboxBaseURL
		}
		authHeader, token, authErr := sandboxAuth(c)
		if authErr != nil {
			return authErr
		}
		tunnelErr := make(chan error, 1)
		go func() {
			tunnelErr <- serveSandboxTunnel(ctx, tunnelSpec{
				CtrlURL:    ctrlURL,
				AuthHeader: authHeader,
				Token:      token,
				SandboxID:  sb.ID,
				Ref:        runSandboxName(sb),
				Local:      opts.local,
				Remote:     opts.remote,
				Bind:       opts.bind,
				Announce:   false,
			})
		}()
		if err := waitForPort(ctx, opts.bind, opts.local, 2*time.Second); err != nil {
			cancel()
			return err
		}
		pterm.Success.Printfln("Forwarding %s → %s:%d", net.JoinHostPort(opts.bind, strconv.Itoa(opts.local)), refLabel(runSandboxName(sb), sb.ID), opts.remote)
		pterm.Println(pterm.Gray(fmt.Sprintf("  Open the local address after the container is listening on :%d.", opts.remote)))
		select {
		case err := <-tunnelErr:
			if err != nil {
				return err
			}
		default:
		}
	}
	pterm.Println(pterm.Gray("  Press Ctrl+C to stop the container and tunnel."))
	pterm.Println(pterm.Gray("  Container output:"))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		cancel()
		_, _ = client.TerminateProcess(context.Background(), sb.ID, proc.ProcessID, durationMs(time.Second)) //nolint:errcheck
	}()

	exitCode, signalName, err := followRunProcess(ctx, client, sb.ID, proc.ProcessID)
	cancel()
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		return err
	}
	return exitFromProcess(exitCode, signalName)
}

func parseRunArgs(c *cli.Context) (runOptions, error) {
	opts := runOptions{
		shape:         strings.TrimSpace(c.String("shape")),
		name:          strings.TrimSpace(c.String("name")),
		local:         c.Int("local"),
		remote:        c.Int("remote"),
		bind:          strings.TrimSpace(c.String("bind")),
		networks:      stringSliceCleanup(c.StringSlice("network")),
		envs:          c.StringSlice("env"),
		identity:      strings.TrimSpace(c.String("identity")),
		user:          strings.TrimSpace(c.String("user")),
		forceSync:     c.Bool("force-sync"),
		assumeYes:     c.Bool("yes"),
		exclude:       append([]string{}, c.StringSlice("exclude")...),
		syncMode:      c.String("sync-mode"),
		pull:          c.Bool("pull"),
		pushLocal:     c.Bool("push-local"),
		removeSandbox: c.Bool("rm"),
		keepContainer: c.Bool("keep-container"),
		noFollow:      c.Bool("no-follow"),
	}
	if opts.shape == "" {
		opts.shape = runDefaultShape
	}
	for _, raw := range runDiskFlagValuesFromContext(c) {
		d, err := parseRunDiskFlag(raw)
		if err != nil {
			return opts, err
		}
		opts.disks = append(opts.disks, d)
	}
	for _, raw := range runSyncFlagValuesFromContext(c) {
		s, err := parseRunSyncFlag(raw)
		if err != nil {
			return opts, err
		}
		opts.syncs = append(opts.syncs, s)
	}

	args := c.Args().Slice()
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if opts.image != "" && i+1 < len(args) {
				opts.imageArgs = append(opts.imageArgs, args[i+1:]...)
				i = len(args)
			}
		case a == "--shape" && i+1 < len(args):
			opts.shape = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--shape="):
			opts.shape = strings.TrimSpace(strings.TrimPrefix(a, "--shape="))
		case a == "--name" && i+1 < len(args):
			opts.name = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--name="):
			opts.name = strings.TrimSpace(strings.TrimPrefix(a, "--name="))
		case a == "--local" && i+1 < len(args):
			opts.local = atoiPort(args[i+1])
			i++
		case strings.HasPrefix(a, "--local="):
			opts.local = atoiPort(strings.TrimPrefix(a, "--local="))
		case a == "--remote" && i+1 < len(args):
			opts.remote = atoiPort(args[i+1])
			i++
		case strings.HasPrefix(a, "--remote="):
			opts.remote = atoiPort(strings.TrimPrefix(a, "--remote="))
		case a == "--bind" && i+1 < len(args):
			opts.bind = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--bind="):
			opts.bind = strings.TrimSpace(strings.TrimPrefix(a, "--bind="))
		case (a == "--network" || a == "--net") && i+1 < len(args):
			opts.networks = append(opts.networks, strings.TrimSpace(args[i+1]))
			i++
		case strings.HasPrefix(a, "--network="):
			opts.networks = append(opts.networks, strings.TrimSpace(strings.TrimPrefix(a, "--network=")))
		case strings.HasPrefix(a, "--net="):
			opts.networks = append(opts.networks, strings.TrimSpace(strings.TrimPrefix(a, "--net=")))
		case a == "--disk" && i+1 < len(args):
			d, err := parseRunDiskFlag(args[i+1])
			if err != nil {
				return opts, err
			}
			opts.disks = append(opts.disks, d)
			i++
		case strings.HasPrefix(a, "--disk="):
			d, err := parseRunDiskFlag(strings.TrimPrefix(a, "--disk="))
			if err != nil {
				return opts, err
			}
			opts.disks = append(opts.disks, d)
		case a == "--sync" && i+1 < len(args):
			s, err := parseRunSyncFlag(args[i+1])
			if err != nil {
				return opts, err
			}
			opts.syncs = append(opts.syncs, s)
			i++
		case strings.HasPrefix(a, "--sync="):
			s, err := parseRunSyncFlag(strings.TrimPrefix(a, "--sync="))
			if err != nil {
				return opts, err
			}
			opts.syncs = append(opts.syncs, s)
		case a == "--env" && i+1 < len(args):
			opts.envs = append(opts.envs, args[i+1])
			i++
		case strings.HasPrefix(a, "--env="):
			opts.envs = append(opts.envs, strings.TrimPrefix(a, "--env="))
		case (a == "--identity" || a == "-i") && i+1 < len(args):
			opts.identity = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--identity="):
			opts.identity = strings.TrimSpace(strings.TrimPrefix(a, "--identity="))
		case (a == "--user" || a == "-u") && i+1 < len(args):
			opts.user = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--user="):
			opts.user = strings.TrimSpace(strings.TrimPrefix(a, "--user="))
		case a == "--force-sync":
			opts.forceSync = true
		case a == "--yes" || a == "-y":
			opts.assumeYes = true
		case a == "--exclude" && i+1 < len(args):
			opts.exclude = append(opts.exclude, args[i+1])
			i++
		case strings.HasPrefix(a, "--exclude="):
			opts.exclude = append(opts.exclude, strings.TrimPrefix(a, "--exclude="))
		case a == "--sync-mode" && i+1 < len(args):
			opts.syncMode = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--sync-mode="):
			opts.syncMode = strings.TrimSpace(strings.TrimPrefix(a, "--sync-mode="))
		case a == "--pull":
			opts.pull = true
		case a == "--push-local":
			opts.pushLocal = true
		case a == "--rm":
			opts.removeSandbox = true
		case a == "--keep-container":
			opts.keepContainer = true
		case a == "--no-follow":
			opts.noFollow = true
		case strings.HasPrefix(a, "-"):
			if opts.image != "" {
				opts.imageArgs = append(opts.imageArgs, a)
			}
		default:
			if opts.image == "" {
				opts.image = strings.TrimSpace(a)
			} else {
				opts.imageArgs = append(opts.imageArgs, a)
			}
		}
	}
	opts.networks = stringSliceCleanup(opts.networks)
	if opts.user == "" {
		opts.user = "root"
	}
	if opts.image == "" {
		return opts, fmt.Errorf("please provide a Docker image\n\n  Example:\n    createos sb run nginx --local 8080 --remote 80")
	}
	if _, err := syncModeToMutagen(opts.syncMode); err != nil {
		return opts, err
	}
	if opts.pull && opts.pushLocal {
		return opts, fmt.Errorf("choose either --pull or --push-local, not both")
	}
	if opts.removeSandbox && opts.noFollow {
		return opts, fmt.Errorf("--rm requires foreground mode; remove --no-follow or delete the sandbox manually later")
	}
	if opts.local > 0 && opts.remote == 0 {
		return opts, fmt.Errorf("--remote is required when --local is set")
	}
	if opts.remote < 0 || opts.remote > 65535 || opts.local < 0 || opts.local > 65535 {
		return opts, fmt.Errorf("--local and --remote must be 1-65535")
	}
	return opts, nil
}

func parseRunDiskFlag(raw string) (runDiskMount, error) {
	raw = strings.TrimSpace(raw)
	comma := strings.IndexByte(raw, ',')
	colon := strings.LastIndexByte(raw, ':')
	if comma <= 0 || colon <= comma+1 || colon == len(raw)-1 {
		return runDiskMount{}, fmt.Errorf("--disk %q must be <name|id>,<sandbox-path>:<container-path>", raw)
	}
	d := runDiskMount{
		diskID:        strings.TrimSpace(raw[:comma]),
		sandboxPath:   strings.TrimSpace(raw[comma+1 : colon]),
		containerPath: strings.TrimSpace(raw[colon+1:]),
	}
	if d.diskID == "" || d.sandboxPath == "" || d.containerPath == "" {
		return runDiskMount{}, fmt.Errorf("--disk %q must be <name|id>,<sandbox-path>:<container-path>", raw)
	}
	if !strings.HasPrefix(d.sandboxPath, "/") || !strings.HasPrefix(d.containerPath, "/") {
		return runDiskMount{}, fmt.Errorf("--disk paths must be absolute: %q", raw)
	}
	return d, nil
}

func parseRunSyncFlag(raw string) (runSyncMount, error) {
	raw = strings.TrimSpace(raw)
	comma := strings.IndexByte(raw, ',')
	colon := strings.LastIndexByte(raw, ':')
	if comma <= 0 || colon <= comma+1 || colon == len(raw)-1 {
		return runSyncMount{}, fmt.Errorf("--sync %q must be <local-dir>,<sandbox-path>:<container-path>", raw)
	}
	s := runSyncMount{
		localPath:     strings.TrimSpace(raw[:comma]),
		sandboxPath:   strings.TrimSpace(raw[comma+1 : colon]),
		containerPath: strings.TrimSpace(raw[colon+1:]),
	}
	if s.localPath == "" || s.sandboxPath == "" || s.containerPath == "" {
		return runSyncMount{}, fmt.Errorf("--sync %q must be <local-dir>,<sandbox-path>:<container-path>", raw)
	}
	if !strings.HasPrefix(s.sandboxPath, "/") || !strings.HasPrefix(s.containerPath, "/") {
		return runSyncMount{}, fmt.Errorf("--sync sandbox and container paths must be absolute: %q", raw)
	}
	return s, nil
}

type runDiskFlagValues struct {
	values []string
}

func newRunDiskFlagValues() *runDiskFlagValues {
	return &runDiskFlagValues{}
}

func (v *runDiskFlagValues) Set(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		v.values = append(v.values, raw)
	}
	return nil
}

func (v *runDiskFlagValues) String() string {
	if v == nil {
		return ""
	}
	return strings.Join(v.values, ",")
}

func runDiskFlagValuesFromContext(c *cli.Context) []string {
	v, ok := c.Generic("disk").(*runDiskFlagValues)
	if !ok || v == nil || len(v.values) == 0 {
		return nil
	}
	return append([]string(nil), v.values...)
}

type runSyncFlagValues struct {
	values []string
}

func newRunSyncFlagValues() *runSyncFlagValues {
	return &runSyncFlagValues{}
}

func (v *runSyncFlagValues) Set(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		v.values = append(v.values, raw)
	}
	return nil
}

func (v *runSyncFlagValues) String() string {
	if v == nil {
		return ""
	}
	return strings.Join(v.values, ",")
}

func runSyncFlagValuesFromContext(c *cli.Context) []string {
	v, ok := c.Generic("sync").(*runSyncFlagValues)
	if !ok || v == nil || len(v.values) == 0 {
		return nil
	}
	return append([]string(nil), v.values...)
}

func buildDockerRunArgs(opts runOptions) []string {
	args := []string{"run"}
	if !opts.keepContainer {
		args = append(args, "--rm")
	}
	if opts.remote > 0 {
		port := strconv.Itoa(opts.remote)
		args = append(args, "-p", "127.0.0.1:"+port+":"+port)
	}
	for _, env := range opts.envs {
		args = append(args, "-e", env)
	}
	for _, d := range opts.disks {
		args = append(args, "-v", d.sandboxPath+":"+d.containerPath)
	}
	for _, s := range opts.syncs {
		args = append(args, "-v", s.sandboxPath+":"+s.containerPath)
	}
	args = append(args, opts.image)
	args = append(args, opts.imageArgs...)
	return args
}

func runDockerPull(c *cli.Context, client *api.SandboxClient, sandboxID, image string) error {
	spinner, _ := pterm.DefaultSpinner.Start("Pulling image...") //nolint:errcheck
	exit, err := client.ExecSandboxStream(c.Context, sandboxID, api.SandboxExecReq{
		Cmd:  "docker",
		Args: []string{"pull", image},
	}, func(ev api.SandboxExecStreamEvent) {
		if ev.Stdout != "" {
			_, _ = os.Stdout.WriteString(ev.Stdout) //nolint:errcheck
		}
		if ev.Stderr != "" {
			_, _ = os.Stderr.WriteString(ev.Stderr) //nolint:errcheck
		}
	})
	if err != nil {
		spinner.Fail("Image pull failed")
		return err
	}
	if exit != 0 {
		spinner.Fail("Image pull failed")
		return fmt.Errorf("docker pull exited with code %d", exit)
	}
	spinner.Success("Image is ready")
	return nil
}

func pushLocalDockerImage(c *cli.Context, client *api.SandboxClient, sandboxID, image string) error {
	if err := inspectLocalDockerImage(c.Context, image); err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "createos-image-*.tar")
	if err != nil {
		return fmt.Errorf("create temp image archive: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() //nolint:errcheck

	spinner, _ := pterm.DefaultSpinner.Start("Saving local image...") //nolint:errcheck
	var stderr bytes.Buffer
	save := exec.CommandContext(c.Context, "docker", "save", image) // #nosec G204 -- image is passed as one docker CLI argument
	save.Stdout = tmp
	save.Stderr = &stderr
	if err := save.Run(); err != nil {
		_ = tmp.Close() //nolint:errcheck
		spinner.Fail("Could not save local image")
		return fmt.Errorf("docker save %s: %w%s", image, err, commandDetail(stderr.String()))
	}
	if err := tmp.Close(); err != nil {
		spinner.Fail("Could not save local image")
		return fmt.Errorf("close image archive: %w", err)
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		spinner.Fail("Could not save local image")
		return fmt.Errorf("stat image archive: %w", err)
	}
	spinner.Success(fmt.Sprintf("Saved local image (%s)", humanBytes(info.Size())))

	f, err := os.Open(tmpPath) // #nosec G304 -- temp path was created by this process
	if err != nil {
		return fmt.Errorf("open image archive: %w", err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck

	remote := "/tmp/createos-images/" + localImageArchiveName(image)
	spinner, _ = pterm.DefaultSpinner.Start("Uploading image to sandbox...") //nolint:errcheck
	if err := client.UploadFile(c.Context, sandboxID, remote, f, info.Size()); err != nil {
		spinner.Fail("Could not upload image")
		return err
	}
	spinner.Success("Uploaded image to sandbox")

	spinner, _ = pterm.DefaultSpinner.Start("Loading image in sandbox...") //nolint:errcheck
	exit, err := client.ExecSandboxStream(c.Context, sandboxID, api.SandboxExecReq{
		Cmd:  "docker",
		Args: []string{"load", "-i", remote},
	}, func(api.SandboxExecStreamEvent) {})
	if err != nil {
		spinner.Fail("Could not load image")
		return err
	}
	if exit != 0 {
		spinner.Fail("Could not load image")
		return fmt.Errorf("docker load exited with code %d", exit)
	}
	spinner.Success("Loaded image in sandbox")
	return nil
}

func cleanupRunSandbox(sandboxID string, client *api.SandboxClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.DestroySandbox(ctx, sandboxID); err != nil {
		pterm.Warning.Printfln("Could not delete sandbox %s: %s", sandboxID, api.UserMessageVerbose(err))
		return
	}
	pterm.Success.Printfln("Deleted sandbox %s", sandboxID)
}

func startRunSyncs(c *cli.Context, client *api.SandboxClient, sandboxID, ref string, opts runOptions) (func(), error) {
	mutagenBin, err := ensureMutagen()
	if err != nil {
		return nil, err
	}
	privPath, pubBytes, cleanupIdentity, err := resolveRunSyncIdentity(sandboxID, opts.identity)
	if err != nil {
		return nil, err
	}
	unlocked, cleanupUnlockedKey, err := unlockSSHKeyIfNeeded(privPath)
	if err != nil {
		cleanupIdentity()
		return nil, err
	}
	privPath = unlocked

	cleanup := func() {
		cleanupUnlockedKey()
		cleanupIdentity()
	}
	user := opts.user
	if user == "" {
		user = "root"
	}
	if _, err := client.AddSSHPubkeys(c.Context, sandboxID, []string{strings.TrimSpace(string(pubBytes))}); err != nil {
		cleanup()
		return nil, fmt.Errorf("could not register sync key with gateway: %w", err)
	}
	if err := ensureAuthorizedKey(c, client, sandboxID, user, ref, pubBytes, true); err != nil {
		cleanup()
		return nil, err
	}

	remoteDirs := make([]string, 0, len(opts.syncs))
	resolved := make([]runSyncMount, 0, len(opts.syncs))
	for _, s := range opts.syncs {
		local, lerr := validateLocalSyncPath(s.localPath, opts.forceSync)
		if lerr != nil {
			cleanup()
			return nil, lerr
		}
		if rerr := validateRemoteSyncPath(s.sandboxPath); rerr != nil {
			cleanup()
			return nil, rerr
		}
		s.localPath = local
		resolved = append(resolved, s)
		remoteDirs = append(remoteDirs, shellQuote(s.sandboxPath))
	}

	authPath := authorizedKeysPath(user)
	prepScript := fmt.Sprintf(`
set -e
if ! [ -x /usr/sbin/sshd ]; then
  echo "this image does not ship sshd — use a rootfs that does (e.g. devbox:1)" >&2
  exit 100
fi
mkdir -p %[1]s /run/sshd %[3]s
chmod 700 %[1]s
chmod 600 %[1]s/authorized_keys
chown -R %[2]s:%[2]s %[1]s 2>/dev/null || true
if ! awk 'NR>1{print $2}' /proc/net/tcp /proc/net/tcp6 2>/dev/null | grep -qi ':0016$'; then
  /usr/sbin/sshd
fi
`, filepath.Dir(authPath), user, strings.Join(remoteDirs, " "))
	if pre, execErr := client.ExecSandbox(c.Context, sandboxID, api.SandboxExecReq{
		Cmd:  "sh",
		Args: []string{"-c", prepScript},
	}); execErr != nil {
		cleanup()
		return nil, fmt.Errorf("could not prepare sshd: %w", execErr)
	} else if pre.Result.ExitCode == 100 {
		cleanup()
		return nil, fmt.Errorf("the sandbox image doesn't have sshd installed — try a rootfs that does (e.g. devbox:1)")
	} else if pre.Result.ExitCode != 0 {
		cleanup()
		return nil, fmt.Errorf("sshd prep failed: %s", strings.TrimSpace(pre.Result.Stderr))
	}

	ctx, cancel := context.WithCancel(c.Context)
	bridge, err := startTunnelBridge(ctx, c, sandboxID, 22)
	if err != nil {
		cancel()
		cleanup()
		return nil, fmt.Errorf("could not open tunnel to the sandbox: %w", err)
	}
	cleanup = func() {
		bridge.close()
		cancel()
		cleanupUnlockedKey()
		cleanupIdentity()
	}
	if err := waitForTCP(ctx, bridge.localAddr, 5*time.Second); err != nil {
		cleanup()
		return nil, fmt.Errorf("sshd did not start in time: %w", err)
	}
	_, port, _ := net.SplitHostPort(bridge.localAddr) //nolint:errcheck

	wrapperDir, wrapperEnv, err := makeSSHWrapper(privPath)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("could not set up ssh wrapper: %w", err)
	}
	cleanup = func() {
		_ = os.RemoveAll(wrapperDir) //nolint:errcheck
		bridge.close()
		cancel()
		cleanupUnlockedKey()
		cleanupIdentity()
	}
	_ = runMutagen(ctx, mutagenBin, wrapperEnv, io.Discard, io.Discard, "daemon", "stop") //nolint:errcheck

	syncMode, err := syncModeToMutagen(opts.syncMode)
	if err != nil {
		cleanup()
		return nil, err
	}
	sessionNames := make([]string, 0, len(resolved))
	for i, s := range resolved {
		sessionName := fmt.Sprintf("createos-run-%s-%d-%d", strings.ReplaceAll(sandboxID, "_", "-"), time.Now().Unix(), i)
		remoteSpec := fmt.Sprintf("%s@127.0.0.1:%s:%s", user, port, s.sandboxPath)
		spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Syncing %s...", s.localPath)) //nolint:errcheck
		createArgs := runMutagenCreateArgs(sessionName, syncMode, s.localPath, remoteSpec, opts.exclude)
		var createBuf bytes.Buffer
		if err := runMutagen(ctx, mutagenBin, wrapperEnv, &createBuf, &createBuf, createArgs...); err != nil {
			spinner.Fail("Could not start sync")
			cleanup()
			detail := strings.TrimSpace(createBuf.String())
			if detail != "" {
				return nil, fmt.Errorf("mutagen sync create failed: %w\n%s", err, detail)
			}
			return nil, fmt.Errorf("mutagen sync create failed: %w", err)
		}
		if err := runMutagen(ctx, mutagenBin, wrapperEnv, &createBuf, &createBuf, "sync", "flush", sessionName); err != nil {
			spinner.Fail("Could not flush sync")
			cleanup()
			detail := strings.TrimSpace(createBuf.String())
			if detail != "" {
				return nil, fmt.Errorf("mutagen sync flush failed: %w\n%s", err, detail)
			}
			return nil, fmt.Errorf("mutagen sync flush failed: %w", err)
		}
		spinner.Success(fmt.Sprintf("Syncing %s -> %s", s.localPath, s.sandboxPath))
		sessionNames = append(sessionNames, sessionName)
	}
	return func() {
		for _, name := range sessionNames {
			_ = runMutagen(context.Background(), mutagenBin, wrapperEnv, io.Discard, io.Discard, "sync", "terminate", name) //nolint:errcheck
		}
		cleanup()
	}, nil
}

func inspectLocalDockerImage(ctx context.Context, image string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", image) // #nosec G204 -- image is passed as one docker CLI argument
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("local Docker image %q was not found%s", image, commandDetail(stderr.String()))
	}
	return nil
}

func resolveRunSyncIdentity(sandboxID, explicit string) (privPath string, pubBytes []byte, cleanup func(), err error) {
	cleanup = func() {}
	if strings.TrimSpace(explicit) != "" {
		priv, pub, err := resolveIdentity(explicit)
		if err != nil {
			return "", nil, cleanup, err
		}
		pubBytes, err := os.ReadFile(pub) // #nosec G304 -- pub is paired with the user-provided private key
		if err != nil {
			return "", nil, cleanup, fmt.Errorf("could not read public key %s: %w", pub, err)
		}
		return priv, pubBytes, cleanup, nil
	}

	alias := sshAlias(sandboxID)
	if !editorAliasRE.MatchString(alias) {
		return "", nil, cleanup, fmt.Errorf("refusing to create shell-unsafe SSH alias %q", alias)
	}
	priv, pubBytes, generated, err := ensureDedicatedKey(alias)
	if err != nil {
		return "", nil, cleanup, err
	}
	if generated {
		cleanup = func() {
			removeDedicatedKey(alias)
		}
	}
	return priv, pubBytes, cleanup, nil
}

func localImageArchiveName(image string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, image)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "image"
	}
	return filepath.Base(name) + ".tar"
}

func commandDetail(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

func runMutagenCreateArgs(sessionName, syncMode, local, remoteSpec string, exclude []string) []string {
	args := mutagenCreateArgs(sessionName, syncMode, local, remoteSpec, true, exclude)
	if len(args) < 2 {
		return args
	}
	insert := []string{
		"--default-file-mode-beta=0644",
		"--default-directory-mode-beta=0755",
	}
	out := make([]string, 0, len(args)+len(insert))
	out = append(out, args[:len(args)-2]...)
	out = append(out, insert...)
	out = append(out, args[len(args)-2:]...)
	return out
}

func waitForRunDisks(ctx context.Context, client *api.SandboxClient, sandboxID string, want []runDiskMount) error {
	deadline := time.Now().Add(runDiskWaitLimit)
	for {
		attached, err := client.ListSandboxDisks(ctx, sandboxID)
		if err != nil {
			return err
		}
		pending := make(map[string]runDiskMount, len(want))
		for _, d := range want {
			pending[d.sandboxPath] = d
		}
		for _, a := range attached {
			d, ok := pending[a.MountPath]
			if !ok {
				continue
			}
			if strings.EqualFold(a.MountStatus, "mounted") {
				delete(pending, a.MountPath)
				continue
			}
			if strings.EqualFold(a.MountStatus, "error") || a.MountError != "" {
				return fmt.Errorf("disk %s failed to mount at %s: %s", d.diskID, d.sandboxPath, strings.TrimSpace(a.MountError))
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			paths := make([]string, 0, len(pending))
			for _, d := range pending {
				paths = append(paths, d.diskID+","+d.sandboxPath+":"+d.containerPath)
			}
			return fmt.Errorf("timed out waiting for disk mounts: %s", strings.Join(paths, ", "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func followRunProcess(ctx context.Context, client *api.SandboxClient, sandboxID, processID string) (*int, string, error) {
	var exitCode *int
	var signalName string
	err := client.ConnectProcess(ctx, sandboxID, processID, 0, func(ev api.ProcessOutputEvent) {
		switch ev.Type {
		case "data":
			if ev.DataBase64 == "" {
				return
			}
			data, err := base64.StdEncoding.DecodeString(ev.DataBase64)
			if err != nil {
				return
			}
			if ev.Stream == "stderr" {
				_, _ = os.Stderr.Write(data) //nolint:errcheck
				return
			}
			_, _ = os.Stdout.Write(data) //nolint:errcheck
		case "exit":
			exitCode = ev.ExitCode
			signalName = ev.Signal
		case "error":
			if ev.Error != "" {
				pterm.Error.Println(ev.Error)
			}
		}
	})
	return exitCode, signalName, err
}

func waitForPort(ctx context.Context, bind string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort(bind, strconv.Itoa(port))
	for {
		conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close() //nolint:errcheck
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("could not start tunnel on %s: %w", addr, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func runSandboxName(sb *api.SandboxCreateResp) string {
	if sb != nil && sb.Name != nil && strings.TrimSpace(*sb.Name) != "" {
		return strings.TrimSpace(*sb.Name)
	}
	return ""
}

func atoiPort(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return n
}
