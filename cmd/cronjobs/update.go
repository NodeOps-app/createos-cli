package cronjobs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

func newCronjobsUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update a cron job's name, schedule, or HTTP settings",
		Description: `Update the name, schedule, or HTTP settings of an existing cron job.

Examples:
  createos cronjobs update --cronjob <id> --name "New name" --schedule "*/5 * * * *" \
    --path /api/ping --method GET

  # With custom headers and JSON body:
  createos cronjobs update --cronjob <id> --path /api/hook --method POST \
    --header "Authorization=Bearer token" --body '{"event":"tick"}'`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project", Usage: "Project ID"},
			&cli.StringFlag{Name: "cronjob", Usage: "Cron job ID"},
			&cli.StringFlag{Name: "name", Usage: "New name for the cron job"},
			&cli.StringFlag{Name: "schedule", Usage: "New cron schedule expression"},
			&cli.StringFlag{Name: "path", Usage: "HTTP path to call (must start with /)"},
			&cli.StringFlag{Name: "method", Usage: "HTTP method: GET, POST, PUT, DELETE, PATCH, HEAD"},
			&cli.StringSliceFlag{Name: "header", Usage: "HTTP header to send with each request (format: Key=Value, repeatable)"},
			&cli.StringFlag{Name: "body", Usage: "JSON body to send with the request"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID, cronjobID, err := resolveCronjob(c, client)
			if err != nil {
				return err
			}

			// Fetch current values to use as defaults.
			existing, err := client.GetCronjob(projectID, cronjobID)
			if err != nil {
				return err
			}

			name := c.String("name")
			schedule := c.String("schedule")
			path := c.String("path")
			method := c.String("method")
			headers := parseHeaders(c.StringSlice("header"))
			bodyStr := c.String("body")

			// Decode existing settings for defaults in both TTY and non-TTY.
			var currentSettings api.HTTPCronjobSettings
			if existing.Settings != nil {
				if err := json.Unmarshal(*existing.Settings, &currentSettings); err != nil {
					return fmt.Errorf("could not parse existing cron job settings: %w", err)
				}
			}

			if !terminal.IsInteractive() {
				// Fall back to existing values for unset flags.
				if name == "" {
					name = existing.Name
				}
				if schedule == "" {
					schedule = existing.Schedule
				}
				if path == "" {
					path = currentSettings.Path
				}
				if method == "" {
					method = currentSettings.Method
				}
				if headers == nil {
					headers = currentSettings.Headers
				}
			} else {
				if name == "" {
					name, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("Cron job name").
						WithDefaultValue(existing.Name).
						Show()
					if err != nil {
						return fmt.Errorf("could not read name: %w", err)
					}
				}
				if schedule == "" {
					schedule, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("Cron schedule").
						WithDefaultValue(existing.Schedule).
						Show()
					if err != nil {
						return fmt.Errorf("could not read schedule: %w", err)
					}
				}
				if path == "" {
					defaultPath := "/"
					if currentSettings.Path != "" {
						defaultPath = currentSettings.Path
					}
					path, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("HTTP path").
						WithDefaultValue(defaultPath).
						Show()
					if err != nil {
						return fmt.Errorf("could not read path: %w", err)
					}
					path = pterm.RemoveColorFromString(path)
				}
				if method == "" {
					defaultMethod := "GET"
					if currentSettings.Method != "" {
						defaultMethod = currentSettings.Method
					}
					methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD"}
					selected, selErr := pterm.DefaultInteractiveSelect.
						WithOptions(methods).
						WithDefaultText("HTTP method").
						WithDefaultOption(defaultMethod).
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
						headers = currentSettings.Headers
					}
				}
				if bodyStr == "" && methodSupportsBody(method) {
					bodyStr, err = pterm.DefaultInteractiveTextInput.
						WithDefaultText("JSON body (leave blank to keep existing)").
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
				Body:    currentSettings.Body,
			}
			if bodyStr != "" {
				settings.Body = &bodyStr
			}
			settingsJSON, err := json.Marshal(settings)
			if err != nil {
				return fmt.Errorf("could not encode settings: %w", err)
			}

			req := api.UpdateCronjobRequest{
				Name:     name,
				Schedule: schedule,
				Settings: settingsJSON,
			}

			if err := client.UpdateCronjob(projectID, cronjobID, req); err != nil {
				return err
			}

			pterm.Success.Println("Cron job updated.")
			return nil
		},
	}
}
