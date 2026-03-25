// Package initcmd provides the init command for linking a local directory to a CreateOS project.
package initcmd

import (
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
)

// NewInitCommand returns the init command.
func NewInitCommand() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Link the current directory to a CreateOS project",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "project",
				Usage: "Project ID to link (skips interactive selection)",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("could not determine current directory: %w", err)
			}

			// Check if already linked
			existing, _ := config.LoadProjectConfig(dir)
			if existing != nil {
				pterm.Warning.Printf("This directory is already linked to project %s\n", existing.ProjectName)
				result, _ := pterm.DefaultInteractiveConfirm.
					WithDefaultText("Overwrite existing link?").
					WithDefaultValue(false).
					Show()
				if !result {
					return nil
				}
			}

			var projectID, projectName string

			if pid := c.String("project"); pid != "" {
				// Non-interactive: validate the project exists
				project, err := client.GetProject(pid)
				if err != nil {
					return err
				}
				projectID = project.ID
				projectName = project.DisplayName
			} else {
				// Interactive: list projects and let user pick
				projects, err := client.ListProjects()
				if err != nil {
					return err
				}
				if len(projects) == 0 {
					fmt.Println("You don't have any projects yet.")
					fmt.Println()
					pterm.Println(pterm.Gray("  Create a project on the CreateOS dashboard first, then run 'createos init' again."))
					return nil
				}

				options := make([]string, len(projects))
				for i, p := range projects {
					desc := ""
					if p.Description != nil && *p.Description != "" {
						desc = " — " + *p.Description
					}
					options[i] = fmt.Sprintf("%s (%s)%s", p.DisplayName, p.ID, desc)
				}

				selected, err := pterm.DefaultInteractiveSelect.
					WithDefaultText("Select a project to link").
					WithOptions(options).
					Show()
				if err != nil {
					return fmt.Errorf("selection cancelled")
				}

				// Find the selected project
				for i, opt := range options {
					if opt == selected {
						projectID = projects[i].ID
						projectName = projects[i].DisplayName
						break
					}
				}
			}

			// Optionally select environment
			var envID string
			envs, err := client.ListEnvironments(projectID)
			if err == nil && len(envs) > 0 {
				if len(envs) == 1 {
					envID = envs[0].ID
				} else {
					envOptions := make([]string, len(envs))
					for i, e := range envs {
						envOptions[i] = fmt.Sprintf("%s (%s)", e.DisplayName, e.ID)
					}
					selected, err := pterm.DefaultInteractiveSelect.
						WithDefaultText("Select default environment").
						WithOptions(envOptions).
						Show()
					if err == nil {
						for i, opt := range envOptions {
							if opt == selected {
								envID = envs[i].ID
								break
							}
						}
					}
				}
			}

			cfg := config.ProjectConfig{
				ProjectID:     projectID,
				EnvironmentID: envID,
				ProjectName:   projectName,
			}

			if err := config.SaveProjectConfig(dir, cfg); err != nil {
				return fmt.Errorf("could not save project config: %w", err)
			}

			// Add to .gitignore
			_ = config.EnsureGitignore(dir)

			pterm.Success.Printf("Linked to %s\n", projectName)
			fmt.Println()
			pterm.Println(pterm.Gray("  Project config saved to .createos.json"))
			pterm.Println(pterm.Gray("  Commands like deploy, status, and logs will now auto-detect this project."))
			return nil
		},
	}
}
