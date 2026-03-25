package env

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newEnvSetCommand() *cli.Command {
	return &cli.Command{
		Name:      "set",
		Usage:     "Set one or more environment variables",
		ArgsUsage: "KEY=value [KEY2=value2 ...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("please provide at least one KEY=value pair\n\n  Example:\n    createos env set DATABASE_URL=postgres://... API_KEY=sk-xxx")
			}

			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, envID, err := resolveProjectEnv(c, client)
			if err != nil {
				return err
			}

			// Parse KEY=value pairs
			toSet := make(map[string]string)
			for _, arg := range c.Args().Slice() {
				parts := strings.SplitN(arg, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid format %q — use KEY=value", arg)
				}
				key := strings.TrimSpace(parts[0])
				if key == "" {
					return fmt.Errorf("empty key in %q — variable names cannot be blank", arg)
				}
				if strings.ContainsAny(key, " \t") {
					return fmt.Errorf("invalid key %q — variable names cannot contain spaces", key)
				}
				toSet[key] = parts[1]
			}

			// Get existing vars and merge
			existing, err := client.GetEnvironmentVariables(projectID, envID)
			if err != nil {
				return err
			}
			if existing == nil {
				existing = make(map[string]string)
			}
			for k, v := range toSet {
				existing[k] = v
			}

			if err := client.UpdateEnvironmentVariables(projectID, envID, existing); err != nil {
				return err
			}

			for k := range toSet {
				pterm.Success.Printf("Set %s\n", k)
			}
			return nil
		},
	}
}
