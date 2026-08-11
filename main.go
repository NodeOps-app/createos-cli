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
		code, message := "error", err.Error()
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			code, message = apiErr.Code(), apiErr.Message
		}

		// JSON mode emits a machine-readable envelope on stdout so a
		// consumer reading a single stream still parses valid JSON.
		// Otherwise the human-readable error goes to stderr, keeping
		// stdout clean for data in pipes and CI logs.
		if !output.AppRenderError(app, code, message) {
			pterm.Error.WithWriter(os.Stderr).Println(message)
		}
		os.Exit(1)
	}
}
