// Package oauth implements the OAuth 2.0 authorization code flow with PKCE.
package oauth

import (
	"context"
	"os/exec"
)

func runCmd(name string, args ...string) error {
	return exec.CommandContext(context.Background(), name, args...).Start() //nolint:gosec
}
