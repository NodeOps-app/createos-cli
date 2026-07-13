// Package main is the entry point for the CreateOS CLI.
package main

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/cmd/root"
	"github.com/NodeOps-app/createos-cli/internal/api"
)

func main() {
	// Load .env file from the current directory if it exists.
	// Existing environment variables are NOT overwritten.
	// Error is intentionally ignored — missing .env is normal.
	godotenv.Load() //nolint:errcheck

	app := root.NewApp()

	if err := app.Run(os.Args); err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			pterm.Error.Println(apiErr.Message)
		} else {
			pterm.Error.Println(err.Error())
		}
		os.Exit(1)
	}
}
