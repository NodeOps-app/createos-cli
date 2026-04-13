// Package initcmd provides the init command for linking a local directory to a CreateOS project.
package initcmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/git"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
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
				if !terminal.IsInteractive() {
					return fmt.Errorf("directory already linked — use --project <id> to re-link non-interactively")
				}
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
				if !terminal.IsInteractive() {
					return fmt.Errorf("no project specified — use --project <id> to link non-interactively\n\n  To see your projects, run:\n    createos projects list")
				}
				// Interactive: list projects and let user pick
				projects, err := client.ListProjects()
				if err != nil {
					return err
				}
				if len(projects) == 0 {
					fmt.Println("You don't have any projects yet.")
					return nil
				}

				// Filter out non-active projects
				activeProjects := make([]api.Project, 0, len(projects))
				for _, p := range projects {
					if p.Status == "active" {
						activeProjects = append(activeProjects, p)
					}
				}
				projects = activeProjects

				if len(projects) == 0 {
					fmt.Println("You don't have any active projects yet.")
					return nil
				}

				// Try to auto-detect the matching VCS project from git remote
				repoFullName := git.GetRemoteFullName(dir)
				if repoFullName != "" {
					for _, p := range projects {
						if p.Type != "vcs" && p.Type != "githubImport" {
							continue
						}
						var src api.VCSSource
						if err := json.Unmarshal(p.Source, &src); err != nil {
							continue
						}
						if src.VCSFullName == repoFullName {
							pterm.Info.Printf("Detected project %s from git remote (%s)\n", p.DisplayName, repoFullName)
							useDetected, _ := pterm.DefaultInteractiveConfirm.
								WithDefaultText(fmt.Sprintf("Link to %s?", p.DisplayName)).
								WithDefaultValue(true).
								Show()
							if useDetected {
								projectID = p.ID
								projectName = p.DisplayName
							}
							break
						}
					}
				}

				// Fall back to interactive selection if no match found
				if projectID == "" {
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
			}

			// Optionally select environment
			var envID string
			envs, err := client.ListEnvironments(projectID)
			if err == nil && len(envs) > 0 {
				if len(envs) == 1 {
					envID = envs[0].ID
				} else if terminal.IsInteractive() {
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
			return nil
		},
	}
}
