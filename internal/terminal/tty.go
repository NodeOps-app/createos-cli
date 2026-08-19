// Package terminal provides helpers for detecting the terminal environment.
package terminal

import (
	"os"

	"golang.org/x/term"
)

// IsInteractive returns true when stdout is a real TTY (i.e. a human is
// watching). Returns false in CI pipelines, scripts, or when output is piped.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) // #nosec G115 -- uintptr->int safe on all supported platforms for fd values
}

// HasPipedStdin reports whether stdin is a pipe or a file rather than a
// keyboard, i.e. `echo hi | createos …` or `createos … < file`. Checked
// separately from IsInteractive because the two streams are redirected
// independently — a human on a TTY can still pipe data in.
func HasPipedStdin() bool {
	return !term.IsTerminal(int(os.Stdin.Fd())) // #nosec G115 -- uintptr->int safe on all supported platforms for fd values
}
