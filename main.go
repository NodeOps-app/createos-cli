// Package main is the entry point for the CreateOS CLI.
package main

import (
	"context"
	"errors"
	"os"

	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/cmd/root"
	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/telemetry"
)

func main() {
	// Init telemetry BEFORE constructing the app. This is required so the
	// HelpPrinter / VersionPrinter overrides (Phase 3) see telemetry.Default
	// as non-nil even when global --help / --version short-circuit App.Before.
	// Init is a no-op when CREATEOS_DO_NOT_TRACK=1 or PostHogAPIKey is empty.
	_ = telemetry.Init(context.Background())

	app := root.NewApp()
	err := app.Run(os.Args)

	// Telemetry finalize runs BEFORE the existing error display + os.Exit.
	root.FinalizeTelemetry(app, err)

	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			pterm.Error.Println(apiErr.Message)
		} else {
			pterm.Error.Println(err.Error())
		}
		os.Exit(1)
	}
}
