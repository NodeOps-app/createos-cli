// Package browser provides cross-platform browser opening.
package browser

import (
	"context"
	"os/exec"
	"runtime"
)

// Open opens the given URL in the default system browser.
func Open(rawURL string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", rawURL}
	default:
		cmd = "xdg-open"
		args = []string{rawURL}
	}
	return exec.CommandContext(context.Background(), cmd, args...).Start() // #nosec G204 -- cmd/args are derived from runtime.GOOS, not user input
}
