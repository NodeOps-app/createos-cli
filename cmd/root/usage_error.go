package root

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
)

// urfave/cli parses global flags only before the first command name, so
// `createos sandbox shapes -o json` dies with "flag provided but not
// defined: -o" — the flag exists, it is simply in the wrong place. The
// message names neither fact, and the same shape has cost real round trips
// in practice.
//
// installUsageErrorHelp attaches the handler to every command in the tree.
// OnUsageError lives on Command and is NOT inherited from the app, so a
// handler set only at the top never runs for `sandbox shapes -o json` —
// the exact case worth catching.
func installUsageErrorHelp(app *cli.App) {
	globals := globalFlagNames(app)
	handler := func(_ *cli.Context, err error, _ bool) error {
		name := undefinedFlagName(err)
		if name == "" || !globals[strings.TrimLeft(name, "-")] {
			return err
		}
		return fmt.Errorf("%w\n\n  %s is a global flag, so it has to come BEFORE the command:\n    %s",
			err, name, correctedCommandLine(name))
	}
	app.OnUsageError = handler
	setUsageErrorHandler(app.Commands, handler)
}

func setUsageErrorHandler(commands []*cli.Command, handler cli.OnUsageErrorFunc) {
	for _, cmd := range commands {
		if cmd == nil {
			continue
		}
		if cmd.OnUsageError == nil {
			cmd.OnUsageError = handler
		}
		setUsageErrorHandler(cmd.Subcommands, handler)
	}
}

func globalFlagNames(app *cli.App) map[string]bool {
	names := make(map[string]bool)
	for _, f := range app.Flags {
		for _, n := range f.Names() {
			names[n] = true
		}
	}
	return names
}

// undefinedFlagName pulls the flag out of the flag package's message,
// which reads: `flag provided but not defined: -o`.
func undefinedFlagName(err error) string {
	const marker = "flag provided but not defined: "
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		return ""
	}
	name := strings.TrimSpace(msg[i+len(marker):])
	if cut := strings.IndexAny(name, " \n"); cut >= 0 {
		name = name[:cut]
	}
	return name
}

// correctedCommandLine rewrites what the user typed with the misplaced
// global flag moved to the front, so the fix can be copied straight back
// into the terminal.
func correctedCommandLine(flagName string) string {
	bare := strings.TrimLeft(flagName, "-")
	args := os.Args[1:]

	moved := make([]string, 0, 2)
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		trimmed := strings.TrimLeft(a, "-")
		if trimmed == bare || strings.HasPrefix(trimmed, bare+"=") {
			moved = append(moved, a)
			// A value-taking flag written as `-o json` carries its value
			// in the next argument; move that too or the corrected line
			// is wrong.
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				moved = append(moved, args[i])
			}
			continue
		}
		rest = append(rest, a)
	}
	if len(moved) == 0 {
		return "createos " + strings.Join(args, " ")
	}
	return "createos " + strings.Join(append(moved, rest...), " ")
}

// installCommandSuggestions replaces urfave's bare "No help topic for
// 'ssh'" with the nearest real command, and makes an unknown command name
// fail the run. Agents and people both guess verb names, and a guess that
// lands one edit away from a real command should not cost a round trip to
// the help output — and a guess that is simply wrong must not exit zero.
//
// This cannot be done with urfave's own CommandNotFoundFunc: ShowCommandHelp
// calls that callback and then unconditionally returns nil (see the
// package's help.go), so nothing set there can ever make app.Run return an
// error. Catching the unresolved name has to happen earlier, in the
// command's own Action, before urfave's help fallback runs.
//
// That is safe to do because an Action only ever runs with a positional
// argument still present when dispatch already failed to match that
// argument against a real subcommand — a match would have called that
// subcommand's Run instead. So "argument present here" and "unknown
// command" are the same condition.
func installCommandSuggestions(app *cli.App) {
	fallback := app.Action
	app.Action = func(c *cli.Context) error {
		if name := c.Args().First(); name != "" {
			return unknownCommandError(app.Commands, "", name)
		}
		if fallback != nil {
			return fallback(c)
		}
		return cli.ShowSubcommandHelp(c)
	}
	for _, cmd := range app.Commands {
		installGroupSuggestions(cmd)
	}
}

func installGroupSuggestions(cmd *cli.Command) {
	if cmd == nil || len(cmd.Subcommands) == 0 {
		return
	}
	prefix := cmd.Name + " "
	fallback := cmd.Action
	cmd.Action = func(c *cli.Context) error {
		if name := c.Args().First(); name != "" {
			return unknownCommandError(cmd.Subcommands, prefix, name)
		}
		if fallback != nil {
			return fallback(c)
		}
		return cli.ShowSubcommandHelp(c)
	}
	for _, sub := range cmd.Subcommands {
		installGroupSuggestions(sub)
	}
}

// unknownCommandError builds the same message the old CommandNotFound
// callback printed, but returns it instead of writing to stderr directly —
// main.go's error renderer prints whatever app.Run returns.
func unknownCommandError(candidates []*cli.Command, prefix, name string) error {
	noun := "command"
	if prefix != "" {
		noun = "subcommand"
	}
	msg := fmt.Sprintf("createos %s: %q is not a %s.", strings.TrimSpace(prefix), name, noun)
	if best := nearestCommand(candidates, name); best != "" {
		msg += fmt.Sprintf("\n\n  Did you mean:\n    createos %s%s", prefix, best)
	}
	msg += fmt.Sprintf("\n\n  See everything with:\n    createos %s--help", prefix)
	return errors.New(msg)
}

// nearestCommand returns the closest command name within a small edit
// distance, or "" when nothing is close enough. The cap matters: a wild
// guess should get the help pointer, not a confidently wrong suggestion.
func nearestCommand(commands []*cli.Command, name string) string {
	name = strings.ToLower(name)
	best, bestDist := "", 3
	for _, cmd := range commands {
		if cmd.Hidden {
			continue
		}
		for _, candidate := range append([]string{cmd.Name}, cmd.Aliases...) {
			if d := editDistance(name, strings.ToLower(candidate)); d < bestDist {
				best, bestDist = cmd.Name, d
			}
		}
	}
	return best
}

// editDistance is Levenshtein over two short command names, with one row
// of state rather than a full matrix.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev = cur
	}
	return prev[len(b)]
}
