// Package main is the entry point for the CreateOS CLI.
package main

import (
	"errors"
	"os"

	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/cmd/root"
	"github.com/NodeOps-app/createos-cli/internal/api"
)

func main() {
	app := root.NewApp()

	if err := app.Run(os.Args); err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			pterm.Error.Println(apiErr.Message)
			if hint := apiErr.Hint(); hint != "" {
				pterm.Println(pterm.Gray("  Hint: " + hint))
			}
		} else {
			pterm.Error.Println(err.Error())
		}
		os.Exit(1)
	}
}
