package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

// matrixDefaultConcurrency matches the sandboxes one account may run at
// once. Raising it past the account limit does not go faster; it just
// fails later.
const matrixDefaultConcurrency = 10

func newMatrixCommand() *cli.Command {
	return &cli.Command{
		Name:      "matrix",
		Usage:     "Run many commands in parallel, each on its own clone of one prepared sandbox",
		ArgsUsage: "<local-dir>",
		Description: `Matrix runs a set of commands at the same time, each on its own sandbox,
and gives you one exit code per command.

The sandboxes are clones, not fresh machines. Matrix builds one golden
sandbox, runs --prepare on it once, pauses it, and forks that snapshot per
job. A dependency install that takes two minutes is paid once, not once per
job. Fork is a snapshot copy, so a clone costs about a second.

Every sandbox is destroyed when its job finishes. The command exits 0 only
if every job exited 0.

Flags must come before the directory. Anything after it is not read as a
flag.

Examples:
  # Three test suites, three sandboxes, one dependency install
  createos sandbox matrix --prepare 'bun install' \
    --job 'bun test unit' --job 'bun test e2e' --job 'bun test perf' .

  # A large matrix from a file, ten at a time
  createos sandbox matrix --prepare 'npm ci' --jobs-file cases.txt --concurrency 10 .

  # Reuse a sandbox you already prepared and paused
  createos sandbox matrix --from my-golden-box --job 'pytest -k slow'

Known limits:
  A fork comes up without the S3 disks its source had mounted (issue #63),
  so matrix refuses --disk rather than run jobs against missing data.
  A clone whose snapshot is not cached on the target host takes 11-13
  seconds to resume, not under a second.
  A clone does not inherit --shape from the golden sandbox, so clones can
  be larger than you asked for and cost more. Matrix does re-apply
  --auto-pause to every clone, because a clone does not inherit that
  either.`,
		Flags: append(composeFlags(),
			&cli.StringFlag{
				Name:  "prepare",
				Usage: "Command to run once on the golden sandbox before it is cloned",
			},
			&cli.StringSliceFlag{
				Name:  "job",
				Usage: "Command to run on its own clone (repeatable)",
			},
			&cli.StringFlag{
				Name:  "jobs-file",
				Usage: "File with one job command per line. Blank lines and lines starting with # are skipped",
			},
			&cli.IntFlag{
				Name:  "concurrency",
				Value: matrixDefaultConcurrency,
				Usage: "How many clones run at the same time",
			},
			&cli.StringFlag{
				Name:  "from",
				Usage: "Clone this existing sandbox instead of building a golden one from <local-dir>",
			},
			&cli.StringFlag{
				Name:  "logs",
				Usage: "Directory for per-job log files. Default: a temporary directory",
			},
			&cli.BoolFlag{
				Name:  "keep-golden",
				Usage: "Keep the golden sandbox after the run instead of destroying it",
			},
		),
		Action: runMatrix,
	}
}

// matrixJobResult is one job's outcome, and one row of the JSON output.
type matrixJobResult struct {
	Index      int    `json:"index"`
	Cmd        string `json:"cmd"`
	Sandbox    string `json:"sandbox,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Log        string `json:"log,omitempty"`
	Error      string `json:"error,omitempty"`
}

func runMatrix(c *cli.Context) error {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}

	// Everything after the directory should have been a flag, and the
	// parser stopped reading them there. Say so before the empty-job-list
	// error hides the real cause.
	if c.Args().Len() > 1 {
		if err := checkMisplacedFlags(c, c.Args().Slice()[1:]); err != nil {
			return err
		}
	}
	jobs, err := matrixJobs(c)
	if err != nil {
		return err
	}
	concurrency := c.Int("concurrency")
	if concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1 (got %d)", concurrency)
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

	logDir, err := matrixLogDir(c.String("logs"))
	if err != nil {
		return err
	}

	golden, cleanupGolden, err := matrixGoldenBox(ctx, c, client, opts, say)
	if err != nil {
		return err
	}
	defer cleanupGolden()

	// Pause the golden box once, here, rather than letting every job race
	// to do it. Fork needs a paused source, and N concurrent pause calls
	// on the same sandbox is a fight nobody needs to have.
	//
	// pauseForFork, not ensureForkable: matrix may pause this sandbox
	// because it built it. With --from the sandbox belongs to the user, so
	// say what is about to happen to it rather than doing it silently.
	if ref := strings.TrimSpace(c.String("from")); ref != "" {
		say("Pausing %s to take the snapshot the clones come from", golden)
	}
	if err := pauseForFork(ctx, client, golden); err != nil {
		return err
	}
	say("Cloning %s into %d sandbox(es), %d at a time", golden, len(jobs), concurrency)
	results := matrixRunJobs(ctx, client, golden, jobs, concurrency, logDir, opts, quiet)

	failed := 0
	for _, r := range results {
		if r.ExitCode != 0 || r.Error != "" {
			failed++
		}
	}

	if quiet {
		output.Render(c, map[string]any{
			"golden": golden,
			"jobs":   results,
			"failed": failed,
		}, func() {})
	} else {
		matrixPrintSummary(results, logDir)
	}

	if failed > 0 {
		return cli.Exit("", 1)
	}
	return nil
}

// matrixJobs collects the job commands from --job and --jobs-file.
func matrixJobs(c *cli.Context) ([]string, error) {
	jobs := append([]string{}, c.StringSlice("job")...)
	if path := c.String("jobs-file"); path != "" {
		f, err := os.Open(path) // #nosec G304 -- the user named this file on their own command line
		if err != nil {
			return nil, fmt.Errorf("read --jobs-file: %w", err)
		}
		defer func() { _ = f.Close() }() //nolint:errcheck // read-only handle
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			jobs = append(jobs, line)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read --jobs-file: %w", err)
		}
	}
	if len(jobs) == 0 {
		return nil, errors.New(
			"no jobs to run\n\n  Give at least one --job, or a --jobs-file:\n    createos sandbox matrix . --job 'bun test unit' --job 'bun test e2e'")
	}
	return jobs, nil
}

func matrixLogDir(want string) (string, error) {
	if want == "" {
		return os.MkdirTemp("", "createos-matrix-*")
	}
	if err := os.MkdirAll(want, 0o750); err != nil {
		return "", fmt.Errorf("create --logs directory: %w", err)
	}
	return filepath.Abs(want)
}

// matrixGoldenBox returns the sandbox id to clone from, plus the cleanup
// that runs when the matrix finishes.
//
// Two ways in. --from names a sandbox the user already prepared, and
// matrix never destroys it — it did not create it. Otherwise matrix builds
// one from <local-dir>, runs --prepare, and owns its teardown.
func matrixGoldenBox(
	ctx context.Context,
	c *cli.Context,
	client *api.SandboxClient,
	opts *composeOptions,
	say func(string, ...any),
) (string, func(), error) {
	noop := func() {}

	if ref := strings.TrimSpace(c.String("from")); ref != "" {
		if c.Args().Len() > 0 {
			return "", noop, errors.New("--from and <local-dir> do the same job — give one or the other")
		}
		id, err := resolveSandboxRef(ctx, client, ref)
		if err != nil {
			return "", noop, err
		}
		if err := matrixRefuseDisks(ctx, client, id); err != nil {
			return "", noop, err
		}
		return id, noop, nil
	}

	dir := strings.TrimSpace(c.Args().First())
	if dir == "" {
		return "", noop, errors.New(
			"please give a directory to clone, or --from an existing sandbox\n\n  Example:\n    createos sandbox matrix . --prepare 'bun install' --job 'bun test'")
	}

	tree, err := stageDir(ctx, dir, stageOptions{Exclude: opts.Exclude})
	if err != nil {
		return "", noop, err
	}
	defer func() { _ = os.Remove(tree.Path) }() //nolint:errcheck // temp file
	say("Packed %d file(s), %s", tree.Files, humanBytes(tree.Size))

	if len(opts.Egress) == 0 {
		say("Egress unrestricted — every clone can reach any host. Restrict it with --egress or --egress-preset.")
	}

	sb, err := createComposeBox(ctx, client, opts)
	if err != nil {
		return "", noop, err
	}
	cleanup := func() {
		if c.Bool("keep-golden") {
			pterm.Info.Printfln("Golden sandbox %s kept. Destroy it with: createos sandbox rm --force %s", sb.ID, sb.ID)
			return
		}
		destroyQuiet(ctx, client, sb.ID, func(msg string) { pterm.Warning.Println(msg) })
	}

	if err := shipTree(ctx, client, sb.ID, tree, composeWorkDir); err != nil {
		cleanup()
		return "", noop, err
	}
	say("Golden sandbox %s is up", sb.ID)

	if prepare := strings.TrimSpace(c.String("prepare")); prepare != "" {
		say("Preparing once: %s", prepare)
		var buf bytes.Buffer
		res, err := runManaged(ctx, client, sb.ID, prepare, composeWorkDir, opts.Env, &buf)
		if err != nil {
			cleanup()
			return "", noop, fmt.Errorf("prepare: %w", err)
		}
		if res.ExitCode != 0 {
			cleanup()
			return "", noop, fmt.Errorf("prepare exited %d — no clones were made\n\n%s",
				res.ExitCode, strings.TrimSpace(buf.String()))
		}
	}
	return sb.ID, cleanup, nil
}

// matrixRefuseDisks stops a run that would silently lose data. A fork does
// not carry its source's S3 disk attachments (issue #63), so jobs would
// read an empty mount path and "pass" against nothing.
func matrixRefuseDisks(ctx context.Context, client *api.SandboxClient, id string) error {
	disks, err := client.ListSandboxDisks(ctx, id)
	if err != nil {
		// Not being able to check is not a reason to refuse the run.
		return nil //nolint:nilerr
	}
	if len(disks) == 0 {
		return nil
	}
	return fmt.Errorf(
		"sandbox %s has %d S3 disk(s) mounted, and a fork does not carry them (issue #63)\n\n  The clones would run against empty mount paths.\n  Copy what the jobs need into the sandbox's own filesystem first, then re-run",
		id, len(disks))
}

// matrixRunJobs clones the golden sandbox once per job and runs them, at
// most `concurrency` at a time.
func matrixRunJobs(
	ctx context.Context,
	client *api.SandboxClient,
	golden string,
	jobs []string,
	concurrency int,
	logDir string,
	opts *composeOptions,
	quiet bool,
) []matrixJobResult {
	results := make([]matrixJobResult, len(jobs))
	slots := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i] = matrixRunOne(ctx, client, golden, i, job, logDir, opts)
			if !quiet {
				matrixPrintOne(results[i])
			}
		}(i, job)
	}
	wg.Wait()
	return results
}

// matrixRunOne clones, runs one job, and destroys the clone.
func matrixRunOne(
	ctx context.Context,
	client *api.SandboxClient,
	golden string,
	index int,
	job, logDir string,
	opts *composeOptions,
) (res matrixJobResult) {
	// Named return, deliberately. The teardown below runs in a defer, and
	// with an unnamed return Go copies the result value before defers run,
	// so a failed destroy would never reach the caller and the matrix
	// would exit 0 having leaked a clone.
	res = matrixJobResult{Index: index, Cmd: job, ExitCode: -1}
	logPath := filepath.Join(logDir, fmt.Sprintf("job-%d.log", index))
	res.Log = logPath

	logFile, err := os.Create(logPath) // #nosec G304 -- logDir is ours or the user's own --logs
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer func() { _ = logFile.Close() }() //nolint:errcheck // the job result carries the real error

	// Every clone comes from the same paused snapshot, so they are made
	// one at a time and resumed on the spot.
	forks, err := forkN(ctx, client, golden, api.SandboxForkReq{Egress: opts.Egress}, 1, nil)
	if err != nil {
		res.Error = err.Error()
		// A fork that was created but never settled is running and
		// billable, and this job is the only thing that knows its id.
		var leak *forkLeak
		if errors.As(err, &leak) {
			for _, id := range leak.IDs {
				destroyQuiet(ctx, client, id, func(msg string) {
					res.Error += "\n  " + msg
				})
			}
		}
		return res
	}
	clone := forks[0]
	res.Sandbox = clone.ID
	defer destroyQuiet(ctx, client, clone.ID, func(msg string) {
		// A teardown failure must reach the result. The job may have
		// passed, but a clone nobody destroyed keeps billing, and
		// swallowing this is how that goes unnoticed.
		if res.Error == "" {
			res.Error = msg
		}
	})

	// A fork does not inherit its source's auto-pause — measured: a golden
	// box with auto_pause=900 produced clones with auto_pause=None (and a
	// different shape). Without this, a matrix that dies mid-run leaves
	// every clone running with nothing left to stop it.
	if opts.AutoPause > 0 {
		secs := int(opts.AutoPause.Seconds())
		if _, pauseErr := client.SetAutoPause(ctx, clone.ID, &secs); pauseErr != nil {
			res.Error = fmt.Sprintf("could not set the auto-pause backstop on %s: %v", clone.ID, pauseErr)
			return res
		}
	}

	out, err := runManaged(ctx, client, clone.ID, job, composeWorkDir, opts.Env, logFile)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.ExitCode = out.ExitCode
	res.DurationMs = out.Duration.Milliseconds()
	return res
}

func matrixPrintOne(r matrixJobResult) {
	switch {
	case r.Error != "":
		pterm.Error.Printfln("job %d failed to run: %s", r.Index, r.Error)
	case r.ExitCode == 0:
		pterm.Success.Printfln("job %d ok in %s — %s", r.Index,
			time.Duration(r.DurationMs)*time.Millisecond, r.Cmd)
	default:
		pterm.Error.Printfln("job %d exited %d — %s", r.Index, r.ExitCode, r.Cmd)
	}
}

func matrixPrintSummary(results []matrixJobResult, logDir string) {
	rows := make([][]string, 0, 1+len(results))
	rows = append(rows, []string{"JOB", "EXIT", "TIME", "COMMAND"})
	for _, r := range results {
		exit := fmt.Sprintf("%d", r.ExitCode)
		if r.Error != "" {
			exit = "error"
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", r.Index),
			exit,
			(time.Duration(r.DurationMs) * time.Millisecond).Round(time.Millisecond).String(),
			r.Cmd,
		})
	}
	_ = pterm.DefaultTable.WithHasHeader().WithData(rows).Render() //nolint:errcheck // a failed table render must not change the exit code
	fmt.Printf("\nLogs: %s\n", logDir)
}
