package env

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/output"
)

func newEnvListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List environment variables for a project environment",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID"},
			&cli.BoolFlag{Name: "hide", Usage: "Mask values"},
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

			vars, err := client.GetEnvironmentVariables(projectID, env.ID)
			if err != nil {
				return err
			}

			if output.IsJSON(c) {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(vars)
			}

			if len(vars) == 0 {
				fmt.Println("No environment variables set.")
				return nil
			}

			tableData := pterm.TableData{{"KEY", "VALUE"}}
			for k, v := range vars {
				val := v
				if c.Bool("hide") {
					val = maskValue(v)
				}
				tableData = append(tableData, []string{k, val})
			}

			if err := pterm.DefaultTable.WithHasHeader().WithData(tableData).Render(); err != nil {
				return err
			}
			fmt.Println()
			return nil
		},
	}
}

func maskValue(v string) string {
	if len(v) <= 6 {
		return "****"
	}
	return v[:2] + "****" + v[len(v)-2:]
}
