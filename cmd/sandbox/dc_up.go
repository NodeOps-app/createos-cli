package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/dclock"
)

// Default rootfs for `dc up` — devbox ships docker, buildx, compose
// plugin, sshd, plus the start-docker helper that brings dockerd up on
// first exec.
const dcRootfs = "devbox:1"

// Remote tree layout.
//
//	/workspace                                  ← Mutagen sync root
//	  └── <project>/                            ← per-project subdir
//	      ├── docker-compose.yml                ← pushed (V1) or synced
//	      └── ... user's source tree ...        ← synced by Mutagen (V2)
const remoteWorkspaceRoot = "/workspace"

func newDCUpCommand() *cli.Command {
	return &cli.Command{
		Name:  "up",
		Usage: "Bring the compose stack up on a remote sandbox",
		Description: `Creates (or reuses) a devbox sandbox, pushes the compose file into
it, starts dockerd, and runs 'docker compose up -d' inside the VM.
Saves state to .createos/dc.lock so subsequent dc commands find the
same sandbox.

V1 limits — pushed once, not synced:
  - Only the compose file is shipped to the VM. Push extra files
    (Dockerfiles, .env, source for bind mounts) with 'createos sb push',
    or set up live two-way sync with 'createos sb sync'. A '--sync'
    flag for one-shot Mutagen bootstrap is on the roadmap.

Examples:
  createos sb dc up
  createos sb dc up -f docker-compose.dev.yml
  createos sb dc up --shape s-4vcpu-4gb --disk-mib 40960
  createos sb dc up --name my-proj`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "Path to docker-compose.yml (default: ./docker-compose.yml)",
				Value:   "docker-compose.yml",
			},
			&cli.StringFlag{
				Name:  "name",
				Usage: "Project / sandbox name (default: cwd basename)",
			},
			&cli.StringFlag{
				Name:  "shape",
				Usage: "VM shape (default: s-2vcpu-2gb)",
				Value: "s-2vcpu-2gb",
			},
			&cli.IntFlag{
				Name:  "disk-mib",
				Usage: "Disk size MiB (default: 20480 = 20 GiB)",
				Value: 20480,
			},
			&cli.BoolFlag{
				Name:  "ingress",
				Usage: "Enable public HTTPS URLs for the sandbox",
			},
			&cli.IntFlag{
				Name:  "docker-timeout",
				Usage: "Seconds to wait for dockerd to come up after start-docker",
				Value: 60,
			},
			&cli.BoolFlag{
				Name:    "detach",
				Aliases: []string{"d"},
				Usage:   "Return immediately after services are up — don't hold tunnels open",
			},
			&cli.StringFlag{
				Name:  "bind",
				Usage: "Bind address for forwarded local ports (default: 127.0.0.1)",
				Value: "127.0.0.1",
			},
			&cli.BoolFlag{
				Name:  "no-sync",
				Usage: "Skip Mutagen sync (compose file is pushed once via the files API; bind mounts to ./src won't see laptop edits)",
			},
			&cli.StringFlag{
				Name:    "identity",
				Aliases: []string{"i"},
				Usage:   "SSH private key for sync (default: ~/.ssh/id_ed25519 then auto-managed ~/.createos/" + "dc_ed25519" + ")",
			},
		},
		Action: runDCUp,
	}
}

func runDCUp(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	// ── 1. Resolve project paths + project name ────────────────────────
	composeAbs, absErr := filepath.Abs(c.String("file"))
	if absErr != nil {
		return fmt.Errorf("resolve --file: %w", absErr)
	}
	if _, statErr := os.Stat(composeAbs); statErr != nil {
		return fmt.Errorf("compose file not readable: %w", statErr)
	}
	projectDir := filepath.Dir(composeAbs)
	if rerr := refuseSensitiveProjectDir(projectDir); rerr != nil {
		return rerr
	}
	projectName := c.String("name")
	if projectName == "" {
		projectName = sanitizeProjectName(filepath.Base(projectDir))
	}
	remoteWorkdir := remoteWorkspaceRoot + "/" + projectName
	remoteComposePath := remoteWorkdir + "/" + filepath.Base(composeAbs)

	pterm.Info.Println(fmt.Sprintf("Project: %s   compose: %s", projectName, composeAbs))

	// ── 2. Resolve sandbox: reuse from lockfile or create ──────────────
	sb, lock, sbErr := resolveOrCreateDCSandbox(c, client, projectDir, projectName)
	if sbErr != nil {
		return sbErr
	}
	pterm.Info.Println(fmt.Sprintf("Sandbox: %s (%s)", sb.ID, sb.Status))

	// ── 3a. Start dockerd (needed by docker compose up; also lets us
	//        cleanly run start-docker before mutagen needs it). ─────────
	if dErr := ensureDockerRunning(c.Context, client, sb.ID, c.Int("docker-timeout")); dErr != nil {
		return fmt.Errorf("start docker: %w", dErr)
	}

	// ── 3b. Sync laptop ↔ VM via Mutagen, OR fall back to one-shot
	//        compose-file push when --no-sync / --detach. Sync requires
	//        the dc up to stay foreground for the SSH bridge, so detach
	//        is incompatible. ───────────────────────────────────────────
	wantSync := !c.Bool("no-sync") && !c.Bool("detach")
	var syncSession *dcSyncSession
	if wantSync {
		s, syncErr := ensureDCSyncSession(c, client, sb.ID, projectName, projectDir, remoteWorkdir, lock)
		if syncErr != nil {
			return fmt.Errorf("set up sync: %w", syncErr)
		}
		syncSession = s
		// Save lockfile early so a crash mid-up doesn't lose the
		// session pointer (terminate would otherwise leak it).
		if saveErr := lock.Save(projectDir); saveErr != nil {
			syncSession.Close()
			return fmt.Errorf("save lockfile: %w", saveErr)
		}
	} else {
		if pushErr := pushComposeFile(c.Context, client, sb.ID, composeAbs, remoteComposePath); pushErr != nil {
			return fmt.Errorf("push compose file: %w", pushErr)
		}
		pterm.Success.Println("Compose file pushed to " + remoteComposePath)
	}
	// Make sure we always close the bridge on exit, even on later errors.
	defer func() {
		if syncSession != nil {
			syncSession.Close()
		}
	}()

	// ── 5. docker compose up -d (streamed) ─────────────────────────────
	pterm.Info.Println("Running 'docker compose up -d' …")
	if upErr := composeUp(c.Context, client, sb.ID, projectName, remoteComposePath); upErr != nil {
		return fmt.Errorf("docker compose up: %w", upErr)
	}

	// ── 6. Parse published ports for the lockfile + summary ────────────
	ports, psErr := composePsPorts(c.Context, client, sb.ID, projectName, remoteComposePath)
	if psErr != nil {
		// Non-fatal — the stack is up; we just can't show ports.
		pterm.Warning.Println("Could not parse compose port map: " + psErr.Error())
	}

	// ── 7. Save lockfile ───────────────────────────────────────────────
	lock.SandboxID = sb.ID
	lock.ProjectName = projectName
	lock.ComposeFile = remoteComposePath
	lock.RemoteWorkdir = remoteWorkdir
	lock.Ports = ports
	if saveErr := lock.Save(projectDir); saveErr != nil {
		return fmt.Errorf("save lockfile: %w", saveErr)
	}

	// ── 8. Print summary ───────────────────────────────────────────────
	printDCUpSummary(sb, ports, c.Bool("ingress"))

	// ── 9. Foreground hold: port-forwards + (if active) keep mutagen
	//        SSH bridge alive. Detach skips both — returns immediately.
	if c.Bool("detach") {
		return nil
	}
	bind := c.String("bind")
	if bind == "" {
		bind = "127.0.0.1"
	}
	return holdTunnels(c, sb.ID, ports, bind, syncSession != nil)
}

// holdTunnels keeps the dc up process foreground so:
//
//   - Each published port's accept loop forwarding localhost:PORT →
//     control → VM stays alive. (These die with the process.)
//   - If sync is active, the SSH bridge for the mutagen daemon stays
//     alive too. (Already opened before this call.)
//
// Blocks on SIGINT/SIGTERM. The compose stack inside the VM keeps
// running — closing tunnels + sync is purely local cleanup. The
// session resumes on the next `dc up`.
func holdTunnels(c *cli.Context, sandboxID string, ports []dclock.Port, bind string, syncActive bool) error {
	ctrlURL := strings.TrimSpace(c.String("sandbox-api-url"))
	if ctrlURL == "" {
		ctrlURL = api.DefaultSandboxBaseURL
	}
	authHeader, token, err := sandboxAuth(c)
	if err != nil {
		return err
	}

	pterm.Println()
	pterm.Info.Println("Forwarding ports (Ctrl+C to stop):")
	var (
		listeners []net.Listener
		lc        net.ListenConfig
	)
	for _, p := range ports {
		addr := net.JoinHostPort(bind, strconv.Itoa(p.LocalPort))
		l, err := lc.Listen(c.Context, "tcp", addr)
		if err != nil {
			// Don't fail the whole 'up' — one port collision is
			// recoverable. Tell the user and keep going so the rest
			// of the stack stays reachable.
			pterm.Warning.Printfln("  %s ← %s (skipped: %v)", addr, p.Service, err)
			continue
		}
		listeners = append(listeners, l)
		pterm.Success.Printfln("  %s ← %s (container :%d)", addr, p.Service, p.ContainerPort)
		go acceptLoop(c.Context, l, ctrlURL, authHeader, token, sandboxID, p.LocalPort)
	}
	if len(listeners) == 0 && !syncActive {
		pterm.Warning.Println("Nothing to hold open — exiting.")
		return nil
	}
	if syncActive {
		pterm.Info.Println("Watching for file changes (Mutagen) …")
	}

	// Hold until signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	<-sigCh
	pterm.Println()
	if syncActive {
		pterm.Info.Println("Closing tunnels + pausing sync …")
	} else {
		pterm.Info.Println("Closing tunnels …")
	}
	var wg sync.WaitGroup
	for _, l := range listeners {
		wg.Add(1)
		go func(l net.Listener) {
			defer wg.Done()
			_ = l.Close() //nolint:errcheck
		}(l)
	}
	wg.Wait()
	pterm.Info.Println("Stack is still running. 'sb dc up' to resume sync; 'sb dc down' to destroy.")
	return nil
}

// acceptLoop serves a single listener until Close. Per-connection
// bridging happens on its own goroutine via bridgeOne so a stuck conn
// can't head-of-line every other client.
func acceptLoop(ctx context.Context, l net.Listener, ctrlURL, authHeader, token, id string, remote int) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go bridgeOne(ctx, ctrlURL, authHeader, token, id, remote, conn)
	}
}

// resolveOrCreateDCSandbox looks for an existing live sandbox via the
// lockfile; if absent/dead, creates a fresh devbox sandbox. Returns
// the live SandboxView plus the Lock object (with prior raw fields
// preserved if reloaded).
func resolveOrCreateDCSandbox(c *cli.Context, client *api.SandboxClient, projectDir, projectName string) (*api.SandboxView, *dclock.Lock, error) {
	existing, lerr := dclock.Load(projectDir)
	if lerr != nil && !errors.Is(lerr, dclock.ErrNotFound) {
		return nil, nil, lerr
	}
	if existing != nil && existing.SandboxID != "" {
		sb, gerr := client.GetSandbox(c.Context, existing.SandboxID)
		if gerr == nil && isReusableStatus(sb.Status) {
			pterm.Info.Println("Reusing sandbox from .createos/dc.lock")
			if sb.Status != "running" {
				// paused / pausing / etc — resume it.
				resumed, rerr := client.ResumeSandbox(c.Context, sb.ID)
				if rerr != nil {
					return nil, nil, fmt.Errorf("resume sandbox %s: %w", sb.ID, rerr)
				}
				ready, werr := waitForStatus(c.Context, client, resumed.ID, "running")
				if werr != nil {
					return nil, nil, fmt.Errorf("wait for resume: %w", werr)
				}
				return ready, existing, nil
			}
			return sb, existing, nil
		}
		// Stale lockfile — sandbox gone / failed. Fall through to create.
		pterm.Warning.Println("Lockfile sandbox " + existing.SandboxID + " is not usable — creating a fresh one")
	}

	pterm.Info.Println("Creating sandbox …")
	req := api.SandboxCreateReq{
		Name:           projectName,
		Shape:          c.String("shape"),
		Rootfs:         dcRootfs,
		DiskMib:        int64(c.Int("disk-mib")),
		IngressEnabled: c.Bool("ingress"),
	}
	created, err := client.CreateSandbox(c.Context, req)
	if err != nil {
		return nil, nil, fmt.Errorf("create sandbox: %w", err)
	}
	ready, err := waitForStatus(c.Context, client, created.ID, "running")
	if err != nil {
		return nil, nil, fmt.Errorf("wait for sandbox to be running: %w", err)
	}
	lock := existing
	if lock == nil {
		lock = &dclock.Lock{}
	}
	return ready, lock, nil
}

// isReusableStatus reports whether a sandbox's status is recoverable
// for `dc up`. running = use directly. paused / pausing / resuming =
// poke and resume. Anything else (destroyed, failed, error) = give up.
func isReusableStatus(s string) bool {
	switch s {
	case "running", "paused", "pausing", "resuming":
		return true
	default:
		return false
	}
}

// refuseSensitiveProjectDir blocks `dc up` from project dirs that
// shouldn't be treated as a project: HOME and "/" most importantly.
// Mirrors the safety check in cmd/sandbox/sync.go.
func refuseSensitiveProjectDir(dir string) error {
	if dir == "/" {
		return fmt.Errorf("refusing to run dc from /")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		clean := filepath.Clean(dir)
		if clean == filepath.Clean(home) {
			return fmt.Errorf("refusing to run dc directly from $HOME — cd into a project subdirectory first")
		}
	}
	return nil
}

// sanitizeProjectName makes a directory basename safe for use as both
// a docker compose project label and an fc-spawn sandbox name (DNS
// label: lowercase a-z 0-9 hyphen, max 63).
func sanitizeProjectName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = out[:63]
	}
	if out == "" {
		out = "project"
	}
	return out
}

// pushComposeFile streams the local compose file into
// /workspace/<project>/<basename> via the agent's file API. Parent dirs
// are auto-created by the agent (per CLAUDE.md /v1/sandboxes/:id/files).
func pushComposeFile(ctx context.Context, client *api.SandboxClient, id, localPath, remotePath string) error {
	f, err := os.Open(localPath) // #nosec G304 -- caller-provided compose path
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return client.UploadFile(ctx, id, remotePath, f, st.Size())
}

// ensureDockerRunning execs `start-docker` (idempotent on devbox:1)
// then polls `docker version` until exit 0 or the timeout fires.
func ensureDockerRunning(ctx context.Context, client *api.SandboxClient, id string, timeoutSec int) error {
	pterm.Info.Println("Starting dockerd …")
	_, err := client.ExecSandbox(ctx, id, api.SandboxExecReq{
		Cmd:  "start-docker",
		Args: nil,
	})
	if err != nil {
		return err
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		resp, perr := client.ExecSandbox(ctx, id, api.SandboxExecReq{
			Cmd:  "docker",
			Args: []string{"version", "--format", "{{.Server.Version}}"},
		})
		if perr == nil && resp.Result.ExitCode == 0 && strings.TrimSpace(resp.Result.Stdout) != "" {
			pterm.Success.Println("dockerd ready (server " + strings.TrimSpace(resp.Result.Stdout) + ")")
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("dockerd did not come up within %ds", timeoutSec)
}

// composeUp streams `docker compose up -d` so the user sees pulls /
// builds / container creation as they happen. Errors out if the inner
// command's exit code is non-zero.
func composeUp(ctx context.Context, client *api.SandboxClient, id, projectName, composeFile string) error {
	req := api.SandboxExecReq{
		Cmd: "docker",
		Args: []string{
			"compose", "-p", projectName, "-f", composeFile, "up", "-d",
		},
	}
	exit, err := client.ExecSandboxStream(ctx, id, req, func(ev api.SandboxExecStreamEvent) {
		switch {
		case ev.Stdout != "":
			_, _ = io.WriteString(os.Stdout, ev.Stdout) //nolint:errcheck
		case ev.Stderr != "":
			_, _ = io.WriteString(os.Stderr, ev.Stderr) //nolint:errcheck
		case ev.Error != "":
			pterm.Error.Println(ev.Error)
		}
	})
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("docker compose up -d exited %d", exit)
	}
	return nil
}

// composePsRow models the subset of `docker compose ps --format json`
// we read. Each JSON object on its own line maps to one row.
type composePsRow struct {
	Name       string `json:"Name"`
	Service    string `json:"Service"`
	Publishers []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

// composePsPorts runs `docker compose ps --format json` (buffered) and
// flattens its Publishers blocks into dclock.Port records. Only ports
// with a non-zero PublishedPort are recorded — unpublished container
// ports are not user-reachable.
func composePsPorts(ctx context.Context, client *api.SandboxClient, id, projectName, composeFile string) ([]dclock.Port, error) {
	resp, err := client.ExecSandbox(ctx, id, api.SandboxExecReq{
		Cmd: "docker",
		Args: []string{
			"compose", "-p", projectName, "-f", composeFile, "ps", "--format", "json",
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.Result.ExitCode != 0 {
		return nil, fmt.Errorf("compose ps exit %d: %s", resp.Result.ExitCode, resp.Result.Stderr)
	}
	out := []dclock.Port{}
	// Docker compose emits one JSON object per line OR a single JSON
	// array depending on plugin version; handle both.
	trimmed := strings.TrimSpace(resp.Result.Stdout)
	if trimmed == "" {
		return out, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var rows []composePsRow
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return nil, fmt.Errorf("parse array: %w", err)
		}
		for _, r := range rows {
			out = append(out, rowToPorts(r)...)
		}
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	for dec.More() {
		var r composePsRow
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("parse ndjson row: %w", err)
		}
		out = append(out, rowToPorts(r)...)
	}
	return out, nil
}

func rowToPorts(r composePsRow) []dclock.Port {
	// docker compose ps emits one Publisher row per address family
	// (0.0.0.0 + [::]) for the same port mapping. Dedupe so the table
	// + lockfile show each user-visible port once.
	seen := map[string]struct{}{}
	var out []dclock.Port
	for _, p := range r.Publishers {
		if p.PublishedPort == 0 {
			continue
		}
		key := fmt.Sprintf("%d/%s/%d", p.PublishedPort, p.Protocol, p.TargetPort)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, dclock.Port{
			Service:       r.Service,
			ContainerPort: p.TargetPort,
			LocalPort:     p.PublishedPort,
			Protocol:      p.Protocol,
		})
	}
	return out
}

// printDCUpSummary renders the table users see after `dc up`. Keeps
// pterm shape consistent with the rest of the CLI.
func printDCUpSummary(sb *api.SandboxView, ports []dclock.Port, ingress bool) {
	pterm.Println()
	pterm.Success.Println("Stack is up.")
	pterm.Println("Sandbox: " + pterm.Cyan(sb.ID))
	if len(ports) == 0 {
		pterm.Println("No published ports detected.")
		return
	}
	rows := [][]string{{"SERVICE", "CONTAINER", "PUBLISHED"}}
	for _, p := range ports {
		rows = append(rows, []string{
			p.Service,
			fmt.Sprintf("%d/%s", p.ContainerPort, defaultProto(p.Protocol)),
			fmt.Sprintf("%d", p.LocalPort),
		})
	}
	_ = pterm.DefaultTable.WithHasHeader().WithData(rows).Render() //nolint:errcheck
	pterm.Println()
	pterm.Info.Println("Forward a port to localhost:")
	pterm.Println(fmt.Sprintf("  createos sb tunnel %s <local>:<%d>", sb.ID, ports[0].LocalPort))
	if ingress && sb.IngressURLTemplate != "" {
		pterm.Info.Println("Or reach published ports directly via ingress:")
		pterm.Println("  " + sb.IngressURLTemplate + "   (substitute <port> with the published port)")
	}
}

func defaultProto(p string) string {
	if p == "" {
		return "tcp"
	}
	return p
}
