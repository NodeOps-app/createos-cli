// Package main is the entry point for the CreateOS CLI.
package main

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/cmd/root"
	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cliargs"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func main() {
	// Load .env file from the current directory if it exists.
	// Existing environment variables are NOT overwritten.
	// Error is intentionally ignored — missing .env is normal.
	godotenv.Load() //nolint:errcheck,gosec

	app := root.NewApp()

	if err := app.Run(cliargs.Hoist(os.Args)); err != nil {
		code, message, hint := "error", err.Error(), ""
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			code, message, hint = apiErr.Code(), apiErr.Message, apiErr.Hint()
		}

		// JSON mode emits a machine-readable envelope on stdout so a
		// consumer reading a single stream still parses valid JSON.
		// Otherwise the human-readable error goes to stderr, keeping
		// stdout clean for data in pipes and CI logs.
		if !output.AppRenderError(app, code, message, hint) {
			errOut := pterm.Error.WithWriter(os.Stderr)
			errOut.Println(message)
			if hint != "" {
				pterm.Fprintln(os.Stderr, pterm.Gray("  Hint: "+hint))
			}
			// Raw error text is withheld unless the user asked for it:
			// Go errors can carry syscall details and local paths.
			if apiErr == nil && api.DebugEnabled() {
				pterm.Fprintln(os.Stderr, pterm.Gray("  debug: "+err.Error()))
			}
		}
		os.Exit(1)
	}
}
