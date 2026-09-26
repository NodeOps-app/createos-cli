package sandbox

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newOffloadCommand() *cli.Command {
	return &cli.Command{
		Name:      "offload",
		Usage:     "Run one command on a throwaway sandbox, then destroy it",
		ArgsUsage: "<local-dir> -- <command>",
		Description: `Offload moves one piece of work off your machine. It creates a sandbox,
uploads the directory, runs the command inside it, brings back anything you
asked for, and destroys the sandbox — whether the command passed or failed.

Use it for work with a finish line: a test suite, a build, a migration, a
script you did not write. For work that must outlive one command — a dev
server, a watcher — create a sandbox and keep it instead.

The upload honours .gitignore, so node_modules and build output stay on your
machine. Outside a git repository a fixed skip-list stands in.

The command's exit code becomes this command's exit code.

Flags must come before the directory. Anything after it belongs to the
command you are running.

Examples:
  # Run a test suite somewhere else
  createos sandbox offload . -- bun test

  # Lock the sandbox to the npm registry and nothing else
  createos sandbox offload --egress-preset npm . -- npm ci

  # Bring the coverage report back
  createos sandbox offload --fetch coverage . -- bun test --coverage

  # Keep the sandbox when the command fails, so you can shell in and look
  createos sandbox offload --keep-on-fail . -- make build`,
		Flags: append(composeFlags(),
			&cli.StringSliceFlag{
				Name:  "fetch",
				Usage: "Path inside the work directory to download when the command finishes (repeatable)",
			},
			&cli.BoolFlag{
				Name:  "keep-on-fail",
				Usage: "Leave the sandbox alive when the command exits non-zero, so you can inspect it",
			},
		),
		Action: runOffload,
	}
}

func runOffload(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	dir, cmd, err := splitDirAndCommand(c)
	if err != nil {
		return err
	}
	opts, err := parseComposeOptions(c)
	if err != nil {
		return err
	}

	ctx := c.Context
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	quiet := output.IsJSON(c)
	say := func(format string, a ...any) {
		if !quiet {
			pterm.Info.Printfln(format, a...)
		}
	}

	tree, err := stageDir(ctx, dir, stageOptions{Exclude: opts.Exclude})
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tree.Path) }() //nolint:errcheck // temp file; removal failure is benign
	say("Packed %d file(s), %s", tree.Files, humanBytes(tree.Size))

	if len(opts.Egress) == 0 {
		say("Egress unrestricted — this sandbox can reach any host. Restrict it with --egress or --egress-preset.")
	}

	sb, err := createComposeBox(ctx, client, opts)
	if err != nil {
		return err
	}
	say("Sandbox %s is up", sb.ID)

	destroyed := false
	defer func() {
		if !destroyed {
			destroyQuiet(ctx, client, sb.ID, func(msg string) { pterm.Warning.Println(msg) })
		}
	}()

	if shipErr := shipTree(ctx, client, sb.ID, tree, composeWorkDir); shipErr != nil {
		return shipErr
	}

	out := io.Writer(os.Stdout)
	if quiet {
		out = io.Discard
	}
	res, err := runManaged(ctx, client, sb.ID, cmd, composeWorkDir, opts.Env, out)
	if err != nil {
		return err
	}

	if len(c.StringSlice("fetch")) > 0 {
		if err := fetchPaths(ctx, client, sb.ID, composeWorkDir, c.StringSlice("fetch"), dir); err != nil {
			return fmt.Errorf("fetch results: %w", err)
		}
		say("Fetched %s into %s", strings.Join(c.StringSlice("fetch"), ", "), dir)
	}

	teardownFailure := ""
	if res.ExitCode != 0 && c.Bool("keep-on-fail") {
		destroyed = true
		pterm.Warning.Printfln("Command exited %d. Sandbox %s kept.", res.ExitCode, sb.ID)
		fmt.Printf("    Look around:  createos sandbox shell %s\n", sb.ID)
		fmt.Printf("    Destroy it:   createos sandbox rm --force %s\n", sb.ID)
	} else {
		destroyQuiet(ctx, client, sb.ID, func(msg string) { teardownFailure = msg })
		destroyed = true
	}

	if quiet {
		output.Render(c, map[string]any{
			"sandbox":          sb.ID,
			"command":          cmd,
			"exit_code":        res.ExitCode,
			"duration_ms":      res.Duration.Milliseconds(),
			"teardown_failure": teardownFailure,
		}, func() {})
	} else if res.ExitCode == 0 && teardownFailure == "" {
		pterm.Success.Printfln("Done in %s", res.Duration.Round(time.Millisecond))
	}

	// A teardown failure has to change the exit code. `offload` promises a
	// throwaway sandbox; a leaked one keeps billing, and a CI job that only
	// reads the exit status would call this a clean run and never find out.
	if teardownFailure != "" {
		return fmt.Errorf(
			"the command exited %d, but the sandbox was not destroyed: %s\n\n  It is still billable. Remove it with:\n    createos sandbox rm --force %s",
			res.ExitCode, teardownFailure, sb.ID)
	}
	if res.ExitCode != 0 {
		return cli.Exit("", res.ExitCode)
	}
	return nil
}

// splitDirAndCommand pulls <local-dir> and the command out of the argument
// list: the first argument is the directory, the rest is the command.
//
// urfave/cli hands `--` through as an ordinary argument rather than eating
// it, so a literal "--" would end up at the front of the command and the
// shell would fail on it. Dropping it here means both spellings work —
// `offload . -- bun test` and `offload . bun test` — which matters because
// the separator is the single most common thing to get wrong on a command
// shaped like this one.
func splitDirAndCommand(c *cli.Context) (dir, cmd string, err error) {
	args := c.Args().Slice()
	if len(args) < 2 {
		return "", "", errors.New(
			"please give a directory and a command\n\n  Example:\n    createos sandbox offload . -- bun test")
	}
	dir = args[0]
	rest := args[1:]
	// Anything between the directory and `--` was meant as a flag and was
	// not read as one. Catch it before it is pasted into the command line
	// and the shell reports something unrelated.
	if sep := slices.Index(rest, "--"); sep > 0 {
		if flagErr := checkMisplacedFlags(c, rest[:sep]); flagErr != nil {
			return "", "", flagErr
		}
	}
	if rest[0] == "--" {
		rest = rest[1:]
	}
	cmd = strings.TrimSpace(strings.Join(rest, " "))
	if cmd == "" {
		return "", "", errors.New(
			"the command is empty\n\n  Example:\n    createos sandbox offload . -- bun test")
	}
	return dir, cmd, nil
}

// fetchPaths downloads the named paths out of the sandbox and unpacks them
// under localRoot, keeping their relative layout.
//
// One tar for the whole set, not one download per path: the file API moves
// a single stream far better than N round trips, and it keeps directory
// trees intact.
//
// Never point this at a mounted S3 disk. Reading a disk mount through the
// file API is a known crash (issue #71) — copy what you need out of the
// mount from inside the sandbox first.
func fetchPaths(ctx context.Context, client *api.SandboxClient, sandboxID, remoteRoot string, paths []string, localRoot string) error {
	quoted := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimPrefix(strings.TrimSpace(p), "/")
		if p == "" || strings.Contains(p, "..") {
			return fmt.Errorf("--fetch %q must be a path inside the work directory", p)
		}
		quoted = append(quoted, shellQuote(p))
	}
	const remoteTar = "/tmp/createos-fetch.tar"
	pack := fmt.Sprintf("set -eu\ncd %s\ntar -cf %s %s\n",
		shellQuote(remoteRoot), remoteTar, strings.Join(quoted, " "))
	resp, err := client.ExecSandbox(ctx, sandboxID, api.SandboxExecReq{Cmd: "bash", Args: []string{"-lc", pack}})
	if err != nil {
		return err
	}
	if resp.Result.ExitCode != 0 {
		return fmt.Errorf("packing the requested paths exited %d: %s",
			resp.Result.ExitCode, strings.TrimSpace(resp.Result.Stderr))
	}

	tmp, err := os.CreateTemp("", "createos-fetch-*.tar")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()           //nolint:errcheck // read path below owns the error
		_ = os.Remove(tmp.Name()) //nolint:errcheck // temp file
	}()
	if _, err := client.DownloadFile(ctx, sandboxID, remoteTar, tmp); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return untarInto(tmp, localRoot)
}

// untarInto extracts r under root, refusing any entry that would escape it.
//
// Every write goes through os.Root, which resolves names relative to an
// open directory descriptor and refuses any component that leaves the
// root — a symlink included. A lexical prefix check is not enough here:
// it validates the pathname this code builds, while MkdirAll and OpenFile
// still follow a symlink that already exists on the caller's disk. A repo
// holding `coverage -> /etc` plus a sandbox-built entry `coverage/passwd`
// is enough to write outside the tree (CWE-22, CWE-59). The archive is
// produced inside a sandbox that ran code the user did not write, so it
// is untrusted by construction.
func untarInto(r io.Reader, root string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = rootDir.Close() }() //nolint:errcheck // read side owns the real error

	tr := tar.NewReader(r)
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		name := path.Clean("/" + filepath.ToSlash(hdr.Name))
		name = strings.TrimPrefix(name, "/")
		if name == "" || name == "." {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := rootDir.MkdirAll(name, 0o750); err != nil {
				return fmt.Errorf("archive entry %q: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			if dir := path.Dir(name); dir != "." {
				if err := rootDir.MkdirAll(dir, 0o750); err != nil {
					return fmt.Errorf("archive entry %q: %w", hdr.Name, err)
				}
			}
			if err := writeFetchedFile(rootDir, tr, name, hdr.FileInfo().Mode()); err != nil {
				return fmt.Errorf("archive entry %q: %w", hdr.Name, err)
			}
		default:
			// Symlinks and devices out of a sandbox have no safe meaning
			// on the caller's disk. Skip them rather than guess.
			continue
		}
	}
}

// fetchFileMaxBytes bounds one extracted file. A sandbox-built archive is
// untrusted, and an unbounded copy is a decompression bomb (CWE-409).
const fetchFileMaxBytes int64 = 2 << 30

func writeFetchedFile(rootDir *os.Root, r io.Reader, name string, mode os.FileMode) error {
	// rootDir resolves name against an open directory descriptor, so a
	// symlink anywhere in the path cannot reach outside the extraction
	// root. One that stays inside it is harmless: the write still lands in
	// the tree the caller asked for.
	f, err := rootDir.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm()&0o755)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // the copy error below is the one that matters
	written, err := io.Copy(f, io.LimitReader(r, fetchFileMaxBytes))
	if err != nil {
		return err
	}
	if written == fetchFileMaxBytes {
		return fmt.Errorf("%s is over the %s per-file fetch limit", name, humanBytes(fetchFileMaxBytes))
	}
	return nil
}
