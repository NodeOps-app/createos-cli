// Package oauth implements the OAuth 2.0 authorization code (PKCE) and device authorization flows.
package oauth

import (
	"context"
	"os/exec"
)

func runCmd(name string, args ...string) error {
	return exec.CommandContext(context.Background(), name, args...).Start() // #nosec G204 -- args are controlled by internal callers, not user input
}
