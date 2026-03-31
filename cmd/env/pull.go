package env

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

func newEnvPullCommand() *cli.Command {
	return &cli.Command{
		Name:  "pull",
		Usage: "Download environment variables to a local .env file",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
			&cli.StringFlag{Name: "file", Usage: "Output file path (default: .env.<environment>)"},
			&cli.BoolFlag{Name: "force", Usage: "Overwrite existing file without confirmation"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, env, err := resolveProjectEnv(c, client)
			if err != nil {
				return err
			}

			filePath := c.String("file")
			if filePath == "" {
				filePath = ".env." + env.UniqueName
			}

			if filepath.IsAbs(filePath) || strings.Contains(filePath, "..") {
				return fmt.Errorf("--file must be a relative path without '..' (got %q)", filePath)
			}

			// Check if file exists
			if !c.Bool("force") {
				if _, err := os.Stat(filePath); err == nil {
					result, _ := pterm.DefaultInteractiveConfirm.
						WithDefaultText(fmt.Sprintf("%s already exists. Overwrite?", filePath)).
						WithDefaultValue(false).
						Show()
					if !result {
						return nil
					}
				}
			}

			vars, err := client.GetEnvironmentVariables(projectID, env.ID)
			if err != nil {
				return err
			}

			if len(vars) == 0 {
				fmt.Println("No environment variables to pull.")
				return nil
			}

			// Sort keys for deterministic output
			keys := make([]string, 0, len(vars))
			for k := range vars {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var b strings.Builder
			for _, k := range keys {
				b.WriteString(k)
				b.WriteString("=")
				b.WriteString(vars[k])
				b.WriteString("\n")
			}

			if err := os.WriteFile(filePath, []byte(b.String()), 0600); err != nil {
				return fmt.Errorf("could not write %s: %w", filePath, err)
			}

			pterm.Success.Printf("Pulled %d variables to %s\n", len(vars), filePath)
			ensureEnvGitignored()
			return nil
		},
	}
}
