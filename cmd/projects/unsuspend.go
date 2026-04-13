package projects

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newUnsuspendCommand() *cli.Command {
	return &cli.Command{
		Name:  "unsuspend",
		Usage: "Resume a suspended project",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.String("project")

			// Try linked project config
			if projectID == "" {
				cfg, _ := config.FindProjectConfig()
				if cfg != nil {
					projectID = cfg.ProjectID
				}
			}

			// Interactive picker filtered to suspended projects
			if projectID == "" && terminal.IsInteractive() {
				projects, err := client.ListProjects()
				if err != nil {
					return err
				}

				suspended := make([]api.Project, 0, len(projects))
				for _, p := range projects {
					if p.Status == "suspended" {
						suspended = append(suspended, p)
					}
				}

				if len(suspended) == 0 {
					return fmt.Errorf("you don't have any suspended projects to resume")
				}

				options := make([]string, len(suspended))
				for i, p := range suspended {
					options[i] = fmt.Sprintf("%s (%s)", p.DisplayName, p.ID)
				}

				selected, err := pterm.DefaultInteractiveSelect.
					WithDefaultText("Select a project to resume").
					WithOptions(options).
					WithFilter(true).
					Show()
				if err != nil {
					return fmt.Errorf("selection cancelled")
				}

				for i, opt := range options {
					if opt == selected {
						projectID = suspended[i].ID
						break
					}
				}
			}

			if projectID == "" {
				return fmt.Errorf("please provide a project ID\n\n  To see your projects, run:\n    createos projects list")
			}

			// Validate status when project was explicitly provided (not from picker)
			project, err := client.GetProject(projectID)
			if err != nil {
				return err
			}

			if project.Status != "suspended" {
				switch project.Status {
				case "active":
					return fmt.Errorf("this project is already running")
				case "suspending":
					return fmt.Errorf("this project is currently being suspended — wait for it to finish before unsuspending")
				default:
					return fmt.Errorf("this project can't be unsuspended right now because it is %s\n\n  Only suspended projects can be resumed. Run 'createos projects get %s' to check its status", project.Status, projectID)
				}
			}

			if terminal.IsInteractive() {
				confirm, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText(fmt.Sprintf("Are you sure you want to resume project %q?", project.DisplayName)).
					WithDefaultValue(false).
					Show()
				if err != nil {
					return fmt.Errorf("could not read confirmation: %w", err)
				}
				if !confirm {
					fmt.Println("Cancelled. Your project was not resumed.")
					return nil
				}
			}

			if err := client.UnsuspendProject(projectID); err != nil {
				return err
			}

			pterm.Success.Println("Project resumed.")
			return nil
		},
	}
}
