// Package terminal provides helpers for detecting the terminal environment.
package terminal

import (
	"os"

	"golang.org/x/term"
)

// IsInteractive returns true when stdout is a real TTY (i.e. a human is
// watching). Returns false in CI pipelines, scripts, or when output is piped.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
