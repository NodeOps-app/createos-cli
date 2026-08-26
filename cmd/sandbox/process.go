package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
	"github.com/NodeOps-app/createos-cli/internal/ui"
)

func newProcessCommand() *cli.Command {
	return &cli.Command{
		Name:    "process",
		Aliases: []string{"proc"},
		Usage:   "Manage long-running and reconnectable sandbox processes",
		Description: `Manage commands that keep a process ID.

Use this when a command should keep running independently of this CLI,
or when you want to list it, reconnect to output, send input, wait for it,
or stop it later.

Use 'sandbox exec' for quick non-interactive one-shot commands.
Use 'sandbox shell' for an immediate interactive terminal that does not
need to be listed or reattached.
Use --pty (alias --tty, -t) when the managed command needs a terminal
instead of plain stdout/stderr pipes.`,
		Subcommands: []*cli.Command{
			newProcessRunCommand(),
			newProcessStartCommand(),
			newProcessShellCommand(),
			newProcessAttachCommand(),
			newProcessListCommand(),
			newProcessGetCommand(),
			newProcessInputCommand(),
			newProcessCloseStdinCommand(),
			newProcessResizeCommand(),
			newProcessSignalCommand(),
			newProcessWaitCommand(),
			newProcessStopCommand(),
		},
	}
}

func newProcessRunCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Run a managed command, stream output, and return its exit code",
		ArgsUsage: "<sandbox> -- <cmd> [args...]",
		Description: `Run a command as a managed process.

Compared with 'sandbox exec', this keeps replayable output and a process ID
while the command runs. Use this when you may need durable output, input,
signals, wait/stop controls, or later inspection.

By default this uses stdout/stderr pipes. Add --pty/--tty/-t for commands
that need terminal behavior, such as REPLs, curses apps, colored TTY output,
or programs that behave differently when not attached to a terminal.`,
		Flags: processCreateFlags(true),
		Action: func(c *cli.Context) error {
			return runProcessCreate(c, true, false)
		},
	}
}

func newProcessStartCommand() *cli.Command {
	return &cli.Command{
		Name:      "start",
		Usage:     "Start a managed command and print its process ID",
		ArgsUsage: "<sandbox> -- <cmd> [args...]",
		Description: `Start a managed command without attaching by default.

Use this for background work you want to inspect or control later with
'process list', 'process attach', 'process wait', 'process signal', or
'process stop'. Add --follow to attach immediately after starting.

By default this uses stdout/stderr pipes. Add --pty/--tty/-t when the
program needs a terminal.`,
		Flags: processStartFlags(),
		Action: func(c *cli.Context) error {
			return runProcessCreate(c, false, false)
		},
	}
}

func newProcessShellCommand() *cli.Command {
	return &cli.Command{
		Name:      "shell",
		Usage:     "Start a persistent shell session and attach to it",
		ArgsUsage: "<sandbox>",
		Description: `Start a managed PTY shell session.

This does not replace 'sandbox shell'. The existing command is unchanged.
Use this when the shell should survive detach and be reattached later.
It creates a process ID, appears in 'process list', and supports switching
between running shell sessions from attach.

Use 'sandbox shell' when you only need a direct interactive shell right now.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "cwd", Usage: "Working directory inside the sandbox"},
			&cli.StringSliceFlag{Name: "env", Usage: "Environment variable override (repeatable): KEY=VALUE"},
			&cli.IntFlag{Name: "rows", Usage: "Initial PTY rows"},
			&cli.IntFlag{Name: "cols", Usage: "Initial PTY columns"},
			&cli.StringFlag{Name: "cmd", Usage: "Explicit shell executable (backend default if omitted)"},
			&cli.BoolFlag{Name: "no-attach", Usage: "Create the shell session and print the process ID without attaching"},
		},
		Action: runProcessShell,
	}
}

func newProcessAttachCommand() *cli.Command {
	return &cli.Command{
		Name:      "attach",
		Aliases:   []string{"connect"},
		Usage:     "Reconnect to a process, or pick a running managed process",
		ArgsUsage: "<sandbox> [<process-id>]",
		Description: `Reconnect to a managed process or shell session.

Use this for process IDs created by 'process run', 'process start', or
'process shell'. For PTY shell sessions, attach is interactive. For pipe
processes, attach follows retained stdout/stderr output.

If you omit the process ID in an interactive terminal, attach shows a picker
of running managed processes. Pick a PTY shell for interactive terminal
attach, or pick a pipe process to follow stdout/stderr output.

Inside a PTY shell session, Ctrl-] detaches, Ctrl-N creates a new shell,
and Ctrl-P opens the process picker.`,
		Flags: []cli.Flag{
			&cli.Int64Flag{Name: "after", Usage: "Replay output after this sequence number"},
			&cli.BoolFlag{Name: "no-follow", Usage: "Replay retained output and exit"},
			&cli.BoolFlag{Name: "stdin", Usage: "Send local stdin to the process (default for PTYs)"},
			&cli.BoolFlag{Name: "raw", Usage: "Print raw output bytes"},
		},
		Action: runProcessAttach,
	}
}

func newProcessListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Aliases:   []string{"ls", "ps"},
		Usage:     "List managed processes and shell sessions",
		ArgsUsage: "<sandbox>",
		Action:    runProcessList,
	}
}

func newProcessGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show details for one managed process",
		ArgsUsage: "<sandbox> <process-id>",
		Action:    runProcessGet,
	}
}

func newProcessInputCommand() *cli.Command {
	return &cli.Command{
		Name:      "input",
		Usage:     "Write input to a managed process or shell session",
		ArgsUsage: "<sandbox> <process-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "text", Usage: "Text to send"},
			&cli.StringFlag{Name: "file", Usage: "File to send, or '-' for stdin"},
			&cli.StringFlag{Name: "base64", Usage: "Already-base64-encoded bytes to send"},
		},
		Action: runProcessInput,
	}
}

func newProcessCloseStdinCommand() *cli.Command {
	return &cli.Command{
		Name:      "close-stdin",
		Usage:     "Close stdin for a pipe process",
		ArgsUsage: "<sandbox> <process-id>",
		Action:    runProcessCloseStdin,
	}
}

func newProcessResizeCommand() *cli.Command {
	return &cli.Command{
		Name:      "resize",
		Usage:     "Resize a managed shell session",
		ArgsUsage: "<sandbox> <process-id>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "rows", Usage: "PTY rows"},
			&cli.IntFlag{Name: "cols", Usage: "PTY columns"},
		},
		Action: runProcessResize,
	}
}

func newProcessSignalCommand() *cli.Command {
	return &cli.Command{
		Name:      "signal",
		Usage:     "Send a signal to a managed process",
		ArgsUsage: "<sandbox> <process-id> <signal>",
		Action:    runProcessSignal,
	}
}

func newProcessWaitCommand() *cli.Command {
	return &cli.Command{
		Name:      "wait",
		Usage:     "Wait for a managed process to exit",
		ArgsUsage: "<sandbox> <process-id>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "all", Usage: "Wait for the command and anything it started"},
			&cli.DurationFlag{Name: "timeout", Usage: "Stop waiting after this long"},
		},
		Action: runProcessWait,
	}
}

func newProcessStopCommand() *cli.Command {
	return &cli.Command{
		Name:      "stop",
		Aliases:   []string{"kill", "rm"},
		Usage:     "Stop a managed process and everything it started",
		ArgsUsage: "<sandbox> <process-id>",
		Flags: []cli.Flag{
			&cli.DurationFlag{Name: "grace", Value: time.Second, Usage: "Time to wait after SIGTERM before force-killing"},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f", "yes", "y"}, Usage: "Skip confirmation prompt"},
		},
		Action: runProcessStop,
	}
}

func processCreateFlags(includeRunFlags bool) []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "cwd", Usage: "Working directory inside the sandbox"},
		&cli.StringSliceFlag{Name: "env", Usage: "Environment variable override (repeatable): KEY=VALUE"},
		&cli.BoolFlag{Name: "pty", Aliases: []string{"tty", "t"}, Usage: "Create a managed PTY instead of separate stdout/stderr pipes"},
		&cli.IntFlag{Name: "rows", Usage: "Initial PTY rows"},
		&cli.IntFlag{Name: "cols", Usage: "Initial PTY columns"},
		&cli.Int64Flag{Name: "after", Usage: "When following output, replay after this sequence number"},
	}
	if includeRunFlags {
		flags = append(flags,
			&cli.BoolFlag{Name: "no-follow", Usage: "Do not attach to output after creating"},
			&cli.BoolFlag{Name: "all", Usage: "Wait for the command and anything it started"},
			&cli.DurationFlag{Name: "timeout", Usage: "Stop waiting after this long"},
		)
	}
	return flags
}

func processStartFlags() []cli.Flag {
	flags := processCreateFlags(false)
	flags = append(flags, &cli.BoolFlag{Name: "follow", Usage: "Attach to output after starting"})
	return flags
}

func runProcessCreate(c *cli.Context, waitForExit bool, shellMode bool) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	subcommand := c.Command.Name
	client, id, ref, err := processClientAndSandbox(c, true)
	if err != nil {
		return err
	}
	req, err := processCreateRequestFromCLI(c, shellMode)
	if err != nil {
		return err
	}
	proc, err := client.CreateProcess(c.Context, id, req)
	if err != nil {
		return err
	}
	follow := waitForExit
	if !waitForExit {
		follow = processBoolFlag(c, subcommand, "follow")
	}
	if processBoolFlag(c, subcommand, "no-follow") {
		follow = false
	}
	if output.IsJSON(c) {
		if waitForExit && follow {
			final, waitErr := waitForManagedProcess(c.Context, client, id, proc.ProcessID, processBoolFlag(c, subcommand, "all"), processDurationFlag(c, subcommand, "timeout"))
			if waitErr != nil {
				return waitErr
			}
			output.Render(c, final, func() {})
			return exitFromProcess(final.ExitCode, final.Signal)
		}
		output.Render(c, proc, func() {})
		return nil
	}
	if !follow {
		printProcessCreated(proc)
		return nil
	}
	if !waitForExit {
		printProcessCreated(proc)
	}
	exitCode, signal, err := attachProcess(c, client, id, ref, proc, processInt64Flag(c, subcommand, "after"), false, false)
	if err != nil {
		return err
	}
	if waitForExit {
		return exitFromProcess(exitCode, signal)
	}
	return nil
}

func runProcessShell(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	subcommand := c.Command.Name
	client, id, ref, err := processClientAndSandbox(c, false)
	if err != nil {
		return err
	}
	envs, err := parseEnvFlags(processStringSliceFlag(c, subcommand, "env"))
	if err != nil {
		return err
	}
	req := api.ProcessCreateRequest{
		Cmd: processStringFlag(c, subcommand, "cmd"),
		Cwd: strings.TrimSpace(processStringFlag(c, subcommand, "cwd")),
		Env: envs,
		PTY: ptyOptionsFromFlags(c, !output.IsJSON(c) && !processBoolFlag(c, subcommand, "no-attach")),
	}
	proc, err := client.CreateProcess(c.Context, id, req)
	if err != nil {
		return err
	}
	if output.IsJSON(c) || processBoolFlag(c, subcommand, "no-attach") {
		output.Render(c, proc, func() { printProcessCreated(proc) })
		return nil
	}
	printProcessCreated(proc)
	exitCode, signal, err := attachProcess(c, client, id, ref, proc, 0, false, false)
	if err != nil {
		return err
	}
	return exitFromProcess(exitCode, signal)
}

func runProcessAttach(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	subcommand := c.Command.Name
	var (
		client    *api.SandboxClient
		id        string
		ref       string
		processID string
		err       error
	)
	if c.Args().Len() == 0 {
		client, id, ref, err = processClientAndSandbox(c, false)
		if err != nil {
			return err
		}
	} else {
		client, id, ref, processID, err = processClientSandboxAndOptionalProcess(c)
		if err != nil {
			return err
		}
	}
	if processID == "" {
		processID, err = pickAttachableProcess(c, client, id, "Attach to which process?")
		if err != nil {
			if errors.Is(err, errNoRunningProcess) && !processBoolFlag(c, subcommand, "no-follow") {
				proc, createErr := promptCreatePTYAndAttach(c, client, id)
				if createErr != nil {
					return createErr
				}
				if proc == nil {
					fmt.Println("Cancelled. Nothing attached.")
					return nil
				}
				printProcessCreated(proc)
				_, _, attachErr := attachProcess(c, client, id, ref, proc, processInitialAttachAfter(c, subcommand, proc, false), false, processBoolFlag(c, subcommand, "stdin"))
				return attachErr
			}
			return err
		}
		if processID == "" {
			fmt.Println("Cancelled. Nothing attached.")
			return nil
		}
	}
	proc, err := client.GetProcess(c.Context, id, processID)
	if err != nil {
		return err
	}
	noFollow := processBoolFlag(c, subcommand, "no-follow")
	exitCode, signal, err := attachProcess(c, client, id, ref, proc, processInitialAttachAfter(c, subcommand, proc, noFollow), noFollow, processBoolFlag(c, subcommand, "stdin"))
	if err != nil {
		return err
	}
	if !noFollow && proc.Kind != "pty" {
		printProcessAttachExit(exitCode, signal)
		return exitFromProcess(exitCode, signal)
	}
	return nil
}

func processArgsRequestHelp(c *cli.Context) bool {
	for _, arg := range c.Args().Slice() {
		if arg == "-h" || arg == "--help" || arg == "help" {
			return true
		}
	}
	return false
}

func runProcessList(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, _, err := processClientAndSandbox(c, false)
	if err != nil {
		return err
	}
	processes, err := client.ListProcesses(c.Context, id)
	if err != nil {
		return err
	}
	output.Render(c, processes, func() {
		if len(processes) == 0 {
			fmt.Println("No managed processes are running or retained in this sandbox.")
			return
		}
		table := pterm.TableData{{"ID", "Kind", "PID", "State", "Exit", "Command", "Output", "Created"}}
		for _, p := range processes {
			table = append(table, []string{
				p.ProcessID,
				p.Kind,
				strconv.Itoa(p.PID),
				processStateLabel(p),
				processExitLabel(p),
				processCommandLabel(p, 40),
				fmt.Sprintf("%d bytes", p.Output.Bytes),
				processLocalDateTime(p.CreatedAt),
			})
		}
		_ = output.RenderTable(table) //nolint:errcheck
	})
	return nil
}

func runProcessGet(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, processID, err := processClientSandboxAndProcess(c)
	if err != nil {
		return err
	}
	proc, err := client.GetProcess(c.Context, id, processID)
	if err != nil {
		return err
	}
	output.Render(c, proc, func() { printProcessDetails(proc) })
	return nil
}

func runProcessInput(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, processID, err := processClientSandboxAndProcess(c)
	if err != nil {
		return err
	}
	encoded, err := processInputBase64(c)
	if err != nil {
		return err
	}
	resp, err := client.WriteProcessInput(c.Context, id, processID, api.ProcessInputRequest{DataBase64: encoded})
	if err != nil {
		return err
	}
	output.Render(c, resp, func() {
		pterm.Success.Printf("Input accepted as write #%d.\n", resp.InputSeq)
	})
	return nil
}

func runProcessCloseStdin(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, processID, err := processClientSandboxAndProcess(c)
	if err != nil {
		return err
	}
	if err := client.CloseProcessStdin(c.Context, id, processID); err != nil {
		return err
	}
	pterm.Success.Println("Stdin closed.")
	return nil
}

func runProcessResize(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, processID, err := processClientSandboxAndProcess(c)
	if err != nil {
		return err
	}
	rows, cols, err := processRowsCols(c)
	if err != nil {
		return err
	}
	if err := client.ResizeProcessPTY(c.Context, id, processID, api.ProcessResizeRequest{Rows: rows, Cols: cols}); err != nil {
		return err
	}
	pterm.Success.Printf("Resized PTY to %dx%d.\n", rows, cols)
	return nil
}

func runProcessSignal(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, processID, err := processClientSandboxAndProcess(c)
	if err != nil {
		return err
	}
	if c.Args().Len() < 3 {
		return fmt.Errorf("please provide a signal\n\n  Example:\n    createos sandbox process signal %s %s SIGINT", c.Args().Get(0), processID)
	}
	sig := strings.ToUpper(strings.TrimSpace(c.Args().Get(2)))
	if sig == "" {
		return fmt.Errorf("please provide a signal, for example SIGINT or SIGTERM")
	}
	if err := client.SignalProcess(c.Context, id, processID, api.ProcessSignalRequest{Signal: sig}); err != nil {
		return err
	}
	pterm.Success.Printf("Sent %s to %s.\n", sig, processID)
	return nil
}

func runProcessWait(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, processID, err := processClientSandboxAndProcess(c)
	if err != nil {
		return err
	}
	proc, err := waitForManagedProcess(c.Context, client, id, processID, processBoolFlag(c, "wait", "all"), processDurationFlag(c, "wait", "timeout"))
	if err != nil {
		return err
	}
	output.Render(c, proc, func() { printProcessDetails(proc) })
	return nil
}

func runProcessStop(c *cli.Context) error {
	if processArgsRequestHelp(c) {
		return cli.ShowSubcommandHelp(c)
	}
	client, id, processID, err := processClientSandboxAndProcess(c)
	if err != nil {
		return err
	}
	force := processBoolFlag(c, "stop", "force") || processBoolFlag(c, "stop", "yes") || processBoolFlag(c, "kill", "force") || processBoolFlag(c, "rm", "force")
	if !force && terminal.IsInteractive() {
		ok, confirmErr := pterm.DefaultInteractiveConfirm.
			WithDefaultText(fmt.Sprintf("Stop process %s and everything it started?", processID)).
			WithDefaultValue(false).
			Show()
		if confirmErr != nil {
			return fmt.Errorf("confirmation cancelled")
		}
		if !ok {
			fmt.Println("Cancelled. Nothing stopped.")
			return nil
		}
	}
	grace := processDurationFlag(c, "stop", "grace")
	if grace == 0 {
		grace = processDurationFlag(c, "kill", "grace")
	}
	if grace == 0 {
		grace = processDurationFlag(c, "rm", "grace")
	}
	proc, err := client.TerminateProcess(c.Context, id, processID, durationMs(grace))
	if err != nil {
		return err
	}
	output.Render(c, proc, func() {
		pterm.Success.Printf("Stopped %s.\n", processID)
		printProcessDetails(proc)
	})
	return nil
}

func processClientAndSandbox(c *cli.Context, allowCommandAfterRef bool) (*api.SandboxClient, string, string, error) {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return nil, "", "", fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	ref := strings.TrimSpace(c.Args().First())
	if allowCommandAfterRef {
		ref, _, _ = parseProcessCommandArgs(c)
	}
	if ref == "" {
		if !terminal.IsInteractive() {
			return nil, "", "", fmt.Errorf("please provide a sandbox ID or name")
		}
		pickedID, label, err := pickByStatus(c, client, "Use which sandbox?", "running")
		if err != nil {
			return nil, "", "", err
		}
		if pickedID == "" {
			return nil, "", "", fmt.Errorf("cancelled")
		}
		return client, pickedID, label, nil
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return nil, "", "", err
	}
	return client, id, ref, nil
}

func processClientSandboxAndProcess(c *cli.Context) (*api.SandboxClient, string, string, error) {
	client, id, ref, processID, err := processClientSandboxAndOptionalProcess(c)
	if err != nil {
		return nil, "", "", err
	}
	if processID == "" {
		return nil, "", "", fmt.Errorf("please provide a process ID\n\n  Example:\n    createos sandbox process attach %s proc_abc123", refLabel(ref, id))
	}
	return client, id, processID, nil
}

func processClientSandboxAndOptionalProcess(c *cli.Context) (*api.SandboxClient, string, string, string, error) {
	client, ok := c.App.Metadata[api.SandboxClientKey].(*api.SandboxClient)
	if !ok {
		return nil, "", "", "", fmt.Errorf("you're not signed in — run 'createos login' to get started")
	}
	if c.Args().Len() < 1 {
		return nil, "", "", "", fmt.Errorf("please provide a sandbox ID or name")
	}
	ref := strings.TrimSpace(c.Args().Get(0))
	if ref == "" {
		return nil, "", "", "", fmt.Errorf("please provide a sandbox ID or name")
	}
	id, err := resolveSandboxRef(c.Context, client, ref)
	if err != nil {
		return nil, "", "", "", err
	}
	processID := strings.TrimSpace(c.Args().Get(1))
	return client, id, ref, processID, nil
}

func processCreateRequestFromCLI(c *cli.Context, shellMode bool) (api.ProcessCreateRequest, error) {
	subcommand := c.Command.Name
	envs, err := parseEnvFlags(processStringSliceFlag(c, subcommand, "env"))
	if err != nil {
		return api.ProcessCreateRequest{}, err
	}
	ref, cmd, args := parseProcessCommandArgs(c)
	_ = ref
	if !shellMode && cmd == "" && !processBoolFlag(c, subcommand, "pty") {
		return api.ProcessCreateRequest{}, fmt.Errorf("please pass the command after '--'\n\n  Example:\n    createos sandbox process run my-box -- npm test")
	}
	req := api.ProcessCreateRequest{
		Cmd:  cmd,
		Args: args,
		Cwd:  strings.TrimSpace(processStringFlag(c, subcommand, "cwd")),
		Env:  envs,
	}
	if processBoolFlag(c, subcommand, "pty") {
		req.PTY = ptyOptionsFromFlags(c, false)
	}
	return req, nil
}

func parseProcessCommandArgs(c *cli.Context) (ref, cmd string, args []string) {
	all := c.Args().Slice()
	if len(all) == 0 {
		return "", "", nil
	}
	leadingDoubleDash := false
	for i, a := range os.Args {
		if (a == "run" || a == "start") && i+1 < len(os.Args) && os.Args[i+1] == "--" {
			leadingDoubleDash = true
			break
		}
	}
	sep := -1
	for i, a := range all {
		if a == "--" {
			sep = i
			break
		}
	}
	switch {
	case leadingDoubleDash:
		cmd = all[0]
		if len(all) > 1 {
			args = all[1:]
		}
	case sep >= 0:
		if sep > 0 {
			ref = strings.TrimSpace(all[0])
		}
		rest := all[sep+1:]
		if len(rest) > 0 {
			cmd = rest[0]
			args = rest[1:]
		}
	default:
		ref = strings.TrimSpace(all[0])
		if len(all) > 1 {
			cmd = all[1]
			args = all[2:]
		}
	}
	return ref, cmd, args
}

func ptyOptionsFromFlags(c *cli.Context, reserveFooter bool) *api.ProcessPTYOptions {
	rows, cols := c.Int("rows"), c.Int("cols")
	if rows <= 0 || cols <= 0 {
		gotRows, gotCols := currentTerminalRowsCols(reserveFooter)
		if rows <= 0 {
			rows = gotRows
		}
		if cols <= 0 {
			cols = gotCols
		}
	}
	return &api.ProcessPTYOptions{Rows: rows, Cols: cols}
}

func currentTerminalRowsCols(reserveFooter bool) (int, int) {
	rows, cols := 24, 80
	fd := stdinFD()
	if term.IsTerminal(fd) {
		if gotCols, gotRows, err := term.GetSize(fd); err == nil {
			if gotRows > 0 {
				rows = gotRows
			}
			if gotCols > 0 {
				cols = gotCols
			}
		}
	}
	if reserveFooter && rows > 1 {
		rows--
	}
	return rows, cols
}

func processRowsCols(c *cli.Context) (int, int, error) {
	rows, cols := c.Int("rows"), c.Int("cols")
	if rows <= 0 {
		rows = processIntFlag(c, "resize", "rows")
	}
	if cols <= 0 {
		cols = processIntFlag(c, "resize", "cols")
	}
	if rows <= 0 || cols <= 0 {
		fd := stdinFD()
		if !term.IsTerminal(fd) {
			return 0, 0, fmt.Errorf("please provide --rows and --cols")
		}
		gotCols, gotRows, err := term.GetSize(fd)
		if err != nil {
			return 0, 0, fmt.Errorf("could not read terminal size: %w", err)
		}
		if rows <= 0 {
			rows = gotRows
		}
		if cols <= 0 {
			cols = gotCols
		}
	}
	if rows <= 0 || cols <= 0 {
		return 0, 0, fmt.Errorf("--rows and --cols must be greater than zero")
	}
	return rows, cols, nil
}

func pickAttachableProcess(c *cli.Context, client *api.SandboxClient, sandboxID, title string) (string, error) {
	if !terminal.IsInteractive() {
		return "", fmt.Errorf("please provide a process ID\n\n  Example:\n    createos sandbox process attach %s <process-id>", sandboxID)
	}
	processes, err := client.ListProcesses(c.Context, sandboxID)
	if err != nil {
		return "", err
	}
	items := make([]ui.PickerItem, 0, len(processes))
	for _, proc := range processes {
		if !processCanBePickedForAttach(proc) {
			continue
		}
		subtitle := fmt.Sprintf("kind: %s, state: %s, pid: %d, command: %s, created: %s, output: %d bytes", processKindLabel(proc), processStateLabel(proc), proc.PID, processCommandLabel(proc, 32), processLocalClock(proc.CreatedAt), proc.Output.Bytes)
		items = append(items, ui.PickerItem{
			Title:    proc.ProcessID,
			Subtitle: subtitle,
			Value:    proc.ProcessID,
		})
	}
	if len(items) == 0 {
		return "", errNoRunningProcess
	}
	return ui.Pick(title, items)
}

func processCanBePickedForAttach(proc api.ProcessDetails) bool {
	return proc.State == "running" && !proc.LeaderExited && !proc.TreeExited
}

var errNoRunningProcess = errors.New("there are no running managed processes in this sandbox")

func promptCreatePTYAndAttach(c *cli.Context, client *api.SandboxClient, sandboxID string) (*api.ProcessDetails, error) {
	if !terminal.IsInteractive() {
		return nil, fmt.Errorf("%w\n\n  Start one:\n    createos sandbox process shell %s", errNoRunningProcess, sandboxID)
	}
	pterm.Warning.Println("There are no running managed processes in this sandbox.")
	ok, err := pterm.DefaultInteractiveConfirm.
		WithDefaultText("Create a new shell session and attach?").
		WithDefaultValue(true).
		Show()
	if err != nil {
		return nil, fmt.Errorf("could not read confirmation: %w", err)
	}
	if !ok {
		return nil, nil
	}
	return client.CreateProcess(c.Context, sandboxID, api.ProcessCreateRequest{PTY: ptyOptionsFromFlags(c, true)})
}

func attachProcess(c *cli.Context, client *api.SandboxClient, sandboxID, ref string, proc *api.ProcessDetails, after int64, noFollow bool, stdinFlag bool) (*int, string, error) {
	offsetRetries := 0
	for {
		exitCode, signal, nextID, err := attachProcessOnce(c, client, sandboxID, ref, proc, after, noFollow, stdinFlag)
		if err != nil {
			return exitCode, signal, err
		}
		if retryAfter, ok := processAttachRetryAfter(nextID); ok {
			offsetRetries++
			if offsetRetries > 1 {
				return nil, "", fmt.Errorf("some previous output is no longer available; try attaching again without --after, or run 'createos sandbox process get %s %s' to inspect this process", refLabel(ref, sandboxID), proc.ProcessID)
			}
			nextProc, getErr := client.GetProcess(c.Context, sandboxID, proc.ProcessID)
			if getErr != nil {
				return nil, "", getErr
			}
			proc = nextProc
			if retryAfter == 0 && proc.Output.OldestSeq > 0 {
				retryAfter = proc.Output.OldestSeq
			}
			after = retryAfter
			stdinFlag = false
			continue
		}
		if nextID == "" || noFollow {
			return exitCode, signal, nil
		}
		if nextID == processAttachPickSentinel {
			prepareTerminalForProcessPicker()
			pickedID, pickErr := pickAttachableProcess(c, client, sandboxID, "Attach to which process?")
			if pickErr != nil {
				return nil, "", pickErr
			}
			if pickedID == "" {
				return nil, "", nil
			}
			nextID = pickedID
		}
		nextProc, getErr := client.GetProcess(c.Context, sandboxID, nextID)
		if getErr != nil {
			return nil, "", getErr
		}
		proc = nextProc
		after = proc.Output.NewestSeq
		stdinFlag = false
		offsetRetries = 0
	}
}

func processInitialAttachAfter(c *cli.Context, subcommand string, proc *api.ProcessDetails, noFollow bool) int64 {
	after := processInt64Flag(c, subcommand, "after")
	if noFollow || processFlagIsSet(c, subcommand, "after") || proc == nil {
		return after
	}
	return proc.Output.NewestSeq
}

const processAttachPickSentinel = "__createos_pick_pty__"
const processAttachRetryPrefix = "__createos_retry_after__:"

func attachProcessOnce(c *cli.Context, client *api.SandboxClient, sandboxID, ref string, proc *api.ProcessDetails, after int64, noFollow bool, stdinFlag bool) (*int, string, string, error) {
	if proc == nil {
		return nil, "", "", fmt.Errorf("process details are missing")
	}
	fd := stdinFD()
	stdinInteractive := term.IsTerminal(fd)
	sendInput := stdinFlag || (proc.Kind == "pty" && stdinInteractive && !noFollow)
	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()
	nextCh := make(chan string, 1)
	if noFollow && proc.Output.NewestSeq <= after {
		return nil, "", "", nil
	}
	if !output.IsJSON(c) && proc.Kind == "pty" && sendInput {
		pterm.Fprintln(os.Stderr, pterm.Gray(fmt.Sprintf("  attached to %s (%s)", proc.ProcessID, refLabel(ref, sandboxID))))
	}
	footer := sendInput && proc.Kind == "pty" && stdinInteractive
	altScreen := footer && processForegroundUsesAltScreen(proc)
	localAltScreen := false
	if sendInput && proc.Kind == "pty" && stdinInteractive {
		oldState, rawErr := term.MakeRaw(fd)
		if rawErr != nil {
			return nil, "", "", fmt.Errorf("could not switch terminal to raw mode: %w", rawErr)
		}
		if altScreen {
			enterLocalAltScreen()
			localAltScreen = true
		}
		if footer && !altScreen {
			applyPTYFooter(proc.ProcessID)
		}
		defer func() {
			if footer {
				clearPTYFooter()
			}
			if altScreen || localAltScreen {
				leaveLocalAltScreen()
			}
			if restoreErr := term.Restore(fd, oldState); restoreErr != nil {
				_ = restoreErr
			}
		}() //nolint:errcheck
		sendCurrentProcessResize(c.Context, client, sandboxID, proc.ProcessID, footer && !altScreen)
		stopResize := watchWindowSize(func() {
			sendCurrentProcessResize(c.Context, client, sandboxID, proc.ProcessID, footer && !altScreen)
			if footer && !altScreen {
				applyPTYFooter(proc.ProcessID)
			}
		})
		defer stopResize()
		if altScreen {
			pulseCurrentProcessResize(ctx, client, sandboxID, proc.ProcessID)
		}
		requestPTYPromptRedraw(ctx, client, sandboxID, proc, after, altScreen)
	}
	if sendInput {
		go copyProcessInput(ctx, cancel, nextCh, client, sandboxID, proc.ProcessID, proc.Kind == "pty")
	}
	var exitCode *int
	var exitSignal string
	nextID := ""
	retryAfter := int64(-1)
	targetNewest := proc.Output.NewestSeq
	err := client.ConnectProcess(ctx, sandboxID, proc.ProcessID, after, func(ev api.ProcessOutputEvent) {
		switch ev.Type {
		case "data":
			data := writeProcessOutput(ev)
			if footer {
				enteredAltScreen := processOutputEntersAltScreen(data)
				leftAltScreen := processOutputLeavesAltScreen(data)
				switch {
				case enteredAltScreen:
					altScreen = true
					localAltScreen = false
					clearPTYFooter()
				case leftAltScreen:
					altScreen = false
					localAltScreen = false
					applyPTYFooter(proc.ProcessID)
				case !altScreen:
					applyPTYFooter(proc.ProcessID)
				}
			}
			if noFollow && targetNewest > 0 && ev.Seq >= targetNewest {
				cancel()
			}
		case "exit":
			exitCode = ev.ExitCode
			exitSignal = ev.Signal
			if noFollow {
				cancel()
			}
		case "error":
			if isProcessOutputOffsetExpired(ev.Error) {
				retryAfter = 0
				if ev.OldestAvailableSeq > 0 {
					retryAfter = ev.OldestAvailableSeq
				}
			} else if ev.Error != "" {
				pterm.Error.Println(ev.Error)
			}
			cancel()
		}
	})
	if errors.Is(err, context.Canceled) {
		select {
		case nextID = <-nextCh:
		default:
		}
		if retryAfter >= 0 {
			nextID = processAttachRetryID(retryAfter)
		}
		return exitCode, exitSignal, nextID, nil
	}
	if isProcessOutputOffsetExpiredError(err) {
		return exitCode, exitSignal, processAttachRetryID(0), nil
	}
	return exitCode, exitSignal, "", err
}

func processAttachRetryID(after int64) string {
	if after < 0 {
		after = 0
	}
	return fmt.Sprintf("%s%d", processAttachRetryPrefix, after)
}

func processAttachRetryAfter(id string) (int64, bool) {
	raw, ok := strings.CutPrefix(id, processAttachRetryPrefix)
	if !ok {
		return 0, false
	}
	after, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || after < 0 {
		return 0, true
	}
	return after, true
}

func isProcessOutputOffsetExpiredError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return isProcessOutputOffsetExpired(apiErr.Message)
	}
	return isProcessOutputOffsetExpired(err.Error())
}

func isProcessOutputOffsetExpired(msg string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(msg), "_", " "))
	return strings.Contains(normalized, "output offset expired")
}

func copyProcessInput(ctx context.Context, detach context.CancelFunc, nextCh chan<- string, client *api.SandboxClient, sandboxID, processID string, pty bool) {
	buf := make([]byte, 32*1024)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if pty {
				if before, action, found := splitOnPTYShortcut(chunk); found {
					if len(before) > 0 {
						encoded := base64.StdEncoding.EncodeToString(before)
						_, _ = client.WriteProcessInput(ctx, sandboxID, processID, api.ProcessInputRequest{DataBase64: encoded}) //nolint:errcheck
					}
					handlePTYShortcut(ctx, client, sandboxID, processID, action, nextCh)
					detach()
					return
				}
			}
			encoded := base64.StdEncoding.EncodeToString(chunk)
			_, _ = client.WriteProcessInput(ctx, sandboxID, processID, api.ProcessInputRequest{DataBase64: encoded}) //nolint:errcheck
		}
		if err != nil {
			if !pty && errors.Is(err, io.EOF) {
				_ = client.CloseProcessStdin(ctx, sandboxID, processID) //nolint:errcheck
			}
			return
		}
	}
}

func splitOnPTYShortcut(data []byte) (before []byte, action string, found bool) {
	for i, b := range data {
		switch b {
		case 0x1d: // Ctrl-]
			return data[:i], "detach", true
		case 0x0e: // Ctrl-N
			return data[:i], "new", true
		case 0x10: // Ctrl-P
			return data[:i], "pick", true
		}
	}
	return data, "", false
}

func handlePTYShortcut(ctx context.Context, client *api.SandboxClient, sandboxID, processID, action string, nextCh chan<- string) {
	clearPTYFooter()
	switch action {
	case "detach":
		fmt.Fprint(os.Stderr, "\r\nDetached. The shell is still running. Reattach with:\r\n") //nolint:errcheck
		fmt.Fprintf(os.Stderr, "  createos sandbox process attach %s %s\r\n", sandboxID, processID)
	case "new":
		nextID, err := createSiblingPTY(ctx, client, sandboxID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\r\nCould not create a new shell session: %v\r\n", err) //nolint:errcheck
			return
		}
		fmt.Fprintf(os.Stderr, "\r\nCreated %s. Switching...\r\n", nextID) //nolint:errcheck
		sendNextProcessID(nextCh, nextID)
	case "pick":
		fmt.Fprint(os.Stderr, "\r\nSwitching shell session...\r\n") //nolint:errcheck
		sendNextProcessID(nextCh, processAttachPickSentinel)
	}
}

func createSiblingPTY(ctx context.Context, client *api.SandboxClient, sandboxID string) (string, error) {
	rows, cols := currentTerminalRowsCols(true)
	proc, err := client.CreateProcess(ctx, sandboxID, api.ProcessCreateRequest{PTY: &api.ProcessPTYOptions{Rows: rows, Cols: cols}})
	if err != nil {
		return "", err
	}
	return proc.ProcessID, nil
}

func sendNextProcessID(nextCh chan<- string, processID string) {
	select {
	case nextCh <- processID:
	default:
	}
}

func sendCurrentProcessResize(ctx context.Context, client *api.SandboxClient, sandboxID, processID string, reserveFooter bool) {
	fd := stdinFD()
	if !term.IsTerminal(fd) {
		return
	}
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return
	}
	if reserveFooter && rows > 1 {
		rows--
	}
	_ = client.ResizeProcessPTY(ctx, sandboxID, processID, api.ProcessResizeRequest{Rows: rows, Cols: cols}) //nolint:errcheck
}

func pulseCurrentProcessResize(ctx context.Context, client *api.SandboxClient, sandboxID, processID string) {
	fd := stdinFD()
	if !term.IsTerminal(fd) {
		return
	}
	cols, rows, err := term.GetSize(fd)
	if err != nil || rows <= 2 || cols <= 0 {
		return
	}
	_ = client.ResizeProcessPTY(ctx, sandboxID, processID, api.ProcessResizeRequest{Rows: rows - 1, Cols: cols}) //nolint:errcheck
	time.AfterFunc(80*time.Millisecond, func() {
		_ = client.ResizeProcessPTY(ctx, sandboxID, processID, api.ProcessResizeRequest{Rows: rows, Cols: cols}) //nolint:errcheck
	})
}

func applyPTYFooter(processID string) {
	fd := stdinFD()
	if !term.IsTerminal(fd) {
		return
	}
	cols, rows, err := term.GetSize(fd)
	if err != nil || rows <= 1 || cols <= 0 {
		return
	}
	bodyRows := rows - 1
	text := processFooterText(processID)
	if len(text) > cols {
		text = text[:cols]
	}
	if len(text) < cols {
		text += strings.Repeat(" ", cols-len(text))
	}
	fmt.Fprintf(os.Stderr, "\x1b7\x1b[0m\x1b[1;%dr\x1b[%d;1H\x1b[7m%s\x1b[0m\x1b8", bodyRows, rows, text) //nolint:errcheck
}

func processFooterText(processID string) string {
	return fmt.Sprintf(" createos %s | Ctrl-] detach | Ctrl-N new | Ctrl-P switch | exit close ", processFooterProcessID(processID))
}

func processFooterProcessID(processID string) string {
	const maxLen = 18
	if len(processID) <= maxLen {
		return processID
	}
	return processID[:10] + "…" + processID[len(processID)-5:]
}

func clearPTYFooter() {
	fd := stdinFD()
	if !term.IsTerminal(fd) {
		return
	}
	_, rows, err := term.GetSize(fd)
	if err != nil || rows <= 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\x1b7\x1b[r\x1b[%d;1H\x1b[2K\x1b8", rows) //nolint:errcheck
}

func enterLocalAltScreen() {
	fd := stdinFD()
	if !term.IsTerminal(fd) {
		return
	}
	fmt.Fprint(os.Stderr, "\x1b[?1049h\x1b[0m\x1b[2J\x1b[H") //nolint:errcheck
}

func leaveLocalAltScreen() {
	fd := stdinFD()
	if !term.IsTerminal(fd) {
		return
	}
	fmt.Fprint(os.Stderr, "\x1b[0m\x1b[?25h\x1b[?1049l") //nolint:errcheck
}

func prepareTerminalForProcessPicker() {
	fd := stdinFD()
	if !term.IsTerminal(fd) {
		return
	}
	fmt.Fprint(os.Stderr, "\x1b[0m\x1b[?25h\x1b[r\x1b[?1049l\x1b[2J\x1b[H") //nolint:errcheck
}

func processOutputEntersAltScreen(data []byte) bool {
	return bytes.Contains(data, []byte("\x1b[?1049h")) ||
		bytes.Contains(data, []byte("\x1b[?1047h")) ||
		bytes.Contains(data, []byte("\x1b[?47h"))
}

func processOutputLeavesAltScreen(data []byte) bool {
	return bytes.Contains(data, []byte("\x1b[?1049l")) ||
		bytes.Contains(data, []byte("\x1b[?1047l")) ||
		bytes.Contains(data, []byte("\x1b[?47l"))
}

func processForegroundUsesAltScreen(proc *api.ProcessDetails) bool {
	switch processCommandName(processForegroundCommand(proc)) {
	case "btop", "top", "htop", "vim", "vi", "nvim", "nano", "less", "more", "man", "ssh", "watch":
		return true
	default:
		return false
	}
}

func requestPTYPromptRedraw(ctx context.Context, client *api.SandboxClient, sandboxID string, proc *api.ProcessDetails, after int64, altScreen bool) {
	if proc == nil || proc.Kind != "pty" || altScreen || after < proc.Output.NewestSeq || !processForegroundIsShell(proc) {
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte{0x0c})                                                    // Ctrl-L: redraw readline shells without submitting a command.
	_, _ = client.WriteProcessInput(ctx, sandboxID, proc.ProcessID, api.ProcessInputRequest{DataBase64: encoded}) //nolint:errcheck
}

func processForegroundIsShell(proc *api.ProcessDetails) bool {
	if proc == nil || proc.Kind != "pty" {
		return false
	}
	cmd := processForegroundCommand(proc)
	if cmd == "" && strings.TrimSpace(proc.Cmd) == "" {
		return true
	}
	switch processCommandName(cmd) {
	case "bash", "sh", "zsh", "fish", "dash", "ash", "ksh":
		return true
	default:
		return false
	}
}

func processForegroundCommand(proc *api.ProcessDetails) string {
	if proc == nil {
		return ""
	}
	if proc.Foreground != nil && strings.TrimSpace(proc.Foreground.Cmd) != "" {
		return strings.TrimSpace(proc.Foreground.Cmd)
	}
	return strings.TrimSpace(proc.Cmd)
}

func processCommandName(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	name := cmd
	if fields := strings.Fields(cmd); len(fields) > 0 {
		name = fields[0]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func writeProcessOutput(ev api.ProcessOutputEvent) []byte {
	if ev.DataBase64 == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(ev.DataBase64)
	if err != nil {
		return nil
	}
	if ev.Stream == "stderr" {
		_, _ = os.Stderr.Write(data) //nolint:errcheck
		return data
	}
	_, _ = os.Stdout.Write(data) //nolint:errcheck
	return data
}

func processInputBase64(c *cli.Context) (string, error) {
	text := processStringFlag(c, "input", "text")
	file := processStringFlag(c, "input", "file")
	encodedFlag := processStringFlag(c, "input", "base64")
	set := 0
	if text != "" {
		set++
	}
	if file != "" {
		set++
	}
	if encodedFlag != "" {
		set++
	}
	if set != 1 {
		return "", fmt.Errorf("provide exactly one of --text, --file, or --base64")
	}
	if encodedFlag != "" {
		return strings.TrimSpace(encodedFlag), nil
	}
	if text != "" {
		return base64.StdEncoding.EncodeToString([]byte(text)), nil
	}
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(file) // #nosec G304 -- user supplied file path to send as process input
	}
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func waitForManagedProcess(ctx context.Context, client *api.SandboxClient, sandboxID, processID string, all bool, timeout time.Duration) (*api.ProcessDetails, error) {
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		pollTimeout := int64(30000)
		if timeout > 0 {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return nil, fmt.Errorf("wait timed out")
			}
			if remaining < 30*time.Second {
				pollTimeout = durationMs(remaining)
			}
		}
		proc, err := client.WaitProcess(ctx, sandboxID, processID, all, pollTimeout)
		if err == nil {
			return proc, nil
		}
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 408 && timeout == 0 {
			continue
		}
		return nil, err
	}
}

func printProcessCreated(proc *api.ProcessDetails) {
	pterm.Success.Printf("Started %s (%s).\n", proc.ProcessID, proc.Kind)
}

func printProcessAttachExit(exitCode *int, signal string) {
	if exitCode != nil {
		pterm.Fprintln(os.Stderr, pterm.Gray(fmt.Sprintf("Process exited with code %d.", *exitCode)))
		return
	}
	if signal != "" {
		pterm.Fprintln(os.Stderr, pterm.Gray(fmt.Sprintf("Process exited from signal %s.", signal)))
	}
}

func printProcessDetails(proc *api.ProcessDetails) {
	if proc == nil {
		return
	}
	cyan := pterm.NewStyle(pterm.FgCyan)
	fmt.Println(proc.ProcessID)
	cyan.Printf("Kind: ")
	fmt.Println(proc.Kind)
	cyan.Printf("PID: ")
	fmt.Println(proc.PID)
	cyan.Printf("State: ")
	fmt.Println(processStateLabel(*proc))
	cyan.Printf("Exit: ")
	fmt.Println(processExitLabel(*proc))
	cyan.Printf("Command: ")
	fmt.Println(processCommandLabel(*proc, 0))
	if proc.Foreground != nil && strings.TrimSpace(proc.Foreground.Cmd) != "" {
		cyan.Printf("Foreground: ")
		fmt.Printf("%s (pid %d)\n", strings.TrimSpace(proc.Foreground.Cmd), proc.Foreground.PID)
	}
	if proc.Cwd != "" {
		cyan.Printf("CWD: ")
		fmt.Println(proc.Cwd)
	}
	cyan.Printf("Output: ")
	fmt.Printf("%d bytes, seq %d..%d\n", proc.Output.Bytes, proc.Output.OldestSeq, proc.Output.NewestSeq)
	cyan.Printf("Created: ")
	fmt.Println(processLocalRFC3339(proc.CreatedAt))
	if proc.FinishedAt != nil {
		cyan.Printf("Finished: ")
		fmt.Println(processLocalRFC3339(*proc.FinishedAt))
	}
}

func processLocalDateTime(t time.Time) string {
	return t.In(time.Local).Format("2006-01-02 15:04:05")
}

func processLocalClock(t time.Time) string {
	return t.In(time.Local).Format("15:04:05")
}

func processLocalRFC3339(t time.Time) string {
	return t.In(time.Local).Format(time.RFC3339)
}

func processKindLabel(proc api.ProcessDetails) string {
	if proc.Kind == "pty" {
		return "shell"
	}
	return "process"
}

func processCommandLabel(proc api.ProcessDetails, maxLen int) string {
	if proc.Foreground != nil && strings.TrimSpace(proc.Foreground.Cmd) != "" {
		return truncateProcessLabel(strings.TrimSpace(proc.Foreground.Cmd), maxLen)
	}
	cmd := strings.TrimSpace(proc.Cmd)
	if cmd == "" && proc.Kind == "pty" {
		cmd = "shell"
	}
	if cmd == "" {
		cmd = "-"
	}
	if len(proc.Args) > 0 {
		cmd += " " + strings.Join(proc.Args, " ")
	}
	return truncateProcessLabel(cmd, maxLen)
}

func truncateProcessLabel(cmd string, maxLen int) string {
	if maxLen > 0 && len(cmd) > maxLen {
		if maxLen <= 1 {
			return "…"
		}
		return cmd[:maxLen-1] + "…"
	}
	return cmd
}

func processStateLabel(proc api.ProcessDetails) string {
	if proc.TreeExited {
		return proc.State + " (all stopped)"
	}
	if proc.LeaderExited {
		return proc.State + " (command exited)"
	}
	return proc.State
}

func processExitLabel(proc api.ProcessDetails) string {
	if proc.ExitCode != nil {
		return strconv.Itoa(*proc.ExitCode)
	}
	if proc.Signal != "" {
		return proc.Signal
	}
	return "-"
}

func exitFromProcess(exitCode *int, signal string) error {
	if exitCode != nil && *exitCode != 0 {
		os.Exit(*exitCode)
	}
	if exitCode == nil && signal != "" {
		return fmt.Errorf("process exited from signal %s", signal)
	}
	return nil
}

func durationMs(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(d / time.Millisecond)
}

func stdinFD() int {
	return int(os.Stdin.Fd()) // #nosec G115 -- file descriptor values fit in int on supported platforms
}

func processStringFlag(c *cli.Context, subcommand, name string) string {
	if v := c.String(name); v != "" {
		return v
	}
	return rawProcessFlagValue(subcommand, name)
}

func processBoolFlag(c *cli.Context, subcommand, name string) bool {
	if c.Bool(name) {
		return true
	}
	raw := rawProcessFlagValue(subcommand, name)
	if raw == "" {
		return rawProcessFlagPresent(subcommand, name)
	}
	v, err := strconv.ParseBool(raw)
	return err == nil && v
}

func processIntFlag(c *cli.Context, subcommand, name string) int {
	if v := c.Int(name); v != 0 {
		return v
	}
	raw := rawProcessFlagValue(subcommand, name)
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

func processInt64Flag(c *cli.Context, subcommand, name string) int64 {
	if v := c.Int64(name); v != 0 {
		return v
	}
	raw := rawProcessFlagValue(subcommand, name)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// Repeatable flags need every occurrence, not just the first.
func processStringSliceFlag(c *cli.Context, subcommand, name string) []string {
	if v := c.StringSlice(name); len(v) > 0 {
		return v
	}
	return rawProcessFlagValues(subcommand, name)
}

func processDurationFlag(c *cli.Context, subcommand, name string) time.Duration {
	if v := c.Duration(name); v != 0 {
		return v
	}
	raw := rawProcessFlagValue(subcommand, name)
	if raw == "" {
		return 0
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}
	return v
}

func processFlagIsSet(c *cli.Context, subcommand, name string) bool {
	return c.IsSet(name) || rawProcessFlagPresent(subcommand, name)
}

func rawProcessFlagPresent(subcommand, name string) bool {
	target := "--" + name
	for i := processSubcommandArgIndex(subcommand) + 1; i > 0 && i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--" {
			break
		}
		if arg == target {
			return true
		}
	}
	return false
}

func rawProcessFlagValues(subcommand, name string) []string {
	start := processSubcommandArgIndex(subcommand)
	if start < 0 {
		return nil
	}
	target := "--" + name
	prefix := target + "="
	var out []string
	for i := start + 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, prefix) {
			out = append(out, strings.TrimPrefix(arg, prefix))
			continue
		}
		if arg == target && i+1 < len(os.Args) {
			next := os.Args[i+1]
			if !strings.HasPrefix(next, "-") {
				out = append(out, next)
				i++
			}
		}
	}
	return out
}

func rawProcessFlagValue(subcommand, name string) string {
	start := processSubcommandArgIndex(subcommand)
	if start < 0 {
		return ""
	}
	target := "--" + name
	prefix := target + "="
	for i := start + 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
		if arg == target && i+1 < len(os.Args) {
			next := os.Args[i+1]
			if !strings.HasPrefix(next, "-") {
				return next
			}
		}
	}
	return ""
}

func processSubcommandArgIndex(subcommand string) int {
	for i, arg := range os.Args {
		if arg == subcommand {
			return i
		}
	}
	return -1
}
