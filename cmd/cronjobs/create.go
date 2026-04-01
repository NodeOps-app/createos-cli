package cronjobs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/cmdutil"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
	"github.com/NodeOps-app/createos-cli/internal/utils"
)

func newCronjobsCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new HTTP cron job for a project",
		Description: `Create a new HTTP cron job that fires on a cron schedule.

Examples:
  createos cronjobs create --project <project-id> --environment <env-id> \
    --name "Cleanup job" --schedule "0 * * * *" \
    --path /api/cleanup --method POST

  # With custom headers and JSON body:
  createos cronjobs create --project <project-id> --environment <env-id> \
    --name "Webhook" --schedule "*/5 * * * *" \
    --path /api/hook --method POST \
    --header "Authorization=Bearer token" --header "X-Source=cron" \
    --body '{"event":"tick"}'`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "environment", Usage: "Environment ID to attach the cron job to"},
			&cli.StringFlag{Name: "name", Usage: "Name for the cron job"},
			&cli.StringFlag{Name: "schedule", Usage: "Cron schedule expression (e.g. \"0 * * * *\")"},
			&cli.StringFlag{Name: "path", Usage: "HTTP path to call (must start with /)"},
			&cli.StringFlag{Name: "method", Usage: "HTTP method: GET, POST, PUT, DELETE, PATCH, HEAD", Value: "GET"},
			&cli.StringSliceFlag{Name: "header", Usage: "HTTP header to send with each request (format: Key=Value, repeatable)"},
			&cli.StringFlag{Name: "body", Usage: "JSON body to send with the request"},
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
			schedule := c.String("schedule")
			environmentID := c.String("environment")
			path := c.String("path")
			method := c.String("method")
			headers := parseHeaders(c.StringSlice("header"))
			bodyStr := c.String("body")

			if !terminal.IsInteractive() {
				// Non-TTY: all values must come from flags.
				if name == "" {
					return fmt.Errorf("please provide a name with --name")
				}
				if schedule == "" {
					return fmt.Errorf("please provide a schedule with --schedule (e.g. \"0 * * * *\")")
				}
				if environmentID == "" {
					return fmt.Errorf("please provide an environment ID with --environment")
				}
				if path == "" {
					return fmt.Errorf("please provide an HTTP path with --path")
				}
			} else {
				// Interactive: prompt for any missing values.
				if name == "" {
					name, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("Cron job name").
						Show()
					if err != nil {
						return fmt.Errorf("could not read name: %w", err)
					}
				}
				if schedule == "" {
					schedule, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("Cron schedule").
						WithDefaultValue("0 * * * *").
						Show()
					if err != nil {
						return fmt.Errorf("could not read schedule: %w", err)
					}
				}
				if environmentID == "" {
					envs, listErr := client.ListEnvironments(projectID)
					if listErr != nil {
						return listErr
					}
					if len(envs) == 0 {
						return fmt.Errorf("no environments found for this project — deploy one first")
					}
					if len(envs) == 1 {
						environmentID = envs[0].ID
					} else {
						options := make([]string, len(envs))
						for i, e := range envs {
							options[i] = fmt.Sprintf("%s (%s)", e.DisplayName, e.Status)
						}
						selected, selErr := pterm.DefaultInteractiveSelect.
							WithOptions(options).
							WithDefaultText("Select an environment").
							Show()
						if selErr != nil {
							return fmt.Errorf("could not read selection: %w", selErr)
						}
						for i, opt := range options {
							if opt == selected {
								environmentID = envs[i].ID
								break
							}
						}
					}
				}
				if path == "" {
					path, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("HTTP path").
						WithDefaultValue("/").
						Show()
					if err != nil {
						return fmt.Errorf("could not read path: %w", err)
					}
					path = pterm.RemoveColorFromString(path)
				}
				if !c.IsSet("method") {
					methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD"}
					selected, selErr := pterm.DefaultInteractiveSelect.
						WithOptions(methods).
						WithDefaultText("HTTP method").
						Show()
					if selErr != nil {
						return fmt.Errorf("could not read method: %w", selErr)
					}
					method = selected
				}
				if headers == nil {
					headers = map[string]string{}
					for {
						pair, inputErr := pterm.DefaultInteractiveTextInput.
							WithDefaultText("Add header (Key=Value, leave blank to skip)").
							Show()
						if inputErr != nil {
							return fmt.Errorf("could not read header: %w", inputErr)
						}
						if pair == "" {
							break
						}
						k, v, _ := strings.Cut(pair, "=")
						if k != "" {
							headers[k] = v
						}
					}
					if len(headers) == 0 {
						headers = nil
					}
				}
				if bodyStr == "" && methodSupportsBody(method) {
					bodyStr, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("JSON body (leave blank to skip)").
						Show()
					if err != nil {
						return fmt.Errorf("could not read body: %w", err)
					}
				}
			}

			settings := api.HTTPCronjobSettings{
				Path:    path,
				Method:  method,
				Headers: headers,
			}
			if bodyStr != "" {
				settings.Body = utils.Ptr(bodyStr)
			}
			settingsJSON, err := json.Marshal(settings)
			if err != nil {
				return fmt.Errorf("could not encode settings: %w", err)
			}

			req := api.CreateCronjobRequest{
				Name:          name,
				Schedule:      schedule,
				Type:          "http",
				EnvironmentID: environmentID,
				Settings:      settingsJSON,
			}

			id, err := client.CreateCronjob(projectID, req)
			if err != nil {
				return err
			}

			pterm.Success.Printf("Cron job created. ID: %s\n", id)
			return nil
		},
	}
}
