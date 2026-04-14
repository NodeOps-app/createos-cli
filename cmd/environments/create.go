package environments

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newEnvironmentsCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "Create a new environment for a project",
		ArgsUsage: " ",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "name", Usage: "Display name for the environment"},
			&cli.StringFlag{Name: "unique-name", Usage: "Unique name (lowercase, 4-32 chars)"},
			&cli.StringFlag{Name: "description", Usage: "Environment description"},
			&cli.StringFlag{Name: "branch", Usage: "Branch to deploy from (required for VCS projects)"},
			&cli.BoolFlag{Name: "auto-promote", Usage: "Automatically promote new deployments"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, err := cmdutil.ResolveProjectID(c.String("project"))
			if err != nil {
				return err
			}

			name := c.String("name")
			uniqueName := c.String("unique-name")
			description := c.String("description")
			branch := c.String("branch")
			autoPromote := c.Bool("auto-promote")

			// Interactive prompts for required fields
			if name == "" {
				if !terminal.IsInteractive() {
					return fmt.Errorf("--name is required in non-interactive mode")
				}
				name, err = pterm.DefaultInteractiveTextInput.
					WithDefaultText("Environment name").
					Show()
				if err != nil {
					return fmt.Errorf("could not read input: %w", err)
				}
				name = strings.TrimSpace(name)
				if name == "" {
					return fmt.Errorf("environment name is required")
				}
			}

			if uniqueName == "" {
				if !terminal.IsInteractive() {
					return fmt.Errorf("--unique-name is required in non-interactive mode")
				}
				uniqueName, err = pterm.DefaultInteractiveTextInput.
					WithDefaultText("Unique name (lowercase, 4-32 chars)").
					Show()
				if err != nil {
					return fmt.Errorf("could not read input: %w", err)
				}
				uniqueName = strings.TrimSpace(strings.ToLower(uniqueName))
				if uniqueName == "" {
					return fmt.Errorf("unique name is required")
				}
			}

			// Check if branch is needed by looking at project type
			project, err := client.GetProject(projectID)
			if err != nil {
				return err
			}

			if project.Type == "vcs" && branch == "" {
				if !terminal.IsInteractive() {
					return fmt.Errorf("--branch is required for VCS projects")
				}
				branch, err = pterm.DefaultInteractiveTextInput.
					WithDefaultText("Branch to deploy from").
					Show()
				if err != nil {
					return fmt.Errorf("could not read input: %w", err)
				}
				branch = strings.TrimSpace(branch)
				if branch == "" {
					return fmt.Errorf("branch is required for VCS projects")
				}
			}

			req := api.CreateEnvironmentRequest{
				DisplayName:          name,
				UniqueName:           strings.ToLower(uniqueName),
				Settings:             map[string]any{"runEnvs": map[string]string{}},
				Resources:            api.ResourceSettings{CPU: 250, Memory: 512, Replicas: 1},
				IsAutoPromoteEnabled: autoPromote,
			}

			if description != "" {
				req.Description = &description
			}

			if branch != "" {
				req.Branch = &branch
			}

			_, err = client.CreateEnvironment(projectID, req)
			if err != nil {
				return err
			}

			pterm.Success.Printfln("Environment %q created.", name)
			return nil
		},
	}
}
