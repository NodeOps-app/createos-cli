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
	"github.com/NodeOps-app/createos-cli/internal/terminal"
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

			envs, err := client.ListEnvironments(projectID)
			if err != nil {
				return err
			}

			// Collect environments that have an endpoint
			var envsWithURL []api.Environment
			for _, env := range envs {
				if env.Extra.Endpoint != "" {
					envsWithURL = append(envsWithURL, env)
				}
			}

			var url string

			switch len(envsWithURL) {
			case 0:
				// No environment URLs — fall back to deployments
				deployments, err := client.ListDeployments(projectID)
				if err != nil {
					return err
				}
				var deploymentsWithURL []api.Deployment
				for _, d := range deployments {
					if d.Extra.Endpoint != "" {
						deploymentsWithURL = append(deploymentsWithURL, d)
					}
				}
				if len(deploymentsWithURL) == 0 {
					fmt.Println("No live URL found — the project may not be deployed yet.")
					return nil
				}
				if len(deploymentsWithURL) == 1 {
					url = deploymentsWithURL[0].Extra.Endpoint
				} else {
					options := make([]string, len(deploymentsWithURL))
					for i, d := range deploymentsWithURL {
						id := d.ID
						if len(id) > 8 {
							id = id[:8]
						}
						options[i] = fmt.Sprintf("%s  %s  %s", d.CreatedAt.Format("Jan 02 15:04"), d.Status, id)
					}
					if !terminal.IsInteractive() {
						return fmt.Errorf("multiple deployments found — use 'createos deployments list' and pass the deployment ID")
					}
					selected, err := pterm.DefaultInteractiveSelect.
						WithOptions(options).
						WithDefaultText("Select a deployment").
						Show()
					if err != nil {
						return fmt.Errorf("could not read selection: %w", err)
					}
					for i, opt := range options {
						if opt == selected {
							url = deploymentsWithURL[i].Extra.Endpoint
							break
						}
					}
				}
			case 1:
				url = envsWithURL[0].Extra.Endpoint
			default:
				options := make([]string, len(envsWithURL))
				for i, env := range envsWithURL {
					options[i] = fmt.Sprintf("%s — %s", env.DisplayName, env.Extra.Endpoint)
				}
				if !terminal.IsInteractive() {
					return fmt.Errorf("multiple environments found — use --project <id> and ensure only one environment is active, or run interactively")
				}
				selected, err := pterm.DefaultInteractiveSelect.
					WithOptions(options).
					WithDefaultText("Select an environment").
					Show()
				if err != nil {
					return fmt.Errorf("could not read selection: %w", err)
				}
				for i, opt := range options {
					if opt == selected {
						url = envsWithURL[i].Extra.Endpoint
						break
					}
				}
			}

			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				url = "https://" + url
			}

			pterm.Info.Printf("Opening %s...\n", url)
			return browser.Open(url)
		},
	}
}
