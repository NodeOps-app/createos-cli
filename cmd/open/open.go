// Package open provides the open command for launching project URLs in a browser.
package open

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/browser"
	"github.com/NodeOps-app/createos-cli/internal/config"
)

// NewOpenCommand returns the open command.
func NewOpenCommand() *cli.Command {
	return &cli.Command{
		Name:  "open",
		Usage: "Open a project's live URL or dashboard in your browser",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "project",
				Usage: "Project ID (auto-detected from .createos.json if not set)",
			},
			&cli.BoolFlag{
				Name:  "dashboard",
				Usage: "Open the CreateOS dashboard instead of the live URL",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.String("project")
			if projectID == "" {
				cfg, err := config.FindProjectConfig()
				if err != nil {
					return err
				}
				if cfg == nil {
					return fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify one:\n    createos open --project <id>")
				}
				projectID = cfg.ProjectID
			}

			if c.Bool("dashboard") {
				url := "https://createos.nodeops.network/projects/" + projectID
				pterm.Info.Printf("Opening dashboard for project %s...\n", projectID)
				return browser.Open(url)
			}

			// Find the live URL from environments
			envs, err := client.ListEnvironments(projectID)
			if err != nil {
				return err
			}

			if len(envs) == 0 {
				fmt.Println("No environments found — the project may not be deployed yet.")
				fmt.Println()
				pterm.Println(pterm.Gray("  Deploy first, then try again."))
				return nil
			}

			// Use the first environment with an endpoint
			var url string
			for _, env := range envs {
				if env.Extra.Endpoint != "" {
					url = env.Extra.Endpoint
					break
				}
			}

			if url == "" {
				fmt.Println("No live URL found for this project.")
				return nil
			}

			// Ensure URL has a scheme
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				url = "https://" + url
			}

			pterm.Info.Printf("Opening %s...\n", url)
			return browser.Open(url)
		},
	}
}
