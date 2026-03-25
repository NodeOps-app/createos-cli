package env

import (
	"fmt"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newEnvRmCommand() *cli.Command {
	return &cli.Command{
		Name:      "rm",
		Usage:     "Remove an environment variable",
		ArgsUsage: "<KEY>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide a variable name to remove\n\n  Example:\n    createos env rm DATABASE_URL")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, envID, err := resolveProjectEnv(c, client)
			if err != nil {
				return err
			}

			key := c.Args().First()

			existing, err := client.GetEnvironmentVariables(projectID, envID)
			if err != nil {
				return err
			}

			if _, ok := existing[key]; !ok {
				return fmt.Errorf("variable %q is not set", key)
			}

			delete(existing, key)

			if err := client.UpdateEnvironmentVariables(projectID, envID, existing); err != nil {
				return err
			}

			pterm.Success.Printf("Removed %s\n", key)
			return nil
		},
	}
}
