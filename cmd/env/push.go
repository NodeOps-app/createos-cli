package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newEnvPushCommand() *cli.Command {
	return &cli.Command{
		Name:  "push",
		Usage: "Upload environment variables from a local .env file",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
			&cli.StringFlag{Name: "file", Usage: "Input file path (default: .env.<environment>)"},
			&cli.BoolFlag{Name: "force", Usage: "Push without confirmation"},
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

			data, err := os.ReadFile(filePath) //nolint:gosec
			if err != nil {
				return fmt.Errorf("could not read %s: %w", filePath, err)
			}

			vars := parseEnvFile(string(data))
			if len(vars) == 0 {
				fmt.Println("No variables found in " + filePath)
				return nil
			}

			if !c.Bool("force") {
				if !terminal.IsInteractive() {
					return fmt.Errorf("use --force to push without a confirmation prompt")
				}
				fmt.Printf("Will push %d variables from %s:\n", len(vars), filePath)
				for k := range vars {
					fmt.Printf("  %s\n", k)
				}
				fmt.Println()
				result, _ := pterm.DefaultInteractiveConfirm.
					WithDefaultText("Continue?").
					WithDefaultValue(true).
					Show()
				if !result {
					return nil
				}
			}

			// Merge with existing
			existing, err := client.GetEnvironmentVariables(projectID, env.ID)
			if err != nil {
				return err
			}
			if existing == nil {
				existing = make(map[string]string)
			}
			for k, v := range vars {
				existing[k] = v
			}

			if err := client.UpdateEnvironmentVariables(projectID, env.ID, existing); err != nil {
				return err
			}

			pterm.Success.Printf("Pushed %d variables from %s\n", len(vars), filePath)
			ensureEnvGitignored()
			return nil
		},
	}
}

func parseEnvFile(content string) map[string]string {
	vars := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		vars[key] = val
	}
	return vars
}
